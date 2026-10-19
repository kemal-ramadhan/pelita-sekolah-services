package model

import "github.com/google/uuid"

import "time"

type TeachingAssesments struct {
	ID                uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	TeacherSubjectsID uuid.UUID `gorm:"type:uuid;not null" json:"teacher_subjects_id"`
	ClassID           uuid.UUID `gorm:"type:uuid;not null" json:"class_id"`
	SemesterID        uuid.UUID `gorm:"type:uuid;not null" json:"semester_id"`
	TotalHours        string    `gorm:"type:varchar(255);not null" json:"total_hours"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}
