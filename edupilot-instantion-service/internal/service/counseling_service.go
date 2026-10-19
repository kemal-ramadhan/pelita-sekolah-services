package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/edupilot-instantion-service/internal/apperr"
	"github.com/edupilot-instantion-service/internal/config"
	"github.com/edupilot-instantion-service/internal/dto"
	"github.com/edupilot-instantion-service/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ==================== Access rules (API_CONTRACT §12) ====================

// CounselingRole is the BK role of the caller inside /instantion/school/counseling/*.
type CounselingRole int

const (
	// CounselingRoleTeacher: a teacher without the counselor flag (no BK access).
	CounselingRoleTeacher CounselingRole = iota
	// CounselingRoleManager: institution / customer account (school management).
	CounselingRoleManager
	// CounselingRoleCounselor: a teacher with employees.is_counselor = true (Guru BK).
	CounselingRoleCounselor
)

// CounselingAction groups the BK endpoints by sensitivity.
type CounselingAction string

const (
	CounselingActSummary     CounselingAction = "summary"      // aggregate counts only
	CounselingActCounselors  CounselingAction = "counselors"   // list Guru BK
	CounselingActToggle      CounselingAction = "toggle"       // assign / remove Guru BK
	CounselingActSessions    CounselingAction = "sessions"     // confidential case data
	CounselingActEventsRead  CounselingAction = "events_read"  // public guidance events + attendance
	CounselingActEventsWrite CounselingAction = "events_write" // create / conclude events
	CounselingActJournals    CounselingAction = "journals"     // counselor's own daily journal
)

// CounselingAllowed is the BK access matrix:
//   - managers: summary, counselor list, toggle, read/attend events — never
//     sessions, outcomes (confidential notes) or journals;
//   - counselors: everything except toggle (sessions/journals limited to their own);
//   - other teachers: nothing.
func CounselingAllowed(role CounselingRole, act CounselingAction) bool {
	switch role {
	case CounselingRoleManager:
		switch act {
		case CounselingActSummary, CounselingActCounselors, CounselingActToggle, CounselingActEventsRead:
			return true
		}
		return false
	case CounselingRoleCounselor:
		return act != CounselingActToggle
	default:
		return false
	}
}

// CounselingForbidden returns the 403 for a denied action.
func CounselingForbidden(role CounselingRole, act CounselingAction) error {
	switch {
	case role == CounselingRoleTeacher:
		return apperr.New(http.StatusForbidden, "NOT_COUNSELOR", "Fitur Bimbingan Konseling hanya dapat diakses oleh Guru BK")
	case act == CounselingActToggle:
		return apperr.New(http.StatusForbidden, "FORBIDDEN_ROLE", "Hanya pengelola sekolah yang dapat menetapkan Guru BK")
	default:
		return apperr.New(http.StatusForbidden, "COUNSELOR_ONLY", "Data konseling bersifat rahasia dan hanya dapat diakses oleh Guru BK")
	}
}

// ==================== Status & time rules ====================

// counselingTransitions: allowed session status changes. completed/cancelled are final.
var counselingTransitions = map[string][]string{
	model.CounselingStatusPending:     {model.CounselingStatusScheduled, model.CounselingStatusRescheduled, model.CounselingStatusCancelled},
	model.CounselingStatusScheduled:   {model.CounselingStatusRescheduled, model.CounselingStatusCompleted, model.CounselingStatusCancelled},
	model.CounselingStatusRescheduled: {model.CounselingStatusScheduled, model.CounselingStatusRescheduled, model.CounselingStatusCompleted, model.CounselingStatusCancelled},
}

// CounselingTransitionAllowed reports whether a session may go from -> to.
func CounselingTransitionAllowed(from, to string) bool {
	for _, s := range counselingTransitions[from] {
		if s == to {
			return true
		}
	}
	return false
}

func invalidTransition(from, to string) error {
	return apperr.Conflict("INVALID_STATUS_TRANSITION", fmt.Sprintf("Status sesi tidak dapat diubah dari %s ke %s", from, to))
}

const maxCounselingDaysAhead = 365

// CounselingSlotError validates a session slot (date YYYY-MM-DD, HH:MM times)
// against now in the school time zone: not in the past, end after start, at
// most a year ahead. "" when valid.
func CounselingSlotError(date, start, end string, now time.Time) string {
	if !dto.ValidDate(date) || !dto.ValidHHMM(start) || !dto.ValidHHMM(end) {
		return "Tanggal/waktu sesi tidak valid"
	}
	if end <= start {
		return "Waktu selesai harus setelah waktu mulai"
	}
	today := now.Format(dateLayout)
	if date < today || (date == today && start < now.Format(shortClock)) {
		return "Jadwal sesi tidak boleh di waktu yang sudah lewat"
	}
	if date > now.AddDate(0, 0, maxCounselingDaysAhead).Format(dateLayout) {
		return "Jadwal sesi maksimal 1 tahun ke depan"
	}
	return ""
}

// eventAttendLeadMinutes: check-in opens this long before the event starts.
const eventAttendLeadMinutes = 30

// EventAttendError: check-in only to a published event, on its date (school
// tz), from 30 minutes before start until its end, and for its audience.
func EventAttendError(ev *model.CounselingEvent, now time.Time, userType string) error {
	if ev.Status != model.EventStatusPublished {
		return apperr.Conflict("EVENT_NOT_OPEN", "Acara tidak sedang dibuka untuk presensi")
	}
	switch {
	case userType == "student" && ev.TargetAudience == dto.EventAudienceTeachers:
		return apperr.New(http.StatusForbidden, "NOT_EVENT_AUDIENCE", "Acara ini bukan untuk siswa")
	case userType == "teacher" && ev.TargetAudience == dto.EventAudienceStudents:
		return apperr.New(http.StatusForbidden, "NOT_EVENT_AUDIENCE", "Acara ini khusus untuk siswa")
	}
	if ev.EventDate != now.Format(dateLayout) {
		return apperr.Conflict("EVENT_NOT_TODAY", "Presensi acara hanya dapat dilakukan pada hari pelaksanaan")
	}
	cur := minutesOfDay(now)
	if cur < parseTimeToMinutes(ev.StartTime)-eventAttendLeadMinutes || cur > parseTimeToMinutes(ev.EndTime) {
		return apperr.Conflict("EVENT_NOT_OPEN", fmt.Sprintf("Presensi acara dibuka %d menit sebelum mulai sampai acara selesai", eventAttendLeadMinutes))
	}
	return nil
}

// EventConcludeError: a published (or already completed, to edit) event can be
// concluded once it has started.
func EventConcludeError(ev *model.CounselingEvent, now time.Time) error {
	if ev.Status != model.EventStatusPublished && ev.Status != model.EventStatusCompleted {
		return apperr.Conflict("INVALID_STATUS_TRANSITION", "Acara yang dibatalkan/draf tidak dapat diberi kesimpulan")
	}
	today := now.Format(dateLayout)
	if ev.EventDate > today || (ev.EventDate == today && now.Format(shortClock) < ev.StartTime) {
		return apperr.Conflict("EVENT_NOT_STARTED", "Kesimpulan hanya dapat disimpan setelah acara dimulai")
	}
	return nil
}

// ==================== Service ====================

type CounselingService struct {
	db *gorm.DB
}

// NewCounselingService: a nil db means config.DB, resolved per call.
func NewCounselingService(db *gorm.DB) *CounselingService {
	return &CounselingService{db: db}
}

func (s *CounselingService) conn(ctx context.Context) *gorm.DB {
	if s.db != nil {
		return s.db.WithContext(ctx)
	}
	return config.DB.WithContext(ctx)
}

func (s *CounselingService) schoolNow(ctx context.Context, db *gorm.DB, schoolID uuid.UUID) time.Time {
	return time.Now().In(schoolLocation(ctx, db, schoolID))
}

