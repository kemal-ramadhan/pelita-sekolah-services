package service

import (
	"context"

	"sort"
	"strings"
	"unicode/utf8"

	"github.com/edupilot-instantion-service/internal/apperr"
	"github.com/edupilot-instantion-service/internal/config"
	"github.com/edupilot-instantion-service/internal/model"
	"github.com/google/uuid"
)

// Internal routes used by edupilot-assesment (API_CONTRACT §13.1). Read-only
// except Notify; never marks activated_at or sends account notifications.

// Principal kinds returned by GET /internal/assessment/principal.
const (
	PrincipalManager   = "manager"
	PrincipalCounselor = "counselor"
	PrincipalTeacher   = "teacher"
	PrincipalStudent   = "student"
	PrincipalNone      = "none"
)

// Roles as carried in the JWT (mirrors middleware constants; service must not import middleware).
const (
	roleInstitution      = "institution"
	roleCustomer         = "customer"
	roleTeacher          = "teacher"
	customerRoleStudents = "students"
)

const (
	maxAssessmentStudents = 5000
	maxNotifyRecipients   = 5000
)

// AssessmentPrincipal is the response of GET /internal/assessment/principal.
// Absent ids are omitted (never "").
type AssessmentPrincipal struct {
	Kind          string     `json:"kind"`
	SchoolID      *uuid.UUID `json:"school_id"`
	SchoolName    string     `json:"school_name"`
	SchoolActive  bool       `json:"school_active"`
	EmployeeID    *uuid.UUID `json:"employee_id,omitempty"`
	IsCounselor   bool       `json:"is_counselor"`
	StudentID     *uuid.UUID `json:"student_id,omitempty"`
	StudentName   string     `json:"student_name,omitempty"`
	StudentStatus string     `json:"student_status,omitempty"`
	ClassID       *uuid.UUID `json:"class_id,omitempty"`
	ClassName     string     `json:"class_name,omitempty"`
	DisplayName   string     `json:"display_name,omitempty"`
}

// PrincipalLookup selects which resolver applies to a (role, customer_role) pair.
// Pure function so the mapping is unit-testable.
func PrincipalLookup(role, customerRole string) string {
	switch {
	case role == roleCustomer && customerRole == customerRoleStudents:
		return PrincipalStudent
	case role == roleInstitution || role == roleCustomer:
		return PrincipalManager
	case role == roleTeacher:
		return PrincipalTeacher
	default:
		return PrincipalNone
	}
}

func withSchool(p *AssessmentPrincipal, s *model.School) {
	id := s.ID
	p.SchoolID = &id
	p.SchoolName = s.Name
	p.SchoolActive = s.IsActive
}

// ResolveAssessmentPrincipal maps an auth user to its EduPilot identity.
func ResolveAssessmentPrincipal(ctx context.Context, userID uint, role, customerRole string) (*AssessmentPrincipal, error) {
	none := &AssessmentPrincipal{Kind: PrincipalNone}
	switch PrincipalLookup(role, customerRole) {
	case PrincipalStudent:
		st, school, err := ResolveStudentPrincipal(ctx, userID)
		if err != nil {
			return nil, err
		}
		if st == nil || school == nil {
			return none, nil
		}
		sid := st.ID
		p := &AssessmentPrincipal{Kind: PrincipalStudent, StudentID: &sid, StudentName: st.FullName,
			StudentStatus: st.Status, DisplayName: st.FullName}
		withSchool(p, school)
		classes, err := activeClassesFor(config.DB.WithContext(ctx), school.ID, []uuid.UUID{st.ID})
		if err != nil {
			return nil, err
		}
		if cl := classes[st.ID]; cl != nil {
			cid := cl.ClassID
			p.ClassID = &cid
			p.ClassName = cl.DisplayName
		}
		return p, nil
	case PrincipalManager:
		school, err := ResolveTenantSchool(ctx, userID)
		if err != nil {
			return nil, err
		}
		if school == nil {
			return none, nil
		}
		p := &AssessmentPrincipal{Kind: PrincipalManager, DisplayName: school.Name}
		withSchool(p, school)
		return p, nil
	case PrincipalTeacher:
		school, emp, err := ResolveTeacherTenant(ctx, userID)
		if err != nil {
			return nil, err
		}
		if school == nil || emp == nil {
			return none, nil
		}
		eid := emp.ID
		kind := PrincipalTeacher
		if emp.IsCounselor {
			kind = PrincipalCounselor
		}
		p := &AssessmentPrincipal{Kind: kind, EmployeeID: &eid, IsCounselor: emp.IsCounselor, DisplayName: emp.FullName}
		withSchool(p, school)
		return p, nil
	}
	return none, nil
}

