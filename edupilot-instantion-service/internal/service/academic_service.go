package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/edupilot-instantion-service/internal/apperr"
	"github.com/edupilot-instantion-service/internal/config"
	"github.com/edupilot-instantion-service/internal/dto"
	"github.com/edupilot-instantion-service/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	maxBulkClasses   = 50
	maxClassCapacity = 200
	maxAllClasses    = 1000
)

type AcademicService struct {
	db *gorm.DB
}

func NewAcademicService(db *gorm.DB) *AcademicService {
	return &AcademicService{db: db}
}

// conn resolves the DB lazily (services are constructed at package init,
// before config.ConnectDB).
func (s *AcademicService) conn(ctx context.Context) *gorm.DB {
	if s.db != nil {
		return s.db.WithContext(ctx)
	}
	return config.DB.WithContext(ctx)
}

func strPtr(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func notFoundOr(err error, msg string) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return apperr.NotFound(msg)
	}
	return err
}

// ==================== References ====================

func (s *AcademicService) GetReferences(ctx context.Context, schoolID uuid.UUID) (map[string][]dto.Option, error) {
	refs := dto.References()
	docTypes, err := s.GetOrSeedDocumentTypes(ctx, schoolID)
	if err != nil {
		return nil, err
	}
	docOptions := make([]dto.Option, 0, len(docTypes))
	for _, dt := range docTypes {
		docOptions = append(docOptions, dto.Option{Value: dt.ID.String(), Label: dt.Name})
	}
	refs[dto.RefDocumentTypes] = docOptions
	return refs, nil
}

// ==================== Academic Years & Semesters ====================

func (s *AcademicService) yearResponses(ctx context.Context, schoolID uuid.UUID, years []model.AcademicYear) ([]dto.AcademicYearResponse, error) {
	result := make([]dto.AcademicYearResponse, 0, len(years))
	if len(years) == 0 {
		return result, nil
	}
	db := s.conn(ctx)
	ids := make([]uuid.UUID, 0, len(years))
	for _, y := range years {
		ids = append(ids, y.ID)
	}

	var semesters []model.Semester
	if err := db.Where("academic_year_id IN ?", ids).Order("number ASC").Find(&semesters).Error; err != nil {
		return nil, err
	}
	semByYear := map[uuid.UUID][]dto.SemesterResponse{}
	for _, sm := range semesters {
		semByYear[sm.AcademicYearID] = append(semByYear[sm.AcademicYearID], dto.SemesterResponse{
			ID: sm.ID, Number: sm.Number, Name: sm.Name,
			StartDate: strPtr(sm.StartDate), EndDate: strPtr(sm.EndDate), IsActive: sm.IsActive,
		})
	}

	type countRow struct {
		YearID uuid.UUID
		N      int
	}
	var classCounts, studentCounts []countRow
	if err := db.Raw(`SELECT academic_year_id AS year_id, COUNT(*) AS n FROM classes
		WHERE school_id = ? AND academic_year_id IN ? AND deleted_at IS NULL GROUP BY academic_year_id`, schoolID, ids).
		Scan(&classCounts).Error; err != nil {
		return nil, err
	}
	if err := db.Raw(`SELECT h.academic_year_id AS year_id, COUNT(*) AS n FROM student_class_histories h
		JOIN students st ON st.id = h.student_id AND st.deleted_at IS NULL
		WHERE h.school_id = ? AND h.academic_year_id IN ? AND h.status = 'active' GROUP BY h.academic_year_id`, schoolID, ids).
		Scan(&studentCounts).Error; err != nil {
		return nil, err
	}
	cc, sc := map[uuid.UUID]int{}, map[uuid.UUID]int{}
	for _, r := range classCounts {
		cc[r.YearID] = r.N
	}
	for _, r := range studentCounts {
		sc[r.YearID] = r.N
	}

	for _, y := range years {
		sems := semByYear[y.ID]
		if sems == nil {
			sems = []dto.SemesterResponse{}
		}
		result = append(result, dto.AcademicYearResponse{
			ID: y.ID, Name: y.Name, StartDate: strPtr(y.StartDate), EndDate: strPtr(y.EndDate), IsActive: y.IsActive,
			Semesters: sems, ClassCount: cc[y.ID], StudentCount: sc[y.ID],
		})
	}
	return result, nil
}

func (s *AcademicService) GetAcademicYears(ctx context.Context, schoolID uuid.UUID) ([]dto.AcademicYearResponse, error) {
	var years []model.AcademicYear
	if err := s.conn(ctx).Where("school_id = ?", schoolID).
		Order("academic_name DESC, created_at DESC").Find(&years).Error; err != nil {
		return nil, err
	}
	return s.yearResponses(ctx, schoolID, years)
}

func (s *AcademicService) getAcademicYear(ctx context.Context, schoolID, id uuid.UUID) (*dto.AcademicYearResponse, error) {
	var y model.AcademicYear
	if err := s.conn(ctx).Where("school_id = ? AND id = ?", schoolID, id).First(&y).Error; err != nil {
		return nil, notFoundOr(err, "Tahun ajaran tidak ditemukan")
	}
	list, err := s.yearResponses(ctx, schoolID, []model.AcademicYear{y})
	if err != nil {
		return nil, err
	}
	return &list[0], nil
}

// validateYearFields normalises name and dates.
func validateYearFields(name string, start, end *string) (string, string, string, error) {
	fe := fieldErrors{}
	name = strings.TrimSpace(name)
	if name == "" {
		fe.add("name", "Nama tahun ajaran wajib diisi")
	}
	checkLen(fe, "name", name, 50)
	st, en := "", ""
	if start != nil {
		st = *start
	}
	if end != nil {
		en = *end
	}
	checkDate(fe, "start_date", &st, false)
	checkDate(fe, "end_date", &en, false)
	if _, bad := fe["start_date"]; !bad && st != "" && en != "" && en <= st {
		if _, bad2 := fe["end_date"]; !bad2 {
			fe.add("end_date", "Tanggal selesai harus setelah tanggal mulai")
		}
	}
	return name, st, en, fe.err()
}

func yearNameTaken(tx *gorm.DB, schoolID uuid.UUID, name string, exclude uuid.UUID) (bool, error) {
	var n int64
	q := tx.Model(&model.AcademicYear{}).Where("school_id = ? AND lower(academic_name) = lower(?)", schoolID, name)
	if exclude != uuid.Nil {
		q = q.Where("id <> ?", exclude)
	}
	err := q.Count(&n).Error
	return n > 0, err
}

