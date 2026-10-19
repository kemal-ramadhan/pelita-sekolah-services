package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/edupilot-instantion-service/internal/apperr"
	"github.com/edupilot-instantion-service/internal/config"
	"github.com/edupilot-instantion-service/internal/dto"
	"github.com/edupilot-instantion-service/internal/model"
	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"
	"gorm.io/gorm"
)

const (
	// MaxStudentImportFileSize is the maximum accepted .xlsx size (§8.9).
	MaxStudentImportFileSize = 10 << 20
	// MaxStudentImportRows is the maximum number of non-empty data rows.
	MaxStudentImportRows = 5000
	// MaxStudentExportRows caps the export.
	MaxStudentExportRows = 10000

	studentUnzipSizeLimit    = 128 << 20
	studentUnzipXMLSizeLimit = 64 << 20
)

type StudentExcelService struct {
	db *gorm.DB
}

func NewStudentExcelService(db *gorm.DB) *StudentExcelService {
	return &StudentExcelService{db: db}
}

func (s *StudentExcelService) conn(ctx context.Context) *gorm.DB {
	if s.db != nil {
		return s.db.WithContext(ctx)
	}
	return config.DB.WithContext(ctx)
}

// StudentTemplateHeaders defines the columns for the Excel template according to API_CONTRACT §8.9
var StudentTemplateHeaders = []string{
	"Nama Lengkap*", "Nama Panggilan", "Jenis Kelamin* (L/P)", "NISN", "NIS", "NIK", "No KK",
	"Tempat Lahir*", "Tanggal Lahir* (YYYY-MM-DD)", "No Akta Lahir", "Agama*", "Kewarganegaraan",
	"Kebutuhan Khusus", "Golongan Darah", "Anak Ke", "Jumlah Saudara", "Alamat", "RT", "RW", "Dusun",
	"Kelurahan/Desa", "Kecamatan", "Kabupaten/Kota", "Provinsi", "Kode Pos", "Jenis Tinggal",
	"Alat Transportasi", "Jarak (km)", "Waktu Tempuh (menit)", "Telepon", "HP", "Email",
	"Tinggi (cm)", "Berat (kg)", "Lingkar Kepala (cm)",
	"Penerima KPS/PKH (Ya/Tidak)", "No KPS/PKH", "Penerima KIP (Ya/Tidak)", "No KIP", "Nama di KIP", "No KKS",
	"Jenis Pendaftaran", "Tanggal Masuk (YYYY-MM-DD)", "Sekolah Asal", "NPSN Sekolah Asal",
	"No Peserta Ujian", "No Seri Ijazah Sebelumnya", "No SKHUN Sebelumnya",
	"Nama Ayah", "NIK Ayah", "Tahun Lahir Ayah", "Pendidikan Ayah", "Pekerjaan Ayah", "Penghasilan Ayah", "HP Ayah",
	"Nama Ibu", "NIK Ibu", "Tahun Lahir Ibu", "Pendidikan Ibu", "Pekerjaan Ibu", "Penghasilan Ibu", "HP Ibu",
	"Nama Wali", "NIK Wali", "Tahun Lahir Wali", "Pendidikan Wali", "Pekerjaan Wali", "Penghasilan Wali", "HP Wali", "Hubungan Wali",
	"Kelas",
}

// textColumns are formatted as text in the template so Excel keeps leading
// zeros and long digit strings (NISN, NIK, phone numbers, ...).
var textColumns = map[string]bool{
	"nisn": true, "nis": true, "nik": true, "no kk": true, "rt": true, "rw": true, "kode pos": true,
	"telepon": true, "hp": true, "nik ayah": true, "hp ayah": true, "nik ibu": true, "hp ibu": true,
	"nik wali": true, "hp wali": true, "npsn sekolah asal": true, "no kps/pkh": true, "no kip": true, "no kks": true,
}

var reParen = regexp.MustCompile(`\([^)]*\)`)

// NormalizeStudentHeader maps a header cell to its column key: lower case,
// without "*" and parenthesised hints ("Jenis Kelamin* (L/P)" → "jenis kelamin").
func NormalizeStudentHeader(h string) string {
	h = strings.ReplaceAll(strings.ToLower(h), "*", "")
	h = reParen.ReplaceAllString(h, "")
	return strings.Join(strings.Fields(h), " ")
}

// cellSetter parses a non-empty cell into the student; the returned string is
// an error message ("" = ok).
type cellSetter func(st *model.Students, v string) string

type studentColumn struct {
	Key   string // normalized header
	Path  string // validation field path (for column-level errors)
	DBCol string // students column updated by an upsert
	Set   cellSetter
}

func setStr(f func(st *model.Students) *string) cellSetter {
	return func(st *model.Students, v string) string { *f(st) = v; return "" }
}

func parseIntCell(v string) (int, bool) {
	v = strings.TrimSpace(v)
	if i, err := strconv.Atoi(v); err == nil {
		return i, true
	}
	if f, err := strconv.ParseFloat(strings.ReplaceAll(v, ",", "."), 64); err == nil && f == float64(int(f)) {
		return int(f), true
	}
	return 0, false
}

func setInt(f func(st *model.Students) **int) cellSetter {
	return func(st *model.Students, v string) string {
		i, ok := parseIntCell(v)
		if !ok {
			return "harus berupa angka bulat"
		}
		*f(st) = &i
		return ""
	}
}

func setFloat(f func(st *model.Students) **float64) cellSetter {
	return func(st *model.Students, v string) string {
		x, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(v), ",", "."), 64)
		if err != nil {
			return "harus berupa angka"
		}
		*f(st) = &x
		return ""
	}
}

