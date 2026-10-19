package model

import (
	"time"

	"github.com/google/uuid"
)

// Semester of an academic year (1 = Ganjil, 2 = Genap), API_CONTRACT §8.2.
// The contract "name" is stored in the legacy semester_name column.
type Semester struct {
	ID             uuid.UUID  `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	AcademicYearID uuid.UUID  `gorm:"type:uuid;not null;index" json:"academic_year_id"`
	SchoolID       *uuid.UUID `gorm:"type:uuid;index" json:"-"`
	SemesterCode   string     `gorm:"type:varchar(255)" json:"-"`
	Name           string     `gorm:"column:semester_name;type:varchar(255)" json:"name"`
	Number         int        `gorm:"not null;default:0" json:"number"`
	StartDate      string     `gorm:"type:varchar(10)" json:"start_date"`
	EndDate        string     `gorm:"type:varchar(10)" json:"end_date"`
	IsActive       bool       `gorm:"not null;default:false" json:"is_active"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}