var errYearExists = apperr.Conflict("YEAR_EXISTS", "Tahun ajaran dengan nama tersebut sudah ada")

func (s *AcademicService) CreateAcademicYear(ctx context.Context, schoolID uuid.UUID, req dto.AcademicYearRequest) (*dto.AcademicYearResponse, error) {
	name, start, end, err := validateYearFields(req.Name, req.StartDate, req.EndDate)
	if err != nil {
		return nil, err
	}
	year := model.AcademicYear{SchoolID: schoolID, Name: name, StartDate: start, EndDate: end}

	err = s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		// Serialise year creation per school (first-year auto activation).
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", "academic_years:"+schoolID.String()).Error; err != nil {
			return err
		}
		taken, err := yearNameTaken(tx, schoolID, name, uuid.Nil)
		if err != nil {
			return err
		}
		if taken {
			return errYearExists
		}
		var active int64
		if err := tx.Model(&model.AcademicYear{}).Where("school_id = ? AND is_active = true", schoolID).Count(&active).Error; err != nil {
			return err
		}
		year.IsActive = active == 0 // first (or only) year becomes active
		if err := tx.Create(&year).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return errYearExists
			}
			return err
		}
		// Auto-create semester Ganjil (1) & Genap (2).
		for _, sm := range []model.Semester{
			{AcademicYearID: year.ID, SchoolID: &schoolID, Name: "Ganjil", Number: 1, IsActive: year.IsActive},
			{AcademicYearID: year.ID, SchoolID: &schoolID, Name: "Genap", Number: 2},
		} {
			sm := sm
			if err := tx.Create(&sm).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.getAcademicYear(ctx, schoolID, year.ID)
}

func (s *AcademicService) UpdateAcademicYear(ctx context.Context, schoolID, id uuid.UUID, req dto.AcademicYearRequest) (*dto.AcademicYearResponse, error) {
	var year model.AcademicYear
	if err := s.conn(ctx).Where("school_id = ? AND id = ?", schoolID, id).First(&year).Error; err != nil {
		return nil, notFoundOr(err, "Tahun ajaran tidak ditemukan")
	}
	start, end := year.StartDate, year.EndDate
	if req.StartDate != nil {
		start = *req.StartDate
	}
	if req.EndDate != nil {
		end = *req.EndDate
	}
	name, start, end, err := validateYearFields(req.Name, &start, &end)
	if err != nil {
		return nil, err
	}
	taken, err := yearNameTaken(s.conn(ctx), schoolID, name, id)
	if err != nil {
		return nil, err
	}
	if taken {
		return nil, errYearExists
	}
	if err := s.conn(ctx).Model(&year).Updates(map[string]interface{}{
		"academic_name": name, "start_date": start, "end_date": end,
	}).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, errYearExists
		}
		return nil, err
	}
	return s.getAcademicYear(ctx, schoolID, id)
}

// schoolYearIDs is a subquery of the tenant's academic year ids.
func schoolYearIDs(tx *gorm.DB, schoolID uuid.UUID) *gorm.DB {
	return tx.Model(&model.AcademicYear{}).Select("id").Where("school_id = ?", schoolID)
}

// activateYearTx makes year the single active year of the school and keeps a
// single active semester (the given one, or the year's current/first one).
func activateYearTx(tx *gorm.DB, schoolID, yearID uuid.UUID, semesterID *uuid.UUID) error {
	if err := tx.Model(&model.AcademicYear{}).Where("school_id = ? AND id <> ? AND is_active = true", schoolID, yearID).
		Update("is_active", false).Error; err != nil {
		return err
	}
	if err := tx.Model(&model.AcademicYear{}).Where("school_id = ? AND id = ?", schoolID, yearID).
		Update("is_active", true).Error; err != nil {
		return err
	}
	if err := tx.Model(&model.Semester{}).Where("academic_year_id IN (?) AND academic_year_id <> ?", schoolYearIDs(tx, schoolID), yearID).
		Update("is_active", false).Error; err != nil {
		return err
	}
	if semesterID != nil {
		if err := tx.Model(&model.Semester{}).Where("academic_year_id = ? AND id <> ?", yearID, *semesterID).
			Update("is_active", false).Error; err != nil {
			return err
		}
		return tx.Model(&model.Semester{}).Where("id = ?", *semesterID).Update("is_active", true).Error
	}
	var active int64
	if err := tx.Model(&model.Semester{}).Where("academic_year_id = ? AND is_active = true", yearID).Count(&active).Error; err != nil {
		return err
	}
	if active == 0 {
		var first model.Semester
		if err := tx.Where("academic_year_id = ?", yearID).Order("number ASC").First(&first).Error; err == nil {
			return tx.Model(&first).Update("is_active", true).Error
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}
	return nil
}

func (s *AcademicService) ActivateAcademicYear(ctx context.Context, schoolID, id uuid.UUID) error {
	return s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		var year model.AcademicYear
		if err := tx.Where("school_id = ? AND id = ?", schoolID, id).First(&year).Error; err != nil {
			return notFoundOr(err, "Tahun ajaran tidak ditemukan")
		}
		return activateYearTx(tx, schoolID, id, nil)
	})
}

func (s *AcademicService) ActivateSemester(ctx context.Context, schoolID, semesterID uuid.UUID) error {
	return s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		var sem model.Semester
		if err := tx.Where("id = ? AND academic_year_id IN (?)", semesterID, schoolYearIDs(tx, schoolID)).First(&sem).Error; err != nil {
			return notFoundOr(err, "Semester tidak ditemukan")
		}
		return activateYearTx(tx, schoolID, sem.AcademicYearID, &sem.ID)
	})
}

func (s *AcademicService) DeleteAcademicYear(ctx context.Context, schoolID, id uuid.UUID) error {
	return s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		var year model.AcademicYear
		if err := tx.Where("school_id = ? AND id = ?", schoolID, id).First(&year).Error; err != nil {
			return notFoundOr(err, "Tahun ajaran tidak ditemukan")
		}
		var classCount, histCount int64
		if err := tx.Model(&model.Classes{}).Where("school_id = ? AND academic_year_id = ?", schoolID, id).Count(&classCount).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.StudentClassHistory{}).Where("school_id = ? AND academic_year_id = ?", schoolID, id).Count(&histCount).Error; err != nil {
			return err
		}
		if classCount > 0 || histCount > 0 {
			return apperr.Conflict("IN_USE", "Tahun ajaran tidak dapat dihapus karena masih memiliki kelas atau riwayat siswa")
		}
		if year.IsActive {
			var others int64
			if err := tx.Model(&model.AcademicYear{}).Where("school_id = ? AND id <> ?", schoolID, id).Count(&others).Error; err != nil {
				return err
			}
			if others > 0 {
				return apperr.Conflict("ACTIVE_YEAR", "Aktifkan tahun ajaran lain sebelum menghapus tahun ajaran aktif")
			}
		}
		if err := tx.Where("academic_year_id = ?", id).Delete(&model.Semester{}).Error; err != nil {
			return err
		}
		return tx.Where("school_id = ? AND id = ?", schoolID, id).Delete(&model.AcademicYear{}).Error
	})
}

