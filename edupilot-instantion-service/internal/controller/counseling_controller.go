package controller

import (
	"net/http"
	"strconv"

	"github.com/edupilot-instantion-service/internal/apperr"
	"github.com/edupilot-instantion-service/internal/dto"
	"github.com/edupilot-instantion-service/internal/middleware"
	"github.com/edupilot-instantion-service/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// counselingSvc resolves config.DB per call (nil db), never at package init.
var counselingSvc = service.NewCounselingService(nil)

// bkActor is the caller of a /instantion/school/counseling/* route.
type bkActor struct {
	SchoolID   uuid.UUID
	Role       service.CounselingRole
	EmployeeID uuid.UUID // set for counselors
}

// counselingActor resolves the caller's BK role and enforces the access matrix
// (API_CONTRACT §12). The teacher allowlist only lets role teacher reach these
// routes; the counselor flag is enforced here, so a regular teacher gets 403.
func counselingActor(c *gin.Context, act service.CounselingAction) (*bkActor, bool) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return nil, false
	}
	var empPtr *uuid.UUID
	if c.GetString(middleware.CtxRole) == middleware.RoleTeacher {
		empID, hasEmp := middleware.EmployeeID(c)
		if !hasEmp {
			respondError(c, service.CounselingForbidden(service.CounselingRoleTeacher, act))
			return nil, false
		}
		empPtr = &empID
	}
	role, err := counselingSvc.ResolveCounselingRole(c.Request.Context(), schoolID, empPtr)
	if err != nil {
		respondError(c, err)
		return nil, false
	}
	if !service.CounselingAllowed(role, act) {
		respondError(c, service.CounselingForbidden(role, act))
		return nil, false
	}
	a := &bkActor{SchoolID: schoolID, Role: role}
	if empPtr != nil {
		a.EmployeeID = *empPtr
	}
	return a, true
}

func queryInt(c *gin.Context, key string) int {
	n, _ := strconv.Atoi(c.Query(key))
	return n
}

// optionalQueryUUID parses an optional uuid query param (400 when malformed).
func optionalQueryUUID(c *gin.Context, key string) (*uuid.UUID, bool) {
	raw := c.Query(key)
	if raw == "" {
		return nil, true
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		respondError(c, apperr.BadRequest(key+" harus berupa UUID yang valid"))
		return nil, false
	}
	return &id, true
}

func counselingListBody(data any, total int64, page, limit int) gin.H {
	page, limit = service.CounselingPage(page, limit)
	return gin.H{"data": data, "total": total, "meta": dto.NewPageMeta(dto.PageQuery{Page: page, Limit: limit}, total)}
}

// GetCounselingSummary: counts only. Counselors get their own numbers,
// managers the whole school.
func GetCounselingSummary(c *gin.Context) {
	a, ok := counselingActor(c, service.CounselingActSummary)
	if !ok {
		return
	}
	var counselorID *uuid.UUID
	if a.Role == service.CounselingRoleCounselor {
		counselorID = &a.EmployeeID
	}
	summary, err := counselingSvc.GetDashboardSummary(c.Request.Context(), a.SchoolID, counselorID)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, summary)
}

// ListCounselors returns the school's Guru BK.
func ListCounselors(c *gin.Context) {
	a, ok := counselingActor(c, service.CounselingActCounselors)
	if !ok {
		return
	}
	counselors, err := counselingSvc.ListCounselors(c.Request.Context(), a.SchoolID)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": counselors})
}

// ToggleTeacherCounselor sets is_counselor (managers only).
func ToggleTeacherCounselor(c *gin.Context) {
	a, ok := counselingActor(c, service.CounselingActToggle)
	if !ok {
		return
	}
	empID, ok := parseUUIDParam(c, "id", "ID Guru")
	if !ok {
		return
	}
	var req dto.CounselorToggleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}
	emp, err := counselingSvc.ToggleTeacherCounselor(c.Request.Context(), a.SchoolID, empID, req.IsCounselor)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": emp})
}