var (
	errCounselingNotFound = apperr.NotFound("Sesi konseling tidak ditemukan")
	errEventNotFound      = apperr.NotFound("Acara konseling tidak ditemukan")
	errJournalNotFound    = apperr.NotFound("Jurnal konseling tidak ditemukan")
	errNotAssigned        = apperr.New(http.StatusForbidden, "NOT_ASSIGNED", "Sesi konseling ini ditangani oleh Guru BK lain")
	errNotJournalOwner    = apperr.New(http.StatusForbidden, "NOT_OWNER", "Jurnal ini milik Guru BK lain")
	errNotEventOwner      = apperr.New(http.StatusForbidden, "NOT_OWNER", "Acara ini dikelola oleh Guru BK lain")
	errCounselorNotFound  = apperr.NotFound("Guru BK tidak ditemukan di sekolah ini")
)

// ResolveCounselingRole: employeeID nil = manager; otherwise counselor when
// the (non-deleted) employee has is_counselor, else plain teacher.
func (s *CounselingService) ResolveCounselingRole(ctx context.Context, schoolID uuid.UUID, employeeID *uuid.UUID) (CounselingRole, error) {
	if employeeID == nil {
		return CounselingRoleManager, nil
	}
	var flags []bool
	if err := s.conn(ctx).Model(&model.Employees{}).
		Where("school_id = ? AND id = ?", schoolID, *employeeID).
		Limit(1).Pluck("is_counselor", &flags).Error; err != nil {
		return CounselingRoleTeacher, err
	}
	if len(flags) == 1 && flags[0] {
		return CounselingRoleCounselor, nil
	}
	return CounselingRoleTeacher, nil
}

// ---------- Counselors ----------

// ListCounselors returns the school's Guru BK (staff view).
func (s *CounselingService) ListCounselors(ctx context.Context, schoolID uuid.UUID) ([]model.Employees, error) {
	counselors := []model.Employees{}
	err := s.conn(ctx).
		Where("school_id = ? AND is_counselor = true", schoolID).
		Order("full_name ASC").
		Find(&counselors).Error
	if err != nil {
		return nil, fmt.Errorf("list counselors: %w", err)
	}
	return counselors, nil
}

// ListCounselorRefs returns {id, full_name} of the school's Guru BK (student view).
func (s *CounselingService) ListCounselorRefs(ctx context.Context, schoolID uuid.UUID) ([]dto.PersonRef, error) {
	refs := []dto.PersonRef{}
	err := s.conn(ctx).Model(&model.Employees{}).
		Select("id, full_name").
		Where("school_id = ? AND is_counselor = true", schoolID).
		Order("full_name ASC").
		Scan(&refs).Error
	return refs, err
}

func (s *CounselingService) findCounselor(db *gorm.DB, schoolID, id uuid.UUID) (*model.Employees, error) {
	var emp model.Employees
	if err := db.Where("school_id = ? AND id = ? AND is_counselor = true", schoolID, id).First(&emp).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errCounselorNotFound
		}
		return nil, err
	}
	return &emp, nil
}

// ToggleTeacherCounselor sets employees.is_counselor (managers only, checked by
// the controller). Removing the flag is refused while the counselor still has
// open (pending/scheduled/rescheduled) sessions.
func (s *CounselingService) ToggleTeacherCounselor(ctx context.Context, schoolID, employeeID uuid.UUID, isCounselor bool) (*model.Employees, error) {
	db := s.conn(ctx)
	var emp model.Employees
	if err := db.Where("school_id = ? AND id = ?", schoolID, employeeID).First(&emp).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperr.NotFound("Data guru tidak ditemukan")
		}
		return nil, err
	}
	if emp.IsCounselor == isCounselor {
		return &emp, nil
	}
	if !isCounselor {
		var open int64
		if err := db.Model(&model.CounselingSession{}).
			Where("school_id = ? AND counselor_id = ? AND status IN ?", schoolID, employeeID, openSessionStatuses).
			Count(&open).Error; err != nil {
			return nil, err
		}
		if open > 0 {
			return nil, apperr.Conflict("HAS_OPEN_SESSIONS", fmt.Sprintf("Guru BK masih memiliki %d sesi konseling yang belum selesai. Selesaikan atau batalkan terlebih dahulu.", open))
		}
	}
	if err := db.Model(&model.Employees{}).Where("school_id = ? AND id = ?", schoolID, employeeID).
		Update("is_counselor", isCounselor).Error; err != nil {
		return nil, fmt.Errorf("update is_counselor: %w", err)
	}
	emp.IsCounselor = isCounselor
	return &emp, nil
}

var openSessionStatuses = []string{model.CounselingStatusPending, model.CounselingStatusScheduled, model.CounselingStatusRescheduled}

// ---------- Sessions ----------

type SessionFilter struct {
	CounselorID    *uuid.UUID
	StudentID      *uuid.UUID
	Status         string
	CounselingType string
	DateFrom       string
	DateTo         string
	ClassID        *uuid.UUID
	Search         string
	Page           int
	Limit          int
}

// Counseling list paging: default 50, max 100 (lists used to be unbounded).
const (
	counselingDefaultLimit = 50
	counselingMaxLimit     = 100
)

// CounselingPage normalizes page/limit for BK lists.
func CounselingPage(page, limit int) (int, int) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = counselingDefaultLimit
	}
	if limit > counselingMaxLimit {
		limit = counselingMaxLimit
	}
	return page, limit
}

func validSessionStatus(st string) bool {
	switch st {
	case model.CounselingStatusPending, model.CounselingStatusScheduled, model.CounselingStatusRescheduled,
		model.CounselingStatusCompleted, model.CounselingStatusCancelled:
		return true
	}
	return false
}

const studentRefColumns = "id, school_id, full_name, nis, nisn, gender, status, user_id"

func studentRefPreload(db *gorm.DB) *gorm.DB { return db.Unscoped().Select(studentRefColumns) }

func sessionPreloads(db *gorm.DB) *gorm.DB {
	return db.
		Preload("Counselor", unscopedPreload).
		Preload("InitiatorStudent", studentRefPreload).
		Preload("Class", unscopedPreload).
		Preload("Participants", func(db *gorm.DB) *gorm.DB { return db.Order("is_initiator DESC, created_at ASC") }).
		Preload("Participants.Student", studentRefPreload).
		Preload("Outcome")
}

const maxPendingRequestsPerStudent = 3

