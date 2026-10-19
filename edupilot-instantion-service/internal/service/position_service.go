package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/edupilot-instantion-service/internal/apperr"
	"github.com/edupilot-instantion-service/internal/config"
	"github.com/edupilot-instantion-service/internal/dto"
	"github.com/edupilot-instantion-service/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// PositionService manages a school's jabatan / tugas tambahan (employee_positions).
type PositionService struct {
	db *gorm.DB
}

// NewPositionService: a nil db means config.DB, resolved per call (the
// controller creates the service at package init, before ConnectDB runs).
func NewPositionService(db *gorm.DB) *PositionService {
	return &PositionService{db: db}
}

func (s *PositionService) conn(ctx context.Context) *gorm.DB {
	if s.db != nil {
		return s.db.WithContext(ctx)
	}
	return config.DB.WithContext(ctx)
}

const (
	maxPositionName     = 255
	maxPositionCategory = 100
	maxPositionCode     = 50
	// PositionCategoryOther is used for legacy rows without a category.
	PositionCategoryOther = "Lainnya"
)

// PositionCategories is the display order of the standard categories; other
// (custom) categories follow alphabetically, "Lainnya" last.
var PositionCategories = []string{"Pimpinan", "Pendidik", "Tenaga Kependidikan", "Koordinator", PositionCategoryOther}

type defaultPosition struct {
	Code, Name, Category string
}

// defaultPositions are seeded once per school (first GET with no positions).
var defaultPositions = []defaultPosition{
	{"KS", "Kepala Sekolah", "Pimpinan"},
	{"WAKA-KUR", "Wakil Kepala Sekolah Bidang Kurikulum", "Pimpinan"},
	{"WAKA-KES", "Wakil Kepala Sekolah Bidang Kesiswaan", "Pimpinan"},
	{"WAKA-SARPRAS", "Wakil Kepala Sekolah Bidang Sarana Prasarana", "Pimpinan"},
	{"WAKA-HUMAS", "Wakil Kepala Sekolah Bidang Humas", "Pimpinan"},
	{"GMP", "Guru Mata Pelajaran", "Pendidik"},
	{"GK", "Guru Kelas", "Pendidik"},
	{"WALAS", "Wali Kelas", "Pendidik"},
	{"GBK", "Guru Bimbingan Konseling (BK)", "Pendidik"},
	{"KTU", "Kepala Tata Usaha", "Tenaga Kependidikan"},
	{"STU", "Staf Tata Usaha", "Tenaga Kependidikan"},
	{"OPS", "Operator Sekolah", "Tenaga Kependidikan"},
	{"BEND", "Bendahara", "Tenaga Kependidikan"},
	{"PUST", "Pustakawan", "Tenaga Kependidikan"},
	{"LABORAN", "Laboran", "Tenaga Kependidikan"},
	{"KAPERPUS", "Kepala Perpustakaan", "Koordinator"},
	{"KALAB", "Kepala Laboratorium", "Koordinator"},
	{"PEMB-OSIS", "Pembina OSIS", "Koordinator"},
	{"PEMB-EKSKUL", "Pembina Ekstrakurikuler", "Koordinator"},
	{"KOORD-KUR", "Koordinator Kurikulum", "Koordinator"},
}

var (
	errPositionNotFound  = apperr.NotFound("Jabatan tidak ditemukan")
	errPositionNameTaken = apperr.Conflict("DUPLICATE_NAME", "Nama jabatan sudah ada di sekolah ini")
	errPositionCodeTaken = apperr.Conflict("DUPLICATE_CODE", "Kode jabatan sudah digunakan di sekolah ini")
)

func categoryRank(cat string) int {
	for i, c := range PositionCategories {
		if strings.EqualFold(c, cat) {
			if c == PositionCategoryOther {
				return 1000
			}
			return i
		}
	}
	return 100
}

// sortPositions orders by category (standard order, then custom A-Z,
// "Lainnya" last) and then by name.
func sortPositions(list []dto.EmployeePositionResponse) {
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		ra, rb := categoryRank(a.Category), categoryRank(b.Category)
		if ra != rb {
			return ra < rb
		}
		if ca, cb := strings.ToLower(a.Category), strings.ToLower(b.Category); ca != cb {
			return ca < cb
		}
		return strings.ToLower(a.Position) < strings.ToLower(b.Position)
	})
}

// canonicalCategory maps a standard category case-insensitively to its
// canonical spelling; custom categories are kept as typed.
func canonicalCategory(cat string) string {
	for _, c := range PositionCategories {
		if strings.EqualFold(c, cat) {
			return c
		}
	}
	return cat
}

