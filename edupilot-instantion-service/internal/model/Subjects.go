package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Subject struct {
	ID           uuid.UUID      `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	SchoolID     uuid.UUID      `gorm:"type:uuid;not null;index" json:"school_id"`
	SubjectCode  string         `gorm:"type:varchar(100);not null" json:"subject_code"`
	Name         string         `gorm:"type:varchar(255);not null" json:"name"`
	ShortName    string         `gorm:"type:varchar(50)" json:"short_name"`
	SubjectType  string         `gorm:"type:varchar(50);default:'wajib_umum'" json:"subject_type"`
	Level        string         `gorm:"type:varchar(100)" json:"level"`
	GradeLevelID *uuid.UUID     `gorm:"type:uuid;index" json:"grade_level_id"`
	Description  string         `gorm:"type:text" json:"description"`
	IsActive     bool           `gorm:"default:true" json:"is_active"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`

	GradeLevel *GradeLevel `gorm:"foreignKey:GradeLevelID" json:"grade_level,omitempty"`
}
