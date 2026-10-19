package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ClassSubject struct {
	ID               uuid.UUID      `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	SchoolID         uuid.UUID      `gorm:"type:uuid;not null;index" json:"school_id"`
	ClassID          uuid.UUID      `gorm:"type:uuid;not null;index" json:"class_id"`
	SubjectID        uuid.UUID      `gorm:"type:uuid;not null;index" json:"subject_id"`
	AcademicYearID   uuid.UUID      `gorm:"type:uuid;not null;index" json:"academic_year_id"`
	SemesterID       *uuid.UUID     `gorm:"type:uuid;index" json:"semester_id"`
	PrimaryTeacherID *uuid.UUID     `gorm:"type:uuid;index" json:"primary_teacher_id"`
	HoursPerWeek     float64        `gorm:"type:numeric(5,2);default:2" json:"hours_per_week"`
	Status           string         `gorm:"type:varchar(50);default:'active'" json:"status"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"index" json:"-"`

	Class          *Classes   `gorm:"foreignKey:ClassID" json:"class,omitempty"`
	Subject        *Subject   `gorm:"foreignKey:SubjectID" json:"subject,omitempty"`
	PrimaryTeacher *Employees `gorm:"foreignKey:PrimaryTeacherID" json:"primary_teacher,omitempty"`
}
