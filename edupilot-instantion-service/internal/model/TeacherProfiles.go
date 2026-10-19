package model

import "github.com/google/uuid"

import "time"

type TeacherProfiles struct {
	ID                 uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	EmployeeID         uuid.UUID `gorm:"type:uuid;not null" json:"employee_id"`
	SchoolID           uuid.UUID `gorm:"type:uuid;not null" json:"school_id"`
	SertificationName  string    `gorm:"type:varchar(255);not null" json:"sertification_name"`
	SertificationCode  string    `gorm:"type:varchar(255);not null" json:"sertification_code"`
	SertificationLevel string    `gorm:"type:varchar(255);not null" json:"sertification_level"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}