// ==================== Grade Levels ====================

func gradeResponse(l model.GradeLevel, classCount int) dto.GradeLevelResponse {
	return dto.GradeLevelResponse{
		ID: l.ID, EducationStage: l.EducationStage, Number: l.Number, Name: l.Name, ShortName: l.ShortName,
		IsFinal: l.IsFinal, SortOrder: l.SortOrder, ClassCount: classCount,
	}
}

// gradeClassCounts counts non-deleted classes per grade level (all years).
func gradeClassCounts(db *gorm.DB, schoolID uuid.UUID) (map[uuid.UUID]int, error) {
	var rows []struct {
		GradeLevelID uuid.UUID
		N            int
	}
	if err := db.Raw(`SELECT grade_level_id, COUNT(*) AS n FROM classes
		WHERE school_id = ? AND grade_level_id IS NOT NULL AND deleted_at IS NULL GROUP BY grade_level_id`, schoolID).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]int, len(rows))
	for _, r := range rows {
		out[r.GradeLevelID] = r.N
	}
	return out, nil
}

func (s *AcademicService) GetGradeLevels(ctx context.Context, schoolID uuid.UUID) ([]dto.GradeLevelResponse, error) {
	db := s.conn(ctx)
	var levels []model.GradeLevel
	if err := db.Where("school_id = ?", schoolID).Order("sort_order ASC, number ASC").Find(&levels).Error; err != nil {
		return nil, err
	}
	counts, err := gradeClassCounts(db, schoolID)
	if err != nil {
		return nil, err
	}
	result := make([]dto.GradeLevelResponse, 0, len(levels))
	for _, l := range levels {
		result = append(result, gradeResponse(l, counts[l.ID]))
	}
	return result, nil
}

func validateGradeRequest(req *dto.GradeLevelRequest) error {
	fe := fieldErrors{}
	req.Name = strings.TrimSpace(req.Name)
	req.ShortName = strings.TrimSpace(req.ShortName)
	if req.Name == "" {
		fe.add("name", "Nama tingkat wajib diisi")
	}
	checkLen(fe, "name", req.Name, 100)
	checkLen(fe, "short_name", req.ShortName, 20)
	if req.Number < 1 || req.Number > 20 {
		fe.add("number", "Nomor tingkat harus antara 1 dan 20")
	}
	normEnum(fe, "education_stage", dto.RefEducationStages, &req.EducationStage)
	if req.SortOrder != nil && (*req.SortOrder < 0 || *req.SortOrder > 1000) {
		fe.add("sort_order", "Urutan harus antara 0 dan 1000")
	}
	return fe.err()
}

func errGradeNumberExists(n int) error {
	return apperr.Conflict("NUMBER_EXISTS", fmt.Sprintf("Tingkat dengan nomor %d sudah ada", n))
}

func (s *AcademicService) CreateGradeLevel(ctx context.Context, schoolID uuid.UUID, req dto.GradeLevelRequest) (*dto.GradeLevelResponse, error) {
	if err := validateGradeRequest(&req); err != nil {
		return nil, err
	}
	db := s.conn(ctx)
	var n int64
	if err := db.Model(&model.GradeLevel{}).Where("school_id = ? AND number = ?", schoolID, req.Number).Count(&n).Error; err != nil {
		return nil, err
	}
	if n > 0 {
		return nil, errGradeNumberExists(req.Number)
	}
	sortOrder := req.Number
	if req.SortOrder != nil {
		sortOrder = *req.SortOrder
	}
	shortName := req.ShortName
	if shortName == "" {
		shortName = strconv.Itoa(req.Number)
	}
	lvl := model.GradeLevel{
		SchoolID: schoolID, EducationStage: req.EducationStage, Number: req.Number, Name: req.Name,
		ShortName: shortName, IsFinal: req.IsFinal, SortOrder: sortOrder,
	}
	if err := db.Create(&lvl).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, errGradeNumberExists(req.Number)
		}
		return nil, err
	}
	r := gradeResponse(lvl, 0)
	return &r, nil
}

func (s *AcademicService) UpdateGradeLevel(ctx context.Context, schoolID, id uuid.UUID, req dto.GradeLevelRequest) (*dto.GradeLevelResponse, error) {
	if err := validateGradeRequest(&req); err != nil {
		return nil, err
	}
	db := s.conn(ctx)
	var lvl model.GradeLevel
	if err := db.Where("school_id = ? AND id = ?", schoolID, id).First(&lvl).Error; err != nil {
		return nil, notFoundOr(err, "Tingkat tidak ditemukan")
	}
	if req.Number != lvl.Number {
		var n int64
		if err := db.Model(&model.GradeLevel{}).Where("school_id = ? AND number = ? AND id <> ?", schoolID, req.Number, id).Count(&n).Error; err != nil {
			return nil, err
		}
		if n > 0 {
			return nil, errGradeNumberExists(req.Number)
		}
		lvl.Number = req.Number
	}
	lvl.Name = req.Name
	if req.EducationStage != "" {
		lvl.EducationStage = req.EducationStage
	}
	if req.ShortName != "" {
		lvl.ShortName = req.ShortName
	}
	lvl.IsFinal = req.IsFinal
	if req.SortOrder != nil {
		lvl.SortOrder = *req.SortOrder
	}
	if err := db.Save(&lvl).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, errGradeNumberExists(req.Number)
		}
		return nil, err
	}
	counts, err := gradeClassCounts(db, schoolID)
	if err != nil {
		return nil, err
	}
	r := gradeResponse(lvl, counts[lvl.ID])
	return &r, nil
}

