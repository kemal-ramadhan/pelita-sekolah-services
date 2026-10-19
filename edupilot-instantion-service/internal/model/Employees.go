package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Employees stores school staff; teachers have EmployeeType == EmployeeTypeTeacher.
// A partial unique index (school_id, employee_code) WHERE deleted_at IS NULL is
// created in config.postMigrate (raw SQL, so existing data can be cleaned first).
type Employees struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	// CustomerID is legacy and never taken from the client. Kept nullable so the column survives.
	CustomerID     *uuid.UUID `gorm:"type:uuid" json:"-"`
	SchoolID       uuid.UUID  `gorm:"type:uuid;not null;index" json:"school_id"`
	FullName       string     `gorm:"type:varchar(255);not null;default:''" json:"full_name"`
	EmployeeCode   string     `gorm:"type:varchar(255);not null" json:"employee_code"`
	Email          *string    `gorm:"type:varchar(255)" json:"email"`
	Phone          *string    `gorm:"type:varchar(50)" json:"phone"`
	NumberType     string     `gorm:"type:varchar(255);" json:"number_type"`
	Number         string     `gorm:"type:varchar(255);" json:"number"`
	EmployeeType   string     `gorm:"type:varchar(255);" json:"employee_type"`
	EmployeeStatus string     `gorm:"type:varchar(255);" json:"employee_status"`
	JoinDate       string     `gorm:"type:varchar(255);" json:"join_date"`
	ResignDate     string     `gorm:"type:varchar(255);" json:"-"`
	Status         string     `gorm:"type:varchar(255);" json:"status"`
	// UserID links the teacher to its auth account (role "teacher"). A partial unique
	// index (user_id) WHERE user_id IS NOT NULL is created in config.postMigrate.
	UserID        *uint          `gorm:"index" json:"user_id"`
	IsCounselor   bool           `gorm:"not null;default:false;index" json:"is_counselor"`
	InvitedAt     *time.Time     `json:"invited_at"`
	ActivatedAt   *time.Time     `json:"activated_at"`
	AccountStatus string         `gorm:"-" json:"account_status"` // computed, see ComputeAccountStatus
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"-"`
}

const EmployeeTypeTeacher = "Teacher"

// Teacher account states (API_CONTRACT §7.3).
const (
	AccountStatusNone    = "none"    // no auth account linked
	AccountStatusInvited = "invited" // account created, not activated yet
	AccountStatusActive  = "active"  // teacher has activated / used the account
)

// ComputeAccountStatus derives account_status from user_id / activated_at.
func ComputeAccountStatus(userID *uint, activatedAt *time.Time) string {
	switch {
	case userID == nil || *userID == 0:
		return AccountStatusNone
	case activatedAt == nil:
		return AccountStatusInvited
	default:
		return AccountStatusActive
	}
}

// RefreshAccountStatus recomputes the AccountStatus field.
func (e *Employees) RefreshAccountStatus() {
	e.AccountStatus = ComputeAccountStatus(e.UserID, e.ActivatedAt)
}

// AfterFind fills the computed AccountStatus on every load.
func (e *Employees) AfterFind(*gorm.DB) error {
	e.RefreshAccountStatus()
	return nil
}
