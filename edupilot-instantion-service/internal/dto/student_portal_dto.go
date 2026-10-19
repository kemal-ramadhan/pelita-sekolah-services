package dto

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ==================== Student accounts / join school (API_CONTRACT §10) ====================

// NotificationResponse is one in-app notification.
type NotificationResponse struct {
	ID        uuid.UUID       `json:"id"`
	SchoolID  *uuid.UUID      `json:"school_id"`
	Type      string          `json:"type"`
	Title     string          `json:"title"`
	Body      string          `json:"body"`
	Data      json.RawMessage `json:"data"`
	ReadAt    *time.Time      `json:"read_at"`
	CreatedAt time.Time       `json:"created_at"`
}

// SchoolBrief is the school shown to students / in join requests.
type SchoolBrief struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	Code     string    `json:"code"`
	Logo     *string   `json:"logo"`
	Timezone string    `json:"timezone,omitempty"`
}

// JoinRequestResponse is the JoinRequest JSON (§10.4).
type JoinRequestResponse struct {
	ID           uuid.UUID   `json:"id"`
	School       SchoolBrief `json:"school"`
	Email        string      `json:"email"`
	FullName     string      `json:"full_name"`
	NISN         string      `json:"nisn"`
	BirthDate    string      `json:"birth_date"`
	Message      string      `json:"message"`
	Status       string      `json:"status"`
	RejectReason string      `json:"reject_reason"`
	StudentID    *uuid.UUID  `json:"student_id"`
	CreatedAt    time.Time   `json:"created_at"`
	ReviewedAt   *time.Time  `json:"reviewed_at"`
}

// JoinRequestWithSuggestions is a school-side list item.
type JoinRequestWithSuggestions struct {
	JoinRequestResponse
	Suggestions []StudentSummaryResponse `json:"suggestions"`
}

// JoinSchoolRequest is the student's POST /instantion/student/join body.
type JoinSchoolRequest struct {
	SchoolCode string `json:"school_code" binding:"required,max=255"`
	FullName   string `json:"full_name" binding:"required,max=255"`
	NISN       string `json:"nisn" binding:"max=20"`
	BirthDate  string `json:"birth_date" binding:"max=10"`
	Message    string `json:"message" binding:"max=1000"`
}

// JoinApproveCreate creates a new student while approving.
type JoinApproveCreate struct {
	FullName   string       `json:"full_name"`
	Gender     string       `json:"gender"`
	BirthPlace string       `json:"birth_place"`
	BirthDate  string       `json:"birth_date"`
	Religion   string       `json:"religion"`
	NISN       string       `json:"nisn"`
	ClassID    OptionalUUID `json:"class_id"`
}

// JoinApproveRequest: exactly one of student_id / create.
type JoinApproveRequest struct {
	StudentID OptionalUUID       `json:"student_id"`
	Create    *JoinApproveCreate `json:"create"`
}

type JoinRejectRequest struct {
	Reason string `json:"reason" binding:"max=1000"`
}

// StudentSelf is the StudentSummary of the logged-in student (+ photo).
type StudentSelf struct {
	StudentSummaryResponse
	Photo *string `json:"photo"`
}

// StudentMeResponse is GET /instantion/student/me.
type StudentMeResponse struct {
	Status         string               `json:"status"` // no_school | pending | active
	School         *SchoolBrief         `json:"school"`
	Student        *StudentSelf         `json:"student"`
	PendingRequest *JoinRequestResponse `json:"pending_request"`
}
