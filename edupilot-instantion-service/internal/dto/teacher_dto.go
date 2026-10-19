package dto

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/edupilot-instantion-service/internal/model"
	"github.com/google/uuid"
)

const (
	DateLayout          = "2006-01-02"
	maxTeacherRelations = 100
)

var hhmmPattern = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

// ValidHHMM reports whether s is a 24h "HH:MM" time.
func ValidHHMM(s string) bool { return hhmmPattern.MatchString(s) }

// ValidDayOfWeek reports whether d is 1 (Monday) .. 7 (Sunday).
func ValidDayOfWeek(d int) bool { return d >= 1 && d <= 7 }

// ValidDate reports whether s is a YYYY-MM-DD calendar date.
func ValidDate(s string) bool {
	_, err := time.Parse(DateLayout, s)
	return err == nil
}

type TeacherSubjectReq struct {
	SubjectID  OptionalUUID `json:"subject_id"`
	AcademicID OptionalUUID `json:"academic_id"`
	IsPrimary  FlexBool     `json:"is_primary"`
}

type TeacherAssignmentReq struct {
	EmployeePositionID OptionalUUID `json:"employee_position_id"`
	StartDate          string       `json:"start_date"`
	EndDate            string       `json:"end_date"`
	IsActive           *FlexBool    `json:"is_active"`
}

type TeacherScheduleReq struct {
	SubjectID OptionalUUID `json:"subject_id"`
	ClassID   OptionalUUID `json:"class_id"`
	DayOfWeek int          `json:"day_of_week"`
	// PeriodID (optional) is a jam pelajaran (schedule_periods) of the school.
	// When given, start_time/end_time are taken from the period (the service
	// fills them); otherwise start_time/end_time are required.
	PeriodID  OptionalUUID `json:"period_id"`
	StartTime string       `json:"start_time"`
	EndTime   string       `json:"end_time"`
}

// TeacherRequest is the body of POST/PUT /instantion/school/teachers.
// On PUT, a nil (absent) subjects/assignments/schedules array keeps existing
// rows; a present array (even empty) replaces them.
type TeacherRequest struct {
	FullName       string                 `json:"full_name" binding:"required,max=255"`
	EmployeeCode   string                 `json:"employee_code" binding:"required,max=255"`
	Email          *string                `json:"email" binding:"omitempty,max=255"`
	Phone          *string                `json:"phone" binding:"omitempty,max=50"`
	NumberType     string                 `json:"number_type" binding:"max=255"`
	Number         string                 `json:"number" binding:"max=255"`
	EmployeeStatus string                 `json:"employee_status" binding:"max=255"`
	JoinDate       string                 `json:"join_date" binding:"max=10"`
	Status         string                 `json:"status" binding:"max=255"`
	IsCounselor    *bool                  `json:"is_counselor"`
	Subjects       []TeacherSubjectReq    `json:"subjects"`
	Assignments    []TeacherAssignmentReq `json:"assignments"`
	Schedules      []TeacherScheduleReq   `json:"schedules"`
	// SendInvite (POST only, default true): create + invite the teacher's auth
	// account when email is present (API_CONTRACT §7.3). Ignored on PUT.
	SendInvite *bool `json:"send_invite"`
}

// WantsInvite reports whether a create request should provision a teacher account.
func (r *TeacherRequest) WantsInvite() bool {
	return r.Email != nil && *r.Email != "" && (r.SendInvite == nil || *r.SendInvite)
}

