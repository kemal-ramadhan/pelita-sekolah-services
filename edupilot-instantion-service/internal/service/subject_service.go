package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/edupilot-instantion-service/internal/apperr"
	"github.com/edupilot-instantion-service/internal/config"
	"github.com/edupilot-instantion-service/internal/dto"
	"github.com/edupilot-instantion-service/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type SubjectService struct {
	db *gorm.DB
}

// NewSubjectService: a nil db means config.DB, resolved per call (the
// controller creates the service at package init, before ConnectDB runs).
func NewSubjectService(db *gorm.DB) *SubjectService {
	return &SubjectService{db: db}
}

func (s *SubjectService) conn(ctx context.Context) *gorm.DB {
	if s.db != nil {
		return s.db.WithContext(ctx)
	}
	return config.DB.WithContext(ctx)
}

const (
	maxSubjectAssignIDs = 200
	maxHoursPerWeek     = 60
)

type SubjectQuery struct {
	Search      string
	SubjectType string
	IsActive    *bool
	Page        int
	Limit       int
	All         bool
}

func gradeLevelShort(gl *model.GradeLevel) *dto.GradeLevelShort {
	if gl == nil {
		return nil
	}
	return &dto.GradeLevelShort{ID: gl.ID, Name: gl.Name, ShortName: gl.ShortName, Number: gl.Number}
}

// subjectToResponse maps a subject (counts are filled by the caller).
func subjectToResponse(sub *model.Subject) *dto.SubjectResponse {
	return &dto.SubjectResponse{
		ID:           sub.ID,
		SchoolID:     sub.SchoolID,
		SubjectCode:  sub.SubjectCode,
		Name:         sub.Name,
		ShortName:    sub.ShortName,
		SubjectType:  sub.SubjectType,
		Level:        sub.Level,
		GradeLevelID: sub.GradeLevelID,
		GradeLevel:   gradeLevelShort(sub.GradeLevel),
		Description:  sub.Description,
		IsActive:     sub.IsActive,
		CreatedAt:    sub.CreatedAt,
		UpdatedAt:    sub.UpdatedAt,
	}
}

func (s *SubjectService) toSubjectResponse(sub *model.Subject) *dto.SubjectResponse {
	return subjectToResponse(sub)
}

