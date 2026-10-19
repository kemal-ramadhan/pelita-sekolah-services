package dto

import (
	"time"

	"github.com/google/uuid"
)

// AttendanceConfigRequest represents request to update attendance settings.
// Every field is optional (partial update); omitted fields keep their value.
type AttendanceConfigRequest struct {
	SchoolLatitude        *float64 `json:"school_latitude"`
	SchoolLongitude       *float64 `json:"school_longitude"`
	RadiusMeters          *int     `json:"radius_meters"`
	LocationName          *string  `json:"location_name"`
	DefaultCheckInTime    *string  `json:"default_check_in_time"`
	DefaultCheckOutTime   *string  `json:"default_check_out_time"`
	LateToleranceMinutes  *int     `json:"late_tolerance_minutes"`
	TeacherScheduleMode   *string  `json:"teacher_schedule_mode"` // "first_schedule" or "fixed"
	AllowSelfCheckInClass *bool    `json:"allow_self_check_in_class"`
	CodeValidMinutes      *int     `json:"code_valid_minutes"`
	AllowRemoteCheckIn    *bool    `json:"allow_remote_check_in"`
}

// AttendanceShiftRequest represents request to create or update an attendance shift.
type AttendanceShiftRequest struct {
	Name                 string `json:"name" binding:"required"`
	StartTime            string `json:"start_time" binding:"required"` // "07:00"
	EndTime              string `json:"end_time" binding:"required"`   // "12:30"
	LateToleranceMinutes *int   `json:"late_tolerance_minutes"`
	CheckOutStartTime    string `json:"check_out_start_time"`
	IsDefault            bool   `json:"is_default"`
}

// AssignClassShiftRequest maps a class to a shift.
type AssignClassShiftRequest struct {
	ClassID uuid.UUID `json:"class_id" binding:"required"`
	ShiftID uuid.UUID `json:"shift_id" binding:"required"`
}

// CheckInRequest represents daily arrival check-in request.
type CheckInRequest struct {
	Method        string   `json:"method"` // "gps", "pjj", "wfh", "dinas_luar"
	Latitude      *float64 `json:"latitude"`
	Longitude     *float64 `json:"longitude"`
	Accuracy      *float64 `json:"accuracy"` // optional, meters
	Notes         string   `json:"notes"`
	AttachmentURL string   `json:"attachment_url"`
}

// CheckOutRequest represents daily departure check-out request.
type CheckOutRequest struct {
	Method    string   `json:"method"` // "gps", "pjj", "wfh", "dinas_luar"
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
	Accuracy  *float64 `json:"accuracy"` // optional, meters
	Notes     string   `json:"notes"`
}

// ManualDailyAttendanceRequest represents institution/admin manual recording or correction.
type ManualDailyAttendanceRequest struct {
	Date          string     `json:"date" binding:"required"`      // "YYYY-MM-DD"
	UserType      string     `json:"user_type" binding:"required"` // "teacher" or "student"
	EmployeeID    *uuid.UUID `json:"employee_id"`
	StudentID     *uuid.UUID `json:"student_id"`
	ClassID       *uuid.UUID `json:"class_id"`
	Status        string     `json:"status" binding:"required"` // present, late, sick, permission, business_trip, remote, absent
	CheckInTime   *string    `json:"check_in_time"`
	CheckOutTime  *string    `json:"check_out_time"`
	LateMinutes   int        `json:"late_minutes"`
	EarlyMinutes  int        `json:"early_minutes"`
	Notes         string     `json:"notes"`
	AttachmentURL string     `json:"attachment_url"`
}

// TeachingSessionStartRequest records teacher entering class and initiating KBM.
type TeachingSessionStartRequest struct {
	ScheduleID uuid.UUID `json:"schedule_id" binding:"required"`
	Topic      string    `json:"topic"`
	Notes      string    `json:"notes"`
	Latitude   *float64  `json:"latitude"`
	Longitude  *float64  `json:"longitude"`
}

// TeachingSessionEndRequest completes KBM session.
type TeachingSessionEndRequest struct {
	Topic string `json:"topic"`
	Notes string `json:"notes"`
}

// StudentSessionAttendanceItem represents one student's attendance in a KBM session.
type StudentSessionAttendanceItem struct {
	StudentID uuid.UUID `json:"student_id" binding:"required"`
	Status    string    `json:"status" binding:"required"` // present, sick, permission, absent, dispensation
	Notes     string    `json:"notes"`
}

// StudentSessionAttendanceBatchRequest updates all student attendance rows in a session.
type StudentSessionAttendanceBatchRequest struct {
	Students []StudentSessionAttendanceItem `json:"students" binding:"required"`
}

// DailyAttendanceSummaryResponse provides high-level count metrics for a specific date.
type DailyAttendanceSummaryResponse struct {
	Date              string  `json:"date"`
	TotalExpected     int     `json:"total_expected"`
	PresentCount      int     `json:"present_count"`
	LateCount         int     `json:"late_count"`
	SickCount         int     `json:"sick_count"`
	PermissionCount   int     `json:"permission_count"`
	RemoteCount       int     `json:"remote_count"`
	BusinessTripCount int     `json:"business_trip_count"`
	AbsentCount       int     `json:"absent_count"`
	PresentRate       float64 `json:"present_rate"`
}