// GeneratePositionCode derives a short code from a position name: the
// initials of its words ("Kepala Tata Usaha" -> "KTU"), or the first letters
// of a single word ("Bendahara" -> "BEND"). taken (upper-case codes) is
// avoided by appending -2, -3, ...
func GeneratePositionCode(name string, taken map[string]bool) string {
	words := strings.FieldsFunc(name, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	var b strings.Builder
	switch len(words) {
	case 0:
		b.WriteString("JAB")
	case 1:
		for i, r := range []rune(words[0]) {
			if i >= 4 {
				break
			}
			b.WriteRune(unicode.ToUpper(r))
		}
	default:
		for _, w := range words {
			b.WriteRune(unicode.ToUpper([]rune(w)[0]))
		}
	}
	base := b.String()
	if len(base) > 20 {
		base = base[:20]
	}
	code := base
	for n := 2; taken[code]; n++ {
		code = fmt.Sprintf("%s-%d", base, n)
	}
	return code
}

func normalizePositionRequest(req *dto.EmployeePositionRequest) error {
	fe := fieldErrors{}
	req.Position = strings.Join(strings.Fields(req.Position), " ")
	req.Category = canonicalCategory(strings.Join(strings.Fields(req.Category), " "))
	req.PositionCode = strings.ToUpper(strings.TrimSpace(req.PositionCode))
	if req.Position == "" {
		fe.add("position", "Nama jabatan wajib diisi")
	}
	if req.Category == "" {
		fe.add("category", "Kategori jabatan wajib diisi")
	}
	checkLen(fe, "position", req.Position, maxPositionName)
	checkLen(fe, "category", req.Category, maxPositionCategory)
	checkLen(fe, "position_code", req.PositionCode, maxPositionCode)
	return fe.err()
}

func positionResponse(p model.EmployeePosition, count int64) dto.EmployeePositionResponse {
	cat := strings.TrimSpace(p.Category)
	if cat == "" {
		cat = PositionCategoryOther
	}
	return dto.EmployeePositionResponse{
		ID: p.ID, PositionCode: p.PositionCode, Position: p.Position, Category: cat,
		AssignmentCount: count, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}
}

func lockPositions(tx *gorm.DB, schoolID uuid.UUID) error {
	return tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", "employee_positions:"+schoolID.String()).Error
}

// assignmentsOfLiveTeachers scopes employee_assignments to non-deleted employees.
func assignmentsOfLiveTeachers(db *gorm.DB, schoolID uuid.UUID) *gorm.DB {
	return db.Table("employee_assignments").
		Where("employee_assignments.school_id = ?", schoolID).
		Where("EXISTS (SELECT 1 FROM employees e WHERE e.id = employee_assignments.employee_id AND e.deleted_at IS NULL)")
}

func (s *PositionService) seedDefaults(db *gorm.DB, schoolID uuid.UUID) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := lockPositions(tx, schoolID); err != nil {
			return err
		}
		var n int64
		if err := tx.Model(&model.EmployeePosition{}).Where("school_id = ?", schoolID).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return nil
		}
		rows := make([]model.EmployeePosition, 0, len(defaultPositions))
		for _, d := range defaultPositions {
			rows = append(rows, model.EmployeePosition{ID: uuid.New(), SchoolID: schoolID, PositionCode: d.Code, Position: d.Name, Category: d.Category})
		}
		return tx.Create(&rows).Error
	})
}

// ListPositions returns the school's positions (seeding the defaults once,
// idempotent and race-safe) with how many active teachers hold each.
func (s *PositionService) ListPositions(ctx context.Context, schoolID uuid.UUID) ([]dto.EmployeePositionResponse, error) {
	db := s.conn(ctx)
	var list []model.EmployeePosition
	if err := db.Where("school_id = ?", schoolID).Find(&list).Error; err != nil {
		return nil, err
	}
	if len(list) == 0 {
		if err := s.seedDefaults(db, schoolID); err != nil {
			return nil, err
		}
		if err := db.Where("school_id = ?", schoolID).Find(&list).Error; err != nil {
			return nil, err
		}
	}
	var counts []struct {
		EmployeePositionID uuid.UUID
		N                  int64
	}
	if err := assignmentsOfLiveTeachers(db, schoolID).
		Select("employee_position_id, COUNT(*) AS n").
		Group("employee_position_id").Scan(&counts).Error; err != nil {
		return nil, err
	}
	byID := make(map[uuid.UUID]int64, len(counts))
	for _, c := range counts {
		byID[c.EmployeePositionID] = c.N
	}
	result := make([]dto.EmployeePositionResponse, 0, len(list))
	for _, p := range list {
		result = append(result, positionResponse(p, byID[p.ID]))
	}
	sortPositions(result)
	return result, nil
}