// AssessmentStudentsRequest is the body of POST /internal/assessment/students.
type AssessmentStudentsRequest struct {
	SchoolID   uuid.UUID   `json:"school_id" binding:"required"`
	ClassIDs   []uuid.UUID `json:"class_ids"`
	StudentIDs []uuid.UUID `json:"student_ids"`
}

type AssessmentStudent struct {
	StudentID uuid.UUID  `json:"student_id"`
	UserID    *uint      `json:"user_id,omitempty"`
	FullName  string     `json:"full_name"`
	NIS       string     `json:"nis"`
	NISN      string     `json:"nisn"`
	Gender    string     `json:"gender"`
	ClassID   *uuid.UUID `json:"class_id,omitempty"`
	ClassName string     `json:"class_name,omitempty"`
}

// ListAssessmentStudents returns active, non-deleted students of the school,
// optionally limited to the given classes (active history) and/or student ids.
func ListAssessmentStudents(ctx context.Context, req AssessmentStudentsRequest) ([]AssessmentStudent, error) {
	q := config.DB.WithContext(ctx).Table("students s").
		Joins("LEFT JOIN student_class_histories h ON h.student_id = s.id AND h.school_id = s.school_id AND h.status = 'active'").
		Joins("LEFT JOIN classes c ON c.id = h.class_id AND c.deleted_at IS NULL").
		Where("s.school_id = ? AND s.status = 'active' AND s.deleted_at IS NULL", req.SchoolID)
	classIDs, studentIDs := nonNilUUIDs(req.ClassIDs), nonNilUUIDs(req.StudentIDs)
	switch {
	case len(classIDs) > 0 && len(studentIDs) > 0:
		q = q.Where("(h.class_id IN ? OR s.id IN ?)", classIDs, studentIDs)
	case len(classIDs) > 0:
		q = q.Where("h.class_id IN ?", classIDs)
	case len(studentIDs) > 0:
		q = q.Where("s.id IN ?", studentIDs)
	}
	var rows []struct {
		ID        uuid.UUID
		UserID    *uint
		FullName  string
		NIS       string
		NISN      string
		Gender    string
		ClassID   *uuid.UUID
		ClassName string
		Alias     string
	}
	err := q.Select(`s.id, s.user_id, s.full_name, COALESCE(s.nis, '') AS nis, COALESCE(s.nisn, '') AS nisn,
		COALESCE(s.gender, '') AS gender, c.id AS class_id, COALESCE(c.class_name, '') AS class_name, COALESCE(c.alias, '') AS alias`).
		Order("c.class_name ASC NULLS LAST, s.full_name ASC, s.id ASC").
		Limit(maxAssessmentStudents).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]AssessmentStudent, 0, len(rows))
	seen := make(map[uuid.UUID]bool, len(rows))
	for _, r := range rows {
		if seen[r.ID] {
			continue
		}
		seen[r.ID] = true
		it := AssessmentStudent{StudentID: r.ID, UserID: r.UserID, FullName: r.FullName, NIS: r.NIS, NISN: r.NISN, Gender: r.Gender}
		if r.ClassID != nil {
			it.ClassID = r.ClassID
			it.ClassName = model.ClassDisplayName(r.ClassName, r.Alias)
		}
		out = append(out, it)
	}
	return out, nil
}

func nonNilUUIDs(ids []uuid.UUID) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id != uuid.Nil {
			out = append(out, id)
		}
	}
	return out
}

type AssessmentClass struct {
	ID               uuid.UUID `json:"id"`
	Name             string    `json:"name"`
	GradeLevel       string    `json:"grade_level,omitempty"`
	AcademicYearName string    `json:"academic_year_name,omitempty"`
	IsActiveYear     bool      `json:"is_active_year"`
	StudentCount     int64     `json:"student_count"`
}

