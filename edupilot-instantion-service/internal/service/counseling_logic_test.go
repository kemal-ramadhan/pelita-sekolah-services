package service

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/edupilot-instantion-service/internal/apperr"
	"github.com/google/uuid"

	"github.com/edupilot-instantion-service/internal/dto"
	"github.com/edupilot-instantion-service/internal/model"
)

func TestCounselingSessionRequestNormalize(t *testing.T) {
	req := dto.CounselingSessionRequest{
		Topic:         "  Konseling Karir dan Pilihan Jurusan Kuliah  ",
		RequestedDate: "2026-10-20",
		StartTime:     "09:00",
		EndTime:       "10:00",
		Place:         "",
	}

	err := req.Normalize()
	if err != "" {
		t.Fatalf("expected valid, got: %s", err)
	}
	if req.Place != "Ruang BK" {
		t.Fatalf("expected default place 'Ruang BK', got: %s", req.Place)
	}
	if req.CounselingType != model.CounselingTypeIndividual {
		t.Fatalf("expected default type 'individual', got: %s", req.CounselingType)
	}

	// Invalid times
	badTimeReq := dto.CounselingSessionRequest{
		Topic:         "Test",
		RequestedDate: "2026-10-20",
		StartTime:     "10:00",
		EndTime:       "09:00",
	}
	if badTimeReq.Normalize() == "" {
		t.Fatalf("expected error for endTime <= startTime")
	}

	// Invalid date
	badDateReq := dto.CounselingSessionRequest{
		Topic:         "Test",
		RequestedDate: "20-10-2026",
		StartTime:     "09:00",
		EndTime:       "10:00",
	}
	if badDateReq.Normalize() == "" {
		t.Fatalf("expected error for invalid date format")
	}
}

func TestCounselingOutcomeRequestNormalize(t *testing.T) {
	req := dto.CounselingOutcomeRequest{
		ConfidentialNotes:  "Catatan rahasia guru BK mengenai dinamika psikologis",
		StudentFeedback:    "Tindak lanjut target belajar siswa",
		OutcomeStatus:      "resolved",
		CreateJournalEntry: true,
	}

	if err := req.Normalize(); err != "" {
		t.Fatalf("expected valid outcome request, got: %s", err)
	}

	// Invalid status
	badStatusReq := dto.CounselingOutcomeRequest{
		OutcomeStatus: "random_status",
	}
	if badStatusReq.Normalize() == "" {
		t.Fatalf("expected error for invalid outcome status")
	}
}

func TestCounselingEventRequestNormalize(t *testing.T) {
	req := dto.CounselingEventRequest{
		Title:       "Seminar Bimbingan Karir & SNMPTN",
		Category:    "Seminar",
		Description: "Persiapan pendaftaran perguruan tinggi",
		EventDate:   "2026-11-15",
		StartTime:   "08:00",
		EndTime:     "11:30",
		Location:    "Aula Sekolah",
	}

	if err := req.Normalize(); err != "" {
		t.Fatalf("expected valid event request, got: %s", err)
	}
	if req.TargetAudience != "all" {
		t.Fatalf("expected default audience 'all', got: %s", req.TargetAudience)
	}
}

func TestCounselingDailyJournalNormalize(t *testing.T) {
	req := dto.CounselingDailyJournalRequest{
		Date:              "2026-10-20",
		ServiceType:       "konseling_individual",
		ActivityTitle:     "Layanan Konseling Siswa Bermasalah Kehadiran",
		TargetDescription: "Siswa Kelas 10A",
		ActivitySummary:   "Membahas motivasi belajar dan kendala transportasi",
	}

	if err := req.Normalize(); err != "" {
		t.Fatalf("expected valid journal request, got: %s", err)
	}

	badJournal := dto.CounselingDailyJournalRequest{
		Date: "2026-10-20",
	}
	if badJournal.Normalize() == "" {
		t.Fatalf("expected error for missing required fields")
	}
}

