package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/edupilot-instantion-service/internal/apperr"
	"github.com/edupilot-instantion-service/internal/config"
	"github.com/edupilot-instantion-service/internal/dto"
	"github.com/edupilot-instantion-service/internal/model"
	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"
	"gorm.io/gorm"
)

const (
	// MaxImportFileSize is the maximum accepted .xlsx size.
	MaxImportFileSize = 5 << 20
	// MaxImportRows is the maximum number of non-empty data rows per import.
	MaxImportRows = 2000

	importUnzipSizeLimit    = 64 << 20
	importUnzipXMLSizeLimit = 32 << 20
)

// ImportColumns is the header row expected in row 1 (API_CONTRACT.md §6).
var ImportColumns = []string{
	"full_name", "employee_code", "email", "phone",
	"number_type", "number", "employee_status", "join_date",
}

var requiredImportColumns = []string{"full_name", "employee_code"}

// importRow is a validated row ready to be inserted.
type importRow struct {
	Row            int
	FullName       string
	EmployeeCode   string
	Email          *string
	Phone          *string
	NumberType     string
	Number         string
	EmployeeStatus string
	JoinDate       string
}

func normalizeHeader(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	h = strings.TrimSuffix(h, "*")
	h = strings.TrimSpace(h)
	h = strings.ReplaceAll(h, " ", "_")
	return h
}

// mapHeader returns column-name -> index, or an error if required columns are missing.
func mapHeader(header []string) (map[string]int, error) {
	idx := map[string]int{}
	known := map[string]bool{}
	for _, c := range ImportColumns {
		known[c] = true
	}
	for i, h := range header {
		n := normalizeHeader(h)
		if known[n] {
			if _, dup := idx[n]; !dup {
				idx[n] = i
			}
		}
	}
	var missing []string
	for _, c := range requiredImportColumns {
		if _, ok := idx[c]; !ok {
			missing = append(missing, c)
		}
	}
	if len(missing) > 0 {
		return nil, apperr.New(http.StatusBadRequest, "INVALID_TEMPLATE",
			fmt.Sprintf("Header baris 1 tidak sesuai template, kolom wajib tidak ada: %s. Kolom yang diharapkan: %s",
				strings.Join(missing, ", "), strings.Join(ImportColumns, ", ")))
	}
	return idx, nil
}

func isBlankRow(row []string) bool {
	for _, c := range row {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}

// normalizeImportDate accepts YYYY-MM-DD, DD/MM/YYYY, DD-MM-YYYY, YYYY/MM/DD or
// an Excel serial date number and returns YYYY-MM-DD.
func normalizeImportDate(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", true
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		if f < 1 || f > 2958465 { // 1900-01-01 .. 9999-12-31
			return "", false
		}
		t, err := excelize.ExcelDateToTime(f, false)
		if err != nil {
			return "", false
		}
		return t.Format(dto.DateLayout), true
	}
	for _, layout := range []string{"2006-01-02", "02/01/2006", "2/1/2006", "02-01-2006", "2-1-2006", "2006/01/02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format(dto.DateLayout), true
		}
	}
	return "", false
}

