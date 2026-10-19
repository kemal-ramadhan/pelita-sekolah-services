package model

import (
	"time"

	"github.com/google/uuid"
)

// Counseling types
const (
	CounselingTypeIndividual   = "individual"
	CounselingTypeGroup        = "group"
	CounselingTypeClassical    = "classical"
	CounselingTypeCaseHandling = "case_handling"
)

// Counseling session statuses
const (
	CounselingStatusPending     = "pending"
	CounselingStatusScheduled   = "scheduled"
	CounselingStatusRescheduled = "rescheduled"
	CounselingStatusCompleted   = "completed"
	CounselingStatusCancelled   = "cancelled"
)

// Counseling outcome statuses
const (
	OutcomeStatusResolved      = "resolved"
	OutcomeStatusNeedsFollowup = "needs_followup"
	OutcomeStatusReferral      = "referral"
)

// Counseling event statuses
const (
	EventStatusDraft     = "draft"
	EventStatusPublished = "published"
	EventStatusCompleted = "completed"
	EventStatusCancelled = "cancelled"
)

// CounselingSession menyimpan pengajuan dan jadwal sesi konseling.
type CounselingSession struct {
	ID                 uuid.UUID  `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	SchoolID           uuid.UUID  `gorm:"type:uuid;not null;index" json:"school_id"`
	CounselorID        uuid.UUID  `gorm:"type:uuid;not null;index" json:"counselor_id"`
	InitiatorStudentID uuid.UUID  `gorm:"type:uuid;not null;index" json:"initiator_student_id"`
	ClassID            *uuid.UUID `gorm:"type:uuid;index" json:"class_id,omitempty"`
	CounselingType     string     `gorm:"type:varchar(30);not null" json:"counseling_type"` // individual, group, classical, case_handling
	Topic              string     `gorm:"type:varchar(255);not null" json:"topic"`
	Description        string     `gorm:"type:text" json:"description"`
	RequestedDate      string     `gorm:"type:varchar(10);not null;index" json:"requested_date"`           // YYYY-MM-DD
	StartTime          string     `gorm:"type:varchar(5);not null" json:"start_time"`                      // HH:MM
	EndTime            string     `gorm:"type:varchar(5);not null" json:"end_time"`                        // HH:MM
	Place              string     `gorm:"type:varchar(150);not null" json:"place"`                         // Ruang BK 1, Ruang Diskusi, Online
	Status             string     `gorm:"type:varchar(30);not null;default:'pending';index" json:"status"` // pending, scheduled, rescheduled, completed, cancelled
	RescheduleReason   string     `gorm:"type:text" json:"reschedule_reason,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`

	Counselor        *Employees              `gorm:"foreignKey:CounselorID" json:"counselor,omitempty"`
	InitiatorStudent *Students               `gorm:"foreignKey:InitiatorStudentID" json:"initiator_student,omitempty"`
	Class            *Classes                `gorm:"foreignKey:ClassID" json:"class,omitempty"`
	Participants     []CounselingParticipant `gorm:"foreignKey:CounselingSessionID" json:"participants,omitempty"`
	Outcome          *CounselingOutcome      `gorm:"foreignKey:CounselingSessionID" json:"outcome,omitempty"`
}

