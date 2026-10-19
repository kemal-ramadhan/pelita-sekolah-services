package controller

import (
	"time"

	"edupilot-assesment/internal/model"
	"edupilot-assesment/internal/service/scoring"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// effectiveSubStatus computes expired/depleted on read (§13.4).
func effectiveSubStatus(s *model.SchoolAssessmentSubscription, at time.Time) string {
	if s.Status != model.SubStatusActive {
		return s.Status
	}
	if !s.ExpiresAt.IsZero() && at.After(s.ExpiresAt) {
		return model.SubStatusExpired
	}
	if s.UsedQuota >= s.TotalQuota {
		return model.SubStatusDepleted
	}
	return model.SubStatusActive
}

func packageBrief(p *model.AssessmentPackage) gin.H {
	if p == nil {
		return nil
	}
	return gin.H{"id": p.ID, "name": p.Name, "code": p.Code, "instrument_codes": nonNilStrings(p.InstrumentCodes)}
}

func subscriptionView(s *model.SchoolAssessmentSubscription) gin.H {
	remaining := s.TotalQuota - s.UsedQuota
	if remaining < 0 {
		remaining = 0
	}
	v := gin.H{
		"id": s.ID, "school_id": s.SchoolID, "package_id": s.PackageID, "package": packageBrief(s.Package),
		"claim_type": s.ClaimType, "total_quota": s.TotalQuota, "used_quota": s.UsedQuota, "remaining_quota": remaining,
		"amount_due": s.AmountDue, "status": effectiveSubStatus(s, time.Now()), "expires_at": s.ExpiresAt,
		"created_at": s.CreatedAt, "voucher": nil,
	}
	if s.SchoolName != "" {
		v["school_name"] = s.SchoolName
	}
	if s.ReviewedAt != nil {
		v["reviewed_at"] = s.ReviewedAt
	}
	if s.Voucher != nil {
		v["voucher"] = gin.H{"code": s.Voucher.Code, "name": s.Voucher.Name}
	}
	return v
}

// sessionState: upcoming | open | closed (computed, §13.4).
func sessionState(s *model.AssessmentSession, at time.Time) string {
	switch {
	case s.Status == model.SessionClosed || at.After(s.ClosesAt):
		return "closed"
	case at.Before(s.OpensAt):
		return "upcoming"
	}
	return "open"
}

type sessionStats struct {
	Targeted, Started, Completed, Flagged int64
}

func sessionView(s *model.AssessmentSession, st sessionStats) gin.H {
	v := gin.H{
		"id": s.ID, "title": s.Title, "description": s.Description, "instrument_codes": nonNilStrings(s.InstrumentCodes),
		"target_type": s.TargetType, "class_ids": nonNilStrings(s.ClassIDs), "opens_at": s.OpensAt, "closes_at": s.ClosesAt,
		"max_attempts": s.MaxAttempts, "status": s.Status, "state": sessionState(s, time.Now()), "created_at": s.CreatedAt,
		"stats":        gin.H{"targeted": st.Targeted, "started": st.Started, "completed": st.Completed, "flagged": st.Flagged},
		"subscription": nil,
	}
	if s.Subscription != nil {
		v["subscription"] = gin.H{"id": s.Subscription.ID, "package": packageBrief(s.Subscription.Package)}
	}
	return v
}

func uuidPtrString(u *uuid.UUID) any {
	if u == nil {
		return nil
	}
	return u.String()
}

// resultView = full staff payload (§13.6).
func resultView(r *model.AssessmentResult) gin.H {
	return gin.H{
		"id": r.ID, "attempt_id": r.AttemptID, "session_id": r.SessionID, "instrument_code": r.InstrumentCode,
		"student_id": r.StudentID, "student_name": r.StudentName, "nis": r.NIS, "class_id": uuidPtrString(r.ClassID), "class_name": r.ClassName,
		"holland_code": r.HollandCode, "vark_category": r.VarkCategory, "recommended_track": r.RecommendedTrack,
		"scores": jsonMapOrEmpty(r.ScoresJSON), "indices": jsonMapOrEmpty(r.IndicesJSON), "recommendations": jsonMapOrEmpty(r.RecommendationsJSON),
		"validity_flags": nonNilStrings(r.ValidityFlags), "flags": nonNilStrings(r.FlagsJSON), "staff_notes": nonNilStrings(r.StaffNotes),
		"report_summary": r.ReportSummary, "interpretation": jsonMapOrEmpty(r.Interpretation), "disclaimer": scoring.Disclaimer,
		"instrument_version": r.InstrumentVersion, "config_version": r.ConfigVersion, "created_at": r.CreatedAt,
	}
}

func resultRow(r *model.AssessmentResult) gin.H {
	return gin.H{
		"id": r.ID, "attempt_id": r.AttemptID, "session_id": r.SessionID, "student_id": r.StudentID, "student_name": r.StudentName,
		"nis": r.NIS, "class_id": uuidPtrString(r.ClassID), "class_name": r.ClassName, "instrument_code": r.InstrumentCode,
		"holland_code": r.HollandCode, "vark_category": r.VarkCategory, "recommended_track": r.RecommendedTrack,
		"flags": nonNilStrings(r.FlagsJSON), "validity_flags": nonNilStrings(r.ValidityFlags), "report_summary": r.ReportSummary,
		"created_at": r.CreatedAt,
	}
}

func jsonMapOrEmpty(m model.JSONMap) model.JSONMap {
	if m == nil {
		return model.JSONMap{}
	}
	return m
}

// studentResultView strips staff-only data (§13.6): FLAG_KESEJAHTERAAN, the
// WEL1 raw value, staff notes; adds the neutral wellbeing message.
func studentResultView(r *model.AssessmentResult) gin.H {
	v := resultView(r)
	welfare := false
	flags := []string{}
	for _, f := range r.FlagsJSON {
		if f == scoring.FlagWelfare {
			welfare = true
			continue
		}
		flags = append(flags, f)
	}
	v["flags"] = flags
	scores := model.JSONMap{}
	for k, x := range jsonMapOrEmpty(r.ScoresJSON) {
		if k == "wellbeing_items" {
			continue
		}
		scores[k] = x
	}
	v["scores"] = scores
	recs := model.JSONMap{}
	for k, x := range jsonMapOrEmpty(r.RecommendationsJSON) {
		if k == "staff_notes" {
			continue
		}
		recs[k] = x
	}
	v["recommendations"] = recs
	delete(v, "staff_notes")
	delete(v, "student_name")
	delete(v, "nis")
	if welfare {
		v["wellbeing_message"] = scoring.WellbeingMessage
	}
	return v
}

func alertView(a *model.AssessmentWelfareAlert) gin.H {
	v := gin.H{
		"id": a.ID, "student_id": a.StudentID, "student_name": a.StudentName, "class_name": a.ClassName,
		"result_id": a.ResultID, "session_id": a.SessionID, "flag": a.Flag, "status": a.Status, "note": a.Note,
		"created_at": a.CreatedAt, "handled_by_name": nil, "handled_at": a.HandledAt,
	}
	if a.HandledByName != "" {
		v["handled_by_name"] = a.HandledByName
	}
	return v
}