// ListCounselingSessions: the counselor's own sessions (always filtered to the
// caller; counselor_id / all are ignored).
func ListCounselingSessions(c *gin.Context) {
	a, ok := counselingActor(c, service.CounselingActSessions)
	if !ok {
		return
	}
	studentID, ok := optionalQueryUUID(c, "student_id")
	if !ok {
		return
	}
	classID, ok := optionalQueryUUID(c, "class_id")
	if !ok {
		return
	}
	filter := service.SessionFilter{
		CounselorID:    &a.EmployeeID,
		StudentID:      studentID,
		ClassID:        classID,
		Status:         c.Query("status"),
		CounselingType: c.Query("counseling_type"),
		DateFrom:       c.Query("date_from"),
		DateTo:         c.Query("date_to"),
		Search:         c.Query("search"),
		Page:           queryInt(c, "page"),
		Limit:          queryInt(c, "limit"),
	}
	sessions, total, err := counselingSvc.ListSessions(c.Request.Context(), a.SchoolID, filter)
	if err != nil {
		respondError(c, err)
		return
	}
	views := make([]dto.CounselingSessionView, 0, len(sessions))
	for i := range sessions {
		views = append(views, service.CounselingSessionToView(&sessions[i]))
	}
	c.JSON(http.StatusOK, counselingListBody(views, total, filter.Page, filter.Limit))
}

// CreateCounselingSession: a counselor creates a (scheduled) session assigned to themself.
func CreateCounselingSession(c *gin.Context) {
	a, ok := counselingActor(c, service.CounselingActSessions)
	if !ok {
		return
	}
	var req dto.CounselingSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}
	sess, err := counselingSvc.CreateSession(c.Request.Context(), a.SchoolID, &req, nil, &a.EmployeeID)
	if err != nil {
		respondError(c, err)
		return
	}
	view, err := counselingSvc.GetSessionForCounselor(c.Request.Context(), a.SchoolID, sess.ID, a.EmployeeID)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": view})
}

// GetCounselingSession: only the assigned counselor.
func GetCounselingSession(c *gin.Context) {
	a, ok := counselingActor(c, service.CounselingActSessions)
	if !ok {
		return
	}
	sessID, ok := parseUUIDParam(c, "id", "ID Sesi Konseling")
	if !ok {
		return
	}
	view, err := counselingSvc.GetSessionForCounselor(c.Request.Context(), a.SchoolID, sessID, a.EmployeeID)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": view})
}

// UpdateCounselingSessionStatus: schedule / reschedule / complete / cancel (assigned counselor).
func UpdateCounselingSessionStatus(c *gin.Context) {
	a, ok := counselingActor(c, service.CounselingActSessions)
	if !ok {
		return
	}
	sessID, ok := parseUUIDParam(c, "id", "ID Sesi Konseling")
	if !ok {
		return
	}
	var req dto.CounselingSessionStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}
	view, err := counselingSvc.UpdateSessionStatus(c.Request.Context(), a.SchoolID, sessID, a.EmployeeID, &req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": view})
}

// SaveCounselingOutcome: outcome + completion (assigned counselor).
func SaveCounselingOutcome(c *gin.Context) {
	a, ok := counselingActor(c, service.CounselingActSessions)
	if !ok {
		return
	}
	sessID, ok := parseUUIDParam(c, "id", "ID Sesi Konseling")
	if !ok {
		return
	}
	var req dto.CounselingOutcomeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}
	outcome, err := counselingSvc.SaveSessionOutcome(c.Request.Context(), a.SchoolID, sessID, a.EmployeeID, &req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": outcome})
}

// ListCounselingEvents: counselors & managers.
func ListCounselingEvents(c *gin.Context) {
	a, ok := counselingActor(c, service.CounselingActEventsRead)
	if !ok {
		return
	}
	page, limit := queryInt(c, "page"), queryInt(c, "limit")
	events, total, err := counselingSvc.ListEvents(c.Request.Context(), a.SchoolID, c.Query("status"), page, limit)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, counselingListBody(events, total, page, limit))
}

// CreateCounselingEvent: counselors only (the event is owned by the caller).
func CreateCounselingEvent(c *gin.Context) {
	a, ok := counselingActor(c, service.CounselingActEventsWrite)
	if !ok {
		return
	}
	var req dto.CounselingEventRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}
	event, err := counselingSvc.CreateEvent(c.Request.Context(), a.SchoolID, a.EmployeeID, &req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": event})
}

