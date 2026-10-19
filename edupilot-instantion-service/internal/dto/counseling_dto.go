package dto

import (
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"github.com/edupilot-instantion-service/internal/model"
	"github.com/google/uuid"
)

// CounselorToggleRequest enables or disables the counselor role on a teacher.
type CounselorToggleRequest struct {
	IsCounselor bool `json:"is_counselor"`
}

// CounselingSessionRequest is used by students or counselors to create a counseling session.
type CounselingSessionRequest struct {
	CounselorID           OptionalUUID `json:"counselor_id"`
	InitiatorStudentID    OptionalUUID `json:"initiator_student_id"`
	ClassID               OptionalUUID `json:"class_id"`
	CounselingType        string       `json:"counseling_type"` // individual, group, classical, case_handling
	Topic                 string       `json:"topic"`
	Description           string       `json:"description"`
	RequestedDate         string       `json:"requested_date"` // YYYY-MM-DD
	StartTime             string       `json:"start_time"`     // HH:MM
	EndTime               string       `json:"end_time"`       // HH:MM
	Place                 string       `json:"place"`
	ParticipantStudentIDs []uuid.UUID  `json:"participant_student_ids"`
}

func (r *CounselingSessionRequest) Normalize() string {
	r.Topic = strings.TrimSpace(r.Topic)
	r.Description = strings.TrimSpace(r.Description)
	r.RequestedDate = strings.TrimSpace(r.RequestedDate)
	r.StartTime = strings.TrimSpace(r.StartTime)
	r.EndTime = strings.TrimSpace(r.EndTime)
	r.Place = strings.TrimSpace(r.Place)
	r.CounselingType = strings.TrimSpace(r.CounselingType)

	if r.Topic == "" {
		return "topic wajib diisi"
	}
	if len(r.Topic) > 255 {
		return "topic maksimal 255 karakter"
	}
	if len(r.Description) > 5000 {
		return "description maksimal 5000 karakter"
	}
	if len(r.Place) > 150 {
		return "place maksimal 150 karakter"
	}
	if len(r.ParticipantStudentIDs) > MaxCounselingParticipants {
		return "participant_student_ids maksimal 10 siswa"
	}
	if r.RequestedDate == "" || !ValidDate(r.RequestedDate) {
		return "requested_date harus berupa tanggal YYYY-MM-DD yang valid"
	}
	if !ValidHHMM(r.StartTime) {
		return "start_time harus berformat HH:MM"
	}
	if !ValidHHMM(r.EndTime) {
		return "end_time harus berformat HH:MM"
	}
	if r.EndTime <= r.StartTime {
		return "end_time harus setelah start_time"
	}
	if r.Place == "" {
		r.Place = "Ruang BK"
	}
	if r.CounselingType == "" {
		r.CounselingType = model.CounselingTypeIndividual
	}
	switch r.CounselingType {
	case model.CounselingTypeIndividual, model.CounselingTypeGroup, model.CounselingTypeClassical, model.CounselingTypeCaseHandling:
	default:
		return "counseling_type tidak valid (pilihan: individual, group, classical, case_handling)"
	}
	return ""
}

// CounselingSessionStatusRequest updates status (approve, reschedule, complete, cancel).
type CounselingSessionStatusRequest struct {
	Status           string `json:"status"` // pending, scheduled, rescheduled, completed, cancelled
	RescheduleReason string `json:"reschedule_reason"`
	NewDate          string `json:"new_date"`       // optional
	NewStartTime     string `json:"new_start_time"` // optional
	NewEndTime       string `json:"new_end_time"`   // optional
	NewPlace         string `json:"new_place"`      // optional
}