// checkPositionUnique enforces a unique name (case-insensitive) and code.
func checkPositionUnique(tx *gorm.DB, schoolID uuid.UUID, name, code string, exclude uuid.UUID) error {
	q := tx.Model(&model.EmployeePosition{}).Where("school_id = ? AND lower(position) = lower(?)", schoolID, name)
	if exclude != uuid.Nil {
		q = q.Where("id <> ?", exclude)
	}
	var n int64
	if err := q.Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return errPositionNameTaken
	}
	if code == "" {
		return nil
	}
	q = tx.Model(&model.EmployeePosition{}).Where("school_id = ? AND upper(position_code) = ?", schoolID, code)
	if exclude != uuid.Nil {
		q = q.Where("id <> ?", exclude)
	}
	if err := q.Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return errPositionCodeTaken
	}
	return nil
}

func takenPositionCodes(tx *gorm.DB, schoolID uuid.UUID, exclude uuid.UUID) (map[string]bool, error) {
	var codes []string
	q := tx.Model(&model.EmployeePosition{}).Where("school_id = ? AND position_code IS NOT NULL", schoolID)
	if exclude != uuid.Nil {
		q = q.Where("id <> ?", exclude)
	}
	if err := q.Pluck("position_code", &codes).Error; err != nil {
		return nil, err
	}
	taken := make(map[string]bool, len(codes))
	for _, c := range codes {
		taken[strings.ToUpper(strings.TrimSpace(c))] = true
	}
	return taken, nil
}

func mapPositionWriteError(err error) error {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return errPositionNameTaken
	}
	return err
}

func (s *PositionService) CreatePosition(ctx context.Context, schoolID uuid.UUID, req dto.EmployeePositionRequest) (*dto.EmployeePositionResponse, error) {
	if err := normalizePositionRequest(&req); err != nil {
		return nil, err
	}
	var pos model.EmployeePosition
	err := s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockPositions(tx, schoolID); err != nil {
			return err
		}
		if err := checkPositionUnique(tx, schoolID, req.Position, req.PositionCode, uuid.Nil); err != nil {
			return err
		}
		code := req.PositionCode
		if code == "" {
			taken, err := takenPositionCodes(tx, schoolID, uuid.Nil)
			if err != nil {
				return err
			}
			code = GeneratePositionCode(req.Position, taken)
		}
		pos = model.EmployeePosition{ID: uuid.New(), SchoolID: schoolID, PositionCode: code, Position: req.Position, Category: req.Category}
		return mapPositionWriteError(tx.Create(&pos).Error)
	})
	if err != nil {
		return nil, err
	}
	r := positionResponse(pos, 0)
	return &r, nil
}

func (s *PositionService) UpdatePosition(ctx context.Context, schoolID, id uuid.UUID, req dto.EmployeePositionRequest) (*dto.EmployeePositionResponse, error) {
	if err := normalizePositionRequest(&req); err != nil {
		return nil, err
	}
	var pos model.EmployeePosition
	var count int64
	err := s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockPositions(tx, schoolID); err != nil {
			return err
		}
		if err := tx.Where("school_id = ? AND id = ?", schoolID, id).First(&pos).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errPositionNotFound
			}
			return err
		}
		if err := checkPositionUnique(tx, schoolID, req.Position, req.PositionCode, id); err != nil {
			return err
		}
		code := req.PositionCode
		if code == "" {
			code = strings.TrimSpace(pos.PositionCode)
		}
		if code == "" {
			taken, err := takenPositionCodes(tx, schoolID, id)
			if err != nil {
				return err
			}
			code = GeneratePositionCode(req.Position, taken)
		}
		pos.PositionCode, pos.Position, pos.Category = code, req.Position, req.Category
		if err := tx.Save(&pos).Error; err != nil {
			return mapPositionWriteError(err)
		}
		return assignmentsOfLiveTeachers(tx, schoolID).Where("employee_position_id = ?", id).Count(&count).Error
	})
	if err != nil {
		return nil, err
	}
	r := positionResponse(pos, count)
	return &r, nil
}

// DeletePosition removes a position; 409 IN_USE while a (non-deleted)
// teacher still holds it. Leftover rows of deleted teachers are removed.
func (s *PositionService) DeletePosition(ctx context.Context, schoolID, id uuid.UUID) error {
	return s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		var pos model.EmployeePosition
		if err := tx.Where("school_id = ? AND id = ?", schoolID, id).First(&pos).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errPositionNotFound
			}
			return err
		}
		var inUse int64
		if err := assignmentsOfLiveTeachers(tx, schoolID).Where("employee_position_id = ?", id).Count(&inUse).Error; err != nil {
			return err
		}
		if inUse > 0 {
			return apperr.Conflict("IN_USE", fmt.Sprintf("Jabatan \"%s\" tidak dapat dihapus karena masih dipegang oleh %d guru/pegawai", pos.Position, inUse))
		}
		if err := tx.Where("school_id = ? AND employee_position_id = ?", schoolID, id).Delete(&model.EmployeeAssignments{}).Error; err != nil {
			return err
		}
		return tx.Delete(&pos).Error
	})
}