// GetCounselingEvent: counselors & managers.
func GetCounselingEvent(c *gin.Context) {
	a, ok := counselingActor(c, service.CounselingActEventsRead)
	if !ok {
		return
	}
	eventID, ok := parseUUIDParam(c, "id", "ID Acara")
	if !ok {
		return
	}
	event, err := counselingSvc.GetEvent(c.Request.Context(), a.SchoolID, eventID)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": event})
}

// AttendCounselingEvent: counselor (as teacher) or manager (as institution) check-in.
func AttendCounselingEvent(c *gin.Context) {
	a, ok := counselingActor(c, service.CounselingActEventsRead)
	if !ok {
		return
	}
	eventID, ok := parseUUIDParam(c, "id", "ID Acara")
	if !ok {
		return
	}
	var req dto.CounselingEventAttendRequest
	_ = c.ShouldBindJSON(&req) // body optional

	uid := middleware.UserID(c)
	who := service.EventAttendee{UserType: "institution", UserID: &uid}
	if a.Role == service.CounselingRoleCounselor {
		emp := a.EmployeeID
		who = service.EventAttendee{UserType: "teacher", EmployeeID: &emp, UserID: &uid}
	}
	att, err := counselingSvc.AttendEvent(c.Request.Context(), a.SchoolID, eventID, who, req.Notes)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": att})
}

// ConcludeCounselingEvent: the owning counselor only.
func ConcludeCounselingEvent(c *gin.Context) {
	a, ok := counselingActor(c, service.CounselingActEventsWrite)
	if !ok {
		return
	}
	eventID, ok := parseUUIDParam(c, "id", "ID Acara")
	if !ok {
		return
	}
	var req dto.CounselingEventConclusionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}
	event, err := counselingSvc.SaveEventConclusion(c.Request.Context(), a.SchoolID, eventID, a.EmployeeID, &req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": event})
}

// ListCounselingJournals: the counselor's own journal entries.
func ListCounselingJournals(c *gin.Context) {
	a, ok := counselingActor(c, service.CounselingActJournals)
	if !ok {
		return
	}
	f := service.JournalFilter{
		StartDate: c.Query("start_date"), EndDate: c.Query("end_date"), ServiceType: c.Query("service_type"),
		Page: queryInt(c, "page"), Limit: queryInt(c, "limit"),
	}
	journals, total, err := counselingSvc.ListJournals(c.Request.Context(), a.SchoolID, a.EmployeeID, f)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, counselingListBody(journals, total, f.Page, f.Limit))
}

// CreateCounselingJournal: owned by the calling counselor.
func CreateCounselingJournal(c *gin.Context) {
	a, ok := counselingActor(c, service.CounselingActJournals)
	if !ok {
		return
	}
	var req dto.CounselingDailyJournalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}
	journal, err := counselingSvc.CreateJournal(c.Request.Context(), a.SchoolID, a.EmployeeID, &req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": journal})
}

// UpdateCounselingJournal: owner only.
func UpdateCounselingJournal(c *gin.Context) {
	a, ok := counselingActor(c, service.CounselingActJournals)
	if !ok {
		return
	}
	journalID, ok := parseUUIDParam(c, "id", "ID Jurnal")
	if !ok {
		return
	}
	var req dto.CounselingDailyJournalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}
	journal, err := counselingSvc.UpdateJournal(c.Request.Context(), a.SchoolID, journalID, a.EmployeeID, &req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": journal})
}

// DeleteCounselingJournal: owner only.
func DeleteCounselingJournal(c *gin.Context) {
	a, ok := counselingActor(c, service.CounselingActJournals)
	if !ok {
		return
	}
	journalID, ok := parseUUIDParam(c, "id", "ID Jurnal")
	if !ok {
		return
	}
	if err := counselingSvc.DeleteJournal(c.Request.Context(), a.SchoolID, journalID, a.EmployeeID); err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Jurnal berhasil dihapus"})
}

// --- STUDENT PORTAL HANDLERS (RequireEnrolledStudent guarantees student + school) ---

func linkedStudentOrAbort(c *gin.Context) (uuid.UUID, uuid.UUID, bool) {
	st := middleware.LinkedStudent(c)
	if st == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Data siswa belum terhubung", "code": "STUDENT_NOT_LINKED"})
		return uuid.Nil, uuid.Nil, false
	}
	return st.ID, st.SchoolID, true
}

