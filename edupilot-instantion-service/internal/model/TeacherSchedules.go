package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// TeacherSchedules is one weekly timetable slot. A schedule that is still
// referenced by teaching sessions (attendance history) is never hard-deleted:
// it is archived via DeletedAt (soft delete), so every GORM query on this model
// (lists, grids, conflict checks, teacher form, live monitor, attendance today)
// ignores it automatically while session history keeps its own class/subject ids.
type TeacherSchedules struct {
	ID               uuid.UUID      `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	SchoolID         uuid.UUID      `gorm:"type:uuid;not null;index" json:"school_id"`
	EmployeeID       uuid.UUID      `gorm:"type:uuid;not null;index" json:"employee_id"`
	SubjectID        uuid.UUID      `gorm:"type:uuid;not null;index" json:"subject_id"`
	ClassID          uuid.UUID      `gorm:"type:uuid;not null;index" json:"class_id"`
	AcademicYearID   *uuid.UUID     `gorm:"type:uuid;index" json:"academic_year_id"`
	SemesterID       *uuid.UUID     `gorm:"type:uuid;index" json:"semester_id"`
	ClassSubjectID   *uuid.UUID     `gorm:"type:uuid;index" json:"class_subject_id"`
	SchedulePeriodID *uuid.UUID     `gorm:"type:uuid;index" json:"schedule_period_id"`
	DayOfWeek        int            `gorm:"type:int;not null;index" json:"day_of_week"` // 1=Monday... 7=Sunday
	StartTime        string         `gorm:"type:varchar(5);not null" json:"start_time"` // "07:00"
	EndTime          string         `gorm:"type:varchar(5);not null" json:"end_time"`   // "07:45"
	Room             string         `gorm:"type:varchar(100)" json:"room"`
	Status           string         `gorm:"type:varchar(50);default:'active'" json:"status"`
	Notes            string         `gorm:"type:text" json:"notes"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"index" json:"-"`

	Teacher        *Employees      `gorm:"foreignKey:EmployeeID" json:"teacher,omitempty"`
	Subject        *Subject        `gorm:"foreignKey:SubjectID" json:"subject,omitempty"`
	Class          *Classes        `gorm:"foreignKey:ClassID" json:"class,omitempty"`
	SchedulePeriod *SchedulePeriod `gorm:"foreignKey:SchedulePeriodID" json:"schedule_period,omitempty"`
}