// parseImportRows validates all data rows. rows[0] is the header.
// It returns the valid rows, per-row errors and the number of non-empty data rows.
func parseImportRows(rows [][]string) ([]importRow, []dto.ImportRowError, int, error) {
	if len(rows) == 0 {
		return nil, nil, 0, apperr.BadRequest("File kosong")
	}
	idx, err := mapHeader(rows[0])
	if err != nil {
		return nil, nil, 0, err
	}

	cell := func(row []string, col string) string {
		i, ok := idx[col]
		if !ok || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}

	var (
		valid  []importRow
		errs   []dto.ImportRowError
		total  int
		seenAt = map[string]int{}
	)
	for i := 1; i < len(rows); i++ {
		row := rows[i]
		rowNum := i + 1
		if isBlankRow(row) {
			continue
		}
		total++
		if total > MaxImportRows {
			return nil, nil, 0, apperr.BadRequest(fmt.Sprintf("Maksimal %d baris data per import", MaxImportRows))
		}

		r := importRow{
			Row:            rowNum,
			FullName:       cell(row, "full_name"),
			EmployeeCode:   cell(row, "employee_code"),
			NumberType:     cell(row, "number_type"),
			Number:         cell(row, "number"),
			EmployeeStatus: cell(row, "employee_status"),
		}
		var msgs []string
		if r.FullName == "" {
			msgs = append(msgs, "full_name wajib diisi")
		} else if len(r.FullName) > 255 {
			msgs = append(msgs, "full_name maksimal 255 karakter")
		}
		if r.EmployeeCode == "" {
			msgs = append(msgs, "employee_code wajib diisi")
		} else if len(r.EmployeeCode) > 255 {
			msgs = append(msgs, "employee_code maksimal 255 karakter")
		} else if first, dup := seenAt[r.EmployeeCode]; dup {
			msgs = append(msgs, fmt.Sprintf("employee_code duplikat dengan baris %d", first))
		} else {
			seenAt[r.EmployeeCode] = rowNum
		}
		if email := cell(row, "email"); email != "" {
			if !dto.ValidEmail(email) || len(email) > 255 {
				msgs = append(msgs, "email tidak valid")
			} else {
				r.Email = &email
			}
		}
		if phone := cell(row, "phone"); phone != "" {
			if len(phone) > 50 {
				msgs = append(msgs, "phone maksimal 50 karakter")
			} else {
				r.Phone = &phone
			}
		}
		for _, f := range []struct{ name, val string }{
			{"number_type", r.NumberType}, {"number", r.Number}, {"employee_status", r.EmployeeStatus},
		} {
			if len(f.val) > 255 {
				msgs = append(msgs, f.name+" maksimal 255 karakter")
			}
		}
		if jd, ok := normalizeImportDate(cell(row, "join_date")); ok {
			r.JoinDate = jd
		} else {
			msgs = append(msgs, "join_date harus berformat YYYY-MM-DD")
		}

		if len(msgs) > 0 {
			errs = append(errs, dto.ImportRowError{Row: rowNum, Message: strings.Join(msgs, "; ")})
			continue
		}
		valid = append(valid, r)
	}
	return valid, errs, total, nil
}