// CreateSession: studentID set = a student request (status pending, type
// individual/group, class from the active history); counselorID set = a
// counselor-created session (assigned to that counselor, status scheduled).
func (s *CounselingService) CreateSession(ctx context.Context, schoolID uuid.UUID, req *dto.CounselingSessionRequest, studentID *uuid.UUID, counselorID *uuid.UUID) (*model.CounselingSession, error) {
	if msg := req.Normalize(); msg != "" {
		return nil, apperr.BadRequest(msg)
	}
	db := s.conn(ctx)
	now := s.schoolNow(ctx, db, schoolID)
	if msg := CounselingSlotError(req.RequestedDate, req.StartTime, req.EndTime, now); msg != "" {
		return nil, apperr.BadRequest(msg)
	}

	session := &model.CounselingSession{
		SchoolID:       schoolID,
		CounselingType: req.CounselingType,
		Topic:          req.Topic,
		Description:    req.Description,
		RequestedDate:  req.RequestedDate,
		StartTime:      req.StartTime,
		EndTime:        req.EndTime,
		Place:          req.Place,
		Status:         model.CounselingStatusPending,
	}

	if studentID != nil {
		switch req.CounselingType {
		case model.CounselingTypeIndividual, model.CounselingTypeGroup:
		default:
			return nil, apperr.BadRequest("Siswa hanya dapat mengajukan konseling individual atau kelompok")
		}
		session.InitiatorStudentID = *studentID
		if h := activeHistoryOf(db, schoolID, *studentID); h != nil {
			cid := h.ClassID
			session.ClassID = &cid
		}
		var pending int64
		if err := db.Model(&model.CounselingSession{}).
			Where("school_id = ? AND initiator_student_id = ? AND status = ?", schoolID, *studentID, model.CounselingStatusPending).
			Count(&pending).Error; err != nil {
			return nil, err
		}
		if pending >= maxPendingRequestsPerStudent {
			return nil, apperr.Conflict("TOO_MANY_PENDING", fmt.Sprintf("Anda masih memiliki %d pengajuan konseling yang menunggu konfirmasi", pending))
		}
	} else {
		if !req.InitiatorStudentID.Valid {
			return nil, apperr.BadRequest("initiator_student_id wajib diisi")
		}
		if err := s.requireActiveStudents(db, schoolID, []uuid.UUID{req.InitiatorStudentID.UUID}); err != nil {
			return nil, apperr.NotFound("Siswa tidak ditemukan atau tidak aktif di sekolah ini")
		}
		session.InitiatorStudentID = req.InitiatorStudentID.UUID
		session.Status = model.CounselingStatusScheduled
		if req.ClassID.Valid {
			var n int64
			if err := db.Model(&model.Classes{}).Where("school_id = ? AND id = ?", schoolID, req.ClassID.UUID).Count(&n).Error; err != nil {
				return nil, err
			}
			if n == 0 {
				return nil, apperr.NotFound("Kelas tidak ditemukan")
			}
			cid := req.ClassID.UUID
			session.ClassID = &cid
		} else if h := activeHistoryOf(db, schoolID, session.InitiatorStudentID); h != nil {
			cid := h.ClassID
			session.ClassID = &cid
		}
	}

	// Counselor assignment.
	switch {
	case counselorID != nil:
		if req.CounselorID.Valid && req.CounselorID.UUID != *counselorID {
			return nil, apperr.BadRequest("Guru BK hanya dapat membuat sesi untuk dirinya sendiri")
		}
		session.CounselorID = *counselorID
	case req.CounselorID.Valid:
		if _, err := s.findCounselor(db, schoolID, req.CounselorID.UUID); err != nil {
			return nil, err
		}
		session.CounselorID = req.CounselorID.UUID
	default:
		// Auto-assign: the counselor with the fewest open sessions.
		var picked []uuid.UUID
		if err := db.Raw(`SELECT e.id FROM employees e
			LEFT JOIN counseling_sessions cs ON cs.counselor_id = e.id AND cs.school_id = e.school_id AND cs.status IN ?
			WHERE e.school_id = ? AND e.is_counselor = true AND e.deleted_at IS NULL
			GROUP BY e.id, e.full_name ORDER BY COUNT(cs.id) ASC, e.full_name ASC LIMIT 1`, openSessionStatuses, schoolID).
			Scan(&picked).Error; err != nil {
			return nil, err
		}
		if len(picked) == 0 {
			return nil, apperr.Conflict("NO_COUNSELOR", "Belum ada Guru BK yang ditugaskan di sekolah ini")
		}
		session.CounselorID = picked[0]
	}

	// Participants (peers): unique, not the initiator, active students of the school.
	peers := make([]uuid.UUID, 0, len(req.ParticipantStudentIDs))
	seen := map[uuid.UUID]bool{session.InitiatorStudentID: true}
	for _, id := range req.ParticipantStudentIDs {
		if id == uuid.Nil || seen[id] {
			continue
		}
		seen[id] = true
		peers = append(peers, id)
	}
	if session.CounselingType == model.CounselingTypeIndividual && len(peers) > 0 {
		return nil, apperr.BadRequest("Konseling individual tidak dapat mengundang siswa lain; pilih konseling kelompok")
	}
	if studentID != nil && session.CounselingType == model.CounselingTypeGroup && len(peers) == 0 {
		return nil, apperr.BadRequest("Konseling kelompok membutuhkan minimal 1 teman yang diundang")
	}
	if err := s.requireActiveStudents(db, schoolID, peers); err != nil {
		return nil, err
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(session).Error; err != nil {
			return err
		}
		parts := make([]model.CounselingParticipant, 0, len(peers)+1)
		parts = append(parts, model.CounselingParticipant{
			SchoolID: schoolID, CounselingSessionID: session.ID, StudentID: session.InitiatorStudentID,
			IsInitiator: true, AttendanceStatus: "present",
		})
		for _, id := range peers {
			parts = append(parts, model.CounselingParticipant{
				SchoolID: schoolID, CounselingSessionID: session.ID, StudentID: id, AttendanceStatus: "present",
			})
		}
		return tx.Create(&parts).Error
	})
	if err != nil {
		return nil, fmt.Errorf("create counseling session: %w", err)
	}
	if session.Status == model.CounselingStatusScheduled {
		s.notifySession(ctx, db, session, "")
	}
	return session, nil
}

// requireActiveStudents: all ids are active, non-deleted students of the school.
func (s *CounselingService) requireActiveStudents(db *gorm.DB, schoolID uuid.UUID, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	var n int64
	if err := db.Model(&model.Students{}).
		Where("school_id = ? AND id IN ? AND status = 'active'", schoolID, ids).
		Count(&n).Error; err != nil {
		return err
	}
	if int(n) != len(ids) {
		return apperr.BadRequest("Sebagian siswa yang diundang tidak ditemukan atau tidak aktif di sekolah ini")
	}
	return nil
}

// ListSessions lists sessions (one page) with batched preloads.
func (s *CounselingService) ListSessions(ctx context.Context, schoolID uuid.UUID, f SessionFilter) ([]model.CounselingSession, int64, error) {
	if f.Status != "" && !validSessionStatus(f.Status) {
		return nil, 0, apperr.BadRequest("Filter status tidak valid")
	}
	if (f.DateFrom != "" && !dto.ValidDate(f.DateFrom)) || (f.DateTo != "" && !dto.ValidDate(f.DateTo)) {
		return nil, 0, apperr.BadRequest("Filter tanggal harus berformat YYYY-MM-DD")
	}
	q := s.conn(ctx).Model(&model.CounselingSession{}).
		Where("counseling_sessions.school_id = ?", schoolID)
	if f.CounselorID != nil {
		q = q.Where("counseling_sessions.counselor_id = ?", *f.CounselorID)
	}
	if f.StudentID != nil {
		q = q.Where(`(counseling_sessions.initiator_student_id = ? OR EXISTS (
			SELECT 1 FROM counseling_participants cp WHERE cp.counseling_session_id = counseling_sessions.id AND cp.student_id = ?))`,
			*f.StudentID, *f.StudentID)
	}
	if f.Status != "" {
		q = q.Where("counseling_sessions.status = ?", f.Status)
	}
	if f.CounselingType != "" {
		q = q.Where("counseling_sessions.counseling_type = ?", f.CounselingType)
	}
	if f.DateFrom != "" {
		q = q.Where("counseling_sessions.requested_date >= ?", f.DateFrom)
	}
	if f.DateTo != "" {
		q = q.Where("counseling_sessions.requested_date <= ?", f.DateTo)
	}
	if f.ClassID != nil {
		q = q.Where("counseling_sessions.class_id = ?", *f.ClassID)
	}
	if search := strings.TrimSpace(f.Search); search != "" {
		if len(search) > 100 {
			search = search[:100]
		}
		like := dto.LikePattern(search)
		q = q.Where("(counseling_sessions.topic ILIKE ? OR counseling_sessions.description ILIKE ?)", like, like)
	}

	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	page, limit := CounselingPage(f.Page, f.Limit)
	sessions := []model.CounselingSession{}
	err := sessionPreloads(q).
		Order("counseling_sessions.requested_date DESC, counseling_sessions.start_time DESC, counseling_sessions.id").
		Offset((page - 1) * limit).Limit(limit).
		Find(&sessions).Error
	if err != nil {
		return nil, 0, err
	}
	return sessions, total, nil
}

func (s *CounselingService) loadSession(db *gorm.DB, schoolID, id uuid.UUID, preload bool) (*model.CounselingSession, error) {
	q := db
	if preload {
		q = sessionPreloads(db)
	}
	var sess model.CounselingSession
	if err := q.Where("school_id = ? AND id = ?", schoolID, id).First(&sess).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errCounselingNotFound
		}
		return nil, err
	}
	return &sess, nil
}

