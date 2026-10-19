package model

import "github.com/google/uuid"

import "time"

type EmployeeCertifications struct {
	ID                uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	EmployeeID        uuid.UUID `gorm:"type:uuid;not null" json:"employee_id"`
	GetCertification  string    `gorm:"type:varchar(255);not null" json:"get_certification"`
	Certificate       string    `gorm:"type:varchar(255);" json:"certificate"`
	CertificateNumber string    `gorm:"type:varchar(255);" json:"certificate_number"`
	IssuedDate        string    `gorm:"type:varchar(255);" json:"issued_date"`
	ExpiryDate        string    `gorm:"type:varchar(255);" json:"expiry_date"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}
