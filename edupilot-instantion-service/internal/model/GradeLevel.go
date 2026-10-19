package model

import (
	"time"

	"github.com/google/uuid"
)

// GradeLevel (tingkat), API_CONTRACT §8.3. Unique (school_id, number) via a
// guarded index in config.postMigrate.
type GradeLevel struct {
	ID             uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	SchoolID       uuid.UUID `gorm:"type:uuid;not null;index" json:"-"`
	EducationStage string    `gorm:"type:varchar(20)" json:"education_stage"`
	Number         int       `gorm:"not null" json:"number"`
	Name           string    `gorm:"type:varchar(100);not null" json:"name"`
	ShortName      string    `gorm:"type:varchar(20)" json:"short_name"`
	IsFinal        bool      `gorm:"not null;default:false" json:"is_final"`
	SortOrder      int       `gorm:"not null;default:0" json:"sort_order"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}