// assignedSession loads a session that must be assigned to counselorID (403 otherwise).
func (s *CounselingService) assignedSession(db *gorm.DB, schoolID, id, counselorID uuid.UUID, preload bool) (*model.CounselingSession, error) {
	sess, err := s.loadSession(db, schoolID, id, preload)
	if err != nil {
		return nil, err
	}
	if sess.CounselorID != counselorID {
		return nil, errNotAssigned
	}
	return sess, nil
}

// GetSessionForCounselor returns a session assigned to the counselor (full view incl. confidential notes).
func (s *CounselingService) GetSessionForCounselor(ctx context.Context, schoolID, id, counselorID uuid.UUID) (*dto.CounselingSessionView, error) {
	sess, err := s.assignedSession(s.conn(ctx), schoolID, id, counselorID, true)
	if err != nil {
		return nil, err
	}
	v := CounselingSessionToView(sess)
	return &v, nil
}

// GetSessionForStudent returns a session the student takes part in (404 otherwise).
func (s *CounselingService) GetSessionForStudent(ctx context.Context, schoolID, id, studentID uuid.UUID) (*dto.StudentCounselingSessionView, error) {
	sess, err := s.loadSession(s.conn(ctx), schoolID, id, true)
	if err != nil {
		return nil, err
	}
	if !studentInSession(sess, studentID) {
		return nil, errCounselingNotFound // never reveal other students' sessions
	}
	v := CounselingSessionToStudentView(sess, studentID)
	return &v, nil
}

func studentInSession(sess *model.CounselingSession, studentID uuid.UUID) bool {
	if sess.InitiatorStudentID == studentID {
		return true
	}
	for _, p := range sess.Participants {
		if p.StudentID == studentID {
			return true
		}
	}
	return false
}

// UpdateSessionStatus applies a status change by the assigned counselor.
func (s *CounselingService) UpdateSessionStatus(ctx context.Context, schoolID, id, counselorID uuid.UUID, req *dto.CounselingSessionStatusRequest) (*dto.CounselingSessionView, error) {
	if msg := req.Normalize(); msg != "" {
		return nil, apperr.BadRequest(msg)
	}
	db := s.conn(ctx)
	var sess *model.CounselingSession
	err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		if err = lockKey(tx, "counseling_session:"+id.String()); err != nil {
			return err
		}
		if sess, err = s.assignedSession(tx, schoolID, id, counselorID, false); err != nil {
			return err
		}
		if !CounselingTransitionAllowed(sess.Status, req.Status) {
			return invalidTransition(sess.Status, req.Status)
		}
		switch req.Status {
		case model.CounselingStatusScheduled, model.CounselingStatusRescheduled:
			if req.NewDate != "" {
				sess.RequestedDate = req.NewDate
			}
			if req.NewStartTime != "" {
				sess.StartTime = req.NewStartTime
			}
			if req.NewEndTime != "" {
				sess.EndTime = req.NewEndTime
			}
			if req.NewPlace != "" {
				sess.Place = req.NewPlace
			}
			if msg := CounselingSlotError(sess.RequestedDate, sess.StartTime, sess.EndTime, s.schoolNow(ctx, tx, schoolID)); msg != "" {
				return apperr.BadRequest(msg)
			}
			if req.RescheduleReason != "" {
				sess.RescheduleReason = req.RescheduleReason
			}
		case model.CounselingStatusCancelled:
			if req.RescheduleReason != "" {
				sess.RescheduleReason = req.RescheduleReason
			}
		}
		sess.Status = req.Status
		return tx.Model(&model.CounselingSession{}).Where("school_id = ? AND id = ?", schoolID, id).
			Updates(map[string]interface{}{
				"status": sess.Status, "requested_date": sess.RequestedDate, "start_time": sess.StartTime,
				"end_time": sess.EndTime, "place": sess.Place, "reschedule_reason": sess.RescheduleReason,
				"updated_at": time.Now(),
			}).Error
	})
	if err != nil {
		return nil, err
	}
	s.notifySession(ctx, db, sess, req.RescheduleReason)
	return s.GetSessionForCounselor(ctx, schoolID, id, counselorID)
}

