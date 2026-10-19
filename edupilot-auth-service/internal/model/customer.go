package model

import "time"

type Customer struct {
	ID            uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID        uint       `gorm:"not null" json:"user_id"`
	User          *User      `gorm:"foreignKey:UserID" json:"-"`
	FullName      *string    `gorm:"type:varchar(255);null" json:"full_name,omitempty"`
	SchoolName    *string    `gorm:"type:varchar(255);null" json:"school_name,omitempty"`
	BirthPlace    *string    `gorm:"type:varchar(255);null" json:"birth_place,omitempty"`
	BirthDate     *time.Time `json:"birth_date,omitempty"`
	Gender        *int8      `gorm:"type:smallint;null" json:"gender,omitempty"`
	Religion      string     `gorm:"type:varchar(255);" json:"religion"`
	MaritalStatus string     `gorm:"type:varchar(255);" json:"marital_status"`
	BloodType     string     `gorm:"type:varchar(255);" json:"blood_type"`
	Role          string     `gorm:"type:varchar(50);not null;default:'students'" json:"role"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}
