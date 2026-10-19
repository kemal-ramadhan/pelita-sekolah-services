package model

import "github.com/google/uuid"

import "time"

type EmployeeEducation struct {
	ID                uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	EmployeeID        uuid.UUID `gorm:"type:uuid;not null" json:"employee_id"`
	Lavel             string    `gorm:"type:varchar(255);not null" json:"lavel"`
	Institution       string    `gorm:"type:varchar(255);not null" json:"institution"`
	Major             string    `gorm:"type:varchar(255);not null" json:"major"`
	GraduateYear      string    `gorm:"type:varchar(255);not null" json:"graduate_year"`
	Certificate       string    `gorm:"type:varchar(255);" json:"certificate"`
	CertificateNumber string    `gorm:"type:varchar(255);" json:"certificate_number"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}
