package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type School struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	// CustomerID = auth user id of a self-registered owner (0 for admin-created schools).
	// Partial unique index (customer_id <> 0) is created in config.postMigrate.
	CustomerID   uint       `gorm:"not null;index" json:"customer_id"`
	Code         string     `gorm:"type:varchar(255);uniqueIndex;not null" json:"code"`
	Name         string     `gorm:"type:varchar(255);not null" json:"name"`
	NPSN         *string    `gorm:"type:varchar(255)" json:"npsn"`
	NSS          *string    `gorm:"type:varchar(255)" json:"nss"`
	SchoolLevel  string     `gorm:"type:varchar(255)" json:"school_level"` // TK|SD|SMP|SMA|SMK|SLB|Lainnya
	SchoolStatus string     `gorm:"type:varchar(255)" json:"school_status"`
	Email        *string    `gorm:"type:varchar(255)" json:"email"`
	Phone        *string    `gorm:"type:varchar(255)" json:"phone"`
	Website      *string    `gorm:"type:varchar(255)" json:"website"`
	Address      *string    `gorm:"type:text" json:"address"`
	ProvinceID   *uuid.UUID `gorm:"type:uuid" json:"province_id"`
	CityID       *uuid.UUID `gorm:"type:uuid" json:"city_id"`
	DistrictID   *uuid.UUID `gorm:"type:uuid" json:"district_id"`
	PostalCode   *string    `gorm:"type:varchar(50)" json:"postal_code"`
	LogoFileID   *uuid.UUID `gorm:"type:uuid" json:"logo_file_id"`
	// Logo is a data URL (data:image/<png|jpeg|webp>;base64,...), API_CONTRACT §7.2.
	Logo     *string `gorm:"type:text" json:"logo"`
	Timezone string  `gorm:"type:varchar(255);default:'Asia/Jakarta'" json:"timezone"`
	IsActive bool    `gorm:"default:true" json:"is_active"`
	// AllowStudentJoin lets self-registered students request to join with the school code (§10.4).
	AllowStudentJoin bool           `gorm:"not null;default:true" json:"allow_student_join"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"index" json:"-"`
}