func (r *CounselingSessionStatusRequest) Normalize() string {
	r.Status = strings.TrimSpace(r.Status)
	r.RescheduleReason = strings.TrimSpace(r.RescheduleReason)
	r.NewDate = strings.TrimSpace(r.NewDate)
	r.NewStartTime = strings.TrimSpace(r.NewStartTime)
	r.NewEndTime = strings.TrimSpace(r.NewEndTime)
	r.NewPlace = strings.TrimSpace(r.NewPlace)

	switch r.Status {
	case model.CounselingStatusPending, model.CounselingStatusScheduled, model.CounselingStatusRescheduled, model.CounselingStatusCompleted, model.CounselingStatusCancelled:
	default:
		return "status tidak valid"
	}
	if r.Status == model.CounselingStatusRescheduled {
		if r.NewDate != "" && !ValidDate(r.NewDate) {
			return "new_date harus berformat YYYY-MM-DD"
		}
		if r.NewStartTime != "" && !ValidHHMM(r.NewStartTime) {
			return "new_start_time harus berformat HH:MM"
		}
		if r.NewEndTime != "" && !ValidHHMM(r.NewEndTime) {
			return "new_end_time harus berformat HH:MM"
		}
		if r.RescheduleReason == "" {
			return "reschedule_reason wajib diisi saat menjadwalkan ulang"
		}
		if r.NewDate == "" && r.NewStartTime == "" && r.NewEndTime == "" && r.NewPlace == "" {
			return "Isi new_date, new_start_time, new_end_time, atau new_place untuk menjadwalkan ulang"
		}
	}
	if r.Status == model.CounselingStatusScheduled {
		if r.NewDate != "" && !ValidDate(r.NewDate) {
			return "new_date harus berformat YYYY-MM-DD"
		}
		if r.NewStartTime != "" && !ValidHHMM(r.NewStartTime) {
			return "new_start_time harus berformat HH:MM"
		}
		if r.NewEndTime != "" && !ValidHHMM(r.NewEndTime) {
			return "new_end_time harus berformat HH:MM"
		}
	}
	if len(r.NewPlace) > 150 {
		return "new_place maksimal 150 karakter"
	}
	if len(r.RescheduleReason) > 2000 {
		return "reschedule_reason maksimal 2000 karakter"
	}
	return ""
}

// CounselingOutcomeRequest records session results, confidential notes, feedback, and attachments.
type CounselingOutcomeRequest struct {
	ConfidentialNotes  string   `json:"confidential_notes"` // Hanya untuk Guru BK & Kepsek
	StudentFeedback    string   `json:"student_feedback"`   // Umpan balik yang dibaca oleh siswa
	OutcomeStatus      string   `json:"outcome_status"`     // resolved, needs_followup, referral
	FollowUpPlan       string   `json:"follow_up_plan"`
	FollowUpDate       *string  `json:"follow_up_date"`
	AttachmentURLs     []string `json:"attachment_urls"`
	CreateJournalEntry bool     `json:"create_journal_entry"`
	JournalServiceType string   `json:"journal_service_type"`
}

func (r *CounselingOutcomeRequest) Normalize() string {
	r.ConfidentialNotes = strings.TrimSpace(r.ConfidentialNotes)
	r.StudentFeedback = strings.TrimSpace(r.StudentFeedback)
	r.OutcomeStatus = strings.TrimSpace(r.OutcomeStatus)
	r.FollowUpPlan = strings.TrimSpace(r.FollowUpPlan)

	if r.OutcomeStatus == "" {
		r.OutcomeStatus = model.OutcomeStatusResolved
	}
	switch r.OutcomeStatus {
	case model.OutcomeStatusResolved, model.OutcomeStatusNeedsFollowup, model.OutcomeStatusReferral:
	default:
		return "outcome_status tidak valid (pilihan: resolved, needs_followup, referral)"
	}
	if r.FollowUpDate != nil && *r.FollowUpDate != "" {
		*r.FollowUpDate = strings.TrimSpace(*r.FollowUpDate)
		if !ValidDate(*r.FollowUpDate) {
			return "follow_up_date harus berformat YYYY-MM-DD"
		}
	} else {
		r.FollowUpDate = nil
	}
	if r.OutcomeStatus == model.OutcomeStatusNeedsFollowup && r.FollowUpPlan == "" {
		return "follow_up_plan wajib diisi bila outcome_status needs_followup"
	}
	r.JournalServiceType = strings.TrimSpace(r.JournalServiceType)
	if r.JournalServiceType != "" && !ValidJournalServiceType(r.JournalServiceType) {
		return "journal_service_type tidak valid"
	}
	if msg := ValidateAttachmentURLs(r.AttachmentURLs); msg != "" {
		return msg
	}
	return ""
}

