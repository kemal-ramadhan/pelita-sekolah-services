package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Classes (rombongan belajar), API_CONTRACT §8.5. Legacy semester_id /
// class_code / class_description are nullable now; the contract "name" is
// stored in the legacy class_name column. teacher_schedules.class_id keeps
// pointing at this table. Unique (school_id, academic_year_id, lower(name))
// among non-deleted rows via a guarded index in config.postMigrate.
type Classes struct {
	ID                 uuid.UUID      `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	SchoolID           uuid.UUID      `gorm:"type:uuid;not null;index" json:"-"`
	SemesterID         *uuid.UUID     `gorm:"type:uuid" json:"-"`
	ClassCode          string         `gorm:"type:varchar(255)" json:"-"`
	Name               string         `gorm:"column:class_name;type:varchar(255);not null" json:"name"`
	ClassDescription   string         `gorm:"type:varchar(255)" json:"-"`
	AcademicYearID     *uuid.UUID     `gorm:"type:uuid;index" json:"academic_year_id"`
	GradeLevelID       *uuid.UUID     `gorm:"type:uuid;index" json:"grade_level_id"`
	MajorID            *uuid.UUID     `gorm:"type:uuid;index" json:"major_id"`
	Alias              string         `gorm:"type:varchar(255)" json:"alias"`
	HomeroomEmployeeID *uuid.UUID     `gorm:"type:uuid" json:"homeroom_employee_id"`
	Capacity           *int           `json:"capacity"`
	Room               string         `gorm:"type:varchar(100)" json:"room"`
	Curriculum         string         `gorm:"type:varchar(20)" json:"curriculum"`
	IsActive           bool           `gorm:"not null;default:true" json:"is_active"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
	DeletedAt          gorm.DeletedAt `gorm:"index" json:"-"`
}

// ClassDisplayName is name + (alias ? " - " + alias : "") (API_CONTRACT §8.5).
func ClassDisplayName(name, alias string) string {
	if alias == "" {
		return name
	}
	return name + " - " + alias
}

// DisplayName returns formatted display name for the class.
func (c Classes) DisplayName() string {
	return ClassDisplayName(c.Name, c.Alias)
}