func parseBoolCell(v string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "ya", "y", "yes", "true", "1":
		return true, true
	case "tidak", "t", "no", "n", "false", "0":
		return false, true
	}
	return false, false
}

func setBool(f func(st *model.Students) *bool) cellSetter {
	return func(st *model.Students, v string) string {
		b, ok := parseBoolCell(v)
		if !ok {
			return "harus Ya atau Tidak"
		}
		*f(st) = b
		return ""
	}
}

func setDate(f func(st *model.Students) *string) cellSetter {
	return func(st *model.Students, v string) string {
		d, ok := normalizeImportDate(v)
		if !ok {
			return "harus berformat YYYY-MM-DD, DD/MM/YYYY atau tanggal Excel"
		}
		*f(st) = d
		return ""
	}
}

// padNISN restores leading zeros Excel drops when NISN is stored as a number.
func padNISN(v string) string {
	if n := len(v); (n == 8 || n == 9) && strings.Trim(v, "0123456789") == "" {
		return strings.Repeat("0", 10-n) + v
	}
	return v
}

var studentColumns = []studentColumn{
	{"nama lengkap", "identity.full_name", "full_name", setStr(func(s *model.Students) *string { return &s.FullName })},
	{"nama panggilan", "identity.nickname", "nickname", setStr(func(s *model.Students) *string { return &s.Nickname })},
	{"jenis kelamin", "identity.gender", "gender", setStr(func(s *model.Students) *string { return &s.Gender })},
	{"nisn", "identity.nisn", "nisn", func(s *model.Students, v string) string { s.NISN = padNISN(v); return "" }},
	{"nis", "identity.nis", "nis", setStr(func(s *model.Students) *string { return &s.NIS })},
	{"nik", "identity.nik", "nik", setStr(func(s *model.Students) *string { return &s.NIK })},
	{"no kk", "identity.no_kk", "no_kk", setStr(func(s *model.Students) *string { return &s.NoKK })},
	{"tempat lahir", "identity.birth_place", "birth_place", setStr(func(s *model.Students) *string { return &s.BirthPlace })},
	{"tanggal lahir", "identity.birth_date", "birth_date", setDate(func(s *model.Students) *string { return &s.BirthDate })},
	{"no akta lahir", "identity.birth_certificate_no", "birth_certificate_no", setStr(func(s *model.Students) *string { return &s.BirthCertificateNo })},
	{"agama", "identity.religion", "religion", setStr(func(s *model.Students) *string { return &s.Religion })},
	{"kewarganegaraan", "identity.citizenship", "citizenship", setStr(func(s *model.Students) *string { return &s.Citizenship })},
	{"kebutuhan khusus", "identity.special_needs", "special_needs", setStr(func(s *model.Students) *string { return &s.SpecialNeeds })},
	{"golongan darah", "identity.blood_type", "blood_type", setStr(func(s *model.Students) *string { return &s.BloodType })},
	{"anak ke", "identity.child_order", "child_order", setInt(func(s *model.Students) **int { return &s.ChildOrder })},
	{"jumlah saudara", "identity.siblings_count", "siblings_count", setInt(func(s *model.Students) **int { return &s.SiblingsCount })},
	{"alamat", "address.street", "street", setStr(func(s *model.Students) *string { return &s.Street })},
	{"rt", "address.rt", "rt", setStr(func(s *model.Students) *string { return &s.RT })},
	{"rw", "address.rw", "rw", setStr(func(s *model.Students) *string { return &s.RW })},
	{"dusun", "address.hamlet", "hamlet", setStr(func(s *model.Students) *string { return &s.Hamlet })},
	{"kelurahan/desa", "address.village", "village", setStr(func(s *model.Students) *string { return &s.Village })},
	{"kecamatan", "address.district", "district", setStr(func(s *model.Students) *string { return &s.District })},
	{"kabupaten/kota", "address.city", "city", setStr(func(s *model.Students) *string { return &s.City })},
	{"provinsi", "address.province", "province", setStr(func(s *model.Students) *string { return &s.Province })},
	{"kode pos", "address.postal_code", "postal_code", setStr(func(s *model.Students) *string { return &s.PostalCode })},
	{"jenis tinggal", "address.residence_type", "residence_type", setStr(func(s *model.Students) *string { return &s.ResidenceType })},
	{"alat transportasi", "address.transportation", "transportation", setStr(func(s *model.Students) *string { return &s.Transportation })},
	{"jarak", "address.distance_km", "distance_km", setFloat(func(s *model.Students) **float64 { return &s.DistanceKm })},
	{"waktu tempuh", "address.travel_time_minutes", "travel_time_minutes", setInt(func(s *model.Students) **int { return &s.TravelTimeMinutes })},
	{"telepon", "contact.phone", "phone", setStr(func(s *model.Students) *string { return &s.Phone })},
	{"hp", "contact.mobile_phone", "mobile_phone", setStr(func(s *model.Students) *string { return &s.MobilePhone })},
	{"email", "contact.email", "email", setStr(func(s *model.Students) *string { return &s.Email })},
	{"tinggi", "physical.height_cm", "height_cm", setInt(func(s *model.Students) **int { return &s.HeightCm })},
	{"berat", "physical.weight_kg", "weight_kg", setInt(func(s *model.Students) **int { return &s.WeightKg })},
	{"lingkar kepala", "physical.head_circumference_cm", "head_circumference_cm", setInt(func(s *model.Students) **int { return &s.HeadCircumferenceCm })},
	{"penerima kps/pkh", "welfare.kps_pkh_receiver", "kps_pkh_receiver", setBool(func(s *model.Students) *bool { return &s.KpsPkhReceiver })},
	{"no kps/pkh", "welfare.kps_pkh_number", "kps_pkh_number", setStr(func(s *model.Students) *string { return &s.KpsPkhNumber })},
	{"penerima kip", "welfare.kip_receiver", "kip_receiver", setBool(func(s *model.Students) *bool { return &s.KipReceiver })},
	{"no kip", "welfare.kip_number", "kip_number", setStr(func(s *model.Students) *string { return &s.KipNumber })},
	{"nama di kip", "welfare.kip_name", "kip_name", setStr(func(s *model.Students) *string { return &s.KipName })},
	{"no kks", "welfare.kks_number", "kks_number", setStr(func(s *model.Students) *string { return &s.KksNumber })},
	{"jenis pendaftaran", "registration.registration_type", "registration_type", setStr(func(s *model.Students) *string { return &s.RegistrationType })},
	{"tanggal masuk", "registration.entry_date", "entry_date", setDate(func(s *model.Students) *string { return &s.EntryDate })},
	{"sekolah asal", "registration.previous_school_name", "previous_school_name", setStr(func(s *model.Students) *string { return &s.PreviousSchoolName })},
	{"npsn sekolah asal", "registration.previous_school_npsn", "previous_school_npsn", setStr(func(s *model.Students) *string { return &s.PreviousSchoolNPSN })},
	{"no peserta ujian", "registration.exam_participant_number", "exam_participant_number", setStr(func(s *model.Students) *string { return &s.ExamParticipantNumber })},
	{"no seri ijazah sebelumnya", "registration.previous_diploma_number", "previous_diploma_number", setStr(func(s *model.Students) *string { return &s.PreviousDiplomaNumber })},
	{"no skhun sebelumnya", "registration.previous_skhun_number", "previous_skhun_number", setStr(func(s *model.Students) *string { return &s.PreviousSkhunNumber })},
}