// StudentListCounselors: {id, full_name} of the school's Guru BK.
func StudentListCounselors(c *gin.Context) {
	_, schoolID, ok := linkedStudentOrAbort(c)
	if !ok {
		return
	}
	refs, err := counselingSvc.ListCounselorRefs(c.Request.Context(), schoolID)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": refs})
}

// StudentListPeers: minimal data of active schoolmates to invite to a group session.
func StudentListPeers(c *gin.Context) {
	studentID, schoolID, ok := linkedStudentOrAbort(c)
	if !ok {
		return
	}
	peers, err := counselingSvc.ListPeers(c.Request.Context(), schoolID, studentID, c.Query("search"))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": peers})
}

// StudentListSessions: sessions where the student is initiator or participant.
func StudentListSessions(c *gin.Context) {
	studentID, schoolID, ok := linkedStudentOrAbort(c)
	if !ok {
		return
	}
	filter := service.SessionFilter{
		StudentID: &studentID, Status: c.Query("status"),
		Page: queryInt(c, "page"), Limit: queryInt(c, "limit"),
	}
	sessions, total, err := counselingSvc.ListSessions(c.Request.Context(), schoolID, filter)
	if err != nil {
		respondError(c, err)
		return
	}
	views := make([]dto.StudentCounselingSessionView, 0, len(sessions))
	for i := range sessions {
		views = append(views, service.CounselingSessionToStudentView(&sessions[i], studentID))
	}
	c.JSON(http.StatusOK, counselingListBody(views, total, filter.Page, filter.Limit))
}

// StudentCreateSession submits a counseling request (status pending).
func StudentCreateSession(c *gin.Context) {
	studentID, schoolID, ok := linkedStudentOrAbort(c)
	if !ok {
		return
	}
	var req dto.CounselingSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}
	sess, err := counselingSvc.CreateSession(c.Request.Context(), schoolID, &req, &studentID, nil)
	if err != nil {
		respondError(c, err)
		return
	}
	view, err := counselingSvc.GetSessionForStudent(c.Request.Context(), schoolID, sess.ID, studentID)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": view})
}

// StudentGetSession: 404 unless the student takes part in the session.
func StudentGetSession(c *gin.Context) {
	studentID, schoolID, ok := linkedStudentOrAbort(c)
	if !ok {
		return
	}
	sessID, ok := parseUUIDParam(c, "id", "ID Sesi")
	if !ok {
		return
	}
	view, err := counselingSvc.GetSessionForStudent(c.Request.Context(), schoolID, sessID, studentID)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": view})
}

// StudentListEvents: published events for students, own check-in only.
// ?scope=upcoming|past (default: upcoming ASC first, then past DESC).
func StudentListEvents(c *gin.Context) {
	studentID, schoolID, ok := linkedStudentOrAbort(c)
	if !ok {
		return
	}
	page, limit := queryInt(c, "page"), queryInt(c, "limit")
	events, total, err := counselingSvc.ListStudentEvents(c.Request.Context(), schoolID, studentID, c.Query("scope"), page, limit)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, counselingListBody(events, total, page, limit))
}

// StudentAttendEvent checks the student in to an event.
func StudentAttendEvent(c *gin.Context) {
	studentID, schoolID, ok := linkedStudentOrAbort(c)
	if !ok {
		return
	}
	eventID, ok := parseUUIDParam(c, "id", "ID Acara")
	if !ok {
		return
	}
	var req dto.CounselingEventAttendRequest
	_ = c.ShouldBindJSON(&req)
	att, err := counselingSvc.StudentAttendEvent(c.Request.Context(), schoolID, eventID, studentID, req.Notes)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": att})
}

// SearchCounselingStudents: GET /counseling/students (counselors only) —
// minimal active-student picker; ?search=&class_id=&page&limit (max 50).
func SearchCounselingStudents(c *gin.Context) {
	a, ok := counselingActor(c, service.CounselingActSessions)
	if !ok {
		return
	}
	classID, ok := optionalQueryUUID(c, "class_id")
	if !ok {
		return
	}
	page, limit := service.CounselingStudentPage(queryInt(c, "page"), queryInt(c, "limit"))
	items, total, err := counselingSvc.SearchStudents(c.Request.Context(), a.SchoolID, c.Query("search"), classID, page, limit)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "total": total, "meta": dto.NewPageMeta(dto.PageQuery{Page: page, Limit: limit}, total)})
}
