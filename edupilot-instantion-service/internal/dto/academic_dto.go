package dto

import (
	"time"

	"github.com/google/uuid"
)

// --- Academic Year & Semester ---

type SemesterResponse struct {
	ID        uuid.UUID `json:"id"`
	Number    int       `json:"number"`
	Name      string    `json:"name"`
	StartDate *string   `json:"start_date"`
	EndDate   *string   `json:"end_date"`
	IsActive  bool      `json:"is_active"`
}

type AcademicYearResponse struct {
	ID           uuid.UUID          `json:"id"`
	Name         string             `json:"name"`
	StartDate    *string            `json:"start_date"`
	EndDate      *string            `json:"end_date"`
	IsActive     bool               `json:"is_active"`
	Semesters    []SemesterResponse `json:"semesters"`
	ClassCount   int                `json:"class_count"`
	StudentCount int                `json:"student_count"`
}

type AcademicYearRequest struct {
	Name      string  `json:"name" binding:"required"`
	StartDate *string `json:"start_date"`
	EndDate   *string `json:"end_date"`
}

// --- Grade Level ---

type GradeLevelResponse struct {
	ID             uuid.UUID `json:"id"`
	EducationStage string    `json:"education_stage"`
	Number         int       `json:"number"`
	Name           string    `json:"name"`
	ShortName      string    `json:"short_name"`
	IsFinal        bool      `json:"is_final"`
	SortOrder      int       `json:"sort_order"`
	ClassCount     int       `json:"class_count"`
}

type GradeLevelRequest struct {
	EducationStage string `json:"education_stage"`
	Number         int    `json:"number" binding:"required"`
	Name           string `json:"name" binding:"required"`
	ShortName      string `json:"short_name"`
	IsFinal        bool   `json:"is_final"`
	SortOrder      *int   `json:"sort_order"`
}

type GradePresetRequest struct {
	Preset    string `json:"preset" binding:"required"` // SD, MI, SMP, MTs, SMA, MA, SMK, SMK_4Y, TK, etc.
	Numbering string `json:"numbering"`                 // arabic, roman
	Replace   bool   `json:"replace"`
}

// --- Major ---