// SaveSessionOutcome stores the outcome (assigned counselor only) and marks the
// session completed. Allowed for scheduled/rescheduled sessions, and to edit
// the outcome of a completed one.
func (s *CounselingService) SaveSessionOutcome(ctx context.Context, schoolID, id, counselorID uuid.UUID, req *dto.CounselingOutcomeRequest) (*model.CounselingOutcome, error) {
	if msg := req.Normalize(); msg != "" {
		return nil, apperr.BadRequest(msg)
	}
	db := s.conn(ctx)
	var outcome model.CounselingOutcome
	var sess *model.CounselingSession
	wasCompleted := false
	err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		if err = lockKey(tx, "counseling_session:"+id.String()); err != nil {
			return err
		}
		if sess, err = s.assignedSession(tx, schoolID, id, counselorID, false); err != nil {
			return err
		}
		wasCompleted = sess.Status == model.CounselingStatusCompleted
		if !wasCompleted && !CounselingTransitionAllowed(sess.Status, model.CounselingStatusCompleted) {
			return apperr.Conflict("INVALID_STATUS_TRANSITION", "Hasil konseling hanya dapat dicatat untuk sesi yang sudah dijadwalkan")
		}
		if req.FollowUpDate != nil && *req.FollowUpDate < sess.RequestedDate {
			return apperr.BadRequest("follow_up_date tidak boleh sebelum tanggal sesi")
		}
		attachments := dto.StringSliceToJSON(req.AttachmentURLs)
		err = tx.Where("school_id = ? AND counseling_session_id = ?", schoolID, id).First(&outcome).Error
		isNew := errors.Is(err, gorm.ErrRecordNotFound)
		if err != nil && !isNew {
			return err
		}
		outcome.SchoolID = schoolID
		outcome.CounselingSessionID = id
		outcome.CounselorID = counselorID
		outcome.ConfidentialNotes = req.ConfidentialNotes
		outcome.StudentFeedback = req.StudentFeedback
		outcome.OutcomeStatus = req.OutcomeStatus
		outcome.FollowUpPlan = req.FollowUpPlan
		outcome.FollowUpDate = req.FollowUpDate
		outcome.AttachmentURLs = attachments
		if isNew {
			err = tx.Create(&outcome).Error
		} else {
			err = tx.Save(&outcome).Error
		}
		if err != nil {
			return err
		}
		if !wasCompleted {
			if err := tx.Model(&model.CounselingSession{}).Where("school_id = ? AND id = ?", schoolID, id).
				Updates(map[string]interface{}{"status": model.CounselingStatusCompleted, "updated_at": time.Now()}).Error; err != nil {
				return err
			}
			sess.Status = model.CounselingStatusCompleted
		}
		if req.CreateJournalEntry {
			serviceType := req.JournalServiceType
			if serviceType == "" {
				serviceType = journalServiceTypeFor(sess.CounselingType)
			}
			sid := sess.ID
			// The journal never copies the confidential notes.
			journal := model.CounselingDailyJournal{
				SchoolID:            schoolID,
				CounselorID:         counselorID,
				Date:                sess.RequestedDate,
				ServiceType:         serviceType,
				ActivityTitle:       truncateRunes("Layanan Konseling: "+sess.Topic, 255),
				TargetDescription:   truncateRunes(fmt.Sprintf("Sesi %s (%s)", sess.CounselingType, sess.Place), 255),
				ActivitySummary:     fmt.Sprintf("Sesi konseling %s telah dilaksanakan pada %s %s-%s.", sess.CounselingType, sess.RequestedDate, sess.StartTime, sess.EndTime),
				EvaluationResult:    "Status hasil: " + req.OutcomeStatus,
				FollowUpAction:      req.FollowUpPlan,
				CounselingSessionID: &sid,
				AttachmentURLs:      "[]",
			}
			if err := tx.Create(&journal).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !wasCompleted {
		s.notifySession(ctx, db, sess, "")
	}
	return &outcome, nil
}

func journalServiceTypeFor(counselingType string) string {
	switch counselingType {
	case model.CounselingTypeIndividual:
		return "konseling_individual"
	case model.CounselingTypeGroup:
		return "konseling_kelompok"
	case model.CounselingTypeClassical:
		return "bimbingan_klasikal"
	default:
		return "layanan_responsif"
	}
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// Notification types for students (API_CONTRACT §10.5 / §12).
const (
	NotifCounselingScheduled   = "counseling_scheduled"
	NotifCounselingRescheduled = "counseling_rescheduled"
	NotifCounselingCancelled   = "counseling_cancelled"
	NotifCounselingCompleted   = "counseling_completed"
)

// CounselingNotification builds the student notification for a session status
// (no confidential content). ok=false for statuses that do not notify.
func CounselingNotification(sess *model.CounselingSession, reason string) (typ, title, body string, ok bool) {
	when := fmt.Sprintf("%s pukul %s–%s di %s", sess.RequestedDate, sess.StartTime, sess.EndTime, sess.Place)
	switch sess.Status {
	case model.CounselingStatusScheduled:
		return NotifCounselingScheduled, "Sesi konseling dijadwalkan", "Sesi konseling Anda dijadwalkan pada " + when + ".", true
	case model.CounselingStatusRescheduled:
		body = "Sesi konseling Anda dijadwalkan ulang ke " + when + "."
		if reason != "" {
			body += " Alasan: " + reason
		}
		return NotifCounselingRescheduled, "Jadwal konseling diubah", body, true
	case model.CounselingStatusCancelled:
		body = "Sesi konseling Anda pada " + sess.RequestedDate + " dibatalkan."
		if reason != "" {
			body += " Alasan: " + reason
		}
		return NotifCounselingCancelled, "Sesi konseling dibatalkan", body, true
	case model.CounselingStatusCompleted:
		return NotifCounselingCompleted, "Sesi konseling selesai", "Guru BK telah mencatat hasil sesi konseling Anda. Lihat umpan balik di menu Konseling.", true
	}
	return "", "", "", false
}

// notifySession notifies every linked student account in the session (one query).
func (s *CounselingService) notifySession(ctx context.Context, db *gorm.DB, sess *model.CounselingSession, reason string) {
	typ, title, body, ok := CounselingNotification(sess, reason)
	if !ok {
		return
	}
	var userIDs []uint
	if err := db.Raw(`SELECT DISTINCT s.user_id FROM counseling_participants p
		JOIN students s ON s.id = p.student_id
		WHERE p.counseling_session_id = ? AND p.school_id = ? AND s.user_id IS NOT NULL`, sess.ID, sess.SchoolID).
		Scan(&userIDs).Error; err != nil {
		return
	}
	schoolID := sess.SchoolID
	for _, uid := range userIDs {
		Notify(ctx, NotificationInput{
			UserID: uid, SchoolID: &schoolID, Type: typ, Title: title, Body: body,
			Data: map[string]any{"session_id": sess.ID.String(), "status": sess.Status},
		})
	}
}

// ---------- Views ----------

func studentInfo(st *model.Students) *dto.CounselingStudentInfo {
	if st == nil {
		return nil
	}
	return &dto.CounselingStudentInfo{ID: st.ID, FullName: st.FullName, NIS: st.NIS, NISN: st.NISN, Gender: st.Gender, Status: st.Status}
}

func classRef(c *model.Classes) *dto.ClassRef {
	if c == nil {
		return nil
	}
	name := c.DisplayName()
	return &dto.ClassRef{ID: c.ID, Name: name, ClassName: c.Name}
}

// CounselingSessionToView: staff view (assigned counselor only).
func CounselingSessionToView(sess *model.CounselingSession) dto.CounselingSessionView {
	v := dto.CounselingSessionView{
		ID: sess.ID, SchoolID: sess.SchoolID, CounselorID: sess.CounselorID, InitiatorStudentID: sess.InitiatorStudentID,
		ClassID: sess.ClassID, CounselingType: sess.CounselingType, Topic: sess.Topic, Description: sess.Description,
		RequestedDate: sess.RequestedDate, StartTime: sess.StartTime, EndTime: sess.EndTime, Place: sess.Place,
		Status: sess.Status, RescheduleReason: sess.RescheduleReason, CreatedAt: sess.CreatedAt, UpdatedAt: sess.UpdatedAt,
		Counselor: sess.Counselor, InitiatorStudent: studentInfo(sess.InitiatorStudent), Class: classRef(sess.Class),
		Participants: make([]dto.CounselingParticipantView, 0, len(sess.Participants)), Outcome: sess.Outcome,
	}
	for _, p := range sess.Participants {
		pv := dto.CounselingParticipantView{
			ID: p.ID, SchoolID: p.SchoolID, CounselingSessionID: p.CounselingSessionID, StudentID: p.StudentID,
			IsInitiator: p.IsInitiator, AttendanceStatus: p.AttendanceStatus, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
			Student: studentInfo(p.Student),
		}
		if p.Student != nil {
			pv.FullName = p.Student.FullName
		}
		v.Participants = append(v.Participants, pv)
	}
	return v
}

// CounselingSessionToStudentView: what a participating student may see. No
// confidential notes, attachments or other students' personal data; the
// description (the initiator's own story) only for the initiator.
func CounselingSessionToStudentView(sess *model.CounselingSession, studentID uuid.UUID) dto.StudentCounselingSessionView {
	isInit := sess.InitiatorStudentID == studentID
	v := dto.StudentCounselingSessionView{
		ID: sess.ID, CounselorID: sess.CounselorID, InitiatorStudentID: sess.InitiatorStudentID, IsInitiator: isInit,
		CounselingType: sess.CounselingType, Topic: sess.Topic, RequestedDate: sess.RequestedDate,
		StartTime: sess.StartTime, EndTime: sess.EndTime, Place: sess.Place, Status: sess.Status,
		RescheduleReason: sess.RescheduleReason, CreatedAt: sess.CreatedAt, UpdatedAt: sess.UpdatedAt,
		Class:        classRef(sess.Class),
		Participants: make([]dto.StudentParticipantView, 0, len(sess.Participants)),
	}
	if v.Class != nil {
		v.Class.ClassName = v.Class.Name
	}
	if isInit {
		v.Description = sess.Description
	}
	if sess.Counselor != nil {
		v.Counselor = &dto.PersonRef{ID: sess.Counselor.ID, FullName: sess.Counselor.FullName}
	}
	for _, p := range sess.Participants {
		pv := dto.StudentParticipantView{StudentID: p.StudentID, IsInitiator: p.IsInitiator}
		if p.Student != nil {
			pv.FullName = p.Student.FullName
			pv.Student = &dto.PersonRef{ID: p.Student.ID, FullName: p.Student.FullName}
		}
		v.Participants = append(v.Participants, pv)
	}
	if sess.Outcome != nil && sess.Status == model.CounselingStatusCompleted {
		v.Outcome = &dto.StudentOutcomeView{
			StudentFeedback: sess.Outcome.StudentFeedback, OutcomeStatus: sess.Outcome.OutcomeStatus,
			FollowUpPlan: sess.Outcome.FollowUpPlan, FollowUpDate: sess.Outcome.FollowUpDate,
		}
	}
	return v
}

// ---------- Events ----------

// CreateEvent: a counselor posts a guidance event (date not in the past, school tz).
func (s *CounselingService) CreateEvent(ctx context.Context, schoolID, counselorID uuid.UUID, req *dto.CounselingEventRequest) (*dto.CounselingEventView, error) {
	if msg := req.Normalize(); msg != "" {
		return nil, apperr.BadRequest(msg)
	}
	db := s.conn(ctx)
	now := s.schoolNow(ctx, db, schoolID)
	if req.EventDate < now.Format(dateLayout) {
		return nil, apperr.BadRequest("Tanggal acara tidak boleh di masa lalu")
	}
	if req.EventDate > now.AddDate(0, 0, maxCounselingDaysAhead).Format(dateLayout) {
		return nil, apperr.BadRequest("Tanggal acara maksimal 1 tahun ke depan")
	}
	event := &model.CounselingEvent{
		SchoolID: schoolID, CounselorID: counselorID, Title: req.Title, Category: req.Category,
		Description: req.Description, EventDate: req.EventDate, StartTime: req.StartTime, EndTime: req.EndTime,
		Location: req.Location, TargetAudience: req.TargetAudience, Status: model.EventStatusPublished,
		AttachmentURLs: dto.StringSliceToJSON(req.AttachmentURLs),
	}
	if err := db.Create(event).Error; err != nil {
		return nil, fmt.Errorf("create counseling event: %w", err)
	}
	return s.GetEvent(ctx, schoolID, event.ID)
}

func validEventStatus(st string) bool {
	switch st {
	case model.EventStatusDraft, model.EventStatusPublished, model.EventStatusCompleted, model.EventStatusCancelled:
		return true
	}
	return false
}

func eventPreloads(db *gorm.DB) *gorm.DB {
	return db.
		Preload("Counselor", func(db *gorm.DB) *gorm.DB { return db.Unscoped().Select("id, full_name") }).
		Preload("Attendances", func(db *gorm.DB) *gorm.DB { return db.Order("created_at ASC") }).
		Preload("Attendances.Student", studentRefPreload).
		Preload("Attendances.Employee", func(db *gorm.DB) *gorm.DB { return db.Unscoped().Select("id, full_name") })
}

func eventToView(ev *model.CounselingEvent) dto.CounselingEventView {
	v := dto.CounselingEventView{CounselingEvent: *ev, Attendances: make([]dto.EventAttendanceView, 0, len(ev.Attendances))}
	v.CounselingEvent.Counselor = nil
	v.CounselingEvent.Attendances = nil
	if ev.Counselor != nil {
		v.Counselor = &dto.PersonRef{ID: ev.Counselor.ID, FullName: ev.Counselor.FullName}
	}
	for _, a := range ev.Attendances {
		av := dto.EventAttendanceView{
			ID: a.ID, SchoolID: a.SchoolID, EventID: a.EventID, UserType: a.UserType, StudentID: a.StudentID,
			EmployeeID: a.EmployeeID, UserID: a.UserID, CheckInTime: a.CheckInTime, Notes: a.Notes, CreatedAt: a.CreatedAt,
			Student: studentInfo(a.Student),
		}
		if a.Employee != nil {
			av.Employee = &dto.PersonRef{ID: a.Employee.ID, FullName: a.Employee.FullName}
		}
		v.Attendances = append(v.Attendances, av)
	}
	v.AttendanceCount = len(v.Attendances)
	return v
}

// ListEvents (staff: counselors & managers) — one page, attendees included.
func (s *CounselingService) ListEvents(ctx context.Context, schoolID uuid.UUID, status string, page, limit int) ([]dto.CounselingEventView, int64, error) {
	if status != "" && !validEventStatus(status) {
		return nil, 0, apperr.BadRequest("Filter status tidak valid")
	}
	q := s.conn(ctx).Model(&model.CounselingEvent{}).Where("school_id = ?", schoolID)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	page, limit = CounselingPage(page, limit)
	var events []model.CounselingEvent
	if err := eventPreloads(q).Order("event_date DESC, start_time DESC, id").
		Offset((page - 1) * limit).Limit(limit).Find(&events).Error; err != nil {
		return nil, 0, err
	}
	out := make([]dto.CounselingEventView, 0, len(events))
	for i := range events {
		out = append(out, eventToView(&events[i]))
	}
	return out, total, nil
}

// GetEvent (staff).
func (s *CounselingService) GetEvent(ctx context.Context, schoolID, eventID uuid.UUID) (*dto.CounselingEventView, error) {
	var ev model.CounselingEvent
	if err := eventPreloads(s.conn(ctx)).Where("school_id = ? AND id = ?", schoolID, eventID).First(&ev).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errEventNotFound
		}
		return nil, err
	}
	v := eventToView(&ev)
	return &v, nil
}