func (s *AcademicService) DeleteGradeLevel(ctx context.Context, schoolID, id uuid.UUID) error {
	return s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		var lvl model.GradeLevel
		if err := tx.Where("school_id = ? AND id = ?", schoolID, id).First(&lvl).Error; err != nil {
			return notFoundOr(err, "Tingkat tidak ditemukan")
		}
		var classCount, histCount int64
		if err := tx.Model(&model.Classes{}).Where("school_id = ? AND grade_level_id = ?", schoolID, id).Count(&classCount).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.StudentClassHistory{}).Where("school_id = ? AND grade_level_id = ?", schoolID, id).Count(&histCount).Error; err != nil {
			return err
		}
		if classCount > 0 || histCount > 0 {
			return apperr.Conflict("IN_USE", "Tingkat tidak dapat dihapus karena masih digunakan oleh kelas")
		}
		if err := tx.Model(&model.Students{}).Where("school_id = ? AND entry_grade_level_id = ?", schoolID, id).
			Update("entry_grade_level_id", nil).Error; err != nil {
			return err
		}
		return tx.Delete(&lvl).Error
	})
}

// toRoman converts 1..39 to roman numerals (others as arabic).
func toRoman(n int) string {
	if n < 1 || n > 39 {
		return strconv.Itoa(n)
	}
	var b strings.Builder
	for _, p := range []struct {
		v int
		s string
	}{{10, "X"}, {9, "IX"}, {5, "V"}, {4, "IV"}, {1, "I"}} {
		for n >= p.v {
			b.WriteString(p.s)
			n -= p.v
		}
	}
	return b.String()
}

// GradePresetLevel is one generated level of a preset.
type GradePresetLevel struct {
	Stage     string
	Number    int
	Name      string
	ShortName string
	IsFinal   bool
}

// GradePresetLevels generates the levels of a preset (API_CONTRACT §8.3).
// preset is matched case-insensitively ("MTs", "mts", "smk_4y"); numbering is
// "arabic" (default) or "roman". The last level is final.
func GradePresetLevels(preset, numbering string) ([]GradePresetLevel, error) {
	numbering = strings.ToLower(strings.TrimSpace(numbering))
	if numbering == "" {
		numbering = "arabic"
	}
	if !dto.ValidEnum(dto.RefNumberings, numbering) {
		return nil, apperr.Validation("Penomoran harus arabic atau roman", map[string]string{"numbering": "Penomoran harus arabic atau roman"})
	}
	roman := numbering == "roman"

	key, ok := dto.MatchEnum(dto.RefGradePresets, preset)
	if !ok {
		return nil, apperr.Validation("Preset tidak dikenal", map[string]string{"preset": "Preset tidak dikenal"})
	}
	ranges := map[string]struct {
		stage    string
		from, to int
	}{
		"SD": {"SD", 1, 6}, "MI": {"MI", 1, 6}, "SMP": {"SMP", 7, 9}, "MTs": {"MTs", 7, 9}, "SMP_LOCAL": {"SMP", 1, 3},
		"SMA": {"SMA", 10, 12}, "MA": {"MA", 10, 12}, "SMA_LOCAL": {"SMA", 1, 3}, "SMK": {"SMK", 10, 12},
		"SMK_4Y": {"SMK", 10, 13}, "SMK_LOCAL": {"SMK", 1, 3},
	}
	if key == "TK" {
		return []GradePresetLevel{
			{"TK", 1, "TK A", "A", false},
			{"TK", 2, "TK B", "B", true},
		}, nil
	}
	r, ok := ranges[key]
	if !ok {
		return nil, apperr.Validation("Preset tidak dikenal", map[string]string{"preset": "Preset tidak dikenal"})
	}
	var out []GradePresetLevel
	for i := r.from; i <= r.to; i++ {
		sh := strconv.Itoa(i)
		if roman {
			sh = toRoman(i)
		}
		out = append(out, GradePresetLevel{r.stage, i, "Kelas " + sh, sh, i == r.to})
	}
	return out, nil
}

func (s *AcademicService) ApplyGradePreset(ctx context.Context, schoolID uuid.UUID, req dto.GradePresetRequest) ([]dto.GradeLevelResponse, error) {
	items, err := GradePresetLevels(req.Preset, req.Numbering)
	if err != nil {
		return nil, err
	}

	err = s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", "grade_levels:"+schoolID.String()).Error; err != nil {
			return err
		}
		var existing int64
		if err := tx.Model(&model.GradeLevel{}).Where("school_id = ?", schoolID).Count(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			if !req.Replace {
				return apperr.Conflict("EXISTS", "Tingkat sudah ada. Gunakan opsi replace untuk mengganti preset.")
			}
			// Unscoped: soft-deleted classes and histories still reference levels.
			var inUse, histUse int64
			if err := tx.Unscoped().Model(&model.Classes{}).
				Where("school_id = ? AND grade_level_id IS NOT NULL", schoolID).Count(&inUse).Error; err != nil {
				return err
			}
			if err := tx.Model(&model.StudentClassHistory{}).Where("school_id = ?", schoolID).Count(&histUse).Error; err != nil {
				return err
			}
			if inUse > 0 || histUse > 0 {
				return apperr.Conflict("IN_USE", "Tidak dapat mengganti preset karena sudah ada kelas yang menggunakannya")
			}
			if err := tx.Model(&model.Students{}).Unscoped().Where("school_id = ? AND entry_grade_level_id IS NOT NULL", schoolID).
				Update("entry_grade_level_id", nil).Error; err != nil {
				return err
			}
			if err := tx.Where("school_id = ?", schoolID).Delete(&model.GradeLevel{}).Error; err != nil {
				return err
			}
		}
		for idx, it := range items {
			lvl := model.GradeLevel{
				SchoolID: schoolID, EducationStage: it.Stage, Number: it.Number, Name: it.Name,
				ShortName: it.ShortName, IsFinal: it.IsFinal, SortOrder: idx + 1,
			}
			if err := tx.Create(&lvl).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.GetGradeLevels(ctx, schoolID)
}

// ==================== Majors ====================

func majorResponse(m model.Major) dto.MajorResponse {
	return dto.MajorResponse{ID: m.ID, Code: m.Code, Name: m.Name, Description: m.Description, IsActive: m.IsActive}
}