func (s *SubjectService) ListSubjects(ctx context.Context, schoolID uuid.UUID, q SubjectQuery) ([]dto.SubjectResponse, *dto.PageMeta, error) {
	conn := s.conn(ctx)
	db := conn.Model(&model.Subject{}).Where("school_id = ?", schoolID)

	if q.Search != "" {
		if len(q.Search) > 100 {
			q.Search = q.Search[:100]
		}
		term := "%" + escapeLike(strings.ToLower(q.Search)) + "%"
		db = db.Where("(lower(name) LIKE ? OR lower(subject_code) LIKE ? OR lower(short_name) LIKE ?)", term, term, term)
	}
	if q.SubjectType != "" {
		db = db.Where("subject_type = ?", q.SubjectType)
	}
	if q.IsActive != nil {
		db = db.Where("is_active = ?", *q.IsActive)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, nil, err
	}

	if !q.All {
		pq := dto.ParsePageQuery(fmt.Sprint(q.Page), fmt.Sprint(q.Limit), "")
		q.Page, q.Limit = pq.Page, pq.Limit
		db = db.Offset(pq.Offset()).Limit(pq.Limit)
	}

	var subjects []model.Subject
	if err := db.Preload("GradeLevel").Order("lower(name) ASC, id ASC").Find(&subjects).Error; err != nil {
		return nil, nil, err
	}

	subjectIDs := make([]uuid.UUID, len(subjects))
	for i, sub := range subjects {
		subjectIDs[i] = sub.ID
	}

	type countRow struct {
		SubjectID uuid.UUID
		Count     int
	}
	teacherCounts := make(map[uuid.UUID]int)
	classCounts := make(map[uuid.UUID]int)
	if len(subjectIDs) > 0 {
		var tRows []countRow
		if err := conn.Table("teacher_subjects ts").
			Select("ts.subject_id, count(DISTINCT ts.employee_id) AS count").
			Joins("JOIN employees e ON e.id = ts.employee_id AND e.deleted_at IS NULL").
			Where("ts.school_id = ? AND ts.subject_id IN ?", schoolID, subjectIDs).
			Group("ts.subject_id").Scan(&tRows).Error; err != nil {
			return nil, nil, err
		}
		for _, r := range tRows {
			teacherCounts[r.SubjectID] = r.Count
		}
		var cRows []countRow
		if err := conn.Table("class_subjects cs").
			Select("cs.subject_id, count(DISTINCT cs.class_id) AS count").
			Joins("JOIN classes c ON c.id = cs.class_id AND c.deleted_at IS NULL").
			Where("cs.school_id = ? AND cs.subject_id IN ? AND cs.deleted_at IS NULL", schoolID, subjectIDs).
			Group("cs.subject_id").Scan(&cRows).Error; err != nil {
			return nil, nil, err
		}
		for _, r := range cRows {
			classCounts[r.SubjectID] = r.Count
		}
	}

	result := make([]dto.SubjectResponse, 0, len(subjects))
	for i := range subjects {
		r := subjectToResponse(&subjects[i])
		r.TeacherCount = teacherCounts[subjects[i].ID]
		r.ClassCount = classCounts[subjects[i].ID]
		result = append(result, *r)
	}

	if q.All {
		return result, nil, nil
	}
	meta := dto.NewPageMeta(dto.PageQuery{Page: q.Page, Limit: q.Limit}, total)
	return result, &meta, nil
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func (s *SubjectService) findSubject(db *gorm.DB, schoolID, id uuid.UUID) (*model.Subject, error) {
	var sub model.Subject
	if err := db.Preload("GradeLevel").Where("school_id = ? AND id = ?", schoolID, id).First(&sub).Error; err != nil {
		return nil, notFoundOr(err, "Mata pelajaran tidak ditemukan")
	}
	return &sub, nil
}

func (s *SubjectService) GetSubject(ctx context.Context, schoolID, id uuid.UUID) (*dto.SubjectDetailResponse, error) {
	db := s.conn(ctx)
	sub, err := s.findSubject(db, schoolID, id)
	if err != nil {
		return nil, err
	}

	teacherItems, err := s.subjectTeachers(db, schoolID, id)
	if err != nil {
		return nil, err
	}

	var classSubs []model.ClassSubject
	if err := db.Preload("Class").Preload("PrimaryTeacher").
		Joins("JOIN classes c ON c.id = class_subjects.class_id AND c.deleted_at IS NULL").
		Where("class_subjects.school_id = ? AND class_subjects.subject_id = ?", schoolID, id).
		Order("c.class_name ASC").
		Find(&classSubs).Error; err != nil {
		return nil, err
	}
	yearNames := map[uuid.UUID]string{}
	if len(classSubs) > 0 {
		yearIDs := make([]uuid.UUID, 0, len(classSubs))
		for _, cs := range classSubs {
			yearIDs = append(yearIDs, cs.AcademicYearID)
		}
		var years []model.AcademicYear
		if err := db.Where("school_id = ? AND id IN ?", schoolID, yearIDs).Find(&years).Error; err != nil {
			return nil, err
		}
		for _, y := range years {
			yearNames[y.ID] = y.Name
		}
	}

	classItems := make([]dto.SubjectClassItem, 0, len(classSubs))
	for _, cs := range classSubs {
		item := dto.SubjectClassItem{ClassID: cs.ClassID, HoursPerWeek: cs.HoursPerWeek, AcademicYearName: yearNames[cs.AcademicYearID]}
		if cs.Class != nil {
			item.ClassName = cs.Class.Name
			item.DisplayName = cs.Class.DisplayName()
		}
		if cs.PrimaryTeacher != nil {
			item.TeacherName = cs.PrimaryTeacher.FullName
		}
		classItems = append(classItems, item)
	}

	base := subjectToResponse(sub)
	base.TeacherCount = len(teacherItems)
	base.ClassCount = len(classItems)
	return &dto.SubjectDetailResponse{SubjectResponse: *base, Teachers: teacherItems, Classes: classItems}, nil
}

// normalizeSubject trims and validates a subject request (no DB access).
func normalizeSubject(req *dto.SubjectRequest) error {
	req.SubjectCode = strings.TrimSpace(req.SubjectCode)
	req.Name = strings.TrimSpace(req.Name)
	req.ShortName = strings.TrimSpace(req.ShortName)
	req.SubjectType = strings.TrimSpace(req.SubjectType)
	req.Level = strings.TrimSpace(req.Level)
	req.Description = strings.TrimSpace(req.Description)
	fe := fieldErrors{}
	if req.SubjectCode == "" {
		fe.add("subject_code", "Kode mata pelajaran wajib diisi")
	} else if len(req.SubjectCode) > 50 {
		fe.add("subject_code", "Kode mata pelajaran maksimal 50 karakter")
	}
	if req.Name == "" {
		fe.add("name", "Nama mata pelajaran wajib diisi")
	} else if len(req.Name) > 255 {
		fe.add("name", "Nama mata pelajaran maksimal 255 karakter")
	}
	if len(req.ShortName) > 50 {
		fe.add("short_name", "Singkatan maksimal 50 karakter")
	}
	if len(req.Level) > 100 {
		fe.add("level", "Jenjang maksimal 100 karakter")
	}
	if len(req.Description) > 2000 {
		fe.add("description", "Deskripsi maksimal 2000 karakter")
	}
	if req.SubjectType != "" && !dto.ValidEnum(dto.RefSubjectTypes, req.SubjectType) {
		fe.add("subject_type", "Jenis mata pelajaran tidak valid")
	}
	return fe.err()
}

// checkSubjectUnique enforces unique code, and unique name per grade level,
// among the school's non-deleted subjects (matches the partial indexes).
func checkSubjectUnique(db *gorm.DB, schoolID uuid.UUID, code, name string, gradeLevelID *uuid.UUID, exclude uuid.UUID) error {
	var n int64
	q := db.Model(&model.Subject{}).Where("school_id = ? AND lower(subject_code) = ?", schoolID, strings.ToLower(code))
	if exclude != uuid.Nil {
		q = q.Where("id <> ?", exclude)
	}
	if err := q.Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return apperr.Conflict("DUPLICATE_CODE", "Kode mata pelajaran sudah digunakan di sekolah ini")
	}
	q = db.Model(&model.Subject{}).Where("school_id = ? AND lower(name) = ?", schoolID, strings.ToLower(name))
	if gradeLevelID != nil {
		q = q.Where("grade_level_id = ?", *gradeLevelID)
	} else {
		q = q.Where("grade_level_id IS NULL")
	}
	if exclude != uuid.Nil {
		q = q.Where("id <> ?", exclude)
	}
	if err := q.Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return apperr.Conflict("DUPLICATE_NAME", "Nama mata pelajaran sudah digunakan di sekolah ini")
	}
	return nil
}

