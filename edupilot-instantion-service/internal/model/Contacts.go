package model

import "github.com/google/uuid"

import "time"

type Contacts struct {
	ID              uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	SchoolID        uuid.UUID `gorm:"type:uuid;not null" json:"school_id"`
	TypeNameContact string    `gorm:"type:varchar(255);not null" json:"type_name_contact"`
	NameContact     string    `gorm:"type:varchar(255);not null" json:"name_contact"`
	Contact         string    `gorm:"type:varchar(255);not null" json:"contact"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}