func (s *AcademicService) GetMajors(ctx context.Context, schoolID uuid.UUID) ([]dto.MajorResponse, error) {
	var list []model.Major
	if err := s.conn(ctx).Where("school_id = ?", schoolID).Order("code ASC, name ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	result := make([]dto.MajorResponse, 0, len(list))
	for _, m := range list {
		result = append(result, majorResponse(m))
	}
	return result, nil
}

func validateMajorRequest(req *dto.MajorRequest) error {
	fe := fieldErrors{}
	req.Code = strings.ToUpper(strings.TrimSpace(req.Code))
	req.Name = strings.TrimSpace(req.Name)
	req.Description = strings.TrimSpace(req.Description)
	if req.Code == "" {
		fe.add("code", "Kode jurusan wajib diisi")
	}
	if req.Name == "" {
		fe.add("name", "Nama jurusan wajib diisi")
	}
	checkLen(fe, "code", req.Code, 50)
	checkLen(fe, "name", req.Name, 255)
	checkLen(fe, "description", req.Description, 2000)
	return fe.err()
}

func majorCodeTaken(db *gorm.DB, schoolID uuid.UUID, code string, exclude uuid.UUID) (bool, error) {
	var n int64
	q := db.Model(&model.Major{}).Where("school_id = ? AND upper(code) = ?", schoolID, code)
	if exclude != uuid.Nil {
		q = q.Where("id <> ?", exclude)
	}
	err := q.Count(&n).Error
	return n > 0, err
}

var errMajorExists = apperr.Conflict("MAJOR_EXISTS", "Kode jurusan sudah digunakan")

func (s *AcademicService) CreateMajor(ctx context.Context, schoolID uuid.UUID, req dto.MajorRequest) (*dto.MajorResponse, error) {
	if err := validateMajorRequest(&req); err != nil {
		return nil, err
	}
	db := s.conn(ctx)
	if taken, err := majorCodeTaken(db, schoolID, req.Code, uuid.Nil); err != nil {
		return nil, err
	} else if taken {
		return nil, errMajorExists
	}
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	major := model.Major{SchoolID: schoolID, Code: req.Code, Name: req.Name, Description: req.Description, IsActive: isActive}
	if err := db.Create(&major).Error; err != nil {
		return nil, err
	}
	r := majorResponse(major)
	return &r, nil
}

func (s *AcademicService) UpdateMajor(ctx context.Context, schoolID, id uuid.UUID, req dto.MajorRequest) (*dto.MajorResponse, error) {
	if err := validateMajorRequest(&req); err != nil {
		return nil, err
	}
	db := s.conn(ctx)
	var major model.Major
	if err := db.Where("school_id = ? AND id = ?", schoolID, id).First(&major).Error; err != nil {
		return nil, notFoundOr(err, "Jurusan tidak ditemukan")
	}
	if taken, err := majorCodeTaken(db, schoolID, req.Code, id); err != nil {
		return nil, err
	} else if taken {
		return nil, errMajorExists
	}
	major.Code, major.Name, major.Description = req.Code, req.Name, req.Description
	if req.IsActive != nil {
		major.IsActive = *req.IsActive
	}
	if err := db.Save(&major).Error; err != nil {
		return nil, err
	}
	r := majorResponse(major)
	return &r, nil
}

func (s *AcademicService) DeleteMajor(ctx context.Context, schoolID, id uuid.UUID) error {
	return s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		var major model.Major
		if err := tx.Where("school_id = ? AND id = ?", schoolID, id).First(&major).Error; err != nil {
			return notFoundOr(err, "Jurusan tidak ditemukan")
		}
		var inUse int64
		if err := tx.Unscoped().Model(&model.Classes{}).Where("school_id = ? AND major_id = ?", schoolID, id).Count(&inUse).Error; err != nil {
			return err
		}
		if inUse > 0 {
			return apperr.Conflict("IN_USE", "Jurusan tidak dapat dihapus karena masih digunakan oleh rombel")
		}
		return tx.Delete(&major).Error
	})
}

// ==================== Classes (Rombel) ====================

type ClassQuery struct {
	AcademicYearID *uuid.UUID
	GradeLevelID   *uuid.UUID
	MajorID        *uuid.UUID
	Search         string
	Page           int
	Limit          int
	All            bool
}

func (s *AcademicService) ListClasses(ctx context.Context, schoolID uuid.UUID, q ClassQuery) ([]dto.ClassResponse, *dto.PageMeta, error) {
	db := s.conn(ctx)
	// Default academic year = the active one.
	yearID := q.AcademicYearID
	if yearID == nil {
		var ids []uuid.UUID
		if err := db.Model(&model.AcademicYear{}).Where("school_id = ? AND is_active = true", schoolID).Limit(1).Pluck("id", &ids).Error; err != nil {
			return nil, nil, err
		}
		if len(ids) > 0 {
			yearID = &ids[0]
		}
	}

	query := db.Model(&model.Classes{}).Where("classes.school_id = ?", schoolID)
	if yearID != nil {
		query = query.Where("classes.academic_year_id = ?", *yearID)
	}
	if q.GradeLevelID != nil {
		query = query.Where("classes.grade_level_id = ?", *q.GradeLevelID)
	}
	if q.MajorID != nil {
		query = query.Where("classes.major_id = ?", *q.MajorID)
	}
	if term := strings.TrimSpace(q.Search); term != "" {
		if len(term) > 100 {
			term = term[:100]
		}
		like := dto.LikePattern(term)
		query = query.Where("(classes.class_name ILIKE ? OR classes.alias ILIKE ?)", like, like)
	}

	var total int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, nil, err
	}

	pq := dto.PageQuery{Page: q.Page, Limit: q.Limit}
	if pq.Page < 1 {
		pq.Page = dto.DefaultPage
	}
	if pq.Limit < 1 {
		pq.Limit = dto.DefaultLimit
	}
	if pq.Limit > dto.MaxLimit {
		pq.Limit = dto.MaxLimit
	}
	if q.All {
		query = query.Limit(maxAllClasses)
	} else {
		query = query.Offset(pq.Offset()).Limit(pq.Limit)
	}

	var classes []model.Classes
	if err := query.Select("classes.*").
		Joins("LEFT JOIN grade_levels gl ON gl.id = classes.grade_level_id").
		Order("gl.sort_order ASC NULLS LAST, gl.number ASC NULLS LAST, lower(classes.class_name) ASC").
		Find(&classes).Error; err != nil {
		return nil, nil, err
	}
	result, err := s.buildClassResponses(ctx, schoolID, classes)
	if err != nil {
		return nil, nil, err
	}
	if q.All {
		return result, nil, nil
	}
	pm := dto.NewPageMeta(pq, total)
	return result, &pm, nil
}

func (s *AcademicService) GetClass(ctx context.Context, schoolID, id uuid.UUID) (*dto.ClassResponse, error) {
	var c model.Classes
	if err := s.conn(ctx).Where("school_id = ? AND id = ?", schoolID, id).First(&c).Error; err != nil {
		return nil, notFoundOr(err, "Kelas tidak ditemukan")
	}
	list, err := s.buildClassResponses(ctx, schoolID, []model.Classes{c})
	if err != nil {
		return nil, err
	}
	return &list[0], nil
}