// ListStudentEvents: published events for students (audience all / students /
// specific_grade) with only the student's own check-in.
//
// scope: "upcoming" (event_date >= today, ASC), "past" (event_date < today,
// DESC) or "" (upcoming first ASC, then past DESC). today = school time zone.
func (s *CounselingService) ListStudentEvents(ctx context.Context, schoolID, studentID uuid.UUID, scope string, page, limit int) ([]dto.StudentCounselingEventView, int64, error) {
	db := s.conn(ctx)
	today := s.schoolNow(ctx, db, schoolID).Format(dateLayout)
	q := db.Model(&model.CounselingEvent{}).
		Where("school_id = ? AND status = ? AND target_audience <> ?", schoolID, model.EventStatusPublished, dto.EventAudienceTeachers)
	var order clause.Expr
	switch scope {
	case "upcoming":
		q = q.Where("event_date >= ?", today)
		order = clause.Expr{SQL: "event_date ASC, start_time ASC, id"}
	case "past":
		q = q.Where("event_date < ?", today)
		order = clause.Expr{SQL: "event_date DESC, start_time DESC, id"}
	case "":
		order = clause.Expr{SQL: `CASE WHEN event_date >= ? THEN 0 ELSE 1 END,
			CASE WHEN event_date >= ? THEN event_date END ASC, event_date DESC, start_time ASC, id`,
			Vars: []interface{}{today, today}, WithoutParentheses: true}
	default:
		return nil, 0, apperr.BadRequest("scope tidak valid (pilihan: upcoming, past)")
	}
	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	page, limit = CounselingPage(page, limit)
	var events []model.CounselingEvent
	if err := q.Preload("Counselor", func(db *gorm.DB) *gorm.DB { return db.Unscoped().Select("id, full_name") }).
		Order(clause.OrderBy{Expression: order}).Offset((page - 1) * limit).Limit(limit).
		Find(&events).Error; err != nil {
		return nil, 0, err
	}
	out := make([]dto.StudentCounselingEventView, 0, len(events))
	if len(events) == 0 {
		return out, total, nil
	}
	ids := make([]uuid.UUID, 0, len(events))
	for _, e := range events {
		ids = append(ids, e.ID)
	}
	var mine []model.CounselingEventAttendance
	if err := db.Where("school_id = ? AND student_id = ? AND event_id IN ?", schoolID, studentID, ids).Find(&mine).Error; err != nil {
		return nil, 0, err
	}
	byEvent := make(map[uuid.UUID]model.CounselingEventAttendance, len(mine))
	for _, a := range mine {
		byEvent[a.EventID] = a
	}
	for _, e := range events {
		v := dto.StudentCounselingEventView{
			ID: e.ID, Title: e.Title, Category: e.Category, Description: e.Description, EventDate: e.EventDate,
			StartTime: e.StartTime, EndTime: e.EndTime, Location: e.Location, TargetAudience: e.TargetAudience,
			Status: e.Status, Attendances: []dto.StudentEventAttendance{},
		}
		if e.Counselor != nil {
			v.Counselor = &dto.PersonRef{ID: e.Counselor.ID, FullName: e.Counselor.FullName}
		}
		if a, ok := byEvent[e.ID]; ok {
			sa := studentEventAttendance(&a)
			v.Attended, v.MyAttendance, v.Attendances = true, &sa, []dto.StudentEventAttendance{sa}
		}
		out = append(out, v)
	}
	return out, total, nil
}

func studentEventAttendance(a *model.CounselingEventAttendance) dto.StudentEventAttendance {
	return dto.StudentEventAttendance{ID: a.ID, EventID: a.EventID, StudentID: a.StudentID, CheckInTime: a.CheckInTime, CreatedAt: a.CreatedAt}
}

