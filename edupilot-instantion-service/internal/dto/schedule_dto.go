package dto

import (
	"time"

	"github.com/google/uuid"
)

// ==================== Subject DTOs ====================

type SubjectRequest struct {
	SubjectCode  string     `json:"subject_code" binding:"required"`
	Name         string     `json:"name" binding:"required"`
	ShortName    string     `json:"short_name"`
	SubjectType  string     `json:"subject_type"` // wajib_umum, peminatan, kejuruan, muatan_lokal, ekstrakurikuler
	Level        string     `json:"level"`
	GradeLevelID *uuid.UUID `json:"grade_level_id"`
	Description  string     `json:"description"`
	IsActive     *bool      `json:"is_active"`
}

type SubjectResponse struct {
	ID           uuid.UUID        `json:"id"`
	SchoolID     uuid.UUID        `json:"school_id"`
	SubjectCode  string           `json:"subject_code"`
	Name         string           `json:"name"`
	ShortName    string           `json:"short_name"`
	SubjectType  string           `json:"subject_type"`
	Level        string           `json:"level"`
	GradeLevelID *uuid.UUID       `json:"grade_level_id,omitempty"`
	GradeLevel   *GradeLevelShort `json:"grade_level,omitempty"`
	Description  string           `json:"description"`
	IsActive     bool             `json:"is_active"`
	TeacherCount int              `json:"teacher_count"`
	ClassCount   int              `json:"class_count"`
	CreatedAt    time.Time        `json:"created_at"`
	UpdatedAt    time.Time        `json:"updated_at"`
}

type SubjectTeacherItem struct {
	TeacherID    uuid.UUID `json:"teacher_id"`
	FullName     string    `json:"full_name"`
	EmployeeCode string    `json:"employee_code"`
	IsPrimary    string    `json:"is_primary"`
}

type SubjectClassItem struct {
	ClassID          uuid.UUID `json:"class_id"`
	ClassName        string    `json:"class_name"`
	DisplayName      string    `json:"display_name"`
	AcademicYearName string    `json:"academic_year_name"`
	TeacherName      string    `json:"teacher_name"`
	HoursPerWeek     float64   `json:"hours_per_week"`
}

type SubjectDetailResponse struct {
	SubjectResponse
	Teachers []SubjectTeacherItem `json:"teachers"`
	Classes  []SubjectClassItem   `json:"classes"`
}

type SubjectPresetRequest struct {
	Preset string `json:"preset" binding:"required"` // SD, SMP, SMA, SMK
}

type AssignTeachersToSubjectRequest struct {
	TeacherIDs []uuid.UUID `json:"teacher_ids" binding:"required"`
	// IsPrimary (optional, bool or "true"/"false", default true) is stored in
	// teacher_subjects.is_primary for every teacher in the list.
	IsPrimary *FlexBool `json:"is_primary"`
}

type SubjectTeacherAssignRequest = AssignTeachersToSubjectRequest

type AssignSubjectsToTeacherRequest struct {
	SubjectIDs []uuid.UUID `json:"subject_ids" binding:"required"`
	// IsPrimary (optional, default true): see AssignTeachersToSubjectRequest.
	IsPrimary *FlexBool `json:"is_primary"`
}

type TeacherSubjectAssignRequest = AssignSubjectsToTeacherRequest

// ==================== Class Subject DTOs ====================

type ClassSubjectRequest struct {
	SubjectID        uuid.UUID  `json:"subject_id" binding:"required"`
	PrimaryTeacherID *uuid.UUID `json:"primary_teacher_id"`
	HoursPerWeek     float64    `json:"hours_per_week"`
	Status           string     `json:"status"`
}

type ClassSubjectBulkRequest struct {
	SubjectIDs   []uuid.UUID `json:"subject_ids" binding:"required"`
	HoursPerWeek float64     `json:"hours_per_week"`
}

type ClassSubjectResponse struct {
	ID               uuid.UUID        `json:"id"`
	SchoolID         uuid.UUID        `json:"school_id"`
	ClassID          uuid.UUID        `json:"class_id"`
	SubjectID        uuid.UUID        `json:"subject_id"`
	AcademicYearID   uuid.UUID        `json:"academic_year_id"`
	SemesterID       *uuid.UUID       `json:"semester_id,omitempty"`
	PrimaryTeacherID *uuid.UUID       `json:"primary_teacher_id,omitempty"`
	HoursPerWeek     float64          `json:"hours_per_week"`
	Status           string           `json:"status"`
	Subject          *SubjectResponse `json:"subject,omitempty"`
	Teacher          *HomeroomShort   `json:"primary_teacher,omitempty"`
	ClassName        string           `json:"class_name,omitempty"`
	CreatedAt        time.Time        `json:"created_at"`
	UpdatedAt        time.Time        `json:"updated_at"`
}

// ==================== Schedule Period DTOs ====================

type SchedulePeriodRequest struct {
	Name        string `json:"name" binding:"required"`
	PeriodOrder int    `json:"period_order"`                  // >= 0; unique per day_of_week
	StartTime   string `json:"start_time" binding:"required"` // "07:00"
	EndTime     string `json:"end_time" binding:"required"`   // "07:45"
	DayOfWeek   int    `json:"day_of_week"`                   // 0=All, 1=Monday...
	IsBreak     bool   `json:"is_break"`
	IsActive    *bool  `json:"is_active"`
}

