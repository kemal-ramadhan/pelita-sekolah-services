package model

import (
	"time"

	"github.com/google/uuid"
)

type TeacherSubjects struct {
	ID         uuid.UUID  `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	SchoolID   uuid.UUID  `gorm:"type:uuid;not null;index" json:"school_id"`
	EmployeeID uuid.UUID  `gorm:"type:uuid;not null;index" json:"employee_id"`
	SubjectID  uuid.UUID  `gorm:"type:uuid;not null;index" json:"subject_id"`
	AcademicID *uuid.UUID `gorm:"type:uuid;index" json:"academic_id"`
	IsPrimary  string     `gorm:"type:varchar(255);default:'true'" json:"is_primary"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`

	Teacher *Employees `gorm:"foreignKey:EmployeeID" json:"teacher,omitempty"`
	Subject *Subject   `gorm:"foreignKey:SubjectID" json:"subject,omitempty"`
}