// guardianColumns: per guardian type, header suffix → guardian field.
var guardianWho = []struct{ Type, Suffix string }{
	{dto.GuardianFather, "ayah"}, {dto.GuardianMother, "ibu"}, {dto.GuardianGuardian, "wali"},
}

var guardianFieldHeader = map[string]string{
	"name": "nama", "nik": "nik", "birth_year": "tahun lahir", "education": "pendidikan",
	"occupation": "pekerjaan", "income": "penghasilan", "phone": "hp", "relationship": "hubungan",
}

// headerLabel returns the template header (without hints) for a normalized key.
func headerLabel(key string) string {
	for _, h := range StudentTemplateHeaders {
		if NormalizeStudentHeader(h) == key {
			return strings.TrimSpace(strings.ReplaceAll(reParen.ReplaceAllString(h, ""), "*", ""))
		}
	}
	return key
}

// pathColumn maps a validation field path to the template header.
func pathColumn(path string) string {
	for _, c := range studentColumns {
		if c.Path == path {
			return headerLabel(c.Key)
		}
	}
	return ""
}

func (s *StudentExcelService) GenerateTemplate(ctx context.Context, schoolID uuid.UUID) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	const sheetSiswa = "Siswa"
	const sheetRef = "Referensi"
	if err := f.SetSheetName("Sheet1", sheetSiswa); err != nil {
		return nil, err
	}
	if _, err := f.NewSheet(sheetRef); err != nil {
		return nil, err
	}

	headerStyle, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "#FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#2563EB"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
	})
	if err != nil {
		return nil, err
	}
	textStyle, err := f.NewStyle(&excelize.Style{NumFmt: 49})
	if err != nil {
		return nil, err
	}

	for colIdx, h := range StudentTemplateHeaders {
		col, _ := excelize.ColumnNumberToName(colIdx + 1)
		_ = f.SetCellValue(sheetSiswa, col+"1", h)
		_ = f.SetColWidth(sheetSiswa, col, col, 20)
		if textColumns[NormalizeStudentHeader(h)] {
			_ = f.SetColStyle(sheetSiswa, col, textStyle)
		}
	}
	lastCol, _ := excelize.ColumnNumberToName(len(StudentTemplateHeaders))
	_ = f.SetCellStyle(sheetSiswa, "A1", lastCol+"1", headerStyle)
	_ = f.SetRowHeight(sheetSiswa, 1, 28)

	// Example row (identifiers as strings so leading zeros survive).
	exampleRow := []interface{}{
		"Ahmad Dahlan", "Ahmad", "L", "0081234567", "2025001", "3201011234567890", "3201019876543210",
		"Jakarta", "2010-05-15", "1234/A/2010", "islam", "WNI",
		"none", "O", 1, 2, "Jl. Merdeka No. 45", "003", "005", "Dusun Krajan",
		"Sukamaju", "Cilodong", "Depok", "Jawa Barat", "16415", "orang_tua",
		"sepeda_motor", 2.5, 15, "021778899", "081234567890", "ahmad@example.com",
		160, 50, 54,
		"Tidak", "", "Tidak", "", "", "",
		"siswa_baru", "2025-07-15", "SD Negeri 1 Depok", "20201234",
		"12-345-678-9", "DN-01/D-SD/24/001", "SKHUN-123456",
		"Budi Santoso", "3201011111110001", 1980, "s1", "wiraswasta", "5m_20m", "081211112222",
		"Siti Aminah", "3201012222220002", 1983, "d3", "ibu_rumah_tangga", "tidak_berpenghasilan", "081233334444",
		"", "", "", "", "", "", "", "",
		"",
	}
	for colIdx, val := range exampleRow {
		cell, _ := excelize.CoordinatesToCellName(colIdx+1, 2)
		_ = f.SetCellValue(sheetSiswa, cell, val)
	}

	for colIdx, h := range []string{"Kategori", "Nilai (Value)", "Keterangan (Label)"} {
		cell, _ := excelize.CoordinatesToCellName(colIdx+1, 1)
		_ = f.SetCellValue(sheetRef, cell, h)
	}
	_ = f.SetCellStyle(sheetRef, "A1", "C1", headerStyle)
	_ = f.SetColWidth(sheetRef, "A", "B", 24)
	_ = f.SetColWidth(sheetRef, "C", "C", 40)

	refRow := 2
	for _, k := range dto.ReferenceKeys() {
		for _, o := range dto.RefOptions(k) {
			_ = f.SetSheetRow(sheetRef, fmt.Sprintf("A%d", refRow), &[]interface{}{k, o.Value, o.Label})
			refRow++
		}
	}

	var activeClasses []model.Classes
	if err := s.conn(ctx).
		Joins("JOIN academic_years ay ON ay.id = classes.academic_year_id AND ay.is_active = true AND ay.school_id = classes.school_id").
		Where("classes.school_id = ?", schoolID).Order("classes.class_name").
		Find(&activeClasses).Error; err != nil {
		return nil, err
	}
	for _, ac := range activeClasses {
		_ = f.SetSheetRow(sheetRef, fmt.Sprintf("A%d", refRow), &[]interface{}{"kelas_aktif", ac.Name, model.ClassDisplayName(ac.Name, ac.Alias)})
		refRow++
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// readStudentWorkbook validates and opens the upload, returning the first
// sheet's rows (bounded in size, unzip size and row count).
func readStudentWorkbook(filename string, size int64, src io.Reader) ([][]string, error) {
	tooLarge := apperr.New(http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "Ukuran file maksimal 10 MB")
	if !strings.EqualFold(filepath.Ext(filename), ".xlsx") {
		return nil, apperr.New(http.StatusBadRequest, "INVALID_FILE_TYPE", "File harus berformat .xlsx")
	}
	if size > MaxStudentImportFileSize {
		return nil, tooLarge
	}
	buf, err := io.ReadAll(io.LimitReader(src, MaxStudentImportFileSize+1))
	if err != nil {
		return nil, apperr.BadRequest("Gagal membaca file")
	}
	if len(buf) > MaxStudentImportFileSize {
		return nil, tooLarge
	}
	if !bytes.HasPrefix(buf, []byte("PK\x03\x04")) {
		return nil, apperr.New(http.StatusBadRequest, "INVALID_FILE_TYPE", "File bukan .xlsx yang valid")
	}
	f, err := excelize.OpenReader(bytes.NewReader(buf), excelize.Options{
		UnzipSizeLimit:    studentUnzipSizeLimit,
		UnzipXMLSizeLimit: studentUnzipXMLSizeLimit,
		RawCellValue:      true, // dates as serial numbers (normalizeImportDate), no locale formatting
	})
	if err != nil {
		return nil, apperr.New(http.StatusBadRequest, "INVALID_FILE", "File .xlsx tidak dapat dibaca")
	}
	defer f.Close()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, apperr.BadRequest("File tidak memiliki sheet")
	}
	it, err := f.Rows(sheets[0])
	if err != nil {
		return nil, apperr.New(http.StatusBadRequest, "INVALID_FILE", "Sheet pertama tidak dapat dibaca")
	}
	defer it.Close()

	var rows [][]string
	nonEmpty := 0
	for it.Next() {
		cols, err := it.Columns()
		if err != nil {
			return nil, apperr.New(http.StatusBadRequest, "INVALID_FILE", "Sheet pertama tidak dapat dibaca")
		}
		if len(rows) > 0 && isBlankRow(cols) {
			rows = append(rows, nil)
			continue
		}
		rows = append(rows, cols)
		if len(rows) > 1 {
			nonEmpty++
			if nonEmpty > MaxStudentImportRows {
				return nil, apperr.BadRequest(fmt.Sprintf("Maksimal %d baris data per import", MaxStudentImportRows))
			}
		}
	}
	if err := it.Error(); err != nil {
		return nil, apperr.New(http.StatusBadRequest, "INVALID_FILE", "Sheet pertama tidak dapat dibaca")
	}
	return rows, nil
}

