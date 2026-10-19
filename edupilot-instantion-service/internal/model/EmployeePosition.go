package model

import (
	"time"

	"github.com/google/uuid"
)

// EmployeePosition is a school's jabatan / tugas tambahan (Kepala Sekolah,
// Wali Kelas, ...). position_code and category are filled by the service; the
// columns are nullable so legacy rows / inserts never break (preMigrate).
type EmployeePosition struct {
	ID           uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	SchoolID     uuid.UUID `gorm:"type:uuid;not null;index" json:"school_id"`
	PositionCode string    `gorm:"type:varchar(255)" json:"position_code"`
	Position     string    `gorm:"type:varchar(255);not null" json:"position"`
	Category     string    `gorm:"type:varchar(255)" json:"category"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