// CounselingEventRequest posts a large event or group seminar.
type CounselingEventRequest struct {
	Title          string   `json:"title"`
	Category       string   `json:"category"` // Seminar, Bimbingan Klasikal, Workshop, Motivasi
	Description    string   `json:"description"`
	EventDate      string   `json:"event_date"` // YYYY-MM-DD
	StartTime      string   `json:"start_time"` // HH:MM
	EndTime        string   `json:"end_time"`   // HH:MM
	Location       string   `json:"location"`
	TargetAudience string   `json:"target_audience"` // all, students, teachers, specific_grade
	AttachmentURLs []string `json:"attachment_urls"`
}

func (r *CounselingEventRequest) Normalize() string {
	r.Title = strings.TrimSpace(r.Title)
	r.Category = strings.TrimSpace(r.Category)
	r.Description = strings.TrimSpace(r.Description)
	r.EventDate = strings.TrimSpace(r.EventDate)
	r.StartTime = strings.TrimSpace(r.StartTime)
	r.EndTime = strings.TrimSpace(r.EndTime)
	r.Location = strings.TrimSpace(r.Location)
	r.TargetAudience = strings.TrimSpace(r.TargetAudience)

	if r.Title == "" {
		return "title wajib diisi"
	}
	if r.EventDate == "" || !ValidDate(r.EventDate) {
		return "event_date harus berformat YYYY-MM-DD"
	}
	if !ValidHHMM(r.StartTime) {
		return "start_time harus berformat HH:MM"
	}
	if !ValidHHMM(r.EndTime) {
		return "end_time harus berformat HH:MM"
	}
	if r.EndTime <= r.StartTime {
		return "end_time harus setelah start_time"
	}
	if r.Location == "" {
		return "location wajib diisi"
	}
	if r.TargetAudience == "" {
		r.TargetAudience = EventAudienceAll
	}
	switch r.TargetAudience {
	case EventAudienceAll, EventAudienceStudents, EventAudienceTeachers, EventAudienceSpecificGrade:
	default:
		return "target_audience tidak valid (pilihan: all, students, teachers, specific_grade)"
	}
	if len(r.Title) > 255 {
		return "title maksimal 255 karakter"
	}
	if len(r.Category) > 100 {
		return "category maksimal 100 karakter"
	}
	if len(r.Location) > 150 {
		return "location maksimal 150 karakter"
	}
	if r.Category == "" {
		r.Category = "Lainnya"
	}
	if msg := ValidateAttachmentURLs(r.AttachmentURLs); msg != "" {
		return msg
	}
	return ""
}

// CounselingEventConclusionRequest saves conclusion and documentation after an event.
type CounselingEventConclusionRequest struct {
	ConclusionNotes string   `json:"conclusion_notes"`
	AttachmentURLs  []string `json:"attachment_urls"`
}

func (r *CounselingEventConclusionRequest) Normalize() string {
	r.ConclusionNotes = strings.TrimSpace(r.ConclusionNotes)
	if r.ConclusionNotes == "" {
		return "conclusion_notes wajib diisi"
	}
	return ValidateAttachmentURLs(r.AttachmentURLs)
}

// CounselingEventAttendRequest records check-in to an event.
type CounselingEventAttendRequest struct {
	Notes string `json:"notes"`
}