// ParsedStudentRow is one data row mapped onto a student (only the provided,
// non-empty cells are set).
type ParsedStudentRow struct {
	Row       int
	Student   model.Students
	Provided  map[string]bool              // DB columns provided by the row
	Guardians map[string]*dto.GuardianItem // by guardian type (only provided ones)
	ClassName string                       // "Kelas" cell
	Errors    []dto.ImportError            // cell parse errors
}

// ParseStudentRows maps the sheet rows (rows[0] = header) to students without
// touching the database. Unknown columns are ignored.
func ParseStudentRows(rows [][]string) ([]ParsedStudentRow, error) {
	if len(rows) == 0 {
		return nil, apperr.BadRequest("File kosong")
	}
	idx := map[string]int{}
	for i, h := range rows[0] {
		k := NormalizeStudentHeader(h)
		if _, dup := idx[k]; k != "" && !dup {
			idx[k] = i
		}
	}
	if _, ok := idx["nama lengkap"]; !ok {
		return nil, apperr.New(http.StatusBadRequest, "INVALID_TEMPLATE",
			"Header baris 1 tidak sesuai template (kolom \"Nama Lengkap\" tidak ditemukan). Unduh template terbaru.")
	}
	cell := func(row []string, key string) string {
		i, ok := idx[key]
		if !ok || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}

	var out []ParsedStudentRow
	for i := 1; i < len(rows); i++ {
		row := rows[i]
		if row == nil || isBlankRow(row) {
			continue
		}
		p := ParsedStudentRow{Row: i + 1, Provided: map[string]bool{}, Guardians: map[string]*dto.GuardianItem{}}
		for _, c := range studentColumns {
			v := cell(row, c.Key)
			if v == "" {
				continue
			}
			if msg := c.Set(&p.Student, v); msg != "" {
				p.Errors = append(p.Errors, dto.ImportError{Row: p.Row, Column: headerLabel(c.Key), Message: headerLabel(c.Key) + " " + msg})
				continue
			}
			p.Provided[c.DBCol] = true
		}
		for _, w := range guardianWho {
			get := func(field string) string { return cell(row, guardianFieldHeader[field]+" "+w.Suffix) }
			g := dto.GuardianItem{
				Type: w.Type, Name: get("name"), NIK: get("nik"), Education: get("education"),
				Occupation: get("occupation"), Income: get("income"), Phone: get("phone"), IsAlive: true,
			}
			if w.Type == dto.GuardianGuardian {
				g.Relationship = get("relationship")
			}
			by := get("birth_year")
			if g.Name == "" && g.NIK == "" && by == "" && g.Education == "" && g.Occupation == "" && g.Income == "" && g.Phone == "" && g.Relationship == "" {
				continue
			}
			if by != "" {
				if y, ok := parseIntCell(by); ok {
					g.BirthYear = &y
				} else {
					col := headerLabel("tahun lahir " + w.Suffix)
					p.Errors = append(p.Errors, dto.ImportError{Row: p.Row, Column: col, Message: col + " harus berupa angka"})
				}
			}
			if g.Occupation == dto.EnumLabel(dto.RefOccupations, "sudah_meninggal") || strings.EqualFold(g.Occupation, "sudah_meninggal") {
				g.IsAlive = false
			}
			gi := g
			p.Guardians[w.Type] = &gi
		}
		p.ClassName = cell(row, "kelas")
		out = append(out, p)
	}
	return out, nil
}

