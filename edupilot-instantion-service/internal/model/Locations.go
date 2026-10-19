package model

import "github.com/google/uuid"

import "time"

type Locations struct {
	ID           uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	SchoolID     uuid.UUID `gorm:"type:uuid;not null" json:"school_id"`
	LocationCode string    `gorm:"type:varchar(255);not null" json:"location_code"`
	LocationName string    `gorm:"type:varchar(255);not null" json:"location_name"`
	LocationType string    `gorm:"type:varchar(255);not null" json:"location_type"`
	Address      string    `gorm:"type:varchar(255);not null" json:"address"`
	Province     string    `gorm:"type:varchar(255);not null" json:"province"`
	City         string    `gorm:"type:varchar(255);not null" json:"city"`
	District     string    `gorm:"type:varchar(255);not null" json:"district"`
	Village      string    `gorm:"type:varchar(255);not null" json:"village"`
	PostCode     string    `gorm:"type:varchar(255);not null" json:"post_code"`
	Latitude     string    `gorm:"type:varchar(255);not null" json:"latitude"`
	Longitude    string    `gorm:"type:varchar(255);not null" json:"longitude"`
	AreaStatus   string    `gorm:"type:varchar(255);not null" json:"area_status"`
	IsPrimary    string    `gorm:"type:varchar(255);not null" json:"is_primary"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