// CounselingDailyJournalRequest logs daily counselor services according to BK guidelines.
type CounselingDailyJournalRequest struct {
	Date                string       `json:"date"`         // YYYY-MM-DD
	ServiceType         string       `json:"service_type"` // orientasi, informasi, konseling_individual, bimbingan_kelompok, konseling_kelompok, mediasi, home_visit, konferensi_kasus, kolaborasi, administrasi
	ActivityTitle       string       `json:"activity_title"`
	TargetDescription   string       `json:"target_description"`
	ActivitySummary     string       `json:"activity_summary"`
	EvaluationResult    string       `json:"evaluation_result"`
	FollowUpAction      string       `json:"follow_up_action"`
	CounselingSessionID OptionalUUID `json:"counseling_session_id"`
	EventID             OptionalUUID `json:"event_id"`
	AttachmentURLs      []string     `json:"attachment_urls"`
}

func (r *CounselingDailyJournalRequest) Normalize() string {
	r.Date = strings.TrimSpace(r.Date)
	r.ServiceType = strings.TrimSpace(r.ServiceType)
	r.ActivityTitle = strings.TrimSpace(r.ActivityTitle)
	r.TargetDescription = strings.TrimSpace(r.TargetDescription)
	r.ActivitySummary = strings.TrimSpace(r.ActivitySummary)
	r.EvaluationResult = strings.TrimSpace(r.EvaluationResult)
	r.FollowUpAction = strings.TrimSpace(r.FollowUpAction)

	if r.Date == "" || !ValidDate(r.Date) {
		return "date harus berformat YYYY-MM-DD"
	}
	if r.ServiceType == "" {
		return "service_type wajib diisi"
	}
	if !ValidJournalServiceType(r.ServiceType) {
		return "service_type tidak valid"
	}
	if len(r.ActivityTitle) > 255 || len(r.TargetDescription) > 255 {
		return "activity_title / target_description maksimal 255 karakter"
	}
	if msg := ValidateAttachmentURLs(r.AttachmentURLs); msg != "" {
		return msg
	}
	if r.ActivityTitle == "" {
		return "activity_title wajib diisi"
	}
	if r.ActivitySummary == "" {
		return "activity_summary wajib diisi"
	}
	return ""
}

// CounselingDashboardSummary provides aggregate statistics for BK.
type CounselingDashboardSummary struct {
	TotalCounselors        int64 `json:"total_counselors"`
	PendingRequestsCount   int64 `json:"pending_requests_count"`
	ScheduledSessionsCount int64 `json:"scheduled_sessions_count"`
	CompletedSessionsCount int64 `json:"completed_sessions_count"`
	UpcomingEventsCount    int64 `json:"upcoming_events_count"`
	TotalJournalEntries    int64 `json:"total_journal_entries"`
}

// Helper to convert string slice to JSON array string
func StringSliceToJSON(slice []string) string {
	if len(slice) == 0 {
		return "[]"
	}
	bytes, err := json.Marshal(slice)
	if err != nil {
		return "[]"
	}
	return string(bytes)
}

// Helper to convert JSON array string to string slice
func JSONToStringSlice(s string) []string {
	if strings.TrimSpace(s) == "" {
		return []string{}
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return []string{}
	}
	return out
}

// ---------- BK constants & validation helpers ----------

// MaxCounselingParticipants caps invited peers per session.
const MaxCounselingParticipants = 10

// Event target audiences.
const (
	EventAudienceAll           = "all"
	EventAudienceStudents      = "students"
	EventAudienceTeachers      = "teachers"
	EventAudienceSpecificGrade = "specific_grade"
)

// JournalServiceTypes are the BK service types accepted in daily journals
// (POP BK, Permendikbud 111/2014) plus the auto-journal fallbacks.
var JournalServiceTypes = []string{
	"orientasi", "informasi", "konseling_individual", "bimbingan_kelompok", "konseling_kelompok",
	"bimbingan_klasikal", "mediasi", "home_visit", "konferensi_kasus", "kolaborasi", "administrasi",
	"layanan_responsif", "lainnya",
}

func ValidJournalServiceType(s string) bool {
	for _, v := range JournalServiceTypes {
		if v == s {
			return true
		}
	}
	return false
}

