package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/edupilot-instantion-service/internal/apperr"
	"github.com/edupilot-instantion-service/internal/config"
	"github.com/edupilot-instantion-service/internal/dto"
	"github.com/edupilot-instantion-service/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	maxBulkMembers   = 500
	maxPromotionRows = 2000
)

type StudentHistoryService struct {
	db *gorm.DB
}

func NewStudentHistoryService(db *gorm.DB) *StudentHistoryService {
	return &StudentHistoryService{db: db}
}

func (s *StudentHistoryService) conn(ctx context.Context) *gorm.DB {
	if s.db != nil {
		return s.db.WithContext(ctx)
	}
	return config.DB.WithContext(ctx)
}

// ==================== Shared tx helpers ====================

// lockClass loads a tenant class FOR UPDATE (serialises capacity checks).
func lockClass(tx *gorm.DB, schoolID, classID uuid.UUID) (*model.Classes, error) {
	var cls model.Classes
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("school_id = ? AND id = ?", schoolID, classID).First(&cls).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperr.NotFound("Kelas tidak ditemukan")
		}
		return nil, err
	}
	if cls.AcademicYearID == nil || cls.GradeLevelID == nil {
		return nil, apperr.BadRequest("Kelas tidak memiliki tahun ajaran atau tingkat yang valid")
	}
	return &cls, nil
}

// activeMemberCount counts active (non-deleted) students placed in a class.
func activeMemberCount(tx *gorm.DB, schoolID, classID uuid.UUID) (int64, error) {
	var n int64
	err := tx.Raw(`SELECT COUNT(*) FROM student_class_histories h
		JOIN students s ON s.id = h.student_id AND s.deleted_at IS NULL
		WHERE h.school_id = ? AND h.class_id = ? AND h.status = 'active'`, schoolID, classID).Scan(&n).Error
	return n, err
}

var errClassFull = apperr.Conflict("CLASS_FULL", "Kapasitas kelas sudah penuh")

// placeInClassTx closes the student's active history (moved within the same
// academic year, promoted across years) and opens a new active row in cls.
// cls must have been loaded with lockClass in the same tx. The student must
// already be verified to belong to the tenant.
func placeInClassTx(tx *gorm.DB, schoolID, studentID uuid.UUID, cls *model.Classes, startDate, notes string, userID uint, force bool) error {
	if startDate == "" {
		startDate = today()
	}
	var prev model.StudentClassHistory
	hasPrev := true
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("school_id = ? AND student_id = ? AND status = 'active'", schoolID, studentID).First(&prev).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		hasPrev = false
	}
	if hasPrev && prev.ClassID == cls.ID {
		return apperr.Conflict("ALREADY_IN_CLASS", "Siswa sudah terdaftar di kelas ini")
	}
	if !force && cls.Capacity != nil && *cls.Capacity > 0 {
		n, err := activeMemberCount(tx, schoolID, cls.ID)
		if err != nil {
			return err
		}
		if n >= int64(*cls.Capacity) {
			return errClassFull
		}
	}
	if hasPrev {
		status := dto.HistoryMoved
		if prev.AcademicYearID != *cls.AcademicYearID {
			status = dto.HistoryPromoted
		}
		if err := tx.Model(&prev).Updates(map[string]interface{}{"status": status, "end_date": startDate}).Error; err != nil {
			return err
		}
	}
	return tx.Create(&model.StudentClassHistory{
		SchoolID:        schoolID,
		StudentID:       studentID,
		ClassID:         cls.ID,
		AcademicYearID:  *cls.AcademicYearID,
		GradeLevelID:    *cls.GradeLevelID,
		Status:          dto.HistoryActive,
		StartDate:       startDate,
		Notes:           notes,
		CreatedByUserID: userID,
	}).Error
}

// closeActiveHistoryTx closes the student's active history row (if any).
func closeActiveHistoryTx(tx *gorm.DB, schoolID, studentID uuid.UUID, status, endDate string) error {
	return tx.Model(&model.StudentClassHistory{}).
		Where("school_id = ? AND student_id = ? AND status = 'active'", schoolID, studentID).
		Updates(map[string]interface{}{"status": status, "end_date": endDate}).Error
}