// EventAttendee identifies who checks in to an event.
type EventAttendee struct {
	UserType   string // student | teacher | institution
	StudentID  *uuid.UUID
	EmployeeID *uuid.UUID
	UserID     *uint
}

// AttendEvent checks in (idempotent: a second check-in returns the first one).
func (s *CounselingService) AttendEvent(ctx context.Context, schoolID, eventID uuid.UUID, who EventAttendee, notes string) (*model.CounselingEventAttendance, error) {
	notes = strings.TrimSpace(notes)
	if len(notes) > 1000 {
		return nil, apperr.BadRequest("notes maksimal 1000 karakter")
	}
	db := s.conn(ctx)
	var ev model.CounselingEvent
	if err := db.Where("school_id = ? AND id = ?", schoolID, eventID).First(&ev).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errEventNotFound
		}
		return nil, err
	}
	q := db.Where("school_id = ? AND event_id = ?", schoolID, eventID)
	switch {
	case who.StudentID != nil:
		q = q.Where("student_id = ?", *who.StudentID)
	case who.EmployeeID != nil:
		q = q.Where("employee_id = ?", *who.EmployeeID)
	case who.UserID != nil:
		q = q.Where("user_type = ? AND user_id = ?", who.UserType, *who.UserID)
	default:
		return nil, apperr.BadRequest("Peserta acara tidak dikenali")
	}
	var existing model.CounselingEventAttendance
	if err := q.First(&existing).Error; err == nil {
		return &existing, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	now := s.schoolNow(ctx, db, schoolID)
	if err := EventAttendError(&ev, now, who.UserType); err != nil {
		return nil, err
	}
	att := model.CounselingEventAttendance{
		SchoolID: schoolID, EventID: eventID, UserType: who.UserType, StudentID: who.StudentID,
		EmployeeID: who.EmployeeID, UserID: who.UserID, CheckInTime: now.Format(clockLayout), Notes: notes,
	}
	if err := db.Create(&att).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) { // concurrent double check-in
			if e2 := q.First(&existing).Error; e2 == nil {
				return &existing, nil
			}
		}
		return nil, fmt.Errorf("attend event: %w", err)
	}
	return &att, nil
}

// StudentAttendEvent returns the student-facing check-in.
func (s *CounselingService) StudentAttendEvent(ctx context.Context, schoolID, eventID, studentID uuid.UUID, notes string) (*dto.StudentEventAttendance, error) {
	att, err := s.AttendEvent(ctx, schoolID, eventID, EventAttendee{UserType: "student", StudentID: &studentID}, notes)
	if err != nil {
		return nil, err
	}
	v := studentEventAttendance(att)
	return &v, nil
}

// SaveEventConclusion: only the counselor who owns the event, after it started.
func (s *CounselingService) SaveEventConclusion(ctx context.Context, schoolID, eventID, counselorID uuid.UUID, req *dto.CounselingEventConclusionRequest) (*dto.CounselingEventView, error) {
	if msg := req.Normalize(); msg != "" {
		return nil, apperr.BadRequest(msg)
	}
	db := s.conn(ctx)
	var ev model.CounselingEvent
	if err := db.Where("school_id = ? AND id = ?", schoolID, eventID).First(&ev).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errEventNotFound
		}
		return nil, err
	}
	if ev.CounselorID != counselorID {
		return nil, errNotEventOwner
	}
	if err := EventConcludeError(&ev, s.schoolNow(ctx, db, schoolID)); err != nil {
		return nil, err
	}
	updates := map[string]interface{}{
		"conclusion_notes": req.ConclusionNotes, "status": model.EventStatusCompleted, "updated_at": time.Now(),
	}
	if req.AttachmentURLs != nil {
		updates["attachment_urls"] = dto.StringSliceToJSON(req.AttachmentURLs)
	}
	if err := db.Model(&model.CounselingEvent{}).Where("school_id = ? AND id = ?", schoolID, eventID).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("conclude event: %w", err)
	}
	return s.GetEvent(ctx, schoolID, eventID)
}

// ---------- Journals (owner counselor only) ----------

type JournalFilter struct {
	StartDate, EndDate, ServiceType string
	Page, Limit                     int
}

func (s *CounselingService) ListJournals(ctx context.Context, schoolID, counselorID uuid.UUID, f JournalFilter) ([]model.CounselingDailyJournal, int64, error) {
	if (f.StartDate != "" && !dto.ValidDate(f.StartDate)) || (f.EndDate != "" && !dto.ValidDate(f.EndDate)) {
		return nil, 0, apperr.BadRequest("Filter tanggal harus berformat YYYY-MM-DD")
	}
	q := s.conn(ctx).Model(&model.CounselingDailyJournal{}).Where("school_id = ? AND counselor_id = ?", schoolID, counselorID)
	if f.StartDate != "" {
		q = q.Where("date >= ?", f.StartDate)
	}
	if f.EndDate != "" {
		q = q.Where("date <= ?", f.EndDate)
	}
	if f.ServiceType != "" {
		q = q.Where("service_type = ?", f.ServiceType)
	}
	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	page, limit := CounselingPage(f.Page, f.Limit)
	journals := []model.CounselingDailyJournal{}
	err := q.Preload("Counselor").Preload("Session").Preload("Event").
		Order("date DESC, created_at DESC").Offset((page - 1) * limit).Limit(limit).
		Find(&journals).Error
	return journals, total, err
}

// journalLinks validates the optional session (own) / event (school) links.
func (s *CounselingService) journalLinks(db *gorm.DB, schoolID, counselorID uuid.UUID, req *dto.CounselingDailyJournalRequest) (*uuid.UUID, *uuid.UUID, error) {
	var sid, eid *uuid.UUID
	if req.CounselingSessionID.Valid {
		if _, err := s.assignedSession(db, schoolID, req.CounselingSessionID.UUID, counselorID, false); err != nil {
			return nil, nil, err
		}
		id := req.CounselingSessionID.UUID
		sid = &id
	}
	if req.EventID.Valid {
		var n int64
		if err := db.Model(&model.CounselingEvent{}).Where("school_id = ? AND id = ?", schoolID, req.EventID.UUID).Count(&n).Error; err != nil {
			return nil, nil, err
		}
		if n == 0 {
			return nil, nil, errEventNotFound
		}
		id := req.EventID.UUID
		eid = &id
	}
	return sid, eid, nil
}

func (s *CounselingService) CreateJournal(ctx context.Context, schoolID, counselorID uuid.UUID, req *dto.CounselingDailyJournalRequest) (*model.CounselingDailyJournal, error) {
	if msg := req.Normalize(); msg != "" {
		return nil, apperr.BadRequest(msg)
	}
	db := s.conn(ctx)
	sid, eid, err := s.journalLinks(db, schoolID, counselorID, req)
	if err != nil {
		return nil, err
	}
	journal := &model.CounselingDailyJournal{
		SchoolID: schoolID, CounselorID: counselorID, Date: req.Date, ServiceType: req.ServiceType,
		ActivityTitle: req.ActivityTitle, TargetDescription: req.TargetDescription, ActivitySummary: req.ActivitySummary,
		EvaluationResult: req.EvaluationResult, FollowUpAction: req.FollowUpAction,
		CounselingSessionID: sid, EventID: eid, AttachmentURLs: dto.StringSliceToJSON(req.AttachmentURLs),
	}
	if err := db.Create(journal).Error; err != nil {
		return nil, fmt.Errorf("create counseling journal: %w", err)
	}
	return journal, nil
}

func (s *CounselingService) ownJournal(db *gorm.DB, schoolID, journalID, counselorID uuid.UUID) (*model.CounselingDailyJournal, error) {
	var j model.CounselingDailyJournal
	if err := db.Where("school_id = ? AND id = ?", schoolID, journalID).First(&j).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errJournalNotFound
		}
		return nil, err
	}
	if j.CounselorID != counselorID {
		return nil, errNotJournalOwner
	}
	return &j, nil
}