// ValidateAttachmentURLs: at most 10 http(s) URLs of ≤ 500 chars.
func ValidateAttachmentURLs(urls []string) string {
	if len(urls) > 10 {
		return "attachment_urls maksimal 10 tautan"
	}
	for i, raw := range urls {
		u := strings.TrimSpace(raw)
		urls[i] = u
		if len(u) > 500 {
			return "attachment_urls: tautan maksimal 500 karakter"
		}
		p, err := url.Parse(u)
		if err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" {
			return "attachment_urls harus berupa tautan http(s) yang valid"
		}
	}
	return ""
}

// ---------- BK response views (confidentiality-aware) ----------

// CounselingStudentInfo is the minimal student data shown to BK staff.
type CounselingStudentInfo struct {
	ID       uuid.UUID `json:"id"`
	FullName string    `json:"full_name"`
	NIS      string    `json:"nis"`
	NISN     string    `json:"nisn"`
	Gender   string    `json:"gender"`
	Status   string    `json:"status"`
}

// PersonRef is an {id, full_name} reference (used in student-facing views).
type PersonRef struct {
	ID       uuid.UUID `json:"id"`
	FullName string    `json:"full_name"`
}

// ClassRef is a minimal class reference.
type ClassRef struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	ClassName string    `json:"class_name"`
}

// CounselingParticipantView (staff).
type CounselingParticipantView struct {
	ID                  uuid.UUID              `json:"id"`
	SchoolID            uuid.UUID              `json:"school_id"`
	CounselingSessionID uuid.UUID              `json:"counseling_session_id"`
	StudentID           uuid.UUID              `json:"student_id"`
	IsInitiator         bool                   `json:"is_initiator"`
	AttendanceStatus    string                 `json:"attendance_status"`
	FullName            string                 `json:"full_name"`
	CreatedAt           time.Time              `json:"created_at"`
	UpdatedAt           time.Time              `json:"updated_at"`
	Student             *CounselingStudentInfo `json:"student,omitempty"`
}

// CounselingSessionView is the staff (assigned counselor) view of a session.
type CounselingSessionView struct {
	ID                 uuid.UUID                   `json:"id"`
	SchoolID           uuid.UUID                   `json:"school_id"`
	CounselorID        uuid.UUID                   `json:"counselor_id"`
	InitiatorStudentID uuid.UUID                   `json:"initiator_student_id"`
	ClassID            *uuid.UUID                  `json:"class_id,omitempty"`
	CounselingType     string                      `json:"counseling_type"`
	Topic              string                      `json:"topic"`
	Description        string                      `json:"description"`
	RequestedDate      string                      `json:"requested_date"`
	StartTime          string                      `json:"start_time"`
	EndTime            string                      `json:"end_time"`
	Place              string                      `json:"place"`
	Status             string                      `json:"status"`
	RescheduleReason   string                      `json:"reschedule_reason,omitempty"`
	CreatedAt          time.Time                   `json:"created_at"`
	UpdatedAt          time.Time                   `json:"updated_at"`
	Counselor          *model.Employees            `json:"counselor,omitempty"`
	InitiatorStudent   *CounselingStudentInfo      `json:"initiator_student,omitempty"`
	Class              *ClassRef                   `json:"class,omitempty"`
	Participants       []CounselingParticipantView `json:"participants"`
	Outcome            *model.CounselingOutcome    `json:"outcome,omitempty"`
}

// StudentParticipantView: what a student sees about co-participants.
type StudentParticipantView struct {
	StudentID   uuid.UUID  `json:"student_id"`
	IsInitiator bool       `json:"is_initiator"`
	FullName    string     `json:"full_name"`
	Student     *PersonRef `json:"student,omitempty"`
}

// StudentOutcomeView: only the parts of the outcome meant for the student
// (never confidential_notes or attachments).
type StudentOutcomeView struct {
	StudentFeedback string  `json:"student_feedback"`
	OutcomeStatus   string  `json:"outcome_status"`
	FollowUpPlan    string  `json:"follow_up_plan,omitempty"`
	FollowUpDate    *string `json:"follow_up_date,omitempty"`
}

