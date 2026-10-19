package model

import (
	"time"

	"github.com/google/uuid"
)

// StudentDocumentType, seeded per school (API_CONTRACT §8.7). is_required was a
// legacy varchar and is converted to boolean in config.preMigrate.
type StudentDocumentType struct {
	ID          uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	SchoolID    uuid.UUID `gorm:"type:uuid;not null;index" json:"-"`
	Name        string    `gorm:"type:varchar(255);not null" json:"name"`
	Description string    `gorm:"type:varchar(255)" json:"description"`
	IsRequired  bool      `gorm:"not null;default:false" json:"is_required"`
	SortOrder   int       `gorm:"not null;default:0" json:"sort_order"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