func (s *CounselingService) UpdateJournal(ctx context.Context, schoolID, journalID, counselorID uuid.UUID, req *dto.CounselingDailyJournalRequest) (*model.CounselingDailyJournal, error) {
	if msg := req.Normalize(); msg != "" {
		return nil, apperr.BadRequest(msg)
	}
	db := s.conn(ctx)
	j, err := s.ownJournal(db, schoolID, journalID, counselorID)
	if err != nil {
		return nil, err
	}
	sid, eid, err := s.journalLinks(db, schoolID, counselorID, req)
	if err != nil {
		return nil, err
	}
	j.Date, j.ServiceType, j.ActivityTitle = req.Date, req.ServiceType, req.ActivityTitle
	j.TargetDescription, j.ActivitySummary = req.TargetDescription, req.ActivitySummary
	j.EvaluationResult, j.FollowUpAction = req.EvaluationResult, req.FollowUpAction
	j.CounselingSessionID, j.EventID = sid, eid
	if req.AttachmentURLs != nil {
		j.AttachmentURLs = dto.StringSliceToJSON(req.AttachmentURLs)
	}
	if err := db.Save(j).Error; err != nil {
		return nil, fmt.Errorf("update counseling journal: %w", err)
	}
	return j, nil
}

func (s *CounselingService) DeleteJournal(ctx context.Context, schoolID, journalID, counselorID uuid.UUID) error {
	db := s.conn(ctx)
	if _, err := s.ownJournal(db, schoolID, journalID, counselorID); err != nil {
		return err
	}
	return db.Where("school_id = ? AND id = ? AND counselor_id = ?", schoolID, journalID, counselorID).
		Delete(&model.CounselingDailyJournal{}).Error
}

// ---------- Summary ----------

// GetDashboardSummary: counts only. counselorID nil = whole school (managers).
func (s *CounselingService) GetDashboardSummary(ctx context.Context, schoolID uuid.UUID, counselorID *uuid.UUID) (*dto.CounselingDashboardSummary, error) {
	db := s.conn(ctx)
	summary := &dto.CounselingDashboardSummary{}
	if err := db.Model(&model.Employees{}).Where("school_id = ? AND is_counselor = true", schoolID).
		Count(&summary.TotalCounselors).Error; err != nil {
		return nil, err
	}
	var rows []struct {
		Status string
		N      int64
	}
	sq := db.Model(&model.CounselingSession{}).Select("status, COUNT(*) AS n").Where("school_id = ?", schoolID)
	if counselorID != nil {
		sq = sq.Where("counselor_id = ?", *counselorID)
	}
	if err := sq.Group("status").Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		switch r.Status {
		case model.CounselingStatusPending:
			summary.PendingRequestsCount += r.N
		case model.CounselingStatusScheduled, model.CounselingStatusRescheduled:
			summary.ScheduledSessionsCount += r.N
		case model.CounselingStatusCompleted:
			summary.CompletedSessionsCount += r.N
		}
	}
	today := s.schoolNow(ctx, db, schoolID).Format(dateLayout)
	if err := db.Model(&model.CounselingEvent{}).
		Where("school_id = ? AND status = ? AND event_date >= ?", schoolID, model.EventStatusPublished, today).
		Count(&summary.UpcomingEventsCount).Error; err != nil {
		return nil, err
	}
	jq := db.Model(&model.CounselingDailyJournal{}).Where("school_id = ?", schoolID)
	if counselorID != nil {
		jq = jq.Where("counselor_id = ?", *counselorID)
	}
	if err := jq.Count(&summary.TotalJournalEntries).Error; err != nil {
		return nil, err
	}
	return summary, nil
}

// ---------- Peers (student invite picker) ----------

const maxPeerResults = 20

// ListPeers returns minimal data (id, name, class) of active, enrolled
// schoolmates for inviting to a group session. Without a search term only
// classmates are listed; a search (≥ 2 chars) looks across the school. Never
// includes counseling data.
func (s *CounselingService) ListPeers(ctx context.Context, schoolID, studentID uuid.UUID, search string) ([]dto.CounselingPeer, error) {
	search = strings.TrimSpace(search)
	if len(search) > 100 {
		search = search[:100]
	}
	db := s.conn(ctx)
	q := db.Table("students s").
		Select("s.id, s.full_name, CASE WHEN COALESCE(c.alias, '') = '' THEN c.class_name ELSE c.class_name || ' - ' || c.alias END AS class_name").
		Joins("JOIN student_class_histories h ON h.student_id = s.id AND h.school_id = s.school_id AND h.status = 'active'").
		Joins("JOIN classes c ON c.id = h.class_id AND c.deleted_at IS NULL").
		Where("s.school_id = ? AND s.id <> ? AND s.status = 'active' AND s.deleted_at IS NULL", schoolID, studentID)
	if len([]rune(search)) >= 2 {
		like := dto.LikePattern(search)
		q = q.Where("(s.full_name ILIKE ? OR s.nis ILIKE ?)", like, like)
	} else {
		h := activeHistoryOf(db, schoolID, studentID)
		if h == nil {
			return []dto.CounselingPeer{}, nil
		}
		q = q.Where("h.class_id = ?", h.ClassID)
	}
	peers := []dto.CounselingPeer{}
	err := q.Order("s.full_name ASC, s.id ASC").Limit(maxPeerResults).Scan(&peers).Error
	return peers, err
}

// Student picker for counselors: default 20, max 50 per page.
const (
	counselingStudentDefaultLimit = 20
	counselingStudentMaxLimit     = 50
)

// CounselingStudentPage normalizes page/limit for the student picker.
func CounselingStudentPage(page, limit int) (int, int) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = counselingStudentDefaultLimit
	}
	if limit > counselingStudentMaxLimit {
		limit = counselingStudentMaxLimit
	}
	return page, limit
}

// SearchStudents: minimal data of the school's active students (name/NIS
// search, optional class filter) so a counselor can pick a session's student.
func (s *CounselingService) SearchStudents(ctx context.Context, schoolID uuid.UUID, search string, classID *uuid.UUID, page, limit int) ([]dto.CounselingStudentSearchItem, int64, error) {
	search = strings.TrimSpace(search)
	if len(search) > 100 {
		search = search[:100]
	}
	q := s.conn(ctx).Table("students s").
		Joins("LEFT JOIN student_class_histories h ON h.student_id = s.id AND h.school_id = s.school_id AND h.status = 'active'").
		Joins("LEFT JOIN classes c ON c.id = h.class_id AND c.deleted_at IS NULL").
		Where("s.school_id = ? AND s.status = 'active' AND s.deleted_at IS NULL", schoolID)
	if search != "" {
		like := dto.LikePattern(search)
		q = q.Where("(s.full_name ILIKE ? OR s.nis ILIKE ? OR s.nisn ILIKE ?)", like, like, like)
	}
	if classID != nil {
		q = q.Where("h.class_id = ?", *classID)
	}
	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	page, limit = CounselingStudentPage(page, limit)
	var rows []struct {
		ID        uuid.UUID
		FullName  string
		NIS       string
		ClassID   *uuid.UUID
		ClassName string
		Alias     string
	}
	if err := q.Select("s.id, s.full_name, COALESCE(s.nis, '') AS nis, c.id AS class_id, COALESCE(c.class_name, '') AS class_name, COALESCE(c.alias, '') AS alias").
		Order("s.full_name ASC, s.id ASC").Offset((page - 1) * limit).Limit(limit).Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	out := make([]dto.CounselingStudentSearchItem, 0, len(rows))
	for _, r := range rows {
		it := dto.CounselingStudentSearchItem{ID: r.ID, FullName: r.FullName, NIS: r.NIS}
		if r.ClassID != nil {
			it.Class = &dto.CounselingStudentClass{ID: *r.ClassID, Name: model.ClassDisplayName(r.ClassName, r.Alias)}
		}
		out = append(out, it)
	}
	return out, total, nil
}