// loadPlaceableStudent loads a tenant student that may be placed in a class.
func loadPlaceableStudent(tx *gorm.DB, schoolID, studentID uuid.UUID) (*model.Students, error) {
	var st model.Students
	if err := tx.Select("id, status, gender").Where("school_id = ? AND id = ?", schoolID, studentID).First(&st).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperr.NotFound("Siswa tidak ditemukan")
		}
		return nil, err
	}
	if st.Status != dto.StudentStatusActive {
		return nil, apperr.Conflict("STUDENT_NOT_ACTIVE", "Hanya siswa berstatus aktif yang dapat ditempatkan di kelas")
	}
	return &st, nil
}

func validOptionalDate(field, v string) error {
	fe := fieldErrors{}
	checkDate(fe, field, &v, false)
	return fe.err()
}

// ==================== Timeline / History ====================

func (s *StudentHistoryService) GetStudentHistory(ctx context.Context, schoolID, studentID uuid.UUID) ([]dto.ClassHistoryResponse, error) {
	db := s.conn(ctx)
	var n int64
	if err := db.Model(&model.Students{}).Where("school_id = ? AND id = ?", schoolID, studentID).Count(&n).Error; err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, apperr.NotFound("Data siswa tidak ditemukan")
	}

	var rows []struct {
		ID               uuid.UUID
		ClassID          uuid.UUID
		ClassName        string
		Alias            string
		GradeLevelName   string
		AcademicYearName string
		Status           string
		StartDate        string
		EndDate          string
		Notes            string
	}
	// Classes may be soft-deleted later; history still shows their name.
	if err := db.Raw(`
		SELECT h.id, h.class_id, COALESCE(c.class_name, '') AS class_name, COALESCE(c.alias, '') AS alias,
		       COALESCE(gl.name, '') AS grade_level_name, COALESCE(ay.academic_name, '') AS academic_year_name,
		       h.status, COALESCE(h.start_date, '') AS start_date, COALESCE(h.end_date, '') AS end_date, COALESCE(h.notes, '') AS notes
		FROM student_class_histories h
		LEFT JOIN classes c ON c.id = h.class_id
		LEFT JOIN grade_levels gl ON gl.id = h.grade_level_id
		LEFT JOIN academic_years ay ON ay.id = h.academic_year_id
		WHERE h.school_id = ? AND h.student_id = ?
		ORDER BY h.created_at DESC, h.start_date DESC`, schoolID, studentID).Scan(&rows).Error; err != nil {
		return nil, err
	}

	result := make([]dto.ClassHistoryResponse, 0, len(rows))
	for _, h := range rows {
		result = append(result, dto.ClassHistoryResponse{
			ID: h.ID,
			Class: &dto.CurrentClassShort{
				ClassID:          h.ClassID,
				DisplayName:      model.ClassDisplayName(h.ClassName, h.Alias),
				GradeLevelName:   h.GradeLevelName,
				AcademicYearName: h.AcademicYearName,
			},
			AcademicYearName: h.AcademicYearName,
			GradeLevelName:   h.GradeLevelName,
			Status:           h.Status,
			StartDate:        h.StartDate,
			EndDate:          h.EndDate,
			Notes:            h.Notes,
		})
	}
	return result, nil
}

// ==================== Placement (Single & Bulk) ====================

func (s *StudentHistoryService) PlaceStudent(ctx context.Context, schoolID, studentID uuid.UUID, userID uint, req dto.PlacementRequest) error {
	if err := validOptionalDate("start_date", req.StartDate); err != nil {
		return err
	}
	return s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := loadPlaceableStudent(tx, schoolID, studentID); err != nil {
			return err
		}
		cls, err := lockClass(tx, schoolID, req.ClassID)
		if err != nil {
			return err
		}
		return placeInClassTx(tx, schoolID, studentID, cls, strings.TrimSpace(req.StartDate), strings.TrimSpace(req.Notes), userID, req.Force)
	})
}