// ListAssessmentClasses: non-deleted classes of the school, active academic year first.
func ListAssessmentClasses(ctx context.Context, schoolID uuid.UUID) ([]AssessmentClass, error) {
	var rows []struct {
		ID               uuid.UUID
		ClassName        string
		Alias            string
		GradeLevel       string
		AcademicYearName string
		IsActiveYear     bool
		StudentCount     int64
	}
	err := config.DB.WithContext(ctx).Raw(`
		SELECT c.id, c.class_name, COALESCE(c.alias, '') AS alias,
		       COALESCE(gl.name, '') AS grade_level,
		       COALESCE(ay.academic_name, '') AS academic_year_name,
		       COALESCE(ay.is_active, false) AS is_active_year,
		       (SELECT COUNT(*) FROM student_class_histories h
		          JOIN students s ON s.id = h.student_id AND s.deleted_at IS NULL AND s.status = 'active'
		         WHERE h.class_id = c.id AND h.school_id = c.school_id AND h.status = 'active') AS student_count
		FROM classes c
		LEFT JOIN grade_levels gl ON gl.id = c.grade_level_id
		LEFT JOIN academic_years ay ON ay.id = c.academic_year_id
		WHERE c.school_id = ? AND c.deleted_at IS NULL
		ORDER BY COALESCE(ay.is_active, false) DESC, COALESCE(gl.number, 0) ASC, c.class_name ASC, c.id ASC`, schoolID).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]AssessmentClass, 0, len(rows))
	for _, r := range rows {
		out = append(out, AssessmentClass{ID: r.ID, Name: model.ClassDisplayName(r.ClassName, r.Alias), GradeLevel: r.GradeLevel,
			AcademicYearName: r.AcademicYearName, IsActiveYear: r.IsActiveYear, StudentCount: r.StudentCount})
	}
	return out, nil
}

// AssessmentNotifyRequest is the body of POST /internal/assessment/notify.
type AssessmentNotifyRequest struct {
	SchoolID     uuid.UUID      `json:"school_id" binding:"required"`
	UserIDs      []uint         `json:"user_ids"`
	ToCounselors bool           `json:"to_counselors"`
	Type         string         `json:"type"`
	Title        string         `json:"title"`
	Body         string         `json:"body"`
	Data         map[string]any `json:"data"`
}

// Validate trims and checks the notification fields against the column sizes.
func (r *AssessmentNotifyRequest) Validate() error {
	r.Type, r.Title, r.Body = strings.TrimSpace(r.Type), strings.TrimSpace(r.Title), strings.TrimSpace(r.Body)
	switch {
	case r.SchoolID == uuid.Nil:
		return apperr.BadRequest("school_id wajib diisi")
	case r.Type == "" || utf8.RuneCountInString(r.Type) > 50:
		return apperr.BadRequest("type wajib diisi (maksimal 50 karakter)")
	case r.Title == "" || utf8.RuneCountInString(r.Title) > 255:
		return apperr.BadRequest("title wajib diisi (maksimal 255 karakter)")
	case r.Body == "" || utf8.RuneCountInString(r.Body) > 2000:
		return apperr.BadRequest("body wajib diisi (maksimal 2000 karakter)")
	case len(r.UserIDs) > maxNotifyRecipients:
		return apperr.BadRequest("user_ids terlalu banyak")
	}
	return nil
}

// NotifyRecipients merges explicit user ids (>0) and counselor ids, deduplicated, sorted.
func NotifyRecipients(userIDs, counselorIDs []uint) []uint {
	set := map[uint]bool{}
	for _, list := range [][]uint{userIDs, counselorIDs} {
		for _, id := range list {
			if id > 0 {
				set[id] = true
			}
		}
	}
	out := make([]uint, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// AssessmentNotify creates in-app notifications (§10.5 table); returns the recipient count.
func AssessmentNotify(ctx context.Context, req AssessmentNotifyRequest) (int, error) {
	db := config.DB.WithContext(ctx)
	var n int64
	if err := db.Model(&model.School{}).Where("id = ?", req.SchoolID).Count(&n).Error; err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, errSchoolNotFound
	}
	var counselors []uint
	if req.ToCounselors {
		if err := db.Model(&model.Employees{}).
			Where("school_id = ? AND is_counselor = true AND user_id IS NOT NULL", req.SchoolID).
			Pluck("user_id", &counselors).Error; err != nil {
			return 0, err
		}
	}
	recipients := NotifyRecipients(req.UserIDs, counselors)
	schoolID := req.SchoolID
	for _, uid := range recipients {
		Notify(ctx, NotificationInput{UserID: uid, SchoolID: &schoolID, Type: req.Type, Title: req.Title, Body: req.Body, Data: req.Data})
	}
	return len(recipients), nil
}