type SchedulePeriodResponse struct {
	ID          uuid.UUID `json:"id"`
	SchoolID    uuid.UUID `json:"school_id"`
	Name        string    `json:"name"`
	PeriodOrder int       `json:"period_order"`
	StartTime   string    `json:"start_time"`
	EndTime     string    `json:"end_time"`
	DayOfWeek   int       `json:"day_of_week"`
	IsBreak     bool      `json:"is_break"`
	IsActive    bool      `json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type SchedulePeriodPresetRequest struct {
	Preset string `json:"preset" binding:"required"` // "40_MIN", "45_MIN", "SD", "SMP", "SMA"
}

type SchedulePresetRequest = SchedulePeriodPresetRequest

// ==================== Schedule DTOs ====================

type ScheduleRequest struct {
	ClassID          uuid.UUID  `json:"class_id" binding:"required"`
	SubjectID        uuid.UUID  `json:"subject_id" binding:"required"`
	TeacherID        uuid.UUID  `json:"teacher_id" binding:"required"`
	AcademicYearID   *uuid.UUID `json:"academic_year_id"`
	SemesterID       *uuid.UUID `json:"semester_id"`
	ClassSubjectID   *uuid.UUID `json:"class_subject_id"`
	SchedulePeriodID *uuid.UUID `json:"schedule_period_id"`
	DayOfWeek        int        `json:"day_of_week" binding:"required"` // 1=Mon ... 7=Sun
	// StartTime/EndTime ("HH:MM") may be omitted when schedule_period_id is
	// given; they then default to the period's times.
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
	Room      string `json:"room"`
	Status    string `json:"status"`
	Notes     string `json:"notes"`
}

type ScheduleResponse struct {
	ID               uuid.UUID               `json:"id"`
	SchoolID         uuid.UUID               `json:"school_id"`
	ClassID          uuid.UUID               `json:"class_id"`
	SubjectID        uuid.UUID               `json:"subject_id"`
	TeacherID        uuid.UUID               `json:"teacher_id"`
	AcademicYearID   *uuid.UUID              `json:"academic_year_id,omitempty"`
	SemesterID       *uuid.UUID              `json:"semester_id,omitempty"`
	ClassSubjectID   *uuid.UUID              `json:"class_subject_id,omitempty"`
	SchedulePeriodID *uuid.UUID              `json:"schedule_period_id,omitempty"`
	DayOfWeek        int                     `json:"day_of_week"`
	StartTime        string                  `json:"start_time"`
	EndTime          string                  `json:"end_time"`
	Room             string                  `json:"room"`
	Status           string                  `json:"status"`
	Notes            string                  `json:"notes"`
	CreatedAt        time.Time               `json:"created_at"`
	UpdatedAt        time.Time               `json:"updated_at"`
	Teacher          *HomeroomShort          `json:"teacher,omitempty"`
	Subject          *SubjectResponse        `json:"subject,omitempty"`
	Class            *ClassSimpleShort       `json:"class,omitempty"`
	Period           *SchedulePeriodResponse `json:"period,omitempty"`
}

type ClassSimpleShort struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	DisplayName string    `json:"display_name"`
}

// Conflict Detail
type ScheduleConflictItem struct {
	ScheduleID  uuid.UUID `json:"schedule_id"`
	Type        string    `json:"type"` // "teacher", "class", "room"
	Description string    `json:"description"`
	DayOfWeek   int       `json:"day_of_week"`
	StartTime   string    `json:"start_time"`
	EndTime     string    `json:"end_time"`
	ClassName   string    `json:"class_name,omitempty"`
	SubjectName string    `json:"subject_name,omitempty"`
	TeacherName string    `json:"teacher_name,omitempty"`
	Room        string    `json:"room,omitempty"`
}

type ScheduleConflictCheckResponse struct {
	HasConflict bool                   `json:"has_conflict"`
	Conflicts   []ScheduleConflictItem `json:"conflicts"`
	// Warnings are non-blocking notes (teacher not mapped to the subject,
	// subject not assigned to the class, weekly JP exceeded). Always an array.
	Warnings []string `json:"warnings"`
}

// SubjectHoursItem compares the planned weekly JP of a class subject
// (class_subjects.hours_per_week) with the JP actually scheduled.
type SubjectHoursItem struct {
	ClassSubjectID uuid.UUID `json:"class_subject_id"`
	SubjectID      uuid.UUID `json:"subject_id"`
	SubjectName    string    `json:"subject_name"`
	HoursPerWeek   float64   `json:"hours_per_week"`
	ScheduledJP    int       `json:"scheduled_jp"`
	Status         string    `json:"status"` // "ok" | "under" | "over"
}

// Timetable Grid Response
type TimetableDay struct {
	DayOfWeek int                `json:"day_of_week"`
	DayName   string             `json:"day_name"`
	Schedules []ScheduleResponse `json:"schedules"`
}

type TimetableGridResponse struct {
	AcademicYearID   *uuid.UUID               `json:"academic_year_id"`
	AcademicYearName string                   `json:"academic_year_name"`
	SemesterID       *uuid.UUID               `json:"semester_id"`
	SemesterName     string                   `json:"semester_name"`
	ClassID          *uuid.UUID               `json:"class_id,omitempty"`
	ClassName        string                   `json:"class_name,omitempty"`
	SubjectID        *uuid.UUID               `json:"subject_id,omitempty"`
	SubjectName      string                   `json:"subject_name,omitempty"`
	TeacherID        *uuid.UUID               `json:"teacher_id,omitempty"`
	TeacherName      string                   `json:"teacher_name,omitempty"`
	Periods          []SchedulePeriodResponse `json:"periods"`
	Days             []TimetableDay           `json:"days"`
	// HoursSummary is only present for a class grid (class_id given).
	HoursSummary []SubjectHoursItem `json:"hours_summary,omitempty"`
}