// buildClassResponses loads years, levels, majors, homerooms and member counts
// for all classes with a constant number of queries.
func (s *AcademicService) buildClassResponses(ctx context.Context, schoolID uuid.UUID, classes []model.Classes) ([]dto.ClassResponse, error) {
	result := make([]dto.ClassResponse, 0, len(classes))
	if len(classes) == 0 {
		return result, nil
	}
	db := s.conn(ctx)
	var yearIDs, gradeIDs, majorIDs, empIDs, classIDs []uuid.UUID
	for _, c := range classes {
		classIDs = append(classIDs, c.ID)
		if c.AcademicYearID != nil {
			yearIDs = append(yearIDs, *c.AcademicYearID)
		}
		if c.GradeLevelID != nil {
			gradeIDs = append(gradeIDs, *c.GradeLevelID)
		}
		if c.MajorID != nil {
			majorIDs = append(majorIDs, *c.MajorID)
		}
		if c.HomeroomEmployeeID != nil {
			empIDs = append(empIDs, *c.HomeroomEmployeeID)
		}
	}

	years := map[uuid.UUID]string{}
	if len(yearIDs) > 0 {
		var ys []model.AcademicYear
		if err := db.Where("school_id = ? AND id IN ?", schoolID, yearIDs).Find(&ys).Error; err != nil {
			return nil, err
		}
		for _, y := range ys {
			years[y.ID] = y.Name
		}
	}
	grades := map[uuid.UUID]model.GradeLevel{}
	if len(gradeIDs) > 0 {
		var gs []model.GradeLevel
		if err := db.Where("school_id = ? AND id IN ?", schoolID, gradeIDs).Find(&gs).Error; err != nil {
			return nil, err
		}
		for _, g := range gs {
			grades[g.ID] = g
		}
	}
	majors := map[uuid.UUID]model.Major{}
	if len(majorIDs) > 0 {
		var ms []model.Major
		if err := db.Where("school_id = ? AND id IN ?", schoolID, majorIDs).Find(&ms).Error; err != nil {
			return nil, err
		}
		for _, m := range ms {
			majors[m.ID] = m
		}
	}
	emps := map[uuid.UUID]string{}
	if len(empIDs) > 0 {
		var es []model.Employees
		if err := db.Select("id, full_name").Where("school_id = ? AND id IN ?", schoolID, empIDs).Find(&es).Error; err != nil {
			return nil, err
		}
		for _, e := range es {
			emps[e.ID] = e.FullName
		}
	}
	type countRow struct {
		ClassID uuid.UUID
		Total   int
		Male    int
		Female  int
	}
	var counts []countRow
	if err := db.Raw(`
		SELECT h.class_id, COUNT(*) AS total,
		       COUNT(*) FILTER (WHERE s.gender = 'L') AS male,
		       COUNT(*) FILTER (WHERE s.gender = 'P') AS female
		FROM student_class_histories h
		JOIN students s ON s.id = h.student_id AND s.deleted_at IS NULL
		WHERE h.school_id = ? AND h.status = 'active' AND h.class_id IN ?
		GROUP BY h.class_id`, schoolID, classIDs).Scan(&counts).Error; err != nil {
		return nil, err
	}
	countBy := map[uuid.UUID]countRow{}
	for _, r := range counts {
		countBy[r.ClassID] = r
	}

	for _, c := range classes {
		resp := dto.ClassResponse{
			ID: c.ID, AcademicYearID: c.AcademicYearID, Name: c.Name, Alias: c.Alias,
			DisplayName: model.ClassDisplayName(c.Name, c.Alias), Capacity: c.Capacity, Room: c.Room,
			Curriculum: c.Curriculum, IsActive: c.IsActive,
		}
		if c.AcademicYearID != nil {
			resp.AcademicYearName = years[*c.AcademicYearID]
		}
		if c.GradeLevelID != nil {
			if g, ok := grades[*c.GradeLevelID]; ok {
				resp.GradeLevel = &dto.GradeLevelShort{ID: g.ID, Number: g.Number, Name: g.Name, ShortName: g.ShortName}
			}
		}
		if c.MajorID != nil {
			if m, ok := majors[*c.MajorID]; ok {
				resp.Major = &dto.MajorShort{ID: m.ID, Code: m.Code, Name: m.Name}
			}
		}
		if c.HomeroomEmployeeID != nil {
			if name, ok := emps[*c.HomeroomEmployeeID]; ok {
				resp.Homeroom = &dto.HomeroomShort{EmployeeID: *c.HomeroomEmployeeID, FullName: name}
			}
		}
		cnt := countBy[c.ID]
		resp.StudentCount, resp.MaleCount, resp.FemaleCount = cnt.Total, cnt.Male, cnt.Female
		result = append(result, resp)
	}
	return result, nil
}

// classRefs validates that every referenced id of a class belongs to the tenant.
func classRefs(tx *gorm.DB, schoolID uuid.UUID, yearID, gradeID uuid.UUID, majorID, homeroomID *uuid.UUID) error {
	fe := fieldErrors{}
	count := func(m interface{}, where string, args ...interface{}) (int64, error) {
		var n int64
		err := tx.Model(m).Where(where, args...).Count(&n).Error
		return n, err
	}
	if n, err := count(&model.AcademicYear{}, "school_id = ? AND id = ?", schoolID, yearID); err != nil {
		return err
	} else if n == 0 {
		fe.add("academic_year_id", "Tahun ajaran tidak ditemukan")
	}
	if n, err := count(&model.GradeLevel{}, "school_id = ? AND id = ?", schoolID, gradeID); err != nil {
		return err
	} else if n == 0 {
		fe.add("grade_level_id", "Tingkat tidak ditemukan")
	}
	if majorID != nil {
		if n, err := count(&model.Major{}, "school_id = ? AND id = ?", schoolID, *majorID); err != nil {
			return err
		} else if n == 0 {
			fe.add("major_id", "Jurusan tidak ditemukan")
		}
	}
	if homeroomID != nil {
		if n, err := count(&model.Employees{}, "school_id = ? AND id = ? AND employee_type = ?", schoolID, *homeroomID, model.EmployeeTypeTeacher); err != nil {
			return err
		} else if n == 0 {
			fe.add("homeroom_employee_id", "Wali kelas harus guru di sekolah ini")
		}
	}
	return fe.err()
}