func TestStringSliceJSONHelpers(t *testing.T) {
	urls := []string{"https://example.com/foto1.jpg", "https://example.com/laporan.pdf"}
	jsonStr := dto.StringSliceToJSON(urls)
	parsed := dto.JSONToStringSlice(jsonStr)

	if len(parsed) != 2 || parsed[0] != urls[0] || parsed[1] != urls[1] {
		t.Fatalf("mismatch in JSON helpers: %v vs %v", parsed, urls)
	}

	emptyParsed := dto.JSONToStringSlice("")
	if len(emptyParsed) != 0 {
		t.Fatalf("expected empty slice for empty string")
	}
}

func TestCounselingAccessMatrix(t *testing.T) {
	all := []CounselingAction{CounselingActSummary, CounselingActCounselors, CounselingActToggle, CounselingActSessions,
		CounselingActEventsRead, CounselingActEventsWrite, CounselingActJournals}
	for _, act := range all {
		if CounselingAllowed(CounselingRoleTeacher, act) {
			t.Fatalf("regular teacher must not access %s", act)
		}
	}
	for _, act := range []CounselingAction{CounselingActSessions, CounselingActJournals, CounselingActEventsWrite} {
		if CounselingAllowed(CounselingRoleManager, act) {
			t.Fatalf("manager must not access confidential %s", act)
		}
	}
	for _, act := range []CounselingAction{CounselingActSummary, CounselingActCounselors, CounselingActToggle, CounselingActEventsRead} {
		if !CounselingAllowed(CounselingRoleManager, act) {
			t.Fatalf("manager should access %s", act)
		}
	}
	if CounselingAllowed(CounselingRoleCounselor, CounselingActToggle) {
		t.Fatal("counselor must not toggle counselors")
	}
	for _, act := range []CounselingAction{CounselingActSessions, CounselingActJournals, CounselingActEventsWrite, CounselingActSummary} {
		if !CounselingAllowed(CounselingRoleCounselor, act) {
			t.Fatalf("counselor should access %s", act)
		}
	}
	codeOf := func(err error) string {
		var ae *apperr.Error
		if !errors.As(err, &ae) || ae.Status != 403 {
			t.Fatalf("expected 403 apperr, got %v", err)
		}
		return ae.Code
	}
	if c := codeOf(CounselingForbidden(CounselingRoleTeacher, CounselingActSessions)); c != "NOT_COUNSELOR" {
		t.Fatalf("got %s", c)
	}
	if c := codeOf(CounselingForbidden(CounselingRoleManager, CounselingActSessions)); c != "COUNSELOR_ONLY" {
		t.Fatalf("got %s", c)
	}
	if c := codeOf(CounselingForbidden(CounselingRoleCounselor, CounselingActToggle)); c != "FORBIDDEN_ROLE" {
		t.Fatalf("got %s", c)
	}
}

func TestCounselingTransitions(t *testing.T) {
	ok := [][2]string{
		{"pending", "scheduled"}, {"pending", "rescheduled"}, {"pending", "cancelled"},
		{"scheduled", "completed"}, {"scheduled", "rescheduled"}, {"scheduled", "cancelled"},
		{"rescheduled", "scheduled"}, {"rescheduled", "completed"}, {"rescheduled", "rescheduled"},
	}
	for _, p := range ok {
		if !CounselingTransitionAllowed(p[0], p[1]) {
			t.Fatalf("%s -> %s should be allowed", p[0], p[1])
		}
	}
	bad := [][2]string{
		{"pending", "completed"}, {"pending", "pending"}, {"scheduled", "pending"},
		{"completed", "scheduled"}, {"completed", "cancelled"}, {"cancelled", "scheduled"}, {"cancelled", "completed"},
	}
	for _, p := range bad {
		if CounselingTransitionAllowed(p[0], p[1]) {
			t.Fatalf("%s -> %s must be rejected", p[0], p[1])
		}
	}
}

