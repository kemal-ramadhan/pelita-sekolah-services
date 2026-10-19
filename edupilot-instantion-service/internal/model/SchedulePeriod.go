package model

import (
	"time"

	"github.com/google/uuid"
)

type SchedulePeriod struct {
	ID             uuid.UUID  `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	SchoolID       uuid.UUID  `gorm:"type:uuid;not null;index" json:"school_id"`
	AcademicYearID *uuid.UUID `gorm:"type:uuid;index" json:"academic_year_id"`
	Name           string     `gorm:"type:varchar(100);not null" json:"name"`
	PeriodOrder    int        `gorm:"type:int;not null" json:"period_order"`
	StartTime      string     `gorm:"type:varchar(5);not null" json:"start_time"`
	EndTime        string     `gorm:"type:varchar(5);not null" json:"end_time"`
	DayOfWeek      int        `gorm:"type:int;default:0" json:"day_of_week"` // 0=All days, 1=Monday... 7=Sunday
	IsBreak        bool       `gorm:"default:false" json:"is_break"`
	IsActive       bool       `gorm:"default:true" json:"is_active"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}