// mapSubjectWriteError maps a unique-index race to the same 409 as the pre-check.
func mapSubjectWriteError(err error) error {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return apperr.Conflict("DUPLICATE_CODE", "Kode atau nama mata pelajaran sudah digunakan di sekolah ini")
	}
	return err
}

func (s *SubjectService) verifyGradeLevel(db *gorm.DB, schoolID uuid.UUID, id *uuid.UUID) error {
	if id == nil {
		return nil
	}
	var n int64
	if err := db.Model(&model.GradeLevel{}).Where("school_id = ? AND id = ?", schoolID, *id).Count(&n).Error; err != nil {
		return err
	}
	if n == 0 {
		return invalidRef("Tingkat kelas tidak ditemukan di sekolah ini")
	}
	return nil
}

func (s *SubjectService) CreateSubject(ctx context.Context, schoolID uuid.UUID, req dto.SubjectRequest) (*dto.SubjectResponse, error) {
	if err := normalizeSubject(&req); err != nil {
		return nil, err
	}
	db := s.conn(ctx)
	if err := s.verifyGradeLevel(db, schoolID, req.GradeLevelID); err != nil {
		return nil, err
	}
	if err := checkSubjectUnique(db, schoolID, req.SubjectCode, req.Name, req.GradeLevelID, uuid.Nil); err != nil {
		return nil, err
	}

	subType := req.SubjectType
	if subType == "" {
		subType = dto.SubjectTypeWajibUmum
	}
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	sub := model.Subject{
		SchoolID:     schoolID,
		SubjectCode:  req.SubjectCode,
		Name:         req.Name,
		ShortName:    req.ShortName,
		SubjectType:  subType,
		Level:        req.Level,
		GradeLevelID: req.GradeLevelID,
		Description:  req.Description,
		IsActive:     isActive,
	}
	if err := db.Create(&sub).Error; err != nil {
		return nil, mapSubjectWriteError(err)
	}
	return s.reloadSubject(db, schoolID, sub.ID)
}

func (s *SubjectService) reloadSubject(db *gorm.DB, schoolID, id uuid.UUID) (*dto.SubjectResponse, error) {
	sub, err := s.findSubject(db, schoolID, id)
	if err != nil {
		return nil, err
	}
	return subjectToResponse(sub), nil
}

func (s *SubjectService) UpdateSubject(ctx context.Context, schoolID, id uuid.UUID, req dto.SubjectRequest) (*dto.SubjectResponse, error) {
	db := s.conn(ctx)
	var sub model.Subject
	if err := db.Where("school_id = ? AND id = ?", schoolID, id).First(&sub).Error; err != nil {
		return nil, notFoundOr(err, "Mata pelajaran tidak ditemukan")
	}
	if err := normalizeSubject(&req); err != nil {
		return nil, err
	}
	if err := s.verifyGradeLevel(db, schoolID, req.GradeLevelID); err != nil {
		return nil, err
	}
	if err := checkSubjectUnique(db, schoolID, req.SubjectCode, req.Name, req.GradeLevelID, id); err != nil {
		return nil, err
	}

	sub.SubjectCode = req.SubjectCode
	sub.Name = req.Name
	sub.ShortName = req.ShortName
	if req.SubjectType != "" {
		sub.SubjectType = req.SubjectType
	}
	sub.Level = req.Level
	sub.GradeLevelID = req.GradeLevelID
	sub.GradeLevel = nil
	sub.Description = req.Description
	if req.IsActive != nil {
		sub.IsActive = *req.IsActive
	}
	if err := db.Save(&sub).Error; err != nil {
		return nil, mapSubjectWriteError(err)
	}
	return s.reloadSubject(db, schoolID, id)
}

func (s *SubjectService) DeleteSubject(ctx context.Context, schoolID, id uuid.UUID) error {
	return s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		var sub model.Subject
		if err := tx.Where("school_id = ? AND id = ?", schoolID, id).First(&sub).Error; err != nil {
			return notFoundOr(err, "Mata pelajaran tidak ditemukan")
		}
		var count int64
		if err := tx.Model(&model.ClassSubject{}).Where("school_id = ? AND subject_id = ?", schoolID, id).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return apperr.Conflict("IN_USE", fmt.Sprintf("Mata pelajaran tidak dapat dihapus karena masih ditetapkan pada %d rombel kelas", count))
		}
		if err := tx.Model(&model.TeacherSchedules{}).Where("school_id = ? AND subject_id = ?", schoolID, id).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return apperr.Conflict("IN_USE", fmt.Sprintf("Mata pelajaran tidak dapat dihapus karena memiliki %d jadwal pelajaran", count))
		}
		if err := tx.Where("school_id = ? AND subject_id = ?", schoolID, id).Delete(&model.TeacherSubjects{}).Error; err != nil {
			return err
		}
		return tx.Delete(&sub).Error
	})
}

