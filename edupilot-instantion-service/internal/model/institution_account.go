package model

import (
	"time"

	"github.com/google/uuid"
)

type InstitutionAccount struct {
	ID        uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	SchoolID  uuid.UUID `gorm:"type:uuid;not null;index" json:"school_id"`
	School    *School   `gorm:"foreignKey:SchoolID" json:"school,omitempty"`
	UserID    uint      `gorm:"not null;uniqueIndex" json:"user_id"` // One user can only be linked to one school
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