func validateClassFields(req *dto.ClassRequest) error {
	fe := fieldErrors{}
	req.Name = strings.TrimSpace(req.Name)
	req.Alias = strings.TrimSpace(req.Alias)
	req.Room = strings.TrimSpace(req.Room)
	if req.Name == "" {
		fe.add("name", "Nama kelas wajib diisi")
	}
	checkLen(fe, "name", req.Name, 50)
	checkLen(fe, "alias", req.Alias, 100)
	checkLen(fe, "room", req.Room, 100)
	checkIntRange(fe, "capacity", req.Capacity, 0, maxClassCapacity)
	normEnum(fe, "curriculum", dto.RefCurricula, &req.Curriculum)
	return fe.err()
}

func classNameTaken(tx *gorm.DB, schoolID, yearID uuid.UUID, name string, exclude uuid.UUID) (bool, error) {
	var n int64
	q := tx.Model(&model.Classes{}).Where("school_id = ? AND academic_year_id = ? AND lower(class_name) = lower(?)", schoolID, yearID, name)
	if exclude != uuid.Nil {
		q = q.Where("id <> ?", exclude)
	}
	err := q.Count(&n).Error
	return n > 0, err
}

func errClassExists(name string) error {
	return apperr.Conflict("CLASS_EXISTS", fmt.Sprintf("Kelas dengan nama %q sudah ada pada tahun ajaran ini", name))
}

func (s *AcademicService) CreateClass(ctx context.Context, schoolID uuid.UUID, req dto.ClassRequest) (*dto.ClassResponse, error) {
	if err := validateClassFields(&req); err != nil {
		return nil, err
	}
	c := model.Classes{
		SchoolID: schoolID, AcademicYearID: &req.AcademicYearID, GradeLevelID: &req.GradeLevelID,
		MajorID: req.MajorID.Ptr(), Name: req.Name, Alias: req.Alias, HomeroomEmployeeID: req.HomeroomEmployeeID.Ptr(),
		Capacity: req.Capacity, Room: req.Room, Curriculum: req.Curriculum, IsActive: true,
	}
	if req.IsActive != nil {
		c.IsActive = *req.IsActive
	}
	err := s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := classRefs(tx, schoolID, req.AcademicYearID, req.GradeLevelID, c.MajorID, c.HomeroomEmployeeID); err != nil {
			return err
		}
		if taken, err := classNameTaken(tx, schoolID, req.AcademicYearID, req.Name, uuid.Nil); err != nil {
			return err
		} else if taken {
			return errClassExists(req.Name)
		}
		if err := tx.Create(&c).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return errClassExists(req.Name)
			}
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.GetClass(ctx, schoolID, c.ID)
}

func (s *AcademicService) UpdateClass(ctx context.Context, schoolID, id uuid.UUID, req dto.ClassRequest) (*dto.ClassResponse, error) {
	if err := validateClassFields(&req); err != nil {
		return nil, err
	}
	err := s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		var c model.Classes
		if err := tx.Where("school_id = ? AND id = ?", schoolID, id).First(&c).Error; err != nil {
			return notFoundOr(err, "Kelas tidak ditemukan")
		}
		majorID, homeroomID := req.MajorID.Ptr(), req.HomeroomEmployeeID.Ptr()
		if err := classRefs(tx, schoolID, req.AcademicYearID, req.GradeLevelID, majorID, homeroomID); err != nil {
			return err
		}
		if taken, err := classNameTaken(tx, schoolID, req.AcademicYearID, req.Name, id); err != nil {
			return err
		} else if taken {
			return errClassExists(req.Name)
		}

		members, err := activeMemberCount(tx, schoolID, id)
		if err != nil {
			return err
		}
		yearChanged := c.AcademicYearID == nil || *c.AcademicYearID != req.AcademicYearID
		gradeChanged := c.GradeLevelID == nil || *c.GradeLevelID != req.GradeLevelID
		if yearChanged && members > 0 {
			return apperr.Conflict("HAS_STUDENTS", "Tahun ajaran kelas tidak dapat diubah karena masih memiliki siswa aktif")
		}
		if gradeChanged {
			// Keep the active memberships consistent with the class level.
			if err := tx.Model(&model.StudentClassHistory{}).Where("school_id = ? AND class_id = ? AND status = 'active'", schoolID, id).
				Update("grade_level_id", req.GradeLevelID).Error; err != nil {
				return err
			}
		}

		c.Name, c.Alias, c.Room, c.Curriculum = req.Name, req.Alias, req.Room, req.Curriculum
		c.AcademicYearID, c.GradeLevelID = &req.AcademicYearID, &req.GradeLevelID
		c.MajorID, c.HomeroomEmployeeID, c.Capacity = majorID, homeroomID, req.Capacity
		if req.IsActive != nil {
			c.IsActive = *req.IsActive
		}
		if err := tx.Save(&c).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return errClassExists(req.Name)
			}
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.GetClass(ctx, schoolID, id)
}

func (s *AcademicService) DeleteClass(ctx context.Context, schoolID, id uuid.UUID) error {
	return s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		cls, err := lockClass(tx, schoolID, id)
		if err != nil {
			var ae *apperr.Error
			if !errors.As(err, &ae) || ae.Status != 400 {
				return err
			}
			// Legacy class without year/level: still deletable.
			var c model.Classes
			if err := tx.Where("school_id = ? AND id = ?", schoolID, id).First(&c).Error; err != nil {
				return notFoundOr(err, "Kelas tidak ditemukan")
			}
			cls = &c
		}
		members, err := activeMemberCount(tx, schoolID, id)
		if err != nil {
			return err
		}
		if members > 0 {
			return apperr.Conflict("HAS_STUDENTS", fmt.Sprintf("Kelas tidak dapat dihapus karena masih memiliki %d siswa aktif", members))
		}
		// Active rows of soft-deleted students would otherwise point at a deleted class.
		if err := tx.Model(&model.StudentClassHistory{}).Where("school_id = ? AND class_id = ? AND status = 'active'", schoolID, id).
			Updates(map[string]interface{}{"status": dto.HistoryWithdrawn, "end_date": today()}).Error; err != nil {
			return err
		}
		// The class's timetable and subject assignments go with it.
		if err := removeSchedulesWhere(tx, schoolID, "class_id = ?", id); err != nil {
			return err
		}
		if err := tx.Where("school_id = ? AND class_id = ?", schoolID, id).Delete(&model.ClassSubject{}).Error; err != nil {
			return err
		}
		return tx.Delete(cls).Error
	})
}