// ==================== Presets ====================

type subjectPreset struct {
	Code, Name, Short, Type string
}

// Madrasah (MI/MTs/MA) split PAI into the PAI-rumpun subjects + Bahasa Arab (KMA 347/2022).
var madrasahPAI = []subjectPreset{
	{"QH", "Al-Qur'an Hadis", "QH", dto.SubjectTypeWajibUmum},
	{"AA", "Akidah Akhlak", "AA", dto.SubjectTypeWajibUmum},
	{"FIK", "Fikih", "Fikih", dto.SubjectTypeWajibUmum},
	{"SKI", "Sejarah Kebudayaan Islam", "SKI", dto.SubjectTypeWajibUmum},
	{"BAR", "Bahasa Arab", "B.Arab", dto.SubjectTypeWajibUmum},
}

var paiPreset = subjectPreset{"PAI", "Pendidikan Agama dan Budi Pekerti", "PAI", dto.SubjectTypeWajibUmum}

// Kurikulum Merdeka structure per stage (Kepmendikbudristek 262/M/2022,
// Permendikbudristek 12/2024). Codes are stable so re-applying is idempotent.
var subjectPresetsCommon = map[string][]subjectPreset{
	"SD": {
		{"PPKN", "Pendidikan Pancasila", "PP", dto.SubjectTypeWajibUmum},
		{"BIN", "Bahasa Indonesia", "B.Indo", dto.SubjectTypeWajibUmum},
		{"MAT", "Matematika", "MTK", dto.SubjectTypeWajibUmum},
		{"IPAS", "Ilmu Pengetahuan Alam dan Sosial (IPAS)", "IPAS", dto.SubjectTypeWajibUmum},
		{"PJOK", "Pendidikan Jasmani, Olahraga, dan Kesehatan", "PJOK", dto.SubjectTypeWajibUmum},
		{"SBK", "Seni dan Budaya", "Seni", dto.SubjectTypeWajibUmum},
		{"BIG", "Bahasa Inggris", "B.Ing", dto.SubjectTypeWajibUmum},
		{"MULOK", "Muatan Lokal / Bahasa Daerah", "Mulok", dto.SubjectTypeMuatanLokal},
	},
	"SMP": {
		{"PPKN", "Pendidikan Pancasila", "PP", dto.SubjectTypeWajibUmum},
		{"BIN", "Bahasa Indonesia", "B.Indo", dto.SubjectTypeWajibUmum},
		{"MAT", "Matematika", "MTK", dto.SubjectTypeWajibUmum},
		{"IPA", "Ilmu Pengetahuan Alam (IPA)", "IPA", dto.SubjectTypeWajibUmum},
		{"IPS", "Ilmu Pengetahuan Sosial (IPS)", "IPS", dto.SubjectTypeWajibUmum},
		{"BIG", "Bahasa Inggris", "B.Ing", dto.SubjectTypeWajibUmum},
		{"PJOK", "Pendidikan Jasmani, Olahraga, dan Kesehatan", "PJOK", dto.SubjectTypeWajibUmum},
		{"INF", "Informatika", "Infor", dto.SubjectTypeWajibUmum},
		{"SBK", "Seni dan Prakarya", "Seni", dto.SubjectTypeWajibUmum},
		{"MULOK", "Muatan Lokal / Bahasa Daerah", "Mulok", dto.SubjectTypeMuatanLokal},
	},
	"SMA": {
		{"PPKN", "Pendidikan Pancasila", "PP", dto.SubjectTypeWajibUmum},
		{"BIN", "Bahasa Indonesia", "B.Indo", dto.SubjectTypeWajibUmum},
		{"MAT", "Matematika", "MTK", dto.SubjectTypeWajibUmum},
		{"BIG", "Bahasa Inggris", "B.Ing", dto.SubjectTypeWajibUmum},
		{"PJOK", "Pendidikan Jasmani, Olahraga, dan Kesehatan", "PJOK", dto.SubjectTypeWajibUmum},
		{"SEJ", "Sejarah", "Sejarah", dto.SubjectTypeWajibUmum},
		{"INF", "Informatika", "Infor", dto.SubjectTypeWajibUmum},
		{"SBK", "Seni dan Budaya", "Seni", dto.SubjectTypeWajibUmum},
		{"FIS", "Fisika", "Fisika", dto.SubjectTypePeminatan},
		{"KIM", "Kimia", "Kimia", dto.SubjectTypePeminatan},
		{"BIO", "Biologi", "Biologi", dto.SubjectTypePeminatan},
		{"EKO", "Ekonomi", "Ekonomi", dto.SubjectTypePeminatan},
		{"SOS", "Sosiologi", "Sosiologi", dto.SubjectTypePeminatan},
		{"GEO", "Geografi", "Geografi", dto.SubjectTypePeminatan},
		{"ANT", "Antropologi", "Antro", dto.SubjectTypePeminatan},
		{"MATL", "Matematika Tingkat Lanjut", "MTK-L", dto.SubjectTypePeminatan},
		{"PKWU", "Prakarya dan Kewirausahaan", "PKWU", dto.SubjectTypePeminatan},
		{"MULOK", "Muatan Lokal", "Mulok", dto.SubjectTypeMuatanLokal},
	},
	"SMK": {
		{"PPKN", "Pendidikan Pancasila", "PP", dto.SubjectTypeWajibUmum},
		{"BIN", "Bahasa Indonesia", "B.Indo", dto.SubjectTypeWajibUmum},
		{"PJOK", "Pendidikan Jasmani, Olahraga, dan Kesehatan", "PJOK", dto.SubjectTypeWajibUmum},
		{"SEJ", "Sejarah", "Sejarah", dto.SubjectTypeWajibUmum},
		{"SBK", "Seni dan Budaya", "Seni", dto.SubjectTypeWajibUmum},
		{"MAT", "Matematika", "MTK", dto.SubjectTypeKejuruan},
		{"BIG", "Bahasa Inggris", "B.Ing", dto.SubjectTypeKejuruan},
		{"INF", "Informatika", "Infor", dto.SubjectTypeKejuruan},
		{"IPAS", "Projek Ilmu Pengetahuan Alam dan Sosial (IPAS)", "P-IPAS", dto.SubjectTypeKejuruan},
		{"PROD", "Dasar-dasar Program Keahlian", "Dasar PK", dto.SubjectTypeKejuruan},
		{"KK", "Konsentrasi Keahlian", "KK", dto.SubjectTypeKejuruan},
		{"PKWU", "Projek Kreatif dan Kewirausahaan", "PKK", dto.SubjectTypeKejuruan},
		{"PKL", "Praktik Kerja Lapangan", "PKL", dto.SubjectTypeKejuruan},
		{"MPP", "Mata Pelajaran Pilihan", "Pilihan", dto.SubjectTypePeminatan},
		{"MULOK", "Muatan Lokal", "Mulok", dto.SubjectTypeMuatanLokal},
	},
}

