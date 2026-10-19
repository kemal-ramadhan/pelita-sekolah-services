package model

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// StringArray is a custom type for storing string slices in PostgreSQL JSONB/text.
type StringArray []string

func (a StringArray) Value() (driver.Value, error) {
	if a == nil {
		return "[]", nil
	}
	return json.Marshal(a)
}

func (a *StringArray) Scan(value interface{}) error {
	if value == nil {
		*a = []string{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		str, ok := value.(string)
		if !ok {
			return errors.New("cannot scan into StringArray")
		}
		bytes = []byte(str)
	}
	return json.Unmarshal(bytes, a)
}

// JSONMap is a custom type for storing key-value JSON objects.
type JSONMap map[string]interface{}

func (m JSONMap) Value() (driver.Value, error) {
	if m == nil {
		return "{}", nil
	}
	return json.Marshal(m)
}

func (m *JSONMap) Scan(value interface{}) error {
	if value == nil {
		*m = JSONMap{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		str, ok := value.(string)
		if !ok {
			return errors.New("cannot scan into JSONMap")
		}
		bytes = []byte(str)
	}
	return json.Unmarshal(bytes, m)
}

// Status values.
const (
	SubStatusPending  = "pending"
	SubStatusActive   = "active"
	SubStatusRejected = "rejected"
	SubStatusExpired  = "expired"
	SubStatusDepleted = "depleted"

	SessionPublished = "published"
	SessionClosed    = "closed"

	AttemptInProgress = "in_progress"
	AttemptQueued     = "queued"
	AttemptProcessing = "processing"
	AttemptCompleted  = "completed"
	AttemptFailed     = "failed"

	TargetAssigned   = "assigned"
	TargetInProgress = "in_progress"
	TargetCompleted  = "completed"

	AlertOpen         = "open"
	AlertAcknowledged = "acknowledged"
	AlertResolved     = "resolved"

	TargetTypeClass    = "class"
	TargetTypeRandom   = "random_students"
	TargetTypeStudents = "students"
)

// AssessmentPackage merepresentasikan produk paket asesmen yang dijual/disediakan platform.
type AssessmentPackage struct {
	ID              uuid.UUID   `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	Name            string      `gorm:"type:varchar(150);not null" json:"name"`
	Code            string      `gorm:"type:varchar(50);not null;uniqueIndex" json:"code"`
	Description     string      `gorm:"type:text" json:"description"`
	Price           float64     `gorm:"type:numeric(12,2);not null;default:0" json:"price"`
	StudentQuota    int         `gorm:"type:int;not null;default:100" json:"student_quota"`
	InstrumentCodes StringArray `gorm:"type:jsonb;not null;default:'[]'" json:"instrument_codes"`
	IsBundle        bool        `gorm:"not null;default:false" json:"is_bundle"`
	IsActive        bool        `gorm:"not null;default:true" json:"is_active"`
	BadgeColor      string      `gorm:"type:varchar(30);default:'indigo'" json:"badge_color"`
	CreatedAt       time.Time   `json:"created_at"`
	UpdatedAt       time.Time   `json:"updated_at"`
}

// AssessmentVoucher merepresentasikan kode voucher promosi/klaim gratis yang dibuat Admin.
type AssessmentVoucher struct {
	ID                   uuid.UUID   `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	Code                 string      `gorm:"type:varchar(50);not null;uniqueIndex" json:"code"`
	Name                 string      `gorm:"type:varchar(150);not null" json:"name"`
	DiscountType         string      `gorm:"type:varchar(30);not null" json:"discount_type"` // free_claim, percentage, fixed_amount
	DiscountValue        float64     `gorm:"type:numeric(12,2);not null;default:100" json:"discount_value"`
	MaxUsage             int         `gorm:"type:int;not null;default:100" json:"max_usage"`
	UsedCount            int         `gorm:"type:int;not null;default:0" json:"used_count"`
	ValidFrom            time.Time   `json:"valid_from"`
	ValidUntil           time.Time   `json:"valid_until"`
	ApplicablePackageIDs StringArray `gorm:"type:jsonb;default:'[]'" json:"applicable_package_ids"` // kosong = semua paket
	IsActive             bool        `gorm:"not null;default:true" json:"is_active"`
	CreatedAt            time.Time   `json:"created_at"`
	UpdatedAt            time.Time   `json:"updated_at"`
}

// SchoolAssessmentSubscription merepresentasikan kuota/paket yang dimiliki sekolah.
// Pembelian berbayar berstatus pending sampai disetujui admin (§13.3/§13.4).
type SchoolAssessmentSubscription struct {
	ID               uuid.UUID  `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	SchoolID         uuid.UUID  `gorm:"type:uuid;not null;index" json:"school_id"`
	SchoolName       string     `gorm:"type:varchar(255);not null;default:''" json:"school_name,omitempty"`
	PackageID        uuid.UUID  `gorm:"type:uuid;not null;index" json:"package_id"`
	VoucherID        *uuid.UUID `gorm:"type:uuid;index" json:"voucher_id,omitempty"`
	ClaimType        string     `gorm:"type:varchar(30);not null" json:"claim_type"` // purchase, voucher
	TotalQuota       int        `gorm:"type:int;not null" json:"total_quota"`
	UsedQuota        int        `gorm:"type:int;not null;default:0" json:"used_quota"`
	AmountDue        float64    `gorm:"type:numeric(12,2);not null;default:0" json:"amount_due"`
	Status           string     `gorm:"type:varchar(30);not null;default:'active';index" json:"status"`
	ExpiresAt        time.Time  `json:"expires_at"`
	RequestedBy      uint       `gorm:"not null;default:0" json:"-"`
	ReviewedByUserID *uint      `json:"-"`
	ReviewedAt       *time.Time `json:"reviewed_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`

	Package *AssessmentPackage `gorm:"foreignKey:PackageID" json:"package,omitempty"`
	Voucher *AssessmentVoucher `gorm:"foreignKey:VoucherID" json:"voucher,omitempty"`
}

// AssessmentVoucherLog mencatat sekolah yang mengklaim voucher (unik per voucher+sekolah).
type AssessmentVoucherLog struct {
	ID             uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	VoucherID      uuid.UUID `gorm:"type:uuid;not null;index" json:"voucher_id"`
	SchoolID       uuid.UUID `gorm:"type:uuid;not null;index" json:"school_id"`
	SchoolName     string    `gorm:"type:varchar(255);not null;default:''" json:"school_name,omitempty"`
	SubscriptionID uuid.UUID `gorm:"type:uuid;not null;index" json:"subscription_id"`
	ClaimedAt      time.Time `json:"claimed_at"`

	Voucher *AssessmentVoucher `gorm:"foreignKey:VoucherID" json:"-"`
}

// AssessmentSession = penugasan asesmen (satu/lebih instrumen) kepada target siswa.
// Kolom lama instrument_code / duration_minutes dibiarkan (nullable) untuk data lama.
type AssessmentSession struct {
	ID              uuid.UUID   `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	SchoolID        uuid.UUID   `gorm:"type:uuid;not null;index" json:"school_id"`
	SubscriptionID  uuid.UUID   `gorm:"type:uuid;not null;index" json:"subscription_id"`
	CreatedByUserID uint        `gorm:"not null;default:0" json:"-"`
	Title           string      `gorm:"type:varchar(200);not null" json:"title"`
	Description     string      `gorm:"type:text" json:"description"`
	TargetType      string      `gorm:"type:varchar(30);not null;default:'class'" json:"target_type"`
	ClassIDs        StringArray `gorm:"type:jsonb;default:'[]'" json:"class_ids"`
	InstrumentCodes StringArray `gorm:"type:jsonb;not null;default:'[]'" json:"instrument_codes"`
	OpensAt         time.Time   `json:"opens_at"`
	ClosesAt        time.Time   `json:"closes_at"`
	MaxAttempts     int         `gorm:"type:int;not null;default:1" json:"max_attempts"`
	Status          string      `gorm:"type:varchar(30);not null;default:'published';index" json:"status"`
	CreatedAt       time.Time   `json:"created_at"`
	UpdatedAt       time.Time   `json:"updated_at"`

	Subscription *SchoolAssessmentSubscription `gorm:"foreignKey:SubscriptionID" json:"subscription,omitempty"`
}

// AssessmentSessionTarget = siswa sasaran sesi (snapshot nama/kelas saat dibuat).
type AssessmentSessionTarget struct {
	ID              uuid.UUID  `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	SessionID       uuid.UUID  `gorm:"type:uuid;not null;index" json:"session_id"`
	StudentID       uuid.UUID  `gorm:"type:uuid;not null;index" json:"student_id"`
	UserID          *uint      `json:"user_id,omitempty"`
	FullName        string     `gorm:"type:varchar(255);not null;default:''" json:"full_name"`
	NIS             string     `gorm:"type:varchar(50);not null;default:''" json:"nis"`
	ClassID         *uuid.UUID `gorm:"type:uuid;index" json:"class_id,omitempty"`
	ClassName       string     `gorm:"type:varchar(255);not null;default:''" json:"class_name"`
	Status          string     `gorm:"type:varchar(30);not null;default:'assigned'" json:"status"`
	QuotaConsumedAt *time.Time `json:"-"`
	CreatedAt       time.Time  `json:"created_at"`
}

// AssessmentAttempt = satu pengerjaan satu instrumen oleh satu siswa.
type AssessmentAttempt struct {
	ID             uuid.UUID   `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	SessionID      uuid.UUID   `gorm:"type:uuid;not null;index" json:"session_id"`
	StudentID      uuid.UUID   `gorm:"type:uuid;not null;index" json:"student_id"`
	UserID         uint        `gorm:"not null;default:0;index" json:"-"`
	SchoolID       uuid.UUID   `gorm:"type:uuid;not null;index" json:"school_id"`
	ClassID        *uuid.UUID  `gorm:"type:uuid" json:"class_id,omitempty"`
	InstrumentCode string      `gorm:"type:varchar(20);not null" json:"instrument_code"`
	Status         string      `gorm:"type:varchar(30);not null;default:'in_progress';index" json:"status"`
	Seed           int64       `gorm:"not null;default:0" json:"seed"`
	QuestionOrder  StringArray `gorm:"type:jsonb;default:'[]'" json:"-"`
	SectionStarts  JSONMap     `gorm:"type:jsonb;default:'{}'" json:"-"`
	StartedAt      time.Time   `json:"started_at"`
	SubmittedAt    *time.Time  `json:"submitted_at,omitempty"`
	DurationSec    int         `gorm:"type:int;not null;default:0" json:"duration_sec"`
	ValidityFlags  StringArray `gorm:"type:jsonb;default:'[]'" json:"validity_flags"`
	RetryCount     int         `gorm:"not null;default:0" json:"-"`
	FailureReason  string      `gorm:"type:text;not null;default:''" json:"-"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`

	Session *AssessmentSession `gorm:"foreignKey:SessionID" json:"session,omitempty"`
}

// AssessmentResponse = butir jawaban (unik per attempt+item, autosave upsert).
type AssessmentResponse struct {
	ID             uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	AttemptID      uuid.UUID `gorm:"type:uuid;not null;index" json:"attempt_id"`
	ItemID         string    `gorm:"type:varchar(50);not null;index" json:"item_id"`
	Value          string    `gorm:"type:varchar(50);not null" json:"value"`
	ReactionTimeMs int       `gorm:"type:int;default:0" json:"reaction_time_ms"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// AssessmentResult = hasil olahan mesin skoring (deterministik, berversi).
type AssessmentResult struct {
	ID                  uuid.UUID   `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	AttemptID           uuid.UUID   `gorm:"type:uuid;not null;uniqueIndex" json:"attempt_id"`
	SessionID           uuid.UUID   `gorm:"type:uuid;not null;index" json:"session_id"`
	StudentID           uuid.UUID   `gorm:"type:uuid;not null;index" json:"student_id"`
	SchoolID            uuid.UUID   `gorm:"type:uuid;not null;index" json:"school_id"`
	ClassID             *uuid.UUID  `gorm:"type:uuid;index" json:"class_id,omitempty"`
	StudentName         string      `gorm:"type:varchar(255);not null;default:''" json:"student_name"`
	NIS                 string      `gorm:"type:varchar(50);not null;default:''" json:"nis"`
	ClassName           string      `gorm:"type:varchar(255);not null;default:''" json:"class_name"`
	InstrumentCode      string      `gorm:"type:varchar(20);not null;index" json:"instrument_code"`
	HollandCode         string      `gorm:"type:varchar(10)" json:"holland_code,omitempty"`
	ScoresJSON          JSONMap     `gorm:"type:jsonb;not null;default:'{}'" json:"scores"`
	IndicesJSON         JSONMap     `gorm:"type:jsonb;not null;default:'{}'" json:"indices"`
	VarkCategory        string      `gorm:"type:varchar(100)" json:"vark_category,omitempty"`
	RecommendedTrack    string      `gorm:"type:varchar(100)" json:"recommended_track,omitempty"`
	FlagsJSON           StringArray `gorm:"type:jsonb;default:'[]'" json:"flags"`
	ValidityFlags       StringArray `gorm:"type:jsonb;default:'[]'" json:"validity_flags"`
	RecommendationsJSON JSONMap     `gorm:"type:jsonb;default:'{}'" json:"recommendations"`
	Interpretation      JSONMap     `gorm:"type:jsonb;default:'{}'" json:"interpretation"`
	StaffNotes          StringArray `gorm:"type:jsonb;default:'[]'" json:"staff_notes"`
	ReportSummary       string      `gorm:"type:text" json:"report_summary"`
	InstrumentVersion   string      `gorm:"type:varchar(50);not null;default:''" json:"instrument_version"`
	ConfigVersion       string      `gorm:"type:varchar(80);not null;default:''" json:"config_version"`
	CreatedAt           time.Time   `json:"created_at"`
	UpdatedAt           time.Time   `json:"updated_at"`
}

// AssessmentQuestionBank menyimpan butir instrumen (sumber metadata skoring).
type AssessmentQuestionBank struct {
	ID                uuid.UUID   `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	InstrumentCode    string      `gorm:"type:varchar(20);not null;index" json:"instrument_code"`
	Subtest           string      `gorm:"type:varchar(50);not null;index" json:"subtest"`
	ItemCode          string      `gorm:"type:varchar(50);not null;uniqueIndex" json:"item_code"`
	QuestionText      string      `gorm:"type:text;not null" json:"question_text"`
	QuestionType      string      `gorm:"type:varchar(30);not null;default:'likert'" json:"question_type"`
	ScaleMin          int         `gorm:"type:int;default:1" json:"scale_min"`
	ScaleMax          int         `gorm:"type:int;default:5" json:"scale_max"`
	ScaleLabels       StringArray `gorm:"type:jsonb;default:'[]'" json:"scale_labels"`
	Options           StringArray `gorm:"type:jsonb;default:'[]'" json:"options"`
	CorrectKey        string      `gorm:"type:varchar(10)" json:"correct_key"`
	Dimension         string      `gorm:"type:varchar(30)" json:"dimension"`
	Scored            bool        `gorm:"not null;default:true" json:"scored"`
	AttentionExpected *int        `json:"attention_expected,omitempty"`
	Weight            float64     `gorm:"type:decimal(5,2);default:1.00" json:"weight"`
	OrderNum          int         `gorm:"type:int;not null;default:0;index" json:"order_num"`
	IsActive          bool        `gorm:"type:boolean;not null;default:true" json:"is_active"`
	CreatedAt         time.Time   `json:"created_at"`
	UpdatedAt         time.Time   `json:"updated_at"`
}

// AssessmentConsent = persetujuan siswa sebelum memulai (FR-03, PRD §11).
type AssessmentConsent struct {
	ID             uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	SessionID      uuid.UUID `gorm:"type:uuid;not null;index" json:"session_id"`
	StudentID      uuid.UUID `gorm:"type:uuid;not null;index" json:"student_id"`
	UserID         uint      `gorm:"not null" json:"-"`
	InstrumentCode string    `gorm:"type:varchar(20);not null" json:"instrument_code"`
	TermsVersion   string    `gorm:"type:varchar(30);not null" json:"terms_version"`
	IP             string    `gorm:"type:varchar(64);not null;default:''" json:"-"`
	AcceptedAt     time.Time `json:"accepted_at"`
}

// AssessmentWelfareAlert = tindak lanjut FLAG_KESEJAHTERAAN (PRD §9.3, FR-19).
type AssessmentWelfareAlert struct {
	ID              uuid.UUID  `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	SchoolID        uuid.UUID  `gorm:"type:uuid;not null;index" json:"school_id"`
	StudentID       uuid.UUID  `gorm:"type:uuid;not null;index" json:"student_id"`
	StudentName     string     `gorm:"type:varchar(255);not null;default:''" json:"student_name"`
	ClassID         *uuid.UUID `gorm:"type:uuid" json:"class_id,omitempty"`
	ClassName       string     `gorm:"type:varchar(255);not null;default:''" json:"class_name"`
	ResultID        uuid.UUID  `gorm:"type:uuid;not null;uniqueIndex" json:"result_id"`
	SessionID       uuid.UUID  `gorm:"type:uuid;not null;index" json:"session_id"`
	Flag            string     `gorm:"type:varchar(50);not null" json:"flag"`
	Status          string     `gorm:"type:varchar(20);not null;default:'open';index" json:"status"`
	Note            string     `gorm:"type:text;not null;default:''" json:"note"`
	HandledByUserID *uint      `json:"-"`
	HandledByName   string     `gorm:"type:varchar(255);not null;default:''" json:"handled_by_name,omitempty"`
	HandledAt       *time.Time `json:"handled_at,omitempty"`
	NotifiedAt      *time.Time `json:"-"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// AssessmentAuditLog = log akses hasil individual oleh staf (FR-18).
type AssessmentAuditLog struct {
	ID              uuid.UUID  `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	SchoolID        uuid.UUID  `gorm:"type:uuid;not null;index" json:"school_id"`
	ActorUserID     uint       `gorm:"not null;index" json:"actor_user_id"`
	ActorKind       string     `gorm:"type:varchar(20);not null" json:"actor_kind"`
	Action          string     `gorm:"type:varchar(50);not null" json:"action"`
	TargetStudentID *uuid.UUID `gorm:"type:uuid;index" json:"target_student_id,omitempty"`
	ResultID        *uuid.UUID `gorm:"type:uuid" json:"result_id,omitempty"`
	IP              string     `gorm:"type:varchar(64);not null;default:''" json:"ip"`
	At              time.Time  `gorm:"not null;index" json:"at"`
}

// CreateQuestion inserts a question keeping its zero values. GORM omits zero
// values of fields that carry a DB default on INSERT (scale_min/max default
// 1/5, scored/is_active default true) and writes the defaults back into the
// struct, which would turn boolean (0..1) / multiple-choice (0/0) scales into
// 1..5 and attention checks into scored items.
func CreateQuestion(db *gorm.DB, q *AssessmentQuestionBank) error {
	want := *q
	if err := db.Create(q).Error; err != nil {
		return err
	}
	if err := db.Exec(`UPDATE assessment_question_banks SET scale_min = ?, scale_max = ?, scored = ?, is_active = ?, weight = ? WHERE id = ?`,
		want.ScaleMin, want.ScaleMax, want.Scored, want.IsActive, want.Weight, q.ID).Error; err != nil {
		return err
	}
	q.ScaleMin, q.ScaleMax, q.Scored, q.IsActive, q.Weight = want.ScaleMin, want.ScaleMax, want.Scored, want.IsActive, want.Weight
	return nil
}