// readImportFile validates and opens the uploaded workbook, returning the first
// sheet's rows (bounded).
func readImportFile(filename string, size int64, src io.Reader) ([][]string, error) {
	if !strings.EqualFold(filepath.Ext(filename), ".xlsx") {
		return nil, apperr.New(http.StatusBadRequest, "INVALID_FILE_TYPE", "File harus berformat .xlsx")
	}
	if size > MaxImportFileSize {
		return nil, apperr.New(http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "Ukuran file maksimal 5 MB")
	}
	buf, err := io.ReadAll(io.LimitReader(src, MaxImportFileSize+1))
	if err != nil {
		return nil, apperr.BadRequest("Gagal membaca file")
	}
	if len(buf) > MaxImportFileSize {
		return nil, apperr.New(http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "Ukuran file maksimal 5 MB")
	}
	if !bytes.HasPrefix(buf, []byte("PK\x03\x04")) {
		return nil, apperr.New(http.StatusBadRequest, "INVALID_FILE_TYPE", "File bukan .xlsx yang valid")
	}

	f, err := excelize.OpenReader(bytes.NewReader(buf), excelize.Options{
		UnzipSizeLimit:    importUnzipSizeLimit,
		UnzipXMLSizeLimit: importUnzipXMLSizeLimit,
		RawCellValue:      true,
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
		rows = append(rows, cols)
		if len(rows) > 1 && !isBlankRow(cols) {
			nonEmpty++
			if nonEmpty > MaxImportRows {
				return nil, apperr.BadRequest(fmt.Sprintf("Maksimal %d baris data per import", MaxImportRows))
			}
		}
	}
	if err := it.Error(); err != nil {
		return nil, apperr.New(http.StatusBadRequest, "INVALID_FILE", "Sheet pertama tidak dapat dibaca")
	}
	return rows, nil
}

// ImportTeachers validates an .xlsx file and (unless dryRun) inserts all valid
// rows in a single transaction. Rows with errors are reported, not inserted.
func ImportTeachers(ctx context.Context, schoolID uuid.UUID, filename string, size int64, src io.Reader, dryRun bool) (*dto.ImportResult, error) {
	rows, err := readImportFile(filename, size, src)
	if err != nil {
		return nil, err
	}
	valid, rowErrs, total, err := parseImportRows(rows)
	if err != nil {
		return nil, err
	}
	if total == 0 {
		return nil, apperr.BadRequest("Tidak ada baris data pada file")
	}

	db := config.DB.WithContext(ctx)

	// Duplicates against existing (non-deleted) employees of this school.
	if len(valid) > 0 {
		codes := make([]string, len(valid))
		for i, r := range valid {
			codes[i] = r.EmployeeCode
		}
		existing := map[string]bool{}
		for start := 0; start < len(codes); start += 500 {
			end := min(start+500, len(codes))
			var found []string
			if err := db.Model(&model.Employees{}).
				Where("school_id = ? AND employee_code IN ?", schoolID, codes[start:end]).
				Pluck("employee_code", &found).Error; err != nil {
				return nil, err
			}
			for _, c := range found {
				existing[c] = true
			}
		}
		if len(existing) > 0 {
			kept := valid[:0]
			for _, r := range valid {
				if existing[r.EmployeeCode] {
					rowErrs = append(rowErrs, dto.ImportRowError{Row: r.Row, Message: "employee_code sudah terdaftar di sekolah ini"})
					continue
				}
				kept = append(kept, r)
			}
			valid = kept
		}
	}
	sort.Slice(rowErrs, func(i, j int) bool { return rowErrs[i].Row < rowErrs[j].Row })

	result := &dto.ImportResult{
		Total:   total,
		Created: len(valid),
		Failed:  len(rowErrs),
		DryRun:  dryRun,
		Errors:  rowErrs,
	}
	if result.Errors == nil {
		result.Errors = []dto.ImportRowError{}
	}
	if dryRun || len(valid) == 0 {
		return result, nil
	}

	employees := make([]model.Employees, len(valid))
	for i, r := range valid {
		employees[i] = model.Employees{
			ID:             uuid.New(),
			SchoolID:       schoolID,
			FullName:       r.FullName,
			EmployeeCode:   r.EmployeeCode,
			Email:          r.Email,
			Phone:          r.Phone,
			NumberType:     r.NumberType,
			Number:         r.Number,
			EmployeeType:   model.EmployeeTypeTeacher,
			EmployeeStatus: r.EmployeeStatus,
			JoinDate:       r.JoinDate,
		}
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		return tx.CreateInBatches(&employees, 200).Error
	})
	if err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, apperr.Conflict("EMPLOYEE_CODE_EXISTS", "Sebagian kode pegawai baru saja digunakan, silakan ulangi import")
		}
		return nil, err
	}
	return result, nil
}

// BuildImportTemplate returns an .xlsx containing only the import header row.
func BuildImportTemplate() (*bytes.Buffer, error) {
	f := excelize.NewFile()
	defer f.Close()

	sheet := "Guru"
	if err := f.SetSheetName(f.GetSheetName(0), sheet); err != nil {
		return nil, err
	}
	header := make([]interface{}, len(ImportColumns))
	for i, c := range ImportColumns {
		header[i] = c
	}
	if err := f.SetSheetRow(sheet, "A1", &header); err != nil {
		return nil, err
	}

	// Text format for code/number/phone-like columns so leading zeros survive.
	textStyle, err := f.NewStyle(&excelize.Style{NumFmt: 49})
	if err != nil {
		return nil, err
	}
	lastCol, _ := excelize.ColumnNumberToName(len(ImportColumns))
	if err := f.SetColStyle(sheet, "A:"+lastCol, textStyle); err != nil {
		return nil, err
	}
	if err := f.SetColWidth(sheet, "A", lastCol, 20); err != nil {
		return nil, err
	}
	boldStyle, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}, NumFmt: 49})
	if err != nil {
		return nil, err
	}
	if err := f.SetCellStyle(sheet, "A1", lastCol+"1", boldStyle); err != nil {
		return nil, err
	}
	return f.WriteToBuffer()
}