// CounselingParticipant mencatat siswa yang terlibat dalam konseling (lintas rombel).
type CounselingParticipant struct {
	ID                  uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	SchoolID            uuid.UUID `gorm:"type:uuid;not null;index" json:"school_id"`
	CounselingSessionID uuid.UUID `gorm:"type:uuid;not null;index" json:"counseling_session_id"`
	StudentID           uuid.UUID `gorm:"type:uuid;not null;index" json:"student_id"`
	IsInitiator         bool      `gorm:"not null;default:false" json:"is_initiator"`
	AttendanceStatus    string    `gorm:"type:varchar(20);not null;default:'present'" json:"attendance_status"` // present, absent, excused
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`

	Student *Students `gorm:"foreignKey:StudentID" json:"student,omitempty"`
}

// CounselingOutcome menyimpan konklusi, catatan rahasia guru BK, umpan balik siswa, dan berkas lampiran.
type CounselingOutcome struct {
	ID                  uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	SchoolID            uuid.UUID `gorm:"type:uuid;not null;index" json:"school_id"`
	CounselingSessionID uuid.UUID `gorm:"type:uuid;not null;uniqueIndex" json:"counseling_session_id"`
	CounselorID         uuid.UUID `gorm:"type:uuid;not null;index" json:"counselor_id"`
	ConfidentialNotes   string    `gorm:"type:text" json:"confidential_notes"`                                // Hanya untuk Guru BK & Kepsek
	StudentFeedback     string    `gorm:"type:text" json:"student_feedback"`                                  // Umpan balik yang dibaca oleh siswa
	OutcomeStatus       string    `gorm:"type:varchar(30);not null;default:'resolved'" json:"outcome_status"` // resolved, needs_followup, referral
	FollowUpPlan        string    `gorm:"type:text" json:"follow_up_plan,omitempty"`
	FollowUpDate        *string   `gorm:"type:varchar(10)" json:"follow_up_date,omitempty"`
	AttachmentURLs      string    `gorm:"type:text" json:"attachment_urls,omitempty"` // JSON array string
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// CounselingEvent menyimpan postingan acara besar konseling, seminar, bimbingan klasikal massal.
type CounselingEvent struct {
	ID              uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	SchoolID        uuid.UUID `gorm:"type:uuid;not null;index" json:"school_id"`
	CounselorID     uuid.UUID `gorm:"type:uuid;not null;index" json:"counselor_id"`
	Title           string    `gorm:"type:varchar(255);not null" json:"title"`
	Category        string    `gorm:"type:varchar(100);not null" json:"category"` // Seminar, Bimbingan Klasikal, Workshop, Motivasi
	Description     string    `gorm:"type:text" json:"description"`
	EventDate       string    `gorm:"type:varchar(10);not null;index" json:"event_date"`                 // YYYY-MM-DD
	StartTime       string    `gorm:"type:varchar(5);not null" json:"start_time"`                        // HH:MM
	EndTime         string    `gorm:"type:varchar(5);not null" json:"end_time"`                          // HH:MM
	Location        string    `gorm:"type:varchar(150);not null" json:"location"`                        // Aula Utama, Zoom Meeting
	TargetAudience  string    `gorm:"type:varchar(50);not null;default:'all'" json:"target_audience"`    // all, students, teachers, specific_grade
	Status          string    `gorm:"type:varchar(30);not null;default:'published';index" json:"status"` // draft, published, completed, cancelled
	ConclusionNotes string    `gorm:"type:text" json:"conclusion_notes,omitempty"`
	AttachmentURLs  string    `gorm:"type:text" json:"attachment_urls,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`

	Counselor   *Employees                  `gorm:"foreignKey:CounselorID" json:"counselor,omitempty"`
	Attendances []CounselingEventAttendance `gorm:"foreignKey:EventID" json:"attendances,omitempty"`
}

// CounselingEventAttendance mencatat kehadiran peserta pada acara besar BK.
type CounselingEventAttendance struct {
	ID          uuid.UUID  `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	SchoolID    uuid.UUID  `gorm:"type:uuid;not null;index" json:"school_id"`
	EventID     uuid.UUID  `gorm:"type:uuid;not null;index" json:"event_id"`
	UserType    string     `gorm:"type:varchar(20);not null" json:"user_type"` // student, teacher, institution
	StudentID   *uuid.UUID `gorm:"type:uuid;index" json:"student_id,omitempty"`
	EmployeeID  *uuid.UUID `gorm:"type:uuid;index" json:"employee_id,omitempty"`
	UserID      *uint      `json:"user_id,omitempty"`
	CheckInTime string     `gorm:"type:varchar(8);not null" json:"check_in_time"`
	Notes       string     `gorm:"type:text" json:"notes,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`

	Student  *Students  `gorm:"foreignKey:StudentID" json:"student,omitempty"`
	Employee *Employees `gorm:"foreignKey:EmployeeID" json:"employee,omitempty"`
}

// CounselingDailyJournal mencatat jurnal kegiatan & layanan harian Guru BK sesuai regulasi BK.
type CounselingDailyJournal struct {
	ID                  uuid.UUID  `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	SchoolID            uuid.UUID  `gorm:"type:uuid;not null;index" json:"school_id"`
	CounselorID         uuid.UUID  `gorm:"type:uuid;not null;index" json:"counselor_id"`
	Date                string     `gorm:"type:varchar(10);not null;index" json:"date"`         // YYYY-MM-DD
	ServiceType         string     `gorm:"type:varchar(50);not null;index" json:"service_type"` // orientasi, informasi, konseling_individual, bimbingan_kelompok, konseling_kelompok, mediasi, home_visit, konferensi_kasus, kolaborasi, administrasi
	ActivityTitle       string     `gorm:"type:varchar(255);not null" json:"activity_title"`
	TargetDescription   string     `gorm:"type:varchar(255)" json:"target_description"` // misal: Siswa Kelas 9B / Orang Tua Siswa X
	ActivitySummary     string     `gorm:"type:text;not null" json:"activity_summary"`
	EvaluationResult    string     `gorm:"type:text" json:"evaluation_result"`
	FollowUpAction      string     `gorm:"type:text" json:"follow_up_action"`
	CounselingSessionID *uuid.UUID `gorm:"type:uuid;index" json:"counseling_session_id,omitempty"`
	EventID             *uuid.UUID `gorm:"type:uuid;index" json:"event_id,omitempty"`
	AttachmentURLs      string     `gorm:"type:text" json:"attachment_urls,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`

	Counselor *Employees         `gorm:"foreignKey:CounselorID" json:"counselor,omitempty"`
	Session   *CounselingSession `gorm:"foreignKey:CounselingSessionID" json:"session,omitempty"`
	Event     *CounselingEvent   `gorm:"foreignKey:EventID" json:"event,omitempty"`
}