func dedupeIDs(ids []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]bool, len(ids))
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id == uuid.Nil || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func (s *StudentHistoryService) BulkAddClassMembers(ctx context.Context, schoolID, classID uuid.UUID, userID uint, req dto.MembersRequest) (*dto.MembersResultResponse, error) {
	ids := dedupeIDs(req.StudentIDs)
	if len(ids) == 0 {
		return nil, apperr.Validation("student_ids wajib diisi", map[string]string{"student_ids": "Pilih minimal satu siswa"})
	}
	if len(ids) > maxBulkMembers {
		return nil, apperr.Validation(fmt.Sprintf("Maksimal %d siswa per permintaan", maxBulkMembers),
			map[string]string{"student_ids": fmt.Sprintf("Maksimal %d siswa", maxBulkMembers)})
	}
	if err := validOptionalDate("start_date", req.StartDate); err != nil {
		return nil, err
	}

	res := &dto.MembersResultResponse{Skipped: []dto.MemberSkipped{}}
	err := s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		cls, err := lockClass(tx, schoolID, classID)
		if err != nil {
			return err
		}
		for _, sID := range ids {
			if _, err := loadPlaceableStudent(tx, schoolID, sID); err != nil {
				var ae *apperr.Error
				if errors.As(err, &ae) {
					res.Skipped = append(res.Skipped, dto.MemberSkipped{StudentID: sID, Reason: ae.Message})
					continue
				}
				return err
			}
			if err := placeInClassTx(tx, schoolID, sID, cls, strings.TrimSpace(req.StartDate), strings.TrimSpace(req.Notes), userID, false); err != nil {
				var ae *apperr.Error
				if errors.As(err, &ae) {
					res.Skipped = append(res.Skipped, dto.MemberSkipped{StudentID: sID, Reason: ae.Message})
					continue
				}
				return err
			}
			res.Placed++
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (s *StudentHistoryService) BulkRemoveClassMembers(ctx context.Context, schoolID, classID uuid.UUID, studentIDs []uuid.UUID) (int, error) {
	ids := dedupeIDs(studentIDs)
	if len(ids) == 0 {
		return 0, apperr.Validation("student_ids wajib diisi", map[string]string{"student_ids": "Pilih minimal satu siswa"})
	}
	if len(ids) > maxBulkMembers {
		return 0, apperr.Validation(fmt.Sprintf("Maksimal %d siswa per permintaan", maxBulkMembers),
			map[string]string{"student_ids": fmt.Sprintf("Maksimal %d siswa", maxBulkMembers)})
	}
	removed := 0
	err := s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := lockClass(tx, schoolID, classID); err != nil {
			return err
		}
		r := tx.Model(&model.StudentClassHistory{}).
			Where("school_id = ? AND class_id = ? AND status = 'active' AND student_id IN ?", schoolID, classID, ids).
			Updates(map[string]interface{}{"status": dto.HistoryWithdrawn, "end_date": today()})
		if r.Error != nil {
			return r.Error
		}
		removed = int(r.RowsAffected)
		return nil
	})
	return removed, err
}

// ==================== Promotions & Preview ====================

var reGradePrefix = regexp.MustCompile(`^([0-9]+|[IVXLC]+)`)

