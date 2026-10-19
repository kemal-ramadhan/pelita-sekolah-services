package model

import "github.com/google/uuid"

import "time"

type EmployeeAssignments struct {
	ID                 uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	EmployeeID         uuid.UUID `gorm:"type:uuid;not null" json:"employee_id"`
	SchoolID           uuid.UUID `gorm:"type:uuid;not null" json:"school_id"`
	EmployeePositionID uuid.UUID `gorm:"type:uuid;not null" json:"employee_position_id"`
	StartDate          string    `gorm:"type:varchar(255);not null" json:"start_date"`
	EndDate            string    `gorm:"type:varchar(255);not null" json:"end_date"`
	IsActive           string    `gorm:"type:varchar(255);not null" json:"is_active"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}