// TodayTeacherStatusResponse returns teacher's status for the current day.
type TodayTeacherStatusResponse struct {
	Date                 string      `json:"date"`
	Timezone             string      `json:"timezone"`
	TargetCheckInTime    string      `json:"target_check_in_time"`
	TargetCheckOutTime   string      `json:"target_check_out_time"`
	FirstScheduleTime    string      `json:"first_schedule_time,omitempty"`
	LateToleranceMinutes int         `json:"late_tolerance_minutes"`
	DailyAttendance      interface{} `json:"daily_attendance"`
	IsCheckedIn          bool        `json:"is_checked_in"`
	IsCheckedOut         bool        `json:"is_checked_out"`
	Config               interface{} `json:"config"`
}

// LiveClassMonitorItem represents active classroom schedule with real-time status.
type LiveClassMonitorItem struct {
	ScheduleID         uuid.UUID  `json:"schedule_id"`
	ClassID            uuid.UUID  `json:"class_id"`
	ClassName          string     `json:"class_name"`
	SubjectID          uuid.UUID  `json:"subject_id"`
	SubjectName        string     `json:"subject_name"`
	TeacherID          uuid.UUID  `json:"teacher_id"`
	TeacherName        string     `json:"teacher_name"`
	Room               string     `json:"room"`
	StartTime          string     `json:"start_time"`
	EndTime            string     `json:"end_time"`
	SessionID          *uuid.UUID `json:"session_id,omitempty"`
	SessionStatus      string     `json:"session_status"` // not_started, in_progress, completed
	TeacherCheckInTime *string    `json:"teacher_check_in_time,omitempty"`
	TeacherStatusLabel string     `json:"teacher_status_label"` // "Hadir Tepat Waktu", "Terlambat 12m", "Belum Masuk"
	LateMinutes        int        `json:"late_minutes"`
	Topic              string     `json:"topic"`
	SessionToken       string     `json:"session_token,omitempty"`
	TokenExpiresAt     *time.Time `json:"token_expires_at,omitempty"`
	StudentsPresent    int        `json:"students_present"`
	StudentsTotal      int        `json:"students_total"`
}

// ==================== Student app (API_CONTRACT §10.5) ====================

// StudentGPSRequest is the body of student daily check-in / check-out (GPS only).
type StudentGPSRequest struct {
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
	Accuracy  *float64 `json:"accuracy"`
}

// StudentClaimRequest: student claims a class session with the 6-char code (QR or typed).
type StudentClaimRequest struct {
	Code      string   `json:"code" binding:"required,max=20"`
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
}

type StudentClaimResponse struct {
	SessionID   uuid.UUID `json:"session_id"`
	SubjectName string    `json:"subject_name"`
	ClassName   string    `json:"class_name"`
	Status      string    `json:"status"`
	CheckInTime *string   `json:"check_in_time"`
}

type SchoolLocation struct {
	Latitude     float64 `json:"latitude"`
	Longitude    float64 `json:"longitude"`
	RadiusMeters int     `json:"radius_meters"`
	LocationName string  `json:"location_name"`
}

type StudentTodaySession struct {
	ScheduleID    uuid.UUID  `json:"schedule_id"`
	SubjectName   string     `json:"subject_name"`
	TeacherName   string     `json:"teacher_name"`
	StartTime     string     `json:"start_time"`
	EndTime       string     `json:"end_time"`
	Room          string     `json:"room"`
	SessionID     *uuid.UUID `json:"session_id"`
	SessionStatus string     `json:"session_status"`
	MyStatus      *string    `json:"my_status"`
	MyCheckInTime *string    `json:"my_check_in_time"`
}

type StudentAttendanceToday struct {
	Date                 string                `json:"date"`
	Now                  string                `json:"now"`
	Timezone             string                `json:"timezone"`
	SchoolLocation       SchoolLocation        `json:"school_location"`
	TargetCheckInTime    string                `json:"target_check_in_time"`
	TargetCheckOutTime   string                `json:"target_check_out_time"`
	LateToleranceMinutes int                   `json:"late_tolerance_minutes"`
	DailyAttendance      interface{}           `json:"daily_attendance"`
	IsCheckedIn          bool                  `json:"is_checked_in"`
	IsCheckedOut         bool                  `json:"is_checked_out"`
	Sessions             []StudentTodaySession `json:"sessions"`
}

type StudentHistorySummary struct {
	Present    int `json:"present"`
	Late       int `json:"late"`
	Sick       int `json:"sick"`
	Permission int `json:"permission"`
	Absent     int `json:"absent"`
}

type StudentHistorySession struct {
	Date          string `json:"date"`
	SubjectName   string `json:"subject_name"`
	TeacherName   string `json:"teacher_name"`
	StartTime     string `json:"start_time"`
	Status        string `json:"status"`
	CheckInMethod string `json:"check_in_method"`
}

type StudentAttendanceHistory struct {
	Month    string                  `json:"month"`
	Summary  StudentHistorySummary   `json:"summary"`
	Daily    interface{}             `json:"daily"`
	Sessions []StudentHistorySession `json:"sessions"`
}