// SuggestPromotedClassName rewrites the grade prefix of a class name for the
// next grade: "7A"→"8A", "VII-A"→"VIII-A", "X TKJ 1"→"XI TKJ 1". The prefix may be
// arabic or roman and must equal the current grade number, or else start with
// the current short name ("A1"→"B1" for TK). Returns "" when nothing matches.
func SuggestPromotedClassName(name string, cur, next dto.GradeLevelShort) string {
	name = strings.TrimSpace(name)
	if m := reGradePrefix.FindString(name); m != "" {
		rest := name[len(m):]
		if isDigit(m[0]) {
			if m == strconv.Itoa(cur.Number) {
				return strconv.Itoa(next.Number) + rest
			}
		} else if m == toRoman(cur.Number) {
			return toRoman(next.Number) + rest
		}
	}
	if cur.ShortName != "" && next.ShortName != "" && strings.HasPrefix(name, cur.ShortName) {
		rest := name[len(cur.ShortName):]
		if rest == "" || !isAlnum(rest[0]) || isDigit(cur.ShortName[len(cur.ShortName)-1]) != isDigit(rest[0]) {
			return next.ShortName + rest
		}
	}
	return ""
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }
func isAlnum(b byte) bool {
	return isDigit(b) || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}

type targetClass struct {
	ID    uuid.UUID
	Name  string
	Alias string
	Grade int
}

// pickTargetClass chooses the suggested class in the next grade: same
// suggested name, else same alias (aliases are carried by copy), else the only
// class of that grade.
func pickTargetClass(targets []targetClass, nextGrade int, suggestedName, alias string) *targetClass {
	var inGrade []targetClass
	for _, t := range targets {
		if t.Grade == nextGrade {
			inGrade = append(inGrade, t)
		}
	}
	if suggestedName != "" {
		for i := range inGrade {
			if strings.EqualFold(strings.TrimSpace(inGrade[i].Name), suggestedName) {
				return &inGrade[i]
			}
		}
	}
	if alias = strings.TrimSpace(alias); alias != "" {
		for i := range inGrade {
			if strings.EqualFold(strings.TrimSpace(inGrade[i].Alias), alias) {
				return &inGrade[i]
			}
		}
	}
	if len(inGrade) == 1 {
		return &inGrade[0]
	}
	return nil
}

// nextGradeLevel returns the grade with the smallest number greater than cur.
func nextGradeLevel(levels []model.GradeLevel, cur int) *model.GradeLevel {
	var best *model.GradeLevel
	for i := range levels {
		if levels[i].Number > cur && (best == nil || levels[i].Number < best.Number) {
			best = &levels[i]
		}
	}
	return best
}

func (s *StudentHistoryService) PreviewPromotions(ctx context.Context, schoolID, fromClassID, toAcademicYearID uuid.UUID) (*dto.PromotionPreviewResponse, error) {
	db := s.conn(ctx)
	var fromClass model.Classes
	if err := db.Where("school_id = ? AND id = ?", schoolID, fromClassID).First(&fromClass).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperr.NotFound("Kelas asal tidak ditemukan")
		}
		return nil, err
	}
	var yearCount int64
	if err := db.Model(&model.AcademicYear{}).Where("school_id = ? AND id = ?", schoolID, toAcademicYearID).Count(&yearCount).Error; err != nil {
		return nil, err
	}
	if yearCount == 0 {
		return nil, apperr.NotFound("Tahun ajaran tujuan tidak ditemukan")
	}

	var levels []model.GradeLevel
	if err := db.Where("school_id = ?", schoolID).Find(&levels).Error; err != nil {
		return nil, err
	}
	byID := map[uuid.UUID]model.GradeLevel{}
	for _, l := range levels {
		byID[l.ID] = l
	}

	var targets []targetClass
	if err := db.Raw(`SELECT c.id, c.class_name AS name, COALESCE(c.alias, '') AS alias, COALESCE(gl.number, 0) AS grade
		FROM classes c LEFT JOIN grade_levels gl ON gl.id = c.grade_level_id
		WHERE c.school_id = ? AND c.academic_year_id = ? AND c.deleted_at IS NULL
		ORDER BY c.class_name`, schoolID, toAcademicYearID).Scan(&targets).Error; err != nil {
		return nil, err
	}

	action := "promote"
	var suggested *targetClass
	if fromClass.GradeLevelID != nil {
		if cur, ok := byID[*fromClass.GradeLevelID]; ok {
			if cur.IsFinal {
				action = "graduate"
			} else if next := nextGradeLevel(levels, cur.Number); next != nil {
				name := SuggestPromotedClassName(fromClass.Name,
					dto.GradeLevelShort{Number: cur.Number, ShortName: cur.ShortName},
					dto.GradeLevelShort{Number: next.Number, ShortName: next.ShortName})
				suggested = pickTargetClass(targets, next.Number, name, fromClass.Alias)
			}
		}
	}

	var students []struct {
		ID       uuid.UUID
		FullName string
		NISN     string
		NIS      string
		Gender   string
	}
	if err := db.Raw(`
		SELECT s.id, s.full_name, COALESCE(s.nisn, '') AS nisn, COALESCE(s.nis, '') AS nis, s.gender
		FROM student_class_histories sch
		JOIN students s ON s.id = sch.student_id AND s.deleted_at IS NULL
		WHERE sch.school_id = ? AND sch.class_id = ? AND sch.status = 'active'
		ORDER BY s.full_name ASC`, schoolID, fromClassID).Scan(&students).Error; err != nil {
		return nil, err
	}

	rows := make([]dto.PromotionPreviewRow, 0, len(students))
	for _, st := range students {
		row := dto.PromotionPreviewRow{
			StudentID:       st.ID,
			FullName:        st.FullName,
			NISN:            st.NISN,
			NIS:             st.NIS,
			Gender:          st.Gender,
			SuggestedAction: action,
		}
		if suggested != nil && action == "promote" {
			id := suggested.ID
			row.SuggestedToClassID = &id
		}
		rows = append(rows, row)
	}

	fromResp, err := NewAcademicService(s.db).GetClass(ctx, schoolID, fromClass.ID)
	if err != nil {
		return nil, err
	}
	return &dto.PromotionPreviewResponse{FromClass: fromResp, Rows: rows}, nil
}

