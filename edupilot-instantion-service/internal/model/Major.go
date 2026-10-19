package model

import (
	"time"

	"github.com/google/uuid"
)

// Major (jurusan / peminatan / program keahlian), API_CONTRACT §8.4.
type Major struct {
	ID          uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	SchoolID    uuid.UUID `gorm:"type:uuid;not null;index" json:"-"`
	Code        string    `gorm:"type:varchar(50)" json:"code"`
	Name        string    `gorm:"type:varchar(255);not null" json:"name"`
	Description string    `gorm:"type:text" json:"description"`
	IsActive    bool      `gorm:"not null;default:true" json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
