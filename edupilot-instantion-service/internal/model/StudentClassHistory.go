package model

import (
	"time"

	"github.com/google/uuid"
)

// StudentClassHistory records class membership (API_CONTRACT §8.8). At most one
// row per student has status "active" (partial unique index, config.postMigrate).
type StudentClassHistory struct {
	ID              uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	SchoolID        uuid.UUID `gorm:"type:uuid;not null;index" json:"-"`
	StudentID       uuid.UUID `gorm:"type:uuid;not null;index" json:"student_id"`
	ClassID         uuid.UUID `gorm:"type:uuid;not null;index:idx_sch_class_status" json:"class_id"`
	AcademicYearID  uuid.UUID `gorm:"type:uuid;not null;index" json:"academic_year_id"`
	GradeLevelID    uuid.UUID `gorm:"type:uuid;not null" json:"grade_level_id"`
	Status          string    `gorm:"type:varchar(20);not null;index:idx_sch_class_status" json:"status"`
	StartDate       string    `gorm:"type:varchar(10)" json:"start_date"`
	EndDate         string    `gorm:"type:varchar(10)" json:"end_date"`
	Notes           string    `gorm:"type:text" json:"notes"`
	CreatedByUserID uint      `json:"created_by_user_id"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}