// CreateBulkClasses creates many classes of one level; names that already
// exist (or repeat) are skipped and reported.
func (s *AcademicService) CreateBulkClasses(ctx context.Context, schoolID uuid.UUID, req dto.ClassBulkRequest) (int, []string, error) {
	fe := fieldErrors{}
	if len(req.Names) == 0 {
		fe.add("names", "Nama kelas wajib diisi")
	}
	if len(req.Names) > maxBulkClasses {
		fe.add("names", fmt.Sprintf("Maksimal %d kelas sekaligus", maxBulkClasses))
	}
	if len(req.Aliases) > len(req.Names) {
		fe.add("aliases", "Jumlah alias tidak boleh melebihi jumlah nama")
	}
	checkIntRange(fe, "capacity", req.Capacity, 0, maxClassCapacity)
	for i, n := range req.Names {
		checkLen(fe, fmt.Sprintf("names[%d]", i), strings.TrimSpace(n), 50)
	}
	for i, a := range req.Aliases {
		checkLen(fe, fmt.Sprintf("aliases[%d]", i), strings.TrimSpace(a), 100)
	}
	if err := fe.err(); err != nil {
		return 0, nil, err
	}

	created := 0
	skipped := []string{}
	err := s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := classRefs(tx, schoolID, req.AcademicYearID, req.GradeLevelID, req.MajorID.Ptr(), nil); err != nil {
			return err
		}
		seen := map[string]bool{}
		for i, name := range req.Names {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			key := strings.ToLower(name)
			if seen[key] {
				skipped = append(skipped, name)
				continue
			}
			seen[key] = true
			if taken, err := classNameTaken(tx, schoolID, req.AcademicYearID, name, uuid.Nil); err != nil {
				return err
			} else if taken {
				skipped = append(skipped, name)
				continue
			}
			alias := ""
			if i < len(req.Aliases) {
				alias = strings.TrimSpace(req.Aliases[i])
			}
			c := model.Classes{
				SchoolID: schoolID, AcademicYearID: &req.AcademicYearID, GradeLevelID: &req.GradeLevelID,
				MajorID: req.MajorID.Ptr(), Name: name, Alias: alias, Capacity: req.Capacity, IsActive: true,
			}
			if err := tx.Create(&c).Error; err != nil {
				return err
			}
			created++
		}
		return nil
	})
	if err != nil {
		return 0, nil, err
	}
	return created, skipped, nil
}

func (s *AcademicService) CopyClasses(ctx context.Context, schoolID uuid.UUID, req dto.ClassCopyRequest) (map[string]int, error) {
	if req.FromAcademicYearID == req.ToAcademicYearID {
		return nil, apperr.Validation("Tahun ajaran asal dan tujuan harus berbeda",
			map[string]string{"to_academic_year_id": "Tahun ajaran asal dan tujuan harus berbeda"})
	}
	created, skipped := 0, 0
	err := s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		var n int64
		if err := tx.Model(&model.AcademicYear{}).Where("school_id = ? AND id IN ?", schoolID,
			[]uuid.UUID{req.FromAcademicYearID, req.ToAcademicYearID}).Count(&n).Error; err != nil {
			return err
		}
		if n != 2 {
			return apperr.NotFound("Tahun ajaran tidak ditemukan")
		}
		var sources []model.Classes
		if err := tx.Where("school_id = ? AND academic_year_id = ?", schoolID, req.FromAcademicYearID).
			Order("class_name").Find(&sources).Error; err != nil {
			return err
		}
		validTeachers := map[uuid.UUID]bool{}
		if req.IncludeHomeroom {
			var ids []uuid.UUID
			if err := tx.Model(&model.Employees{}).Where("school_id = ? AND employee_type = ?", schoolID, model.EmployeeTypeTeacher).
				Pluck("id", &ids).Error; err != nil {
				return err
			}
			for _, id := range ids {
				validTeachers[id] = true
			}
		}
		for _, src := range sources {
			if src.GradeLevelID == nil {
				skipped++
				continue
			}
			if taken, err := classNameTaken(tx, schoolID, req.ToAcademicYearID, src.Name, uuid.Nil); err != nil {
				return err
			} else if taken {
				skipped++
				continue
			}
			var homeroom *uuid.UUID
			if req.IncludeHomeroom && src.HomeroomEmployeeID != nil && validTeachers[*src.HomeroomEmployeeID] {
				homeroom = src.HomeroomEmployeeID
			}
			c := model.Classes{
				SchoolID: schoolID, AcademicYearID: &req.ToAcademicYearID, GradeLevelID: src.GradeLevelID,
				MajorID: src.MajorID, Name: src.Name, Alias: src.Alias, HomeroomEmployeeID: homeroom,
				Capacity: src.Capacity, Room: src.Room, Curriculum: src.Curriculum, IsActive: true,
			}
			if err := tx.Create(&c).Error; err != nil {
				return err
			}
			created++
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return map[string]int{"created": created, "skipped": skipped}, nil
}

// ==================== Document Types Seed ====================

var defaultDocumentTypes = []struct {
	Name       string
	IsRequired bool
}{
	{"Kartu Keluarga", true},
	{"Akta Kelahiran", true},
	{"Ijazah", true},
	{"SKHUN/SHUN", false},
	{"KIP/KPS/PKH", false},
	{"Pas Foto", false},
	{"Rapor", false},
	{"Surat Pindah", false},
	{"Lainnya", false},
}

// GetOrSeedDocumentTypes returns the school's document types, seeding the
// defaults once (idempotent and race-safe via an advisory lock).
func (s *AcademicService) GetOrSeedDocumentTypes(ctx context.Context, schoolID uuid.UUID) ([]model.StudentDocumentType, error) {
	db := s.conn(ctx)
	var list []model.StudentDocumentType
	if err := db.Where("school_id = ?", schoolID).Order("sort_order ASC, name ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	if len(list) > 0 {
		return list, nil
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", "document_types:"+schoolID.String()).Error; err != nil {
			return err
		}
		var n int64
		if err := tx.Model(&model.StudentDocumentType{}).Where("school_id = ?", schoolID).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return nil
		}
		for i, dt := range defaultDocumentTypes {
			item := model.StudentDocumentType{SchoolID: schoolID, Name: dt.Name, IsRequired: dt.IsRequired, SortOrder: i + 1}
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := db.Where("school_id = ?", schoolID).Order("sort_order ASC, name ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}