func TestCounselingSlotError(t *testing.T) {
	loc := LoadSchoolLocation("Asia/Jakarta")
	now := time.Date(2026, 10, 14, 10, 30, 0, 0, loc)
	cases := []struct {
		date, start, end string
		ok               bool
	}{
		{"2026-10-14", "11:00", "12:00", true},
		{"2026-10-14", "10:30", "11:00", true},
		{"2026-10-14", "10:00", "11:00", false}, // started in the past
		{"2026-10-13", "11:00", "12:00", false},
		{"2026-10-15", "12:00", "11:00", false}, // end before start
		{"2027-12-01", "09:00", "10:00", false}, // > 1 year ahead
		{"2026-13-01", "09:00", "10:00", false},
	}
	for _, c := range cases {
		msg := CounselingSlotError(c.date, c.start, c.end, now)
		if (msg == "") != c.ok {
			t.Fatalf("%v: got %q", c, msg)
		}
	}
	// 23:30 UTC on the 13th is already the 14th 06:30 in Jakarta.
	utc := time.Date(2026, 10, 13, 23, 30, 0, 0, time.UTC).In(loc)
	if msg := CounselingSlotError("2026-10-13", "23:45", "23:59", utc); msg == "" {
		t.Fatal("date must be evaluated in the school time zone")
	}
}

func TestEventAttendAndConcludeRules(t *testing.T) {
	loc := LoadSchoolLocation("Asia/Jakarta")
	now := time.Date(2026, 10, 14, 9, 0, 0, 0, loc)
	ev := &model.CounselingEvent{EventDate: "2026-10-14", StartTime: "09:15", EndTime: "11:00", Status: model.EventStatusPublished, TargetAudience: "all"}
	if err := EventAttendError(ev, now, "student"); err != nil {
		t.Fatalf("should open 30 min before start: %v", err)
	}
	if err := EventAttendError(ev, now.Add(3*time.Hour), "student"); err == nil {
		t.Fatal("closed after the event ends")
	}
	if err := EventAttendError(ev, now.Add(-time.Hour), "student"); err == nil {
		t.Fatal("closed more than 30 min before start")
	}
	if err := EventAttendError(ev, now.AddDate(0, 0, 1), "student"); err == nil {
		t.Fatal("only on the event date")
	}
	tev := *ev
	tev.TargetAudience = "teachers"
	if err := EventAttendError(&tev, now, "student"); err == nil {
		t.Fatal("students cannot attend teacher-only events")
	}
	sev := *ev
	sev.TargetAudience = "students"
	if err := EventAttendError(&sev, now, "teacher"); err == nil {
		t.Fatal("teachers cannot attend student-only events")
	}
	done := *ev
	done.Status = model.EventStatusCompleted
	if err := EventAttendError(&done, now, "student"); err == nil {
		t.Fatal("completed events are closed")
	}
	if err := EventConcludeError(ev, now); err == nil {
		t.Fatal("cannot conclude before the event starts")
	}
	if err := EventConcludeError(ev, now.Add(30*time.Minute)); err != nil {
		t.Fatalf("conclude after start: %v", err)
	}
	if err := EventConcludeError(&done, now.Add(30*time.Minute)); err != nil {
		t.Fatalf("editing a completed conclusion is allowed: %v", err)
	}
	cancelled := *ev
	cancelled.Status = model.EventStatusCancelled
	if err := EventConcludeError(&cancelled, now.Add(time.Hour)); err == nil {
		t.Fatal("cancelled events cannot be concluded")
	}
}

