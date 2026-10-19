package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Students represents a student record according to Kemendikbud Dapodik standards (API_CONTRACT §8.6).
// Legacy columns customer_id and student_code are nullable now.
type Students struct {
	ID           uuid.UUID  `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	CustomerID   *uuid.UUID `gorm:"type:uuid" json:"customer_id,omitempty"`
	SchoolID     uuid.UUID  `gorm:"type:uuid;not null;index" json:"school_id"`
	StudentCode  string     `gorm:"type:varchar(255)" json:"student_code,omitempty"`
	NIS          string     `gorm:"type:varchar(50);index" json:"nis"`
	NISN         string     `gorm:"type:varchar(50);index" json:"nisn"`
	JoinDate     string     `gorm:"type:varchar(20)" json:"join_date"`
	GraduateDate string     `gorm:"type:varchar(20)" json:"graduate_date"`
	Status       string     `gorm:"type:varchar(50);not null;default:'active';index" json:"status"`

	// --- Dapodik: Identitas ---
	FullName           string `gorm:"type:varchar(255);not null;default:'';index" json:"full_name"`
	Nickname           string `gorm:"type:varchar(100)" json:"nickname"`
	Gender             string `gorm:"type:varchar(10);not null;default:''" json:"gender"` // L, P
	NIK                string `gorm:"type:varchar(50);index" json:"nik"`
	NoKK               string `gorm:"type:varchar(50)" json:"no_kk"`
	BirthPlace         string `gorm:"type:varchar(100);not null;default:''" json:"birth_place"`
	BirthDate          string `gorm:"type:varchar(20);not null;default:''" json:"birth_date"`
	BirthCertificateNo string `gorm:"type:varchar(100)" json:"birth_certificate_no"`
	Religion           string `gorm:"type:varchar(50);not null;default:''" json:"religion"`
	Citizenship        string `gorm:"type:varchar(50);default:'WNI'" json:"citizenship"`
	Country            string `gorm:"type:varchar(100);default:'Indonesia'" json:"country"`
	SpecialNeeds       string `gorm:"type:varchar(50);default:'none'" json:"special_needs"`
	BloodType          string `gorm:"type:varchar(10)" json:"blood_type"`
	Photo              string `gorm:"type:text" json:"-"` // Data URL (data:image/...;base64,...), excluded from default select
	ChildOrder         *int   `json:"child_order"`
	SiblingsCount      *int   `json:"siblings_count"`
	Hobby              string `gorm:"type:varchar(100)" json:"hobby"`
	Aspiration         string `gorm:"type:varchar(100)" json:"aspiration"`

	// --- Dapodik: Kontak ---
	Phone       string `gorm:"type:varchar(50)" json:"phone"`
	MobilePhone string `gorm:"type:varchar(50)" json:"mobile_phone"`
	Email       string `gorm:"type:varchar(255)" json:"email"`

	// --- Dapodik: Alamat Lengkap ---
	Street            string   `gorm:"type:varchar(255)" json:"street"`
	RT                string   `gorm:"type:varchar(20)" json:"rt"`
	RW                string   `gorm:"type:varchar(20)" json:"rw"`
	Hamlet            string   `gorm:"type:varchar(100)" json:"hamlet"`   // Dusun
	Village           string   `gorm:"type:varchar(100)" json:"village"`  // Kelurahan / Desa
	District          string   `gorm:"type:varchar(100)" json:"district"` // Kecamatan
	City              string   `gorm:"type:varchar(100)" json:"city"`     // Kab / Kota
	Province          string   `gorm:"type:varchar(100)" json:"province"`
	PostalCode        string   `gorm:"type:varchar(20)" json:"postal_code"`
	Latitude          *float64 `json:"latitude"`
	Longitude         *float64 `json:"longitude"`
	ResidenceType     string   `gorm:"type:varchar(50)" json:"residence_type"`
	Transportation    string   `gorm:"type:varchar(50)" json:"transportation"`
	DistanceKm        *float64 `json:"distance_km"`
	TravelTimeMinutes *int     `json:"travel_time_minutes"`

	// --- Dapodik: Fisik (Periodik) ---
	HeightCm            *int `json:"height_cm"`
	WeightKg            *int `json:"weight_kg"`
	HeadCircumferenceCm *int `json:"head_circumference_cm"`

	// --- Dapodik: Kesejahteraan ---
	KpsPkhReceiver    bool   `gorm:"not null;default:false" json:"kps_pkh_receiver"`
	KpsPkhNumber      string `gorm:"type:varchar(100)" json:"kps_pkh_number"`
	KipReceiver       bool   `gorm:"not null;default:false" json:"kip_receiver"`
	KipNumber         string `gorm:"type:varchar(100)" json:"kip_number"`
	KipName           string `gorm:"type:varchar(255)" json:"kip_name"`
	KksNumber         string `gorm:"type:varchar(100)" json:"kks_number"`
	PipEligible       bool   `gorm:"not null;default:false" json:"pip_eligible"`
	PipReason         string `gorm:"type:varchar(255)" json:"pip_reason"`
	BankName          string `gorm:"type:varchar(100)" json:"bank_name"`
	BankAccountNumber string `gorm:"type:varchar(100)" json:"bank_account_number"`
	BankAccountName   string `gorm:"type:varchar(255)" json:"bank_account_name"`

	// --- Dapodik: Registrasi / Masuk ---
	RegistrationType      string     `gorm:"type:varchar(50);default:'siswa_baru'" json:"registration_type"`
	EntryDate             string     `gorm:"type:varchar(20)" json:"entry_date"`
	EntryGradeLevelID     *uuid.UUID `gorm:"type:uuid" json:"entry_grade_level_id"`
	PreviousSchoolName    string     `gorm:"type:varchar(255)" json:"previous_school_name"`
	PreviousSchoolNPSN    string     `gorm:"type:varchar(50)" json:"previous_school_npsn"`
	PreviousSkhunNumber   string     `gorm:"type:varchar(100)" json:"previous_skhun_number"`
	PreviousDiplomaNumber string     `gorm:"type:varchar(100)" json:"previous_diploma_number"`
	ExamParticipantNumber string     `gorm:"type:varchar(100)" json:"exam_participant_number"`

	// --- Dapodik: Keluar ---
	ExitDate                    string `gorm:"type:varchar(20)" json:"exit_date"`
	ExitReason                  string `gorm:"type:varchar(255)" json:"exit_reason"`
	ExitDestination             string `gorm:"type:varchar(255)" json:"exit_destination"`
	GraduationCertificateNumber string `gorm:"type:varchar(100)" json:"graduation_certificate_number"`

	// --- Data Relasi / JSON: Ortu & Wali, Prestasi, Beasiswa ---
	GuardiansJSON    string `gorm:"type:text" json:"-"` // JSON array of GuardianItem
	AchievementsJSON string `gorm:"type:text" json:"-"` // JSON array of AchievementItem
	ScholarshipsJSON string `gorm:"type:text" json:"-"` // JSON array of ScholarshipItem

	// --- Student account (API_CONTRACT §10.3) ---
	// UserID links the student to an auth account (customers.role "students").
	// Partial unique index (user_id) WHERE user_id IS NOT NULL in config.postMigrate.
	UserID      *uint      `gorm:"index" json:"user_id"`
	InvitedAt   *time.Time `json:"invited_at"`
	ActivatedAt *time.Time `json:"activated_at"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}
