package model

import (
	"time"

	"github.com/google/uuid"
)

// AttendanceConfig stores GPS geofencing settings and default attendance policies for a school.
type AttendanceConfig struct {
	ID                    uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	SchoolID              uuid.UUID `gorm:"type:uuid;not null;uniqueIndex" json:"school_id"`
	SchoolLatitude        float64   `gorm:"type:decimal(10,8);not null;default:-6.200000" json:"school_latitude"`
	SchoolLongitude       float64   `gorm:"type:decimal(11,8);not null;default:106.816666" json:"school_longitude"`
	RadiusMeters          int       `gorm:"type:int;not null;default:100" json:"radius_meters"`
	LocationName          string    `gorm:"type:varchar(255);default:'Kampus Utama'" json:"location_name"`
	DefaultCheckInTime    string    `gorm:"type:varchar(5);not null;default:'07:00'" json:"default_check_in_time"`
	DefaultCheckOutTime   string    `gorm:"type:varchar(5);not null;default:'15:00'" json:"default_check_out_time"`
	LateToleranceMinutes  int       `gorm:"type:int;not null;default:15" json:"late_tolerance_minutes"`
	TeacherScheduleMode   string    `gorm:"type:varchar(50);not null;default:'first_schedule'" json:"teacher_schedule_mode"` // "first_schedule" or "fixed"
	AllowSelfCheckInClass bool      `gorm:"not null;default:true" json:"allow_self_check_in_class"`
	CodeValidMinutes      int       `gorm:"type:int;not null;default:30" json:"code_valid_minutes"`
	// AllowRemoteCheckIn enables teacher methods pjj/wfh/dinas_luar (API_CONTRACT §9).
	AllowRemoteCheckIn bool      `gorm:"not null;default:true" json:"allow_remote_check_in"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// AttendanceShift defines scheduled arrival/departure time slots (e.g. Sesi Pagi 07:00, Sesi Siang 12:30).
type AttendanceShift struct {
	ID                   uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	SchoolID             uuid.UUID `gorm:"type:uuid;not null;index" json:"school_id"`
	Name                 string    `gorm:"type:varchar(100);not null" json:"name"`
	StartTime            string    `gorm:"type:varchar(5);not null" json:"start_time"` // "07:00"
	EndTime              string    `gorm:"type:varchar(5);not null" json:"end_time"`   // "12:30"
	LateToleranceMinutes int       `gorm:"type:int;not null;default:15" json:"late_tolerance_minutes"`
	CheckOutStartTime    string    `gorm:"type:varchar(5);not null" json:"check_out_start_time"`
	IsDefault            bool      `gorm:"not null;default:false" json:"is_default"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

// ClassAttendanceShift maps a class to a specific shift.
type ClassAttendanceShift struct {
	ID        uuid.UUID        `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	SchoolID  uuid.UUID        `gorm:"type:uuid;not null;index" json:"school_id"`
	ClassID   uuid.UUID        `gorm:"type:uuid;not null;uniqueIndex" json:"class_id"`
	ShiftID   uuid.UUID        `gorm:"type:uuid;not null;index" json:"shift_id"`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
	Shift     *AttendanceShift `gorm:"foreignKey:ShiftID" json:"shift,omitempty"`
	Class     *Classes         `gorm:"foreignKey:ClassID" json:"class,omitempty"`
}

// DailyAttendance represents daily school check-in and check-out for teachers and students.
type DailyAttendance struct {
	ID         uuid.UUID  `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	SchoolID   uuid.UUID  `gorm:"type:uuid;not null;index" json:"school_id"`
	Date       string     `gorm:"type:varchar(10);not null;index" json:"date"`      // "YYYY-MM-DD"
	UserType   string     `gorm:"type:varchar(20);not null;index" json:"user_type"` // "teacher" | "student"
	EmployeeID *uuid.UUID `gorm:"type:uuid;index" json:"employee_id,omitempty"`
	StudentID  *uuid.UUID `gorm:"type:uuid;index" json:"student_id,omitempty"`
	ClassID    *uuid.UUID `gorm:"type:uuid;index" json:"class_id,omitempty"`
	ShiftID    *uuid.UUID `gorm:"type:uuid;index" json:"shift_id,omitempty"`
	Status     string     `gorm:"type:varchar(50);not null;default:'present'" json:"status"` // present, late, sick, permission, business_trip, remote, absent

	// Check-in details
	CheckInTime     *string  `gorm:"type:varchar(8)" json:"check_in_time,omitempty"`    // "06:55:00"
	CheckInStatus   string   `gorm:"type:varchar(20)" json:"check_in_status,omitempty"` // "on_time", "late"
	LateMinutes     int      `gorm:"type:int;default:0" json:"late_minutes"`
	CheckInMethod   string   `gorm:"type:varchar(50)" json:"check_in_method,omitempty"` // "gps", "pjj", "wfh", "dinas_luar", "manual"
	CheckInLat      *float64 `gorm:"type:decimal(10,8)" json:"check_in_lat,omitempty"`
	CheckInLng      *float64 `gorm:"type:decimal(11,8)" json:"check_in_lng,omitempty"`
	CheckInDistance *float64 `gorm:"type:decimal(10,2)" json:"check_in_distance,omitempty"`
	CheckInNotes    string   `gorm:"type:text" json:"check_in_notes,omitempty"`

	// Check-out details
	CheckOutTime     *string  `gorm:"type:varchar(8)" json:"check_out_time,omitempty"`
	CheckOutStatus   string   `gorm:"type:varchar(20)" json:"check_out_status,omitempty"` // "on_time", "early"
	EarlyMinutes     int      `gorm:"type:int;default:0" json:"early_minutes"`
	CheckOutMethod   string   `gorm:"type:varchar(50)" json:"check_out_method,omitempty"`
	CheckOutLat      *float64 `gorm:"type:decimal(10,8)" json:"check_out_lat,omitempty"`
	CheckOutLng      *float64 `gorm:"type:decimal(11,8)" json:"check_out_lng,omitempty"`
	CheckOutDistance *float64 `gorm:"type:decimal(10,2)" json:"check_out_distance,omitempty"`
	CheckOutNotes    string   `gorm:"type:text" json:"check_out_notes,omitempty"`

	AttachmentURL    string    `gorm:"type:text" json:"attachment_url,omitempty"`
	ApprovedByUserID *uint     `json:"approved_by_user_id,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`

	Employee *Employees `gorm:"foreignKey:EmployeeID" json:"employee,omitempty"`
	Student  *Students  `gorm:"foreignKey:StudentID" json:"student,omitempty"`
	Class    *Classes   `gorm:"foreignKey:ClassID" json:"class,omitempty"`
}

// ClassTeachingSession records teacher arrival in class for a scheduled subject and manages the session lifecycle.
type ClassTeachingSession struct {
	ID                   uuid.UUID  `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	SchoolID             uuid.UUID  `gorm:"type:uuid;not null;index" json:"school_id"`
	ScheduleID           uuid.UUID  `gorm:"type:uuid;not null;index" json:"schedule_id"`
	Date                 string     `gorm:"type:varchar(10);not null;index" json:"date"` // "YYYY-MM-DD"
	ClassID              uuid.UUID  `gorm:"type:uuid;not null;index" json:"class_id"`
	SubjectID            uuid.UUID  `gorm:"type:uuid;not null;index" json:"subject_id"`
	TeacherID            uuid.UUID  `gorm:"type:uuid;not null;index" json:"teacher_id"`
	AcademicYearID       *uuid.UUID `gorm:"type:uuid;index" json:"academic_year_id,omitempty"`
	SemesterID           *uuid.UUID `gorm:"type:uuid;index" json:"semester_id,omitempty"`
	ScheduledStartTime   string     `gorm:"type:varchar(5);not null" json:"scheduled_start_time"`
	ScheduledEndTime     string     `gorm:"type:varchar(5);not null" json:"scheduled_end_time"`
	TeacherCheckInTime   *string    `gorm:"type:varchar(8)" json:"teacher_check_in_time,omitempty"`
	TeacherCheckInStatus string     `gorm:"type:varchar(20)" json:"teacher_check_in_status,omitempty"` // "on_time", "late"
	LateMinutes          int        `gorm:"type:int;default:0" json:"late_minutes"`
	TeacherCheckOutTime  *string    `gorm:"type:varchar(8)" json:"teacher_check_out_time,omitempty"`
	TeachingTopic        string     `gorm:"type:varchar(255)" json:"teaching_topic"`
	TeachingNotes        string     `gorm:"type:text" json:"teaching_notes"`
	Status               string     `gorm:"type:varchar(50);default:'not_started'" json:"status"`  // not_started, in_progress, completed, cancelled
	SessionToken         string     `gorm:"type:varchar(10);index" json:"session_token,omitempty"` // 6 chars uppercase code
	TokenExpiresAt       *time.Time `json:"token_expires_at,omitempty"`
	AllowSelfCheckIn     bool       `gorm:"not null;default:true" json:"allow_self_check_in"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`

	Teacher  *Employees        `gorm:"foreignKey:TeacherID" json:"teacher,omitempty"`
	Subject  *Subject          `gorm:"foreignKey:SubjectID" json:"subject,omitempty"`
	Class    *Classes          `gorm:"foreignKey:ClassID" json:"class,omitempty"`
	Schedule *TeacherSchedules `gorm:"foreignKey:ScheduleID" json:"schedule,omitempty"`
}

// StudentSessionAttendance tracks individual student attendance for a particular subject/classroom session.
type StudentSessionAttendance struct {
	ID               uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	SchoolID         uuid.UUID `gorm:"type:uuid;not null;index" json:"school_id"`
	SessionID        uuid.UUID `gorm:"type:uuid;not null;index" json:"session_id"`
	StudentID        uuid.UUID `gorm:"type:uuid;not null;index" json:"student_id"`
	ClassID          uuid.UUID `gorm:"type:uuid;not null;index" json:"class_id"`
	Date             string    `gorm:"type:varchar(10);not null" json:"date"`
	Status           string    `gorm:"type:varchar(20);not null;default:'present'" json:"status"`        // present, sick, permission, absent, dispensation
	CheckInMethod    string    `gorm:"type:varchar(50);default:'teacher_manual'" json:"check_in_method"` // teacher_manual, unique_code, qr_scan, admin_correction
	CheckInTime      *string   `gorm:"type:varchar(8)" json:"check_in_time,omitempty"`
	Notes            string    `gorm:"type:text" json:"notes"`
	RecordedByUserID uint      `json:"recorded_by_user_id"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`

	Student *Students `gorm:"foreignKey:StudentID" json:"student,omitempty"`
}