type MajorResponse struct {
	ID          uuid.UUID `json:"id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	IsActive    bool      `json:"is_active"`
}

type MajorRequest struct {
	Code        string `json:"code" binding:"required"`
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
	IsActive    *bool  `json:"is_active"`
}

// --- Class (Rombel) ---

type GradeLevelShort struct {
	ID        uuid.UUID `json:"id"`
	Number    int       `json:"number"`
	Name      string    `json:"name"`
	ShortName string    `json:"short_name"`
}

type HomeroomShort struct {
	EmployeeID uuid.UUID `json:"employee_id"`
	FullName   string    `json:"full_name"`
}

type MajorShort struct {
	ID   uuid.UUID `json:"id"`
	Code string    `json:"code"`
	Name string    `json:"name"`
}

type ClassResponse struct {
	ID               uuid.UUID        `json:"id"`
	AcademicYearID   *uuid.UUID       `json:"academic_year_id"`
	AcademicYearName string           `json:"academic_year_name"`
	GradeLevel       *GradeLevelShort `json:"grade_level"`
	Major            *MajorShort      `json:"major,omitempty"`
	Name             string           `json:"name"`
	Alias            string           `json:"alias"`
	DisplayName      string           `json:"display_name"`
	Homeroom         *HomeroomShort   `json:"homeroom"`
	Capacity         *int             `json:"capacity"`
	Room             string           `json:"room"`
	Curriculum       string           `json:"curriculum"`
	IsActive         bool             `json:"is_active"`
	StudentCount     int              `json:"student_count"`
	MaleCount        int              `json:"male_count"`
	FemaleCount      int              `json:"female_count"`
}

type ClassRequest struct {
	AcademicYearID     uuid.UUID    `json:"academic_year_id" binding:"required"`
	GradeLevelID       uuid.UUID    `json:"grade_level_id" binding:"required"`
	MajorID            OptionalUUID `json:"major_id"`
	Name               string       `json:"name" binding:"required"`
	Alias              string       `json:"alias"`
	HomeroomEmployeeID OptionalUUID `json:"homeroom_employee_id"`
	Capacity           *int         `json:"capacity"`
	Room               string       `json:"room"`
	Curriculum         string       `json:"curriculum"`
	IsActive           *bool        `json:"is_active"`
}

type ClassBulkRequest struct {
	AcademicYearID uuid.UUID    `json:"academic_year_id" binding:"required"`
	GradeLevelID   uuid.UUID    `json:"grade_level_id" binding:"required"`
	MajorID        OptionalUUID `json:"major_id"`
	Names          []string     `json:"names" binding:"required"`
	Aliases        []string     `json:"aliases"`
	Capacity       *int         `json:"capacity"`
}

type ClassCopyRequest struct {
	FromAcademicYearID uuid.UUID `json:"from_academic_year_id" binding:"required"`
	ToAcademicYearID   uuid.UUID `json:"to_academic_year_id" binding:"required"`
	IncludeHomeroom    bool      `json:"include_homeroom"`
}

// --- Student Shapes (Dapodik) ---

type CurrentClassShort struct {
	ClassID          uuid.UUID `json:"class_id"`
	DisplayName      string    `json:"display_name"`
	GradeLevelName   string    `json:"grade_level_name"`
	AcademicYearName string    `json:"academic_year_name"`
}

type GuardianItem struct {
	ID           string `json:"id,omitempty"`
	Type         string `json:"type"` // father, mother, guardian
	Name         string `json:"name"`
	NIK          string `json:"nik"`
	BirthYear    *int   `json:"birth_year"`
	Education    string `json:"education"`
	Occupation   string `json:"occupation"`
	Income       string `json:"income"`
	SpecialNeeds string `json:"special_needs"`
	Phone        string `json:"phone"`
	Email        string `json:"email"`
	Address      string `json:"address"`
	Relationship string `json:"relationship"`
	IsAlive      bool   `json:"is_alive"`
}

type AchievementItem struct {
	ID        string `json:"id,omitempty"`
	Name      string `json:"name"`
	Type      string `json:"type"`  // akademik, non_akademik
	Level     string `json:"level"` // sekolah, kecamatan, kabupaten, provinsi, nasional, internasional
	Rank      string `json:"rank"`
	Year      *int   `json:"year"`
	Organizer string `json:"organizer"`
}

type ScholarshipItem struct {
	ID        string `json:"id,omitempty"`
	Name      string `json:"name"`
	Type      string `json:"type"` // anak_berprestasi, anak_miskin, pendidikan, unggulan, lainnya
	Provider  string `json:"provider"`
	StartYear *int   `json:"start_year"`
	EndYear   *int   `json:"end_year"`
	Amount    *int64 `json:"amount"`
}

type StudentIdentity struct {
	FullName           string  `json:"full_name"`
	Nickname           string  `json:"nickname"`
	Gender             string  `json:"gender"`
	NISN               string  `json:"nisn"`
	NIS                string  `json:"nis"`
	NIK                string  `json:"nik"`
	NoKK               string  `json:"no_kk"`
	BirthPlace         string  `json:"birth_place"`
	BirthDate          string  `json:"birth_date"`
	BirthCertificateNo string  `json:"birth_certificate_no"`
	Religion           string  `json:"religion"`
	Citizenship        string  `json:"citizenship"`
	Country            string  `json:"country"`
	SpecialNeeds       string  `json:"special_needs"`
	BloodType          string  `json:"blood_type"`
	Photo              *string `json:"photo"`
	ChildOrder         *int    `json:"child_order"`
	SiblingsCount      *int    `json:"siblings_count"`
	Hobby              string  `json:"hobby"`
	Aspiration         string  `json:"aspiration"`
}

type StudentContact struct {
	Phone       string `json:"phone"`
	MobilePhone string `json:"mobile_phone"`
	Email       string `json:"email"`
}

type StudentAddress struct {
	Street            string   `json:"street"`
	RT                string   `json:"rt"`
	RW                string   `json:"rw"`
	Hamlet            string   `json:"hamlet"`
	Village           string   `json:"village"`
	District          string   `json:"district"`
	City              string   `json:"city"`
	Province          string   `json:"province"`
	PostalCode        string   `json:"postal_code"`
	Latitude          *float64 `json:"latitude"`
	Longitude         *float64 `json:"longitude"`
	ResidenceType     string   `json:"residence_type"`
	Transportation    string   `json:"transportation"`
	DistanceKm        *float64 `json:"distance_km"`
	TravelTimeMinutes *int     `json:"travel_time_minutes"`
}

type StudentPhysical struct {
	HeightCm            *int `json:"height_cm"`
	WeightKg            *int `json:"weight_kg"`
	HeadCircumferenceCm *int `json:"head_circumference_cm"`
}

type StudentWelfare struct {
	KpsPkhReceiver    bool   `json:"kps_pkh_receiver"`
	KpsPkhNumber      string `json:"kps_pkh_number"`
	KipReceiver       bool   `json:"kip_receiver"`
	KipNumber         string `json:"kip_number"`
	KipName           string `json:"kip_name"`
	KksNumber         string `json:"kks_number"`
	PipEligible       bool   `json:"pip_eligible"`
	PipReason         string `json:"pip_reason"`
	BankName          string `json:"bank_name"`
	BankAccountNumber string `json:"bank_account_number"`
	BankAccountName   string `json:"bank_account_name"`
}

type StudentRegistration struct {
	RegistrationType      string       `json:"registration_type"`
	EntryDate             string       `json:"entry_date"`
	EntryGradeLevelID     OptionalUUID `json:"entry_grade_level_id"`
	PreviousSchoolName    string       `json:"previous_school_name"`
	PreviousSchoolNPSN    string       `json:"previous_school_npsn"`
	PreviousSkhunNumber   string       `json:"previous_skhun_number"`
	PreviousDiplomaNumber string       `json:"previous_diploma_number"`
	ExamParticipantNumber string       `json:"exam_participant_number"`
}

type StudentExit struct {
	ExitDate                    string `json:"exit_date"`
	ExitReason                  string `json:"exit_reason"`
	ExitDestination             string `json:"exit_destination"`
	GraduationCertificateNumber string `json:"graduation_certificate_number"`
}

type StudentDetailResponse struct {
	ID           uuid.UUID           `json:"id"`
	SchoolID     uuid.UUID           `json:"school_id"`
	Status       string              `json:"status"`
	Completeness int                 `json:"completeness"`
	Identity     StudentIdentity     `json:"identity"`
	Contact      StudentContact      `json:"contact"`
	Address      StudentAddress      `json:"address"`
	Physical     StudentPhysical     `json:"physical"`
	Welfare      StudentWelfare      `json:"welfare"`
	Registration StudentRegistration `json:"registration"`
	Exit         StudentExit         `json:"exit"`
	Guardians    []GuardianItem      `json:"guardians"`
	Achievements []AchievementItem   `json:"achievements"`
	Scholarships []ScholarshipItem   `json:"scholarships"`
	CurrentClass *CurrentClassShort  `json:"current_class"`
	// Student account (§10.3).
	UserID        *uint      `json:"user_id"`
	AccountStatus string     `json:"account_status"`
	InvitedAt     *time.Time `json:"invited_at"`
	ActivatedAt   *time.Time `json:"activated_at"`
	CreatedAt     string     `json:"created_at"`
	UpdatedAt     string     `json:"updated_at"`
}

type StudentSummaryResponse struct {
	ID           uuid.UUID          `json:"id"`
	FullName     string             `json:"full_name"`
	Nickname     string             `json:"nickname"`
	Gender       string             `json:"gender"`
	NISN         string             `json:"nisn"`
	NIS          string             `json:"nis"`
	BirthPlace   string             `json:"birth_place"`
	BirthDate    string             `json:"birth_date"`
	Status       string             `json:"status"`
	CurrentClass *CurrentClassShort `json:"current_class"`
	Completeness int                `json:"completeness"`
	// Student account (§10.3).
	UserID        *uint  `json:"user_id"`
	AccountStatus string `json:"account_status"`
}

type StudentStatsGradeLevel struct {
	GradeLevelID uuid.UUID `json:"grade_level_id"`
	Name         string    `json:"name"`
	Male         int       `json:"male"`
	Female       int       `json:"female"`
	Total        int       `json:"total"`
}

type StudentStatsResponse struct {
	Total        int                      `json:"total"`
	Active       int                      `json:"active"`
	Male         int                      `json:"male"`
	Female       int                      `json:"female"`
	ByGradeLevel []StudentStatsGradeLevel `json:"by_grade_level"`
	Unassigned   int                      `json:"unassigned"`
	Incomplete   int                      `json:"incomplete"`
}

type StudentRequest struct {
	Identity     StudentIdentity      `json:"identity" binding:"required"`
	Contact      *StudentContact      `json:"contact"`
	Address      *StudentAddress      `json:"address"`
	Physical     *StudentPhysical     `json:"physical"`
	Welfare      *StudentWelfare      `json:"welfare"`
	Registration *StudentRegistration `json:"registration"`
	Exit         *StudentExit         `json:"exit"`
	Guardians    []GuardianItem       `json:"guardians"`
	Achievements []AchievementItem    `json:"achievements"`
	Scholarships []ScholarshipItem    `json:"scholarships"`
	Status       string               `json:"status"`
	ClassID      OptionalUUID         `json:"class_id"`
}

type StudentStatusRequest struct {
	Status          string `json:"status" binding:"required"`
	ExitDate        string `json:"exit_date"`
	ExitReason      string `json:"exit_reason"`
	ExitDestination string `json:"exit_destination"`
}

// --- Student Documents ---

type DocumentTypeResponse struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	IsRequired  bool      `json:"is_required"`
	SortOrder   int       `json:"sort_order"`
}

type StudentDocumentResponse struct {
	ID               uuid.UUID             `json:"id"`
	DocumentTypeID   uuid.UUID             `json:"document_type_id"`
	DocumentTypeName string                `json:"document_type_name"`
	DocumentType     *DocumentTypeResponse `json:"document_type,omitempty"`
	FileName         string                `json:"file_name"`
	MimeType         string                `json:"mime_type"`
	SizeBytes        int64                 `json:"size_bytes"`
	Notes            string                `json:"notes"`
	CreatedAt        string                `json:"created_at"`
}

// --- Class History & Promotion ---

type ClassHistoryResponse struct {
	ID               uuid.UUID          `json:"id"`
	Class            *CurrentClassShort `json:"class"`
	AcademicYearName string             `json:"academic_year_name"`
	GradeLevelName   string             `json:"grade_level_name"`
	Status           string             `json:"status"`
	StartDate        string             `json:"start_date"`
	EndDate          string             `json:"end_date"`
	Notes            string             `json:"notes"`
}

type PlacementRequest struct {
	ClassID   uuid.UUID `json:"class_id" binding:"required"`
	StartDate string    `json:"start_date"`
	Notes     string    `json:"notes"`
	Force     bool      `json:"force"`
}

type MembersRequest struct {
	StudentIDs []uuid.UUID `json:"student_ids" binding:"required"`
	StartDate  string      `json:"start_date"`
	Notes      string      `json:"notes"`
}

type MemberSkipped struct {
	StudentID uuid.UUID `json:"student_id"`
	Reason    string    `json:"reason"`
}

type MembersResultResponse struct {
	Placed  int             `json:"placed"`
	Skipped []MemberSkipped `json:"skipped"`
}

type PromotionItem struct {
	StudentID uuid.UUID    `json:"student_id" binding:"required"`
	Action    string       `json:"action" binding:"required"` // promote, retain, graduate
	ToClassID OptionalUUID `json:"to_class_id"`
	// Force allows graduating a student who is not in a final grade.
	Force bool `json:"force,omitempty"`
}

type PromotionRequest struct {
	ToAcademicYearID uuid.UUID       `json:"to_academic_year_id" binding:"required"`
	Items            []PromotionItem `json:"items" binding:"required"`
	// Force applies to every item (see PromotionItem.Force).
	Force bool `json:"force,omitempty"`
}

type PromotionError struct {
	StudentID uuid.UUID `json:"student_id"`
	Message   string    `json:"message"`
}

type PromotionResultResponse struct {
	Promoted  int              `json:"promoted"`
	Retained  int              `json:"retained"`
	Graduated int              `json:"graduated"`
	Errors    []PromotionError `json:"errors"`
}

type PromotionPreviewRow struct {
	StudentID          uuid.UUID  `json:"student_id"`
	FullName           string     `json:"full_name"`
	NISN               string     `json:"nisn"`
	NIS                string     `json:"nis"`
	Gender             string     `json:"gender"`
	SuggestedAction    string     `json:"suggested_action"`
	SuggestedToClassID *uuid.UUID `json:"suggested_to_class_id"`
}

type PromotionPreviewResponse struct {
	FromClass *ClassResponse        `json:"from_class"`
	Rows      []PromotionPreviewRow `json:"rows"`
}

// --- Import ---

type ImportError struct {
	Row     int    `json:"row"`
	Column  string `json:"column,omitempty"`
	Message string `json:"message"`
}

type StudentImportResult struct {
	Total   int           `json:"total"`
	Created int           `json:"created"`
	Updated int           `json:"updated"`
	Failed  int           `json:"failed"`
	DryRun  bool          `json:"dry_run"`
	Errors  []ImportError `json:"errors"`
}
