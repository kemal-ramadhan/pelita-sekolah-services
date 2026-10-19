package model

import (
	"time"

	"github.com/google/uuid"
)

// Notification types created by the backend (API_CONTRACT §10.5).
const (
	NotifJoinApproved     = "join_approved"
	NotifJoinRejected     = "join_rejected"
	NotifAccountLinked    = "account_linked"
	NotifAttendanceMarked = "attendance_marked"
	NotifLateCheckIn      = "late_check_in"
)

// Notification is an in-app message for an auth user (students app).
type Notification struct {
	ID        uuid.UUID  `gorm:"type:uuid;default:uuid_generate_v4();primaryKey"`
	UserID    uint       `gorm:"not null;index:idx_notifications_user_created,priority:1"`
	SchoolID  *uuid.UUID `gorm:"type:uuid;index"`
	Type      string     `gorm:"type:varchar(50);not null"`
	Title     string     `gorm:"type:varchar(255);not null"`
	Body      string     `gorm:"type:text"`
	Data      string     `gorm:"type:jsonb;not null;default:'{}'"`
	ReadAt    *time.Time
	CreatedAt time.Time `gorm:"index:idx_notifications_user_created,priority:2,sort:desc"`
}