// guardianErrorColumn maps "guardians[i].field" to the template header.
func guardianErrorColumn(path string, order []string) string {
	var i int
	var field string
	if _, err := fmt.Sscanf(path, "guardians[%d].", &i); err != nil {
		return ""
	}
	if dot := strings.Index(path, "]."); dot >= 0 {
		field = path[dot+2:]
	}
	if i < 0 || i >= len(order) {
		return ""
	}
	for _, w := range guardianWho {
		if w.Type == order[i] {
			if h, ok := guardianFieldHeader[field]; ok {
				return headerLabel(h + " " + w.Suffix)
			}
		}
	}
	return ""
}

type importClass struct {
	cls     model.Classes
	members int // active members + planned placements
}

func (s *StudentExcelService) ImportStudents(ctx context.Context, schoolID uuid.UUID, userID uint, filename string, size int64, fileReader io.Reader, dryRun bool, mode string) (*dto.StudentImportResult, error) {
	if mode == "" {
		mode = "create"
	}
	if mode != "create" && mode != "upsert" {
		return nil, apperr.Validation("mode harus create atau upsert", map[string]string{"mode": "mode harus create atau upsert"})
	}
	rows, err := readStudentWorkbook(filename, size, fileReader)
	if err != nil {
		return nil, err
	}
	parsed, err := ParseStudentRows(rows)
	if err != nil {
		return nil, err
	}
	if len(parsed) == 0 {
		return nil, apperr.BadRequest("Tidak ada baris data pada file")
	}
	db := s.conn(ctx)

	// Existing identifiers of the school (non-deleted).
	var existing []model.Students
	if err := db.Select("id, nisn, nis, nik").Where("school_id = ?", schoolID).Find(&existing).Error; err != nil {
		return nil, err
	}
	byNISN, byNIS, byNIK := map[string]uuid.UUID{}, map[string]uuid.UUID{}, map[string]uuid.UUID{}
	for _, e := range existing {
		if e.NISN != "" {
			byNISN[e.NISN] = e.ID
		}
		if e.NIS != "" {
			byNIS[e.NIS] = e.ID
		}
		if e.NIK != "" {
			byNIK[e.NIK] = e.ID
		}
	}

	// Upsert targets, loaded in one query (without photo).
	targets := map[int]uuid.UUID{}
	if mode == "upsert" {
		for i, p := range parsed {
			if id, ok := byNISN[p.Student.NISN]; ok && p.Student.NISN != "" {
				targets[i] = id
			} else if id, ok := byNIS[p.Student.NIS]; ok && p.Student.NIS != "" {
				targets[i] = id
			}
		}
	}
	current := map[uuid.UUID]model.Students{}
	if len(targets) > 0 {
		var ids []uuid.UUID
		for _, id := range targets {
			ids = append(ids, id)
		}
		var list []model.Students
		if err := db.Select(strings.ReplaceAll(studentColumnsNoPhoto(db), "s.", "")).
			Where("school_id = ? AND id IN ?", schoolID, ids).Find(&list).Error; err != nil {
			return nil, err
		}
		for _, st := range list {
			current[st.ID] = st
		}
	}

	// Classes of the active academic year by lower(name) and lower(display_name).
	var activeClasses []model.Classes
	if err := db.Joins("JOIN academic_years ay ON ay.id = classes.academic_year_id AND ay.is_active = true AND ay.school_id = classes.school_id").
		Where("classes.school_id = ?", schoolID).Find(&activeClasses).Error; err != nil {
		return nil, err
	}
	classByName := map[string]*importClass{}
	for _, c := range activeClasses {
		if c.AcademicYearID == nil || c.GradeLevelID == nil {
			continue
		}
		n, err := activeMemberCount(db, schoolID, c.ID)
		if err != nil {
			return nil, err
		}
		ic := &importClass{cls: c, members: int(n)}
		classByName[strings.ToLower(strings.TrimSpace(c.Name))] = ic
		classByName[strings.ToLower(model.ClassDisplayName(c.Name, c.Alias))] = ic
	}
	activeClassOf, err := activeClassesFor(db, schoolID, func() []uuid.UUID {
		var ids []uuid.UUID
		for _, id := range targets {
			ids = append(ids, id)
		}
		return ids
	}())
	if err != nil {
		return nil, err
	}

	type validRow struct {
		Row      int
		IsUpdate bool
		Student  model.Students
		Columns  []string
		Class    *importClass
	}
	res := &dto.StudentImportResult{Total: len(parsed), DryRun: dryRun, Errors: []dto.ImportError{}}
	var valid []validRow
	seenNISN, seenNIS, seenNIK := map[string]int{}, map[string]int{}, map[string]int{}

	for i, p := range parsed {
		rowErrs := append([]dto.ImportError(nil), p.Errors...)
		targetID, isUpdate := targets[i]

		st := p.Student
		cols := make([]string, 0, len(p.Provided)+3)
		if isUpdate {
			merged := current[targetID]
			mergeProvided(&merged, &st, p.Provided)
			st = merged
			for col := range p.Provided {
				cols = append(cols, col)
			}
			cols = append(cols, "updated_at")
		} else {
			st.SchoolID = schoolID
			st.Status = dto.StudentStatusActive
		}

		// Guardians: replace provided guardian types, keep the others.
		var guardians []dto.GuardianItem
		if isUpdate && hasJSONItems(st.GuardiansJSON) {
			_ = json.Unmarshal([]byte(st.GuardiansJSON), &guardians)
		}
		if len(p.Guardians) > 0 {
			kept := guardians[:0]
			for _, g := range guardians {
				if _, replaced := p.Guardians[g.Type]; !replaced {
					kept = append(kept, g)
				}
			}
			guardians = kept
			for _, w := range guardianWho {
				if g, ok := p.Guardians[w.Type]; ok {
					guardians = append(guardians, *g)
				}
			}
			cols = append(cols, "guardians_json")
		}
		order := make([]string, len(guardians))
		for gi, g := range guardians {
			order[gi] = g.Type
		}

		fe := fieldErrors{}
		validateStudentFields(&st, fe)
		guardians = normalizeGuardians(guardians, fe)
		if guardians == nil {
			guardians = []dto.GuardianItem{}
		}
		b, _ := json.Marshal(guardians)
		st.GuardiansJSON = string(b)
		if !isUpdate {
			st.AchievementsJSON, st.ScholarshipsJSON = "[]", "[]"
		}
		keys := make([]string, 0, len(fe))
		for k := range fe {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			col := pathColumn(k)
			if col == "" {
				col = guardianErrorColumn(k, order)
			}
			rowErrs = append(rowErrs, dto.ImportError{Row: p.Row, Column: col, Message: fe[k]})
		}

		// Uniqueness against the school and within the file.
		for _, u := range []struct {
			val, col string
			db       map[string]uuid.UUID
			seen     map[string]int
		}{{st.NISN, "NISN", byNISN, seenNISN}, {st.NIS, "NIS", byNIS, seenNIS}, {st.NIK, "NIK", byNIK, seenNIK}} {
			if u.val == "" {
				continue
			}
			if owner, ok := u.db[u.val]; ok && (!isUpdate || owner != targetID) {
				rowErrs = append(rowErrs, dto.ImportError{Row: p.Row, Column: u.col, Message: u.col + " sudah digunakan oleh siswa lain"})
			} else if first, dup := u.seen[u.val]; dup {
				rowErrs = append(rowErrs, dto.ImportError{Row: p.Row, Column: u.col, Message: fmt.Sprintf("%s duplikat dengan baris %d", u.col, first)})
			}
		}

		// Class placement (active academic year).
		var cls *importClass
		if p.ClassName != "" {
			cls = classByName[strings.ToLower(strings.Join(strings.Fields(p.ClassName), " "))]
			switch {
			case cls == nil:
				rowErrs = append(rowErrs, dto.ImportError{Row: p.Row, Column: "Kelas", Message: fmt.Sprintf("Kelas %q tidak ditemukan pada tahun ajaran aktif", p.ClassName)})
			case st.Status != dto.StudentStatusActive:
				rowErrs = append(rowErrs, dto.ImportError{Row: p.Row, Column: "Kelas", Message: "Hanya siswa aktif yang dapat ditempatkan di kelas"})
				cls = nil
			default:
				if cur := activeClassOf[targetID]; isUpdate && cur != nil && cur.ClassID == cls.cls.ID {
					cls = nil // already a member
				} else if cls.cls.Capacity != nil && *cls.cls.Capacity > 0 && cls.members >= *cls.cls.Capacity && len(rowErrs) == 0 {
					rowErrs = append(rowErrs, dto.ImportError{Row: p.Row, Column: "Kelas", Message: "Kapasitas kelas sudah penuh (CLASS_FULL)"})
					cls = nil
				}
			}
		}

		if len(rowErrs) > 0 {
			res.Failed++
			res.Errors = append(res.Errors, rowErrs...)
			continue
		}
		for _, u := range []struct {
			val  string
			seen map[string]int
		}{{st.NISN, seenNISN}, {st.NIS, seenNIS}, {st.NIK, seenNIK}} {
			if u.val != "" {
				u.seen[u.val] = p.Row
			}
		}
		if cls != nil {
			cls.members++
		}
		if isUpdate {
			res.Updated++
		} else {
			res.Created++
		}
		valid = append(valid, validRow{Row: p.Row, IsUpdate: isUpdate, Student: st, Columns: cols, Class: cls})
	}

	if dryRun || len(valid) == 0 {
		return res, nil
	}

	err = db.Transaction(func(tx *gorm.DB) error {
		locked := map[uuid.UUID]*model.Classes{}
		for _, vr := range valid {
			st := vr.Student
			if vr.IsUpdate {
				if len(vr.Columns) > 0 {
					if err := tx.Model(&model.Students{ID: st.ID}).Where("school_id = ?", schoolID).
						Select(vr.Columns).Updates(&st).Error; err != nil {
						return duplicateStudentErr(err)
					}
				}
			} else if err := tx.Create(&st).Error; err != nil {
				return duplicateStudentErr(err)
			}
			if vr.Class != nil {
				cls, ok := locked[vr.Class.cls.ID]
				if !ok {
					c, err := lockClass(tx, schoolID, vr.Class.cls.ID)
					if err != nil {
						return err
					}
					cls, locked[c.ID] = c, c
				}
				if err := placeInClassTx(tx, schoolID, st.ID, cls, "", "Impor data siswa", userID, false); err != nil {
					if ae, ok := err.(*apperr.Error); ok && ae.Code == "ALREADY_IN_CLASS" {
						continue
					}
					if ae, ok := err.(*apperr.Error); ok {
						return apperr.New(ae.Status, ae.Code, fmt.Sprintf("Baris %d: %s", vr.Row, ae.Message))
					}
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// mergeProvided copies the provided columns of src onto dst using the column
// setters' targets (src was filled by the same setters).
func mergeProvided(dst, src *model.Students, provided map[string]bool) {
	for _, c := range studentColumns {
		if !provided[c.DBCol] {
			continue
		}
		copyStudentColumn(dst, src, c.DBCol)
	}
}

// copyStudentColumn copies one imported column value.
func copyStudentColumn(dst, src *model.Students, col string) {
	switch col {
	case "full_name":
		dst.FullName = src.FullName
	case "nickname":
		dst.Nickname = src.Nickname
	case "gender":
		dst.Gender = src.Gender
	case "nisn":
		dst.NISN = src.NISN
	case "nis":
		dst.NIS = src.NIS
	case "nik":
		dst.NIK = src.NIK
	case "no_kk":
		dst.NoKK = src.NoKK
	case "birth_place":
		dst.BirthPlace = src.BirthPlace
	case "birth_date":
		dst.BirthDate = src.BirthDate
	case "birth_certificate_no":
		dst.BirthCertificateNo = src.BirthCertificateNo
	case "religion":
		dst.Religion = src.Religion
	case "citizenship":
		dst.Citizenship = src.Citizenship
	case "special_needs":
		dst.SpecialNeeds = src.SpecialNeeds
	case "blood_type":
		dst.BloodType = src.BloodType
	case "child_order":
		dst.ChildOrder = src.ChildOrder
	case "siblings_count":
		dst.SiblingsCount = src.SiblingsCount
	case "street":
		dst.Street = src.Street
	case "rt":
		dst.RT = src.RT
	case "rw":
		dst.RW = src.RW
	case "hamlet":
		dst.Hamlet = src.Hamlet
	case "village":
		dst.Village = src.Village
	case "district":
		dst.District = src.District
	case "city":
		dst.City = src.City
	case "province":
		dst.Province = src.Province
	case "postal_code":
		dst.PostalCode = src.PostalCode
	case "residence_type":
		dst.ResidenceType = src.ResidenceType
	case "transportation":
		dst.Transportation = src.Transportation
	case "distance_km":
		dst.DistanceKm = src.DistanceKm
	case "travel_time_minutes":
		dst.TravelTimeMinutes = src.TravelTimeMinutes
	case "phone":
		dst.Phone = src.Phone
	case "mobile_phone":
		dst.MobilePhone = src.MobilePhone
	case "email":
		dst.Email = src.Email
	case "height_cm":
		dst.HeightCm = src.HeightCm
	case "weight_kg":
		dst.WeightKg = src.WeightKg
	case "head_circumference_cm":
		dst.HeadCircumferenceCm = src.HeadCircumferenceCm
	case "kps_pkh_receiver":
		dst.KpsPkhReceiver = src.KpsPkhReceiver
	case "kps_pkh_number":
		dst.KpsPkhNumber = src.KpsPkhNumber
	case "kip_receiver":
		dst.KipReceiver = src.KipReceiver
	case "kip_number":
		dst.KipNumber = src.KipNumber
	case "kip_name":
		dst.KipName = src.KipName
	case "kks_number":
		dst.KksNumber = src.KksNumber
	case "registration_type":
		dst.RegistrationType = src.RegistrationType
	case "entry_date":
		dst.EntryDate = src.EntryDate
	case "previous_school_name":
		dst.PreviousSchoolName = src.PreviousSchoolName
	case "previous_school_npsn":
		dst.PreviousSchoolNPSN = src.PreviousSchoolNPSN
	case "exam_participant_number":
		dst.ExamParticipantNumber = src.ExamParticipantNumber
	case "previous_diploma_number":
		dst.PreviousDiplomaNumber = src.PreviousDiplomaNumber
	case "previous_skhun_number":
		dst.PreviousSkhunNumber = src.PreviousSkhunNumber
	}
}

func derefInt(p *int) interface{} {
	if p == nil {
		return nil
	}
	return *p
}

func derefFloat(p *float64) interface{} {
	if p == nil {
		return nil
	}
	return *p
}

func (s *StudentExcelService) ExportStudents(ctx context.Context, schoolID uuid.UUID, q StudentListQuery) ([]byte, error) {
	db := s.conn(ctx)
	query, err := NewStudentService(s.db).filteredStudents(ctx, schoolID, q)
	if err != nil {
		return nil, err
	}
	var students []model.Students
	if err := query.Select(studentColumnsNoPhoto(db)).Order(studentOrder(q.Sort)).
		Limit(MaxStudentExportRows).Find(&students).Error; err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(students))
	for _, st := range students {
		ids = append(ids, st.ID)
	}
	classes, err := activeClassesFor(db, schoolID, ids)
	if err != nil {
		return nil, err
	}
	classNames := map[uuid.UUID]string{}
	if len(classes) > 0 {
		var cids []uuid.UUID
		for _, c := range classes {
			cids = append(cids, c.ClassID)
		}
		var cl []model.Classes
		if err := db.Unscoped().Select("id, class_name").Where("school_id = ? AND id IN ?", schoolID, cids).Find(&cl).Error; err != nil {
			return nil, err
		}
		for _, c := range cl {
			classNames[c.ID] = c.Name
		}
	}

	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	const sheetName = "Data Siswa"
	if err := f.SetSheetName("Sheet1", sheetName); err != nil {
		return nil, err
	}
	sw, err := f.NewStreamWriter(sheetName)
	if err != nil {
		return nil, err
	}
	headerStyle, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Color: "#FFFFFF"},
		Fill: excelize.Fill{Type: "pattern", Color: []string{"#047857"}, Pattern: 1},
	})
	if err != nil {
		return nil, err
	}
	headers := append(append([]string(nil), StudentTemplateHeaders...), "Status", "Kelas Saat Ini")
	_ = sw.SetColWidth(1, len(headers), 20)
	hdr := make([]interface{}, len(headers))
	for i, h := range headers {
		hdr[i] = excelize.Cell{StyleID: headerStyle, Value: h}
	}
	if err := sw.SetRow("A1", hdr); err != nil {
		return nil, err
	}

	for rowIdx := range students {
		st := &students[rowIdx]
		var guardians []dto.GuardianItem
		if hasJSONItems(st.GuardiansJSON) {
			_ = json.Unmarshal([]byte(st.GuardiansJSON), &guardians)
		}
		var ayah, ibu, wali dto.GuardianItem
		for _, g := range guardians {
			switch g.Type {
			case dto.GuardianFather:
				ayah = g
			case dto.GuardianMother:
				ibu = g
			case dto.GuardianGuardian:
				if wali.Name == "" {
					wali = g
				}
			}
		}
		className, display := "", ""
		if c := classes[st.ID]; c != nil {
			className, display = classNames[c.ClassID], c.DisplayName
		}
		vals := []interface{}{
			st.FullName, st.Nickname, st.Gender, st.NISN, st.NIS, st.NIK, st.NoKK,
			st.BirthPlace, st.BirthDate, st.BirthCertificateNo, st.Religion, st.Citizenship,
			st.SpecialNeeds, st.BloodType, derefInt(st.ChildOrder), derefInt(st.SiblingsCount), st.Street, st.RT, st.RW, st.Hamlet,
			st.Village, st.District, st.City, st.Province, st.PostalCode, st.ResidenceType,
			st.Transportation, derefFloat(st.DistanceKm), derefInt(st.TravelTimeMinutes), st.Phone, st.MobilePhone, st.Email,
			derefInt(st.HeightCm), derefInt(st.WeightKg), derefInt(st.HeadCircumferenceCm),
			boolToYaTidak(st.KpsPkhReceiver), st.KpsPkhNumber, boolToYaTidak(st.KipReceiver), st.KipNumber, st.KipName, st.KksNumber,
			st.RegistrationType, st.EntryDate, st.PreviousSchoolName, st.PreviousSchoolNPSN,
			st.ExamParticipantNumber, st.PreviousDiplomaNumber, st.PreviousSkhunNumber,
			ayah.Name, ayah.NIK, derefInt(ayah.BirthYear), ayah.Education, ayah.Occupation, ayah.Income, ayah.Phone,
			ibu.Name, ibu.NIK, derefInt(ibu.BirthYear), ibu.Education, ibu.Occupation, ibu.Income, ibu.Phone,
			wali.Name, wali.NIK, derefInt(wali.BirthYear), wali.Education, wali.Occupation, wali.Income, wali.Phone, wali.Relationship,
			className,
			st.Status,
			display,
		}
		cell, _ := excelize.CoordinatesToCellName(1, rowIdx+2)
		if err := sw.SetRow(cell, vals); err != nil {
			return nil, err
		}
	}
	if err := sw.Flush(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func boolToYaTidak(b bool) string {
	if b {
		return "Ya"
	}
	return "Tidak"
}
