package model

import (
	"time"

	"github.com/google/uuid"
)

// Join request statuses (API_CONTRACT §10.4).
const (
	JoinStatusPending   = "pending"
	JoinStatusApproved  = "approved"
	JoinStatusRejected  = "rejected"
	JoinStatusCancelled = "cancelled"
)

// SchoolJoinRequest is a self-registered student's request to join a school.
// One pending request per user (partial unique index in config.postMigrate).
type SchoolJoinRequest struct {
	ID               uuid.UUID  `gorm:"type:uuid;default:uuid_generate_v4();primaryKey"`
	SchoolID         uuid.UUID  `gorm:"type:uuid;not null;index"`
	UserID           uint       `gorm:"not null;index"`
	Email            string     `gorm:"type:varchar(255);not null;default:''"`
	FullName         string     `gorm:"type:varchar(255);not null"`
	NISN             string     `gorm:"type:varchar(50)"`
	BirthDate        string     `gorm:"type:varchar(10)"`
	Message          string     `gorm:"type:text"`
	Status           string     `gorm:"type:varchar(20);not null;default:'pending';index"`
	StudentID        *uuid.UUID `gorm:"type:uuid"`
	ReviewedByUserID *uint
	ReviewedAt       *time.Time
	RejectReason     string `gorm:"type:text"`
	CreatedAt        time.Time
	UpdatedAt        time.Time

	School *School `gorm:"foreignKey:SchoolID"`
}