func TestCounselingStatusRequestNormalize(t *testing.T) {
	r := dto.CounselingSessionStatusRequest{Status: "rescheduled", NewDate: "2026-10-20"}
	if r.Normalize() == "" {
		t.Fatal("reschedule without reason must fail")
	}
	r = dto.CounselingSessionStatusRequest{Status: "rescheduled", RescheduleReason: "Rapat"}
	if r.Normalize() == "" {
		t.Fatal("reschedule without new slot must fail")
	}
	r = dto.CounselingSessionStatusRequest{Status: "rescheduled", RescheduleReason: "Rapat", NewDate: "2026-10-20"}
	if msg := r.Normalize(); msg != "" {
		t.Fatalf("valid reschedule: %s", msg)
	}
	r = dto.CounselingSessionStatusRequest{Status: "done"}
	if r.Normalize() == "" {
		t.Fatal("unknown status must fail")
	}
	o := dto.CounselingOutcomeRequest{OutcomeStatus: "needs_followup"}
	if o.Normalize() == "" {
		t.Fatal("needs_followup requires follow_up_plan")
	}
	o = dto.CounselingOutcomeRequest{AttachmentURLs: []string{"javascript:alert(1)"}}
	if o.Normalize() == "" {
		t.Fatal("non-http attachment must fail")
	}
	j := dto.CounselingDailyJournalRequest{Date: "2026-10-20", ServiceType: "bogus", ActivityTitle: "x", ActivitySummary: "y"}
	if j.Normalize() == "" {
		t.Fatal("unknown service_type must fail")
	}
}

func TestStudentSessionViewHidesConfidentialData(t *testing.T) {
	me, peer := uuid.New(), uuid.New()
	date := "2026-10-25"
	sess := &model.CounselingSession{
		ID: uuid.New(), InitiatorStudentID: peer, CounselingType: "group", Topic: "Belajar",
		Description: "cerita pribadi pengaju", Status: model.CounselingStatusCompleted,
		Counselor: &model.Employees{ID: uuid.New(), FullName: "Bu BK", Email: strPtr("bk@example.com")},
		Participants: []model.CounselingParticipant{
			{StudentID: peer, IsInitiator: true, Student: &model.Students{ID: peer, FullName: "Peer", NIK: "3201"}},
			{StudentID: me, Student: &model.Students{ID: me, FullName: "Me"}},
		},
		Outcome: &model.CounselingOutcome{ConfidentialNotes: "RAHASIA", StudentFeedback: "Semangat", OutcomeStatus: "resolved", FollowUpDate: &date, AttachmentURLs: `["https://x/y"]`},
	}
	v := CounselingSessionToStudentView(sess, me)
	b, _ := json.Marshal(v)
	out := string(b)
	for _, leak := range []string{"RAHASIA", "confidential_notes", "cerita pribadi", "3201", "bk@example.com", "attachment_urls"} {
		if strings.Contains(out, leak) {
			t.Fatalf("student view leaks %q: %s", leak, out)
		}
	}
	if v.Outcome == nil || v.Outcome.StudentFeedback != "Semangat" || v.IsInitiator {
		t.Fatalf("unexpected view: %+v", v)
	}
	// The initiator sees their own description.
	if iv := CounselingSessionToStudentView(sess, peer); iv.Description == "" || !iv.IsInitiator {
		t.Fatal("initiator should see own description")
	}
	// Outcome is only shown once completed.
	sess.Status = model.CounselingStatusScheduled
	if CounselingSessionToStudentView(sess, me).Outcome != nil {
		t.Fatal("outcome hidden before completion")
	}
	// Staff view keeps only minimal student info.
	sv, _ := json.Marshal(CounselingSessionToView(sess))
	if strings.Contains(string(sv), "3201") {
		t.Fatal("staff view must not include full student records (NIK)")
	}
}

func TestCounselingNotificationHasNoConfidentialContent(t *testing.T) {
	sess := &model.CounselingSession{RequestedDate: "2026-10-20", StartTime: "09:00", EndTime: "10:00", Place: "Ruang BK", Topic: "masalah keluarga"}
	for _, st := range []string{"scheduled", "rescheduled", "cancelled", "completed"} {
		sess.Status = st
		typ, title, body, ok := CounselingNotification(sess, "")
		if !ok || typ == "" || title == "" || body == "" {
			t.Fatalf("%s should notify", st)
		}
		if strings.Contains(body, "keluarga") {
			t.Fatalf("notification must not include the topic: %s", body)
		}
	}
	sess.Status = "pending"
	if _, _, _, ok := CounselingNotification(sess, ""); ok {
		t.Fatal("pending does not notify")
	}
}