func (s *StudentHistoryService) ExecutePromotions(ctx context.Context, schoolID uuid.UUID, userID uint, req dto.PromotionRequest) (*dto.PromotionResultResponse, error) {
	if len(req.Items) == 0 {
		return nil, apperr.Validation("items wajib diisi", map[string]string{"items": "Pilih minimal satu siswa"})
	}
	if len(req.Items) > maxPromotionRows {
		return nil, apperr.Validation(fmt.Sprintf("Maksimal %d siswa per proses", maxPromotionRows),
			map[string]string{"items": fmt.Sprintf("Maksimal %d siswa", maxPromotionRows)})
	}
	now := today()
	res := &dto.PromotionResultResponse{Errors: []dto.PromotionError{}}
	fail := func(id uuid.UUID, msg string) {
		res.Errors = append(res.Errors, dto.PromotionError{StudentID: id, Message: msg})
	}

	err := s.conn(ctx).Transaction(func(tx *gorm.DB) error {
		var toYear model.AcademicYear
		if err := tx.Where("school_id = ? AND id = ?", schoolID, req.ToAcademicYearID).First(&toYear).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return apperr.NotFound("Tahun ajaran tujuan tidak ditemukan")
			}
			return err
		}
		var levels []model.GradeLevel
		if err := tx.Where("school_id = ?", schoolID).Find(&levels).Error; err != nil {
			return err
		}
		grades := map[uuid.UUID]model.GradeLevel{}
		for _, l := range levels {
			grades[l.ID] = l
		}
		classes := map[uuid.UUID]*model.Classes{}
		seen := map[uuid.UUID]bool{}

		for _, item := range req.Items {
			if item.StudentID == uuid.Nil {
				continue
			}
			if seen[item.StudentID] {
				fail(item.StudentID, "Siswa muncul lebih dari sekali")
				continue
			}
			seen[item.StudentID] = true

			var st model.Students
			if err := tx.Select("id, status").Where("school_id = ? AND id = ?", schoolID, item.StudentID).First(&st).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					fail(item.StudentID, "Siswa tidak ditemukan")
					continue
				}
				return err
			}
			if st.Status != dto.StudentStatusActive {
				fail(item.StudentID, "Siswa tidak berstatus aktif")
				continue
			}

			var cur model.StudentClassHistory
			hasCur := true
			if err := tx.Where("school_id = ? AND student_id = ? AND status = 'active'", schoolID, st.ID).First(&cur).Error; err != nil {
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
				hasCur = false
			}
			curGrade, hasGrade := grades[cur.GradeLevelID]
			force := req.Force || item.Force

			switch item.Action {
			case "graduate":
				if !force && (!hasCur || !hasGrade || !curGrade.IsFinal) {
					fail(st.ID, "Kelulusan hanya untuk siswa di tingkat akhir (gunakan force untuk memaksa)")
					continue
				}
				if hasCur {
					if err := tx.Model(&cur).Updates(map[string]interface{}{"status": dto.HistoryGraduated, "end_date": now}).Error; err != nil {
						return err
					}
				}
				if err := tx.Model(&model.Students{}).Where("id = ?", st.ID).Updates(map[string]interface{}{
					"status": dto.StudentStatusGraduated, "exit_date": now, "exit_reason": "Lulus",
				}).Error; err != nil {
					return err
				}
				res.Graduated++

			case "promote", "retain":
				if !item.ToClassID.Valid {
					fail(st.ID, "Kelas tujuan wajib dipilih untuk naik/tinggal kelas")
					continue
				}
				if !hasCur || !hasGrade {
					fail(st.ID, "Siswa belum memiliki kelas aktif")
					continue
				}
				if cur.AcademicYearID == toYear.ID {
					fail(st.ID, "Siswa sudah berada di tahun ajaran tujuan")
					continue
				}
				toClass, ok := classes[item.ToClassID.UUID]
				if !ok {
					c, err := lockClass(tx, schoolID, item.ToClassID.UUID)
					if err != nil {
						var ae *apperr.Error
						if errors.As(err, &ae) {
							fail(st.ID, "Kelas tujuan tidak ditemukan")
							continue
						}
						return err
					}
					toClass = c
					classes[c.ID] = c
				}
				if *toClass.AcademicYearID != toYear.ID {
					fail(st.ID, "Kelas tujuan tidak berada di tahun ajaran tujuan")
					continue
				}
				toGrade, ok := grades[*toClass.GradeLevelID]
				if !ok {
					fail(st.ID, "Tingkat kelas tujuan tidak valid")
					continue
				}
				if item.Action == "promote" && toGrade.Number <= curGrade.Number {
					fail(st.ID, "Naik kelas harus ke tingkat yang lebih tinggi")
					continue
				}
				if item.Action == "retain" && toGrade.Number != curGrade.Number {
					fail(st.ID, "Tinggal kelas harus ke tingkat yang sama")
					continue
				}
				if !force && toClass.Capacity != nil && *toClass.Capacity > 0 {
					n, err := activeMemberCount(tx, schoolID, toClass.ID)
					if err != nil {
						return err
					}
					if n >= int64(*toClass.Capacity) {
						fail(st.ID, "Kapasitas kelas tujuan sudah penuh")
						continue
					}
				}
				closeStatus := dto.HistoryPromoted
				if item.Action == "retain" {
					closeStatus = dto.HistoryRetained
				}
				if err := tx.Model(&cur).Updates(map[string]interface{}{"status": closeStatus, "end_date": now}).Error; err != nil {
					return err
				}
				if err := tx.Create(&model.StudentClassHistory{
					SchoolID:        schoolID,
					StudentID:       st.ID,
					ClassID:         toClass.ID,
					AcademicYearID:  toYear.ID,
					GradeLevelID:    toGrade.ID,
					Status:          dto.HistoryActive,
					StartDate:       firstNonEmpty(toYear.StartDate, now),
					CreatedByUserID: userID,
				}).Error; err != nil {
					return err
				}
				if item.Action == "promote" {
					res.Promoted++
				} else {
					res.Retained++
				}
			default:
				fail(st.ID, "Aksi tidak dikenal: "+item.Action)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