// subjectPresetFor returns the preset list for a stage (pure, unit tested).
func subjectPresetFor(preset string) ([]subjectPreset, bool) {
	var base string
	madrasah := false
	switch strings.ToUpper(strings.TrimSpace(preset)) {
	case "SD":
		base = "SD"
	case "MI":
		base, madrasah = "SD", true
	case "SMP":
		base = "SMP"
	case "MTS":
		base, madrasah = "SMP", true
	case "SMA":
		base = "SMA"
	case "MA":
		base, madrasah = "SMA", true
	case "SMK", "MAK":
		base = "SMK"
	default:
		return nil, false
	}
	out := make([]subjectPreset, 0, len(subjectPresetsCommon[base])+len(madrasahPAI))
	if madrasah {
		out = append(out, madrasahPAI...)
	} else {
		out = append(out, paiPreset)
	}
	return append(out, subjectPresetsCommon[base]...), true
}

// ApplySubjectPresets creates the stage's subjects that do not exist yet
// (same code, or same name without grade level) and returns the created ones.
func (s *SubjectService) ApplySubjectPresets(ctx context.Context, schoolID uuid.UUID, preset string) ([]dto.SubjectResponse, error) {
	defs, ok := subjectPresetFor(preset)
	if !ok {
		return nil, apperr.BadRequest("Preset tidak valid. Pilihan: SD, MI, SMP, MTS, SMA, MA, SMK")
	}

	created := make([]dto.SubjectResponse, 0, len(defs))
	err := s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		var existing []model.Subject
		if err := tx.Select("subject_code", "name", "grade_level_id").Where("school_id = ?", schoolID).Find(&existing).Error; err != nil {
			return err
		}
		codes, names := map[string]bool{}, map[string]bool{}
		for _, e := range existing {
			codes[strings.ToLower(e.SubjectCode)] = true
			if e.GradeLevelID == nil {
				names[strings.ToLower(e.Name)] = true
			}
		}
		for _, d := range defs {
			if codes[strings.ToLower(d.Code)] || names[strings.ToLower(d.Name)] {
				continue
			}
			sub := model.Subject{
				SchoolID:    schoolID,
				SubjectCode: d.Code,
				Name:        d.Name,
				ShortName:   d.Short,
				SubjectType: d.Type,
				IsActive:    true,
			}
			if err := tx.Create(&sub).Error; err != nil {
				return mapSubjectWriteError(err)
			}
			codes[strings.ToLower(d.Code)], names[strings.ToLower(d.Name)] = true, true
			created = append(created, *subjectToResponse(&sub))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// ==================== Teacher - Subject Mapping ====================

// uniqueIDs drops duplicates and enforces the list size limit.
func uniqueIDs(ids []uuid.UUID, field string) ([]uuid.UUID, error) {
	if len(ids) > maxSubjectAssignIDs {
		return nil, apperr.BadRequest(fmt.Sprintf("%s maksimal %d item", field, maxSubjectAssignIDs))
	}
	seen := make(map[uuid.UUID]bool, len(ids))
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id == uuid.Nil || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out, nil
}

// primaryString maps the optional is_primary flag to the legacy varchar
// column ("true"/"false"; default "true").
func primaryString(v *dto.FlexBool) string {
	if v == nil {
		return "true"
	}
	return v.String()
}

// verifyAllOwned checks every id exists in the tenant's table (non-deleted).
func verifyAllOwned(db *gorm.DB, model interface{}, schoolID uuid.UUID, ids []uuid.UUID, msg string) error {
	if len(ids) == 0 {
		return nil
	}
	var n int64
	if err := db.Model(model).Where("school_id = ? AND id IN ?", schoolID, ids).Count(&n).Error; err != nil {
		return err
	}
	if n != int64(len(ids)) {
		return invalidRef(msg)
	}
	return nil
}

func (s *SubjectService) subjectTeachers(db *gorm.DB, schoolID, subjectID uuid.UUID) ([]dto.SubjectTeacherItem, error) {
	var rows []model.TeacherSubjects
	if err := db.Preload("Teacher").
		Joins("JOIN employees e ON e.id = teacher_subjects.employee_id AND e.deleted_at IS NULL").
		Where("teacher_subjects.school_id = ? AND teacher_subjects.subject_id = ?", schoolID, subjectID).
		Order("e.full_name ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	res := make([]dto.SubjectTeacherItem, 0, len(rows))
	seen := map[uuid.UUID]bool{}
	for _, r := range rows {
		if seen[r.EmployeeID] {
			continue // one entry per teacher (the teacher form may store one row per year)
		}
		seen[r.EmployeeID] = true
		item := dto.SubjectTeacherItem{TeacherID: r.EmployeeID, IsPrimary: r.IsPrimary}
		if r.Teacher != nil {
			item.FullName = r.Teacher.FullName
			item.EmployeeCode = r.Teacher.EmployeeCode
		}
		res = append(res, item)
	}
	return res, nil
}

func (s *SubjectService) GetSubjectTeachers(ctx context.Context, schoolID, subjectID uuid.UUID) ([]dto.SubjectTeacherItem, error) {
	db := s.conn(ctx)
	if _, err := s.findSubject(db, schoolID, subjectID); err != nil {
		return nil, err
	}
	return s.subjectTeachers(db, schoolID, subjectID)
}

// AssignTeachersToSubject replaces the subject's teacher set. Existing rows of
// teachers that stay are kept (with their academic_id from the teacher form).
func (s *SubjectService) AssignTeachersToSubject(ctx context.Context, schoolID, subjectID uuid.UUID, teacherIDs []uuid.UUID, isPrimary *dto.FlexBool) error {
	primary := primaryString(isPrimary)
	ids, err := uniqueIDs(teacherIDs, "teacher_ids")
	if err != nil {
		return err
	}
	return s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		var sub model.Subject
		if err := tx.Where("school_id = ? AND id = ?", schoolID, subjectID).First(&sub).Error; err != nil {
			return notFoundOr(err, "Mata pelajaran tidak ditemukan")
		}
		if err := verifyAllOwned(tx, &model.Employees{}, schoolID, ids, "Guru tidak ditemukan di sekolah ini"); err != nil {
			return err
		}
		q := tx.Where("school_id = ? AND subject_id = ?", schoolID, subjectID)
		if len(ids) > 0 {
			q = q.Where("employee_id NOT IN ?", ids)
		}
		if err := q.Delete(&model.TeacherSubjects{}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.TeacherSubjects{}).Where("school_id = ? AND subject_id = ?", schoolID, subjectID).
			Update("is_primary", primary).Error; err != nil {
			return err
		}
		var have []uuid.UUID
		if err := tx.Model(&model.TeacherSubjects{}).Where("school_id = ? AND subject_id = ?", schoolID, subjectID).
			Distinct().Pluck("employee_id", &have).Error; err != nil {
			return err
		}
		exists := map[uuid.UUID]bool{}
		for _, id := range have {
			exists[id] = true
		}
		for _, tid := range ids {
			if exists[tid] {
				continue
			}
			row := model.TeacherSubjects{SchoolID: schoolID, EmployeeID: tid, SubjectID: subjectID, IsPrimary: primary}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *SubjectService) GetTeacherSubjects(ctx context.Context, schoolID, teacherID uuid.UUID) ([]dto.SubjectResponse, error) {
	db := s.conn(ctx)
	var emp model.Employees
	if err := db.Where("school_id = ? AND id = ?", schoolID, teacherID).First(&emp).Error; err != nil {
		return nil, notFoundOr(err, "Guru tidak ditemukan")
	}
	var subs []model.Subject
	if err := db.Preload("GradeLevel").
		Where("school_id = ? AND id IN (SELECT subject_id FROM teacher_subjects WHERE school_id = ? AND employee_id = ?)", schoolID, schoolID, teacherID).
		Order("lower(name) ASC").
		Find(&subs).Error; err != nil {
		return nil, err
	}
	res := make([]dto.SubjectResponse, 0, len(subs))
	for i := range subs {
		res = append(res, *subjectToResponse(&subs[i]))
	}
	return res, nil
}

// AssignSubjectsToTeacher replaces the teacher's subject set (rows of subjects
// that stay are kept, including their academic_id).
func (s *SubjectService) AssignSubjectsToTeacher(ctx context.Context, schoolID, teacherID uuid.UUID, subjectIDs []uuid.UUID, isPrimary *dto.FlexBool) error {
	primary := primaryString(isPrimary)
	ids, err := uniqueIDs(subjectIDs, "subject_ids")
	if err != nil {
		return err
	}
	return s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		var emp model.Employees
		if err := tx.Where("school_id = ? AND id = ?", schoolID, teacherID).First(&emp).Error; err != nil {
			return notFoundOr(err, "Guru tidak ditemukan")
		}
		if err := verifyAllOwned(tx, &model.Subject{}, schoolID, ids, "Mata pelajaran tidak ditemukan di sekolah ini"); err != nil {
			return err
		}
		q := tx.Where("school_id = ? AND employee_id = ?", schoolID, teacherID)
		if len(ids) > 0 {
			q = q.Where("subject_id NOT IN ?", ids)
		}
		if err := q.Delete(&model.TeacherSubjects{}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.TeacherSubjects{}).Where("school_id = ? AND employee_id = ?", schoolID, teacherID).
			Update("is_primary", primary).Error; err != nil {
			return err
		}
		var have []uuid.UUID
		if err := tx.Model(&model.TeacherSubjects{}).Where("school_id = ? AND employee_id = ?", schoolID, teacherID).
			Distinct().Pluck("subject_id", &have).Error; err != nil {
			return err
		}
		exists := map[uuid.UUID]bool{}
		for _, id := range have {
			exists[id] = true
		}
		for _, sid := range ids {
			if exists[sid] {
				continue
			}
			row := model.TeacherSubjects{SchoolID: schoolID, EmployeeID: teacherID, SubjectID: sid, IsPrimary: primary}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// ==================== Class Subjects (Mapel Rombel) ====================

func classSubjectToResponse(r *model.ClassSubject, className string) dto.ClassSubjectResponse {
	var subResp *dto.SubjectResponse
	if r.Subject != nil {
		subResp = subjectToResponse(r.Subject)
	}
	var teacherShort *dto.HomeroomShort
	if r.PrimaryTeacher != nil {
		teacherShort = &dto.HomeroomShort{EmployeeID: r.PrimaryTeacher.ID, FullName: r.PrimaryTeacher.FullName}
	}
	return dto.ClassSubjectResponse{
		ID:               r.ID,
		SchoolID:         r.SchoolID,
		ClassID:          r.ClassID,
		SubjectID:        r.SubjectID,
		AcademicYearID:   r.AcademicYearID,
		SemesterID:       r.SemesterID,
		PrimaryTeacherID: r.PrimaryTeacherID,
		HoursPerWeek:     r.HoursPerWeek,
		Status:           r.Status,
		Subject:          subResp,
		Teacher:          teacherShort,
		ClassName:        className,
		CreatedAt:        r.CreatedAt,
		UpdatedAt:        r.UpdatedAt,
	}
}

func findClass(db *gorm.DB, schoolID, classID uuid.UUID) (*model.Classes, error) {
	var cls model.Classes
	if err := db.Where("school_id = ? AND id = ?", schoolID, classID).First(&cls).Error; err != nil {
		return nil, notFoundOr(err, "Kelas tidak ditemukan")
	}
	return &cls, nil
}

func (s *SubjectService) ListClassSubjects(ctx context.Context, schoolID, classID uuid.UUID) ([]dto.ClassSubjectResponse, error) {
	db := s.conn(ctx)
	cls, err := findClass(db, schoolID, classID)
	if err != nil {
		return nil, err
	}
	q := db.Preload("Subject").Preload("Subject.GradeLevel").Preload("PrimaryTeacher").
		Where("school_id = ? AND class_id = ?", schoolID, classID)
	if cls.AcademicYearID != nil {
		q = q.Where("academic_year_id = ?", *cls.AcademicYearID)
	}
	var rows []model.ClassSubject
	if err := q.Order("created_at ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	res := make([]dto.ClassSubjectResponse, 0, len(rows))
	for i := range rows {
		res = append(res, classSubjectToResponse(&rows[i], cls.DisplayName()))
	}
	return res, nil
}

func normalizeHours(h float64) (float64, error) {
	if h == 0 {
		return 2, nil
	}
	if h < 0 || h > maxHoursPerWeek {
		return 0, apperr.Validation(fmt.Sprintf("JP per minggu harus antara 1 dan %d", maxHoursPerWeek),
			map[string]string{"hours_per_week": fmt.Sprintf("JP per minggu harus antara 1 dan %d", maxHoursPerWeek)})
	}
	return h, nil
}

// ensureTeacherSubject maps teacher → subject when not mapped yet.
func ensureTeacherSubject(tx *gorm.DB, schoolID, teacherID, subjectID uuid.UUID) error {
	var n int64
	if err := tx.Model(&model.TeacherSubjects{}).
		Where("school_id = ? AND employee_id = ? AND subject_id = ?", schoolID, teacherID, subjectID).
		Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	return tx.Create(&model.TeacherSubjects{SchoolID: schoolID, EmployeeID: teacherID, SubjectID: subjectID, IsPrimary: "true"}).Error
}

func (s *SubjectService) AssignClassSubject(ctx context.Context, schoolID, classID uuid.UUID, req dto.ClassSubjectRequest) (*dto.ClassSubjectResponse, error) {
	hours, err := normalizeHours(req.HoursPerWeek)
	if err != nil {
		return nil, err
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = "active"
	}
	if len(status) > 50 {
		return nil, apperr.BadRequest("Status tidak valid")
	}

	var cls *model.Classes
	var cs model.ClassSubject
	err = s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		if cls, err = findClass(tx, schoolID, classID); err != nil {
			return err
		}
		if cls.AcademicYearID == nil {
			return apperr.BadRequest("Kelas belum memiliki tahun ajaran")
		}
		var sub model.Subject
		if err := tx.Where("school_id = ? AND id = ?", schoolID, req.SubjectID).First(&sub).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return invalidRef("Mata pelajaran tidak ditemukan di sekolah ini")
			}
			return err
		}
		if req.PrimaryTeacherID != nil {
			if err := verifyAllOwned(tx, &model.Employees{}, schoolID, []uuid.UUID{*req.PrimaryTeacherID}, "Guru tidak ditemukan di sekolah ini"); err != nil {
				return err
			}
		}
		var n int64
		if err := tx.Model(&model.ClassSubject{}).Where("school_id = ? AND class_id = ? AND subject_id = ? AND academic_year_id = ?",
			schoolID, classID, req.SubjectID, *cls.AcademicYearID).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return apperr.Conflict("DUPLICATE", "Mata pelajaran ini sudah terdaftar di kelas tersebut")
		}
		cs = model.ClassSubject{
			SchoolID:         schoolID,
			ClassID:          classID,
			SubjectID:        req.SubjectID,
			AcademicYearID:   *cls.AcademicYearID,
			PrimaryTeacherID: req.PrimaryTeacherID,
			HoursPerWeek:     hours,
			Status:           status,
		}
		if err := tx.Create(&cs).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return apperr.Conflict("DUPLICATE", "Mata pelajaran ini sudah terdaftar di kelas tersebut")
			}
			return err
		}
		if req.PrimaryTeacherID != nil {
			return ensureTeacherSubject(tx, schoolID, *req.PrimaryTeacherID, req.SubjectID)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	db := s.conn(ctx)
	if err := db.Preload("Subject").Preload("Subject.GradeLevel").Preload("PrimaryTeacher").First(&cs, "id = ?", cs.ID).Error; err != nil {
		return nil, err
	}
	resp := classSubjectToResponse(&cs, cls.DisplayName())
	return &resp, nil
}

// BulkAssignClassSubjects adds the subjects not yet assigned to the class
// (idempotent) and returns how many were created.
func (s *SubjectService) BulkAssignClassSubjects(ctx context.Context, schoolID, classID uuid.UUID, req dto.ClassSubjectBulkRequest) (int, error) {
	ids, err := uniqueIDs(req.SubjectIDs, "subject_ids")
	if err != nil {
		return 0, err
	}
	hours, err := normalizeHours(req.HoursPerWeek)
	if err != nil {
		return 0, err
	}
	createdCount := 0
	err = s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		cls, err := findClass(tx, schoolID, classID)
		if err != nil {
			return err
		}
		if cls.AcademicYearID == nil {
			return apperr.BadRequest("Kelas belum memiliki tahun ajaran")
		}
		if err := verifyAllOwned(tx, &model.Subject{}, schoolID, ids, "Mata pelajaran tidak ditemukan di sekolah ini"); err != nil {
			return err
		}
		var have []uuid.UUID
		if len(ids) > 0 {
			if err := tx.Model(&model.ClassSubject{}).
				Where("school_id = ? AND class_id = ? AND academic_year_id = ? AND subject_id IN ?", schoolID, classID, *cls.AcademicYearID, ids).
				Pluck("subject_id", &have).Error; err != nil {
				return err
			}
		}
		exists := map[uuid.UUID]bool{}
		for _, id := range have {
			exists[id] = true
		}
		for _, sid := range ids {
			if exists[sid] {
				continue
			}
			cs := model.ClassSubject{
				SchoolID:       schoolID,
				ClassID:        classID,
				SubjectID:      sid,
				AcademicYearID: *cls.AcademicYearID,
				HoursPerWeek:   hours,
				Status:         "active",
			}
			if err := tx.Create(&cs).Error; err != nil {
				return err
			}
			createdCount++
		}
		return nil
	})
	return createdCount, err
}

// DeleteClassSubject removes a class subject identified by its class_subjects
// row id (what the dashboard sends) or by the subject id; either way the row
// must belong to this class and tenant (else 404).
func (s *SubjectService) DeleteClassSubject(ctx context.Context, schoolID, classID, id uuid.UUID) error {
	return s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []model.ClassSubject
		if err := tx.Where("school_id = ? AND class_id = ? AND (id = ? OR subject_id = ?)", schoolID, classID, id, id).
			Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return apperr.NotFound("Mata pelajaran tidak terdaftar di kelas ini")
		}
		csIDs := make([]uuid.UUID, 0, len(rows))
		subIDs := make([]uuid.UUID, 0, len(rows))
		for _, r := range rows {
			csIDs = append(csIDs, r.ID)
			subIDs = append(subIDs, r.SubjectID)
		}
		var count int64
		if err := tx.Model(&model.TeacherSchedules{}).
			Where("school_id = ? AND class_id = ? AND (class_subject_id IN ? OR subject_id IN ?)", schoolID, classID, csIDs, subIDs).
			Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return apperr.Conflict("HAS_SCHEDULES", "Mata pelajaran tidak dapat dihapus dari kelas karena sudah memiliki jadwal KBM")
		}
		return tx.Where("school_id = ? AND id IN ?", schoolID, csIDs).Delete(&model.ClassSubject{}).Error
	})
}