// StudentCounselingSessionView is the student-facing session.
type StudentCounselingSessionView struct {
	ID                 uuid.UUID                `json:"id"`
	CounselorID        uuid.UUID                `json:"counselor_id"`
	InitiatorStudentID uuid.UUID                `json:"initiator_student_id"`
	IsInitiator        bool                     `json:"is_initiator"`
	CounselingType     string                   `json:"counseling_type"`
	Topic              string                   `json:"topic"`
	Description        string                   `json:"description"`
	RequestedDate      string                   `json:"requested_date"`
	StartTime          string                   `json:"start_time"`
	EndTime            string                   `json:"end_time"`
	Place              string                   `json:"place"`
	Status             string                   `json:"status"`
	RescheduleReason   string                   `json:"reschedule_reason,omitempty"`
	CreatedAt          time.Time                `json:"created_at"`
	UpdatedAt          time.Time                `json:"updated_at"`
	Counselor          *PersonRef               `json:"counselor,omitempty"`
	Class              *ClassRef                `json:"class,omitempty"`
	Participants       []StudentParticipantView `json:"participants"`
	Outcome            *StudentOutcomeView      `json:"outcome,omitempty"`
}

// EventAttendanceView (staff): attendee with minimal person data.
type EventAttendanceView struct {
	ID          uuid.UUID              `json:"id"`
	SchoolID    uuid.UUID              `json:"school_id"`
	EventID     uuid.UUID              `json:"event_id"`
	UserType    string                 `json:"user_type"`
	StudentID   *uuid.UUID             `json:"student_id,omitempty"`
	EmployeeID  *uuid.UUID             `json:"employee_id,omitempty"`
	UserID      *uint                  `json:"user_id,omitempty"`
	CheckInTime string                 `json:"check_in_time"`
	Notes       string                 `json:"notes,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
	Student     *CounselingStudentInfo `json:"student,omitempty"`
	Employee    *PersonRef             `json:"employee,omitempty"`
}

// CounselingEventView (staff).
type CounselingEventView struct {
	model.CounselingEvent
	Counselor   *PersonRef            `json:"counselor,omitempty"`
	Attendances []EventAttendanceView `json:"attendances"`
	// AttendanceCount is the number of check-ins.
	AttendanceCount int `json:"attendance_count"`
}

// StudentEventAttendance is the student's own check-in.
type StudentEventAttendance struct {
	ID          uuid.UUID  `json:"id"`
	EventID     uuid.UUID  `json:"event_id"`
	StudentID   *uuid.UUID `json:"student_id,omitempty"`
	CheckInTime string     `json:"check_in_time"`
	CreatedAt   time.Time  `json:"created_at"`
}

// StudentCounselingEventView: event as shown to students; attendances holds
// only the student's own check-in (never other attendees).
type StudentCounselingEventView struct {
	ID             uuid.UUID                `json:"id"`
	Title          string                   `json:"title"`
	Category       string                   `json:"category"`
	Description    string                   `json:"description"`
	EventDate      string                   `json:"event_date"`
	StartTime      string                   `json:"start_time"`
	EndTime        string                   `json:"end_time"`
	Location       string                   `json:"location"`
	TargetAudience string                   `json:"target_audience"`
	Status         string                   `json:"status"`
	Counselor      *PersonRef               `json:"counselor,omitempty"`
	Attended       bool                     `json:"attended"`
	MyAttendance   *StudentEventAttendance  `json:"my_attendance"`
	Attendances    []StudentEventAttendance `json:"attendances"`
}

// CounselingPeer is a schoolmate a student may invite (no counseling data).
type CounselingPeer struct {
	ID        uuid.UUID `json:"id"`
	FullName  string    `json:"full_name"`
	ClassName string    `json:"class_name"`
}

// CounselingStudentClass is the student's current class in the BK student picker.
type CounselingStudentClass struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"` // display name (name - alias)
}

// CounselingStudentSearchItem is one row of GET /counseling/students (counselors only).
type CounselingStudentSearchItem struct {
	ID       uuid.UUID               `json:"id"`
	FullName string                  `json:"full_name"`
	NIS      string                  `json:"nis"`
	Class    *CounselingStudentClass `json:"class"`
}