// Normalize trims fields, drops completely blank relation rows and validates
// everything that does not need the database. Returns "" when valid.
func (r *TeacherRequest) Normalize() string {
	r.FullName = strings.TrimSpace(r.FullName)
	r.EmployeeCode = strings.TrimSpace(r.EmployeeCode)
	r.Email = TrimPtr(r.Email)
	r.Phone = TrimPtr(r.Phone)
	r.NumberType = strings.TrimSpace(r.NumberType)
	r.Number = strings.TrimSpace(r.Number)
	r.EmployeeStatus = strings.TrimSpace(r.EmployeeStatus)
	r.JoinDate = strings.TrimSpace(r.JoinDate)
	r.Status = strings.TrimSpace(r.Status)

	if r.FullName == "" {
		return "full_name wajib diisi"
	}
	if r.EmployeeCode == "" {
		return "employee_code wajib diisi"
	}
	if r.Email != nil && !ValidEmail(*r.Email) {
		return "email harus berupa email yang valid"
	}
	if r.JoinDate != "" && !ValidDate(r.JoinDate) {
		return "join_date harus berformat YYYY-MM-DD"
	}

	if r.Subjects != nil {
		out := make([]TeacherSubjectReq, 0, len(r.Subjects))
		seen := map[string]bool{}
		for i, s := range r.Subjects {
			if !s.SubjectID.Valid && !s.AcademicID.Valid {
				continue
			}
			if !s.SubjectID.Valid {
				return fmt.Sprintf("subjects[%d].subject_id wajib diisi", i)
			}
			key := s.SubjectID.UUID.String() + "|" + s.AcademicID.UUID.String()
			if seen[key] {
				return fmt.Sprintf("subjects[%d] duplikat", i)
			}
			seen[key] = true
			out = append(out, s)
		}
		r.Subjects = out
	}

	if r.Assignments != nil {
		out := make([]TeacherAssignmentReq, 0, len(r.Assignments))
		for i, a := range r.Assignments {
			a.StartDate = strings.TrimSpace(a.StartDate)
			a.EndDate = strings.TrimSpace(a.EndDate)
			if !a.EmployeePositionID.Valid && a.StartDate == "" && a.EndDate == "" {
				continue
			}
			if !a.EmployeePositionID.Valid {
				return fmt.Sprintf("assignments[%d].employee_position_id wajib diisi", i)
			}
			if !ValidDate(a.StartDate) {
				return fmt.Sprintf("assignments[%d].start_date harus berformat YYYY-MM-DD", i)
			}
			if a.EndDate != "" {
				if !ValidDate(a.EndDate) {
					return fmt.Sprintf("assignments[%d].end_date harus berformat YYYY-MM-DD", i)
				}
				if a.EndDate < a.StartDate {
					return fmt.Sprintf("assignments[%d].end_date tidak boleh sebelum start_date", i)
				}
			}
			out = append(out, a)
		}
		r.Assignments = out
	}

	if r.Schedules != nil {
		out := make([]TeacherScheduleReq, 0, len(r.Schedules))
		for i, s := range r.Schedules {
			s.StartTime = strings.TrimSpace(s.StartTime)
			s.EndTime = strings.TrimSpace(s.EndTime)
			if !s.SubjectID.Valid && !s.ClassID.Valid && !s.PeriodID.Valid && s.DayOfWeek == 0 && s.StartTime == "" && s.EndTime == "" {
				continue
			}
			if msg := ValidateSchedule(s); msg != "" {
				return fmt.Sprintf("schedules[%d].%s", i, msg)
			}
			out = append(out, s)
		}
		r.Schedules = out
	}

	if len(r.Subjects) > maxTeacherRelations || len(r.Assignments) > maxTeacherRelations || len(r.Schedules) > maxTeacherRelations {
		return fmt.Sprintf("maksimal %d item untuk subjects/assignments/schedules", maxTeacherRelations)
	}
	return ""
}

// ValidateSchedule checks a single schedule entry; returns "" when valid.
func ValidateSchedule(s TeacherScheduleReq) string {
	if !s.SubjectID.Valid {
		return "subject_id wajib diisi"
	}
	if !s.ClassID.Valid {
		return "class_id wajib diisi"
	}
	if !ValidDayOfWeek(s.DayOfWeek) {
		return "day_of_week harus antara 1 dan 7"
	}
	if s.PeriodID.Valid {
		// Times come from the period (resolved against the database).
		return ""
	}
	if !ValidHHMM(s.StartTime) {
		return "start_time harus berformat HH:MM"
	}
	if !ValidHHMM(s.EndTime) {
		return "end_time harus berformat HH:MM"
	}
	if s.EndTime <= s.StartTime {
		return "end_time harus setelah start_time"
	}
	return ""
}

// TeacherScheduleItem is a schedule row of GET /teachers/:id; period_id
// mirrors schedule_period_id (the field name used by the teacher form).
type TeacherScheduleItem struct {
	model.TeacherSchedules
	PeriodID *uuid.UUID `json:"period_id"`
}

// PositionShort is the jabatan of an assignment (null when it was deleted).
type PositionShort struct {
	ID           uuid.UUID `json:"id"`
	PositionCode string    `json:"position_code"`
	Position     string    `json:"position"`
	Category     string    `json:"category"`
}

// TeacherAssignmentItem is an assignment row of GET /teachers/:id.
type TeacherAssignmentItem struct {
	model.EmployeeAssignments
	Position *PositionShort `json:"position"`
}

// TeacherDetail is returned by GET /teachers/:id: Teacher + its relations.
type TeacherDetail struct {
	model.Employees
	Subjects    []model.TeacherSubjects `json:"subjects"`
	Assignments []TeacherAssignmentItem `json:"assignments"`
	Schedules   []TeacherScheduleItem   `json:"schedules"`
}

type ImportRowError struct {
	Row     int    `json:"row"`
	Message string `json:"message"`
}

type ImportResult struct {
	Total   int              `json:"total"`
	Created int              `json:"created"`
	Failed  int              `json:"failed"`
	DryRun  bool             `json:"dry_run"`
	Errors  []ImportRowError `json:"errors"`
}
