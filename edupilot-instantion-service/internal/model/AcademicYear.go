package model

import (
	"time"

	"github.com/google/uuid"
)

// AcademicYear (tahun ajaran), API_CONTRACT §8.2. The legacy columns
// academic_code / year are nullable now; the contract "name" is stored in the
// legacy academic_name column (backfilled from year/academic_code in
// config.preMigrate). Exactly one active year per school: enforced in the
// service (tx) and by a partial unique index created in config.postMigrate.
type AcademicYear struct {
	ID           uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	SchoolID     uuid.UUID `gorm:"type:uuid;not null;index" json:"school_id"`
	AcademicCode string    `gorm:"type:varchar(255)" json:"-"`
	Name         string    `gorm:"column:academic_name;type:varchar(255)" json:"name"`
	Year         string    `gorm:"type:varchar(255)" json:"-"`
	StartDate    string    `gorm:"type:varchar(10)" json:"start_date"`
	EndDate      string    `gorm:"type:varchar(10)" json:"end_date"`
	IsActive     bool      `gorm:"not null;default:false" json:"is_active"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
