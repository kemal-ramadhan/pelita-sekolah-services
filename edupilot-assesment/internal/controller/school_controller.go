package controller

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"sort"
	"strings"
	"time"

	"edupilot-assesment/internal/apperr"
	"edupilot-assesment/internal/client"
	"edupilot-assesment/internal/config"
	"edupilot-assesment/internal/middleware"
	"edupilot-assesment/internal/model"
	"edupilot-assesment/internal/service/scoring"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// SchoolController — managers + Guru BK (contract §13.4).
type SchoolController struct{}

func NewSchoolController() *SchoolController { return &SchoolController{} }

func schoolOf(c *gin.Context) (*client.Principal, uuid.UUID) {
	p := middleware.GetPrincipal(c)
	return p, *p.SchoolID
}

func actorName(c *gin.Context, p *client.Principal) string {
	if p.Kind != middleware.KindManager && p.DisplayName != "" {
		return p.DisplayName
	}
	if e := c.GetString(middleware.CtxEmail); e != "" {
		return e
	}
	return p.DisplayName
}

func audit(c *gin.Context, p *client.Principal, action string, studentID, resultID *uuid.UUID) {
	row := model.AssessmentAuditLog{
		ID: uuid.New(), SchoolID: *p.SchoolID, ActorUserID: middleware.UserID(c), ActorKind: p.Kind, Action: action,
		TargetStudentID: studentID, ResultID: resultID, IP: c.ClientIP(), At: time.Now(),
	}
	if err := config.DB.Create(&row).Error; err != nil {
		config.Logger.Error("audit log write failed", zap.Error(err))
	}
}

func (sc *SchoolController) GetMe(c *gin.Context) {
	p, schoolID := schoolOf(c)
	mirrorOK(c, http.StatusOK, map[string]any{
		"kind": p.Kind, "can_manage": p.Kind == middleware.KindManager, "is_counselor": p.Kind == middleware.KindCounselor || p.IsCounselor,
		"school_id": schoolID, "school_name": p.SchoolName,
	})
}

// ---------- Billing ----------

func (sc *SchoolController) GetSubscriptions(c *gin.Context) {
	_, schoolID := schoolOf(c)
	subs := []model.SchoolAssessmentSubscription{}
	if err := config.DB.Preload("Package").Preload("Voucher").Where("school_id = ?", schoolID).Order("created_at DESC").Find(&subs).Error; err != nil {
		respondError(c, err)
		return
	}
	out := make([]gin.H, 0, len(subs))
	total, used, active := 0, 0, 0
	for i := range subs {
		v := subscriptionView(&subs[i])
		out = append(out, v)
		if v["status"] == model.SubStatusActive {
			total += subs[i].TotalQuota
			used += subs[i].UsedQuota
			active++
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": out, "summary": gin.H{
		"total_quota": total, "used_quota": used, "remaining_quota": total - used, "active_packages": active,
	}})
}

func (sc *SchoolController) GetAvailablePackages(c *gin.Context) {
	rows := []model.AssessmentPackage{}
	if err := config.DB.Where("is_active = true").Order("price ASC, name ASC").Find(&rows).Error; err != nil {
		respondError(c, err)
		return
	}
	dataOK(c, http.StatusOK, rows, "")
}

func activePackage(db *gorm.DB, id uuid.UUID) (*model.AssessmentPackage, error) {
	var pkg model.AssessmentPackage
	if err := db.First(&pkg, "id = ? AND is_active = true", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, apperr.NotFound("Paket asesmen tidak ditemukan atau tidak aktif")
		}
		return nil, err
	}
	return &pkg, nil
}

func (sc *SchoolController) ClaimVoucher(c *gin.Context) {
	p, schoolID := schoolOf(c)
	var req struct {
		VoucherCode string    `json:"voucher_code"`
		PackageID   uuid.UUID `json:"package_id"`
	}
	if !bindJSON(c, &req) {
		return
	}
	code := strings.ToUpper(strings.TrimSpace(req.VoucherCode))
	if code == "" || req.PackageID == uuid.Nil {
		respondError(c, apperr.BadRequest("voucher_code dan package_id wajib diisi"))
		return
	}
	var sub model.SchoolAssessmentSubscription
	err := config.DB.Transaction(func(tx *gorm.DB) error {
		var v model.AssessmentVoucher
		if err := tx.Where("upper(code) = ? AND is_active = true", code).First(&v).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return apperr.Invalid("INVALID_VOUCHER", "Kode voucher tidak valid atau sudah tidak aktif")
			}
			return err
		}
		now := time.Now()
		if now.Before(v.ValidFrom) || now.After(v.ValidUntil) {
			return apperr.Invalid("VOUCHER_EXPIRED", "Voucher telah kedaluwarsa atau belum berlaku")
		}
		pkg, err := activePackage(tx, req.PackageID)
		if err != nil {
			return err
		}
		if len(v.ApplicablePackageIDs) > 0 && !contains(v.ApplicablePackageIDs, pkg.ID.String()) {
			return apperr.Invalid("VOUCHER_NOT_APPLICABLE", "Voucher ini tidak berlaku untuk paket yang dipilih")
		}
		var claimed int64
		tx.Model(&model.AssessmentVoucherLog{}).Where("voucher_id = ? AND school_id = ?", v.ID, schoolID).Count(&claimed)
		if claimed > 0 {
			return apperr.Conflict("ALREADY_CLAIMED", "Sekolah sudah pernah mengklaim voucher ini")
		}
		// Atomic usage increment (used_count < max_usage inside the UPDATE).
		res := tx.Model(&model.AssessmentVoucher{}).Where("id = ? AND used_count < max_usage", v.ID).Update("used_count", gorm.Expr("used_count + 1"))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return apperr.Conflict("VOUCHER_EXHAUSTED", "Kuota pemakaian voucher ini sudah habis")
		}
		due := 0.0
		switch v.DiscountType {
		case "percentage":
			due = math.Round(pkg.Price*(1-v.DiscountValue/100)*100) / 100
		case "fixed_amount":
			due = math.Max(0, pkg.Price-v.DiscountValue)
		}
		status := model.SubStatusActive
		if v.DiscountType != "free_claim" && due > 0 {
			status = model.SubStatusPending
		}
		sub = model.SchoolAssessmentSubscription{
			ID: uuid.New(), SchoolID: schoolID, SchoolName: p.SchoolName, PackageID: pkg.ID, VoucherID: &v.ID, ClaimType: "voucher",
			TotalQuota: pkg.StudentQuota, AmountDue: due, Status: status, ExpiresAt: now.Add(365 * 24 * time.Hour), RequestedBy: middleware.UserID(c),
		}
		if err := tx.Create(&sub).Error; err != nil {
			return err
		}
		log := model.AssessmentVoucherLog{ID: uuid.New(), VoucherID: v.ID, SchoolID: schoolID, SchoolName: p.SchoolName, SubscriptionID: sub.ID, ClaimedAt: now}
		if err := tx.Create(&log).Error; err != nil {
			if isUniqueViolation(err) {
				return apperr.Conflict("ALREADY_CLAIMED", "Sekolah sudah pernah mengklaim voucher ini")
			}
			return err
		}
		return nil
	})
	if err != nil {
		respondError(c, err)
		return
	}
	config.DB.Preload("Package").Preload("Voucher").First(&sub, "id = ?", sub.ID)
	msg := fmt.Sprintf("Voucher berhasil diklaim. Kuota %d siswa aktif.", sub.TotalQuota)
	if sub.Status == model.SubStatusPending {
		msg = "Voucher berhasil diklaim. Langganan menunggu persetujuan admin setelah pembayaran."
	}
	dataOK(c, http.StatusCreated, subscriptionView(&sub), msg)
}

// PurchasePackage: price 0 → active, otherwise pending until an admin approves.
func (sc *SchoolController) PurchasePackage(c *gin.Context) {
	p, schoolID := schoolOf(c)
	var req struct {
		PackageID uuid.UUID `json:"package_id"`
	}
	if !bindJSON(c, &req) {
		return
	}
	pkg, err := activePackage(config.DB, req.PackageID)
	if err != nil {
		respondError(c, err)
		return
	}
	status := model.SubStatusPending
	if pkg.Price <= 0 {
		status = model.SubStatusActive
	}
	sub := model.SchoolAssessmentSubscription{
		ID: uuid.New(), SchoolID: schoolID, SchoolName: p.SchoolName, PackageID: pkg.ID, ClaimType: "purchase",
		TotalQuota: pkg.StudentQuota, AmountDue: pkg.Price, Status: status, ExpiresAt: time.Now().Add(365 * 24 * time.Hour), RequestedBy: middleware.UserID(c),
	}
	if err := config.DB.Create(&sub).Error; err != nil {
		respondError(c, err)
		return
	}
	sub.Package = pkg
	msg := "Pengajuan pembelian tercatat dan menunggu persetujuan admin."
	if status == model.SubStatusActive {
		msg = fmt.Sprintf("Paket %s aktif. Kuota %d siswa.", pkg.Name, pkg.StudentQuota)
	}
	dataOK(c, http.StatusCreated, subscriptionView(&sub), msg)
}

// ---------- Classes & students (from instantion) ----------

func (sc *SchoolController) GetClasses(c *gin.Context) {
	_, schoolID := schoolOf(c)
	classes, err := client.Default.Classes(c.Request.Context(), schoolID)
	if err != nil {
		respondError(c, err)
		return
	}
	dataOK(c, http.StatusOK, classes, "")
}

func (sc *SchoolController) GetStudents(c *gin.Context) {
	_, schoolID := schoolOf(c)
	classID, ok := queryUUID(c, "class_id")
	if !ok {
		return
	}
	var classIDs []uuid.UUID
	if classID != nil {
		classIDs = []uuid.UUID{*classID}
	}
	all, err := client.Default.Students(c.Request.Context(), schoolID, classIDs, nil)
	if err != nil {
		respondError(c, err)
		return
	}
	p := parsePage(c)
	term := strings.ToLower(p.Search)
	filtered := []gin.H{}
	for _, s := range all {
		if term != "" && !strings.Contains(strings.ToLower(s.FullName), term) && !strings.Contains(strings.ToLower(s.NIS), term) && !strings.Contains(s.NISN, term) {
			continue
		}
		filtered = append(filtered, gin.H{"student_id": s.StudentID, "full_name": s.FullName, "nis": s.NIS, "class_id": uuidPtrString(s.ClassID), "class_name": s.ClassName})
	}
	total := int64(len(filtered))
	start := p.Offset()
	if start > len(filtered) {
		start = len(filtered)
	}
	end := start + p.Limit
	if end > len(filtered) {
		end = len(filtered)
	}
	listOK(c, filtered[start:end], p, total)
}

// ---------- Sessions ----------

func loadSessionStats(db *gorm.DB, ids []uuid.UUID) map[uuid.UUID]sessionStats {
	out := map[uuid.UUID]sessionStats{}
	if len(ids) == 0 {
		return out
	}
	type row struct {
		SessionID uuid.UUID
		N         int64
	}
	var rows []row
	set := func(rs []row, f func(*sessionStats, int64)) {
		for _, r := range rs {
			s := out[r.SessionID]
			f(&s, r.N)
			out[r.SessionID] = s
		}
	}
	db.Raw(`SELECT session_id, COUNT(*) AS n FROM assessment_session_targets WHERE session_id IN ? GROUP BY session_id`, ids).Scan(&rows)
	set(rows, func(s *sessionStats, n int64) { s.Targeted = n })
	rows = nil
	db.Raw(`SELECT session_id, COUNT(DISTINCT student_id) AS n FROM assessment_attempts WHERE session_id IN ? GROUP BY session_id`, ids).Scan(&rows)
	set(rows, func(s *sessionStats, n int64) { s.Started = n })
	rows = nil
	db.Raw(`SELECT session_id, COUNT(*) AS n FROM assessment_session_targets WHERE session_id IN ? AND status = 'completed' GROUP BY session_id`, ids).Scan(&rows)
	set(rows, func(s *sessionStats, n int64) { s.Completed = n })
	rows = nil
	db.Raw(`SELECT session_id, COUNT(DISTINCT student_id) AS n FROM assessment_results WHERE session_id IN ?
		AND (validity_flags <> '[]'::jsonb OR jsonb_exists(flags_json, 'FLAG_KESEJAHTERAAN')) GROUP BY session_id`, ids).Scan(&rows)
	set(rows, func(s *sessionStats, n int64) { s.Flagged = n })
	return out
}

func (sc *SchoolController) GetSessions(c *gin.Context) {
	_, schoolID := schoolOf(c)
	p := parsePage(c)
	q := config.DB.Model(&model.AssessmentSession{}).Where("school_id = ?", schoolID)
	now := time.Now()
	switch c.Query("status") {
	case "published":
		q = q.Where("status = ?", model.SessionPublished)
	case "closed":
		q = q.Where("status = ? OR closes_at < ?", model.SessionClosed, now)
	case "open":
		q = q.Where("status = ? AND opens_at <= ? AND closes_at >= ?", model.SessionPublished, now, now)
	case "upcoming":
		q = q.Where("status = ? AND opens_at > ?", model.SessionPublished, now)
	}
	if p.Search != "" {
		q = q.Where("title ILIKE ?", likePattern(p.Search))
	}
	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		respondError(c, err)
		return
	}
	rows := []model.AssessmentSession{}
	if err := q.Preload("Subscription.Package").Order("created_at DESC").Offset(p.Offset()).Limit(p.Limit).Find(&rows).Error; err != nil {
		respondError(c, err)
		return
	}
	ids := make([]uuid.UUID, len(rows))
	for i := range rows {
		ids[i] = rows[i].ID
	}
	stats := loadSessionStats(config.DB, ids)
	out := make([]gin.H, 0, len(rows))
	for i := range rows {
		out = append(out, sessionView(&rows[i], stats[rows[i].ID]))
	}
	listOK(c, out, p, total)
}

func findSchoolSession(schoolID, id uuid.UUID) (*model.AssessmentSession, error) {
	var s model.AssessmentSession
	if err := config.DB.Preload("Subscription.Package").First(&s, "id = ? AND school_id = ?", id, schoolID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, apperr.NotFound("Sesi asesmen tidak ditemukan")
		}
		return nil, err
	}
	return &s, nil
}

func (sc *SchoolController) GetSession(c *gin.Context) {
	_, schoolID := schoolOf(c)
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	s, err := findSchoolSession(schoolID, id)
	if err != nil {
		respondError(c, err)
		return
	}
	v := sessionView(s, loadSessionStats(config.DB, []uuid.UUID{s.ID})[s.ID])
	targets := []model.AssessmentSessionTarget{}
	config.DB.Where("session_id = ?", s.ID).Order("class_name ASC, full_name ASC").Find(&targets)
	var atts []model.AssessmentAttempt
	config.DB.Select("student_id, instrument_code, status, created_at").Where("session_id = ?", s.ID).Order("created_at ASC").Find(&atts)
	latest := map[uuid.UUID]map[string]string{}
	for _, a := range atts {
		if latest[a.StudentID] == nil {
			latest[a.StudentID] = map[string]string{}
		}
		latest[a.StudentID][a.InstrumentCode] = a.Status
	}
	tv := make([]gin.H, 0, len(targets))
	for _, t := range targets {
		inst := map[string]string{}
		for _, code := range s.InstrumentCodes {
			st := latest[t.StudentID][code]
			if st == "" {
				st = "not_started"
			}
			inst[code] = st
		}
		tv = append(tv, gin.H{"student_id": t.StudentID, "full_name": t.FullName, "nis": t.NIS, "class_id": uuidPtrString(t.ClassID),
			"class_name": t.ClassName, "status": t.Status, "instruments": inst})
	}
	v["targets"] = tv
	dataOK(c, http.StatusOK, v, "")
}

type createSessionReq struct {
	SubscriptionID  uuid.UUID   `json:"subscription_id"`
	Title           string      `json:"title"`
	Description     string      `json:"description"`
	InstrumentCodes []string    `json:"instrument_codes"`
	TargetType      string      `json:"target_type"`
	ClassIDs        []uuid.UUID `json:"class_ids"`
	StudentIDs      []uuid.UUID `json:"student_ids"`
	RandomCount     int         `json:"random_count"`
	OpensAt         time.Time   `json:"opens_at"`
	ClosesAt        time.Time   `json:"closes_at"`
	MaxAttempts     *int        `json:"max_attempts"`
}

func (sc *SchoolController) CreateSession(c *gin.Context) {
	_, schoolID := schoolOf(c)
	var req createSessionReq
	if !bindJSON(c, &req) {
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	maxAttempts := 1
	if req.MaxAttempts != nil {
		maxAttempts = *req.MaxAttempts
	}
	var verr error
	switch {
	case req.SubscriptionID == uuid.Nil:
		verr = apperr.BadRequest("subscription_id wajib diisi")
	case req.Title == "" || len(req.Title) > 200:
		verr = apperr.BadRequest("title wajib diisi (maks. 200 karakter)")
	case req.OpensAt.IsZero() || req.ClosesAt.IsZero() || !req.ClosesAt.After(req.OpensAt):
		verr = apperr.BadRequest("closes_at harus setelah opens_at")
	case maxAttempts < 1 || maxAttempts > 5:
		verr = apperr.BadRequest("max_attempts harus 1–5")
	}
	if verr == nil {
		switch req.TargetType {
		case model.TargetTypeClass:
			if len(req.ClassIDs) == 0 {
				verr = apperr.BadRequest("class_ids wajib diisi untuk target kelas")
			}
		case model.TargetTypeRandom:
			if len(req.ClassIDs) == 0 || req.RandomCount <= 0 {
				verr = apperr.BadRequest("class_ids dan random_count wajib diisi untuk target acak")
			}
		case model.TargetTypeStudents:
			if len(req.StudentIDs) == 0 {
				verr = apperr.BadRequest("student_ids wajib diisi untuk target siswa")
			}
		default:
			verr = apperr.BadRequest("target_type harus class, random_students, atau students")
		}
	}
	if verr != nil {
		respondError(c, verr)
		return
	}
	var sub model.SchoolAssessmentSubscription
	if err := config.DB.Preload("Package").First(&sub, "id = ? AND school_id = ?", req.SubscriptionID, schoolID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			respondError(c, apperr.Invalid("INVALID_SUBSCRIPTION", "Paket langganan tidak ditemukan"))
			return
		}
		respondError(c, err)
		return
	}
	if effectiveSubStatus(&sub, time.Now()) != model.SubStatusActive {
		respondError(c, apperr.Invalid("INVALID_SUBSCRIPTION", "Paket langganan tidak aktif (pending, kedaluwarsa, atau kuota habis)"))
		return
	}
	codes, err := normInstruments(req.InstrumentCodes)
	if err == nil && sub.Package != nil {
		for _, code := range codes {
			if !contains(sub.Package.InstrumentCodes, code) {
				err = apperr.Invalid("INVALID_INSTRUMENT", "Instrumen "+code+" tidak termasuk dalam paket langganan")
				break
			}
		}
	}
	if err != nil {
		respondError(c, err)
		return
	}

	var classIDs, studentIDs []uuid.UUID
	if req.TargetType == model.TargetTypeStudents {
		studentIDs = req.StudentIDs
	} else {
		classIDs = req.ClassIDs
	}
	students, err := client.Default.Students(c.Request.Context(), schoolID, classIDs, studentIDs)
	if err != nil {
		respondError(c, err)
		return
	}
	if req.TargetType == model.TargetTypeRandom && req.RandomCount < len(students) {
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		rng.Shuffle(len(students), func(i, j int) { students[i], students[j] = students[j], students[i] })
		students = students[:req.RandomCount]
	}
	if len(students) == 0 {
		respondError(c, apperr.Invalid("NO_TARGETS", "Tidak ada siswa aktif pada target yang dipilih"))
		return
	}
	if remaining := sub.TotalQuota - sub.UsedQuota; len(students) > remaining {
		respondError(c, apperr.Invalid("QUOTA_EXCEEDED", fmt.Sprintf("Jumlah target (%d) melebihi sisa kuota paket (%d)", len(students), remaining)).With("remaining_quota", remaining))
		return
	}
	cls := []string{}
	for _, id := range req.ClassIDs {
		if !contains(cls, id.String()) {
			cls = append(cls, id.String())
		}
	}
	session := model.AssessmentSession{
		ID: uuid.New(), SchoolID: schoolID, SubscriptionID: sub.ID, CreatedByUserID: middleware.UserID(c),
		Title: req.Title, Description: strings.TrimSpace(req.Description), TargetType: req.TargetType, ClassIDs: cls,
		InstrumentCodes: codes, OpensAt: req.OpensAt, ClosesAt: req.ClosesAt, MaxAttempts: maxAttempts, Status: model.SessionPublished,
	}
	targets := make([]model.AssessmentSessionTarget, 0, len(students))
	seen := map[uuid.UUID]bool{}
	for _, s := range students {
		if seen[s.StudentID] {
			continue
		}
		seen[s.StudentID] = true
		targets = append(targets, model.AssessmentSessionTarget{
			ID: uuid.New(), SessionID: session.ID, StudentID: s.StudentID, UserID: s.UserID, FullName: s.FullName,
			NIS: s.NIS, ClassID: s.ClassID, ClassName: s.ClassName, Status: model.TargetAssigned,
		})
	}
	err = config.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&session).Error; err != nil {
			return err
		}
		return tx.CreateInBatches(&targets, 500).Error
	})
	if err != nil {
		respondError(c, err)
		return
	}
	session.Subscription = &sub
	dataOK(c, http.StatusCreated, sessionView(&session, sessionStats{Targeted: int64(len(targets))}), "Sesi asesmen berhasil dibuat dan ditugaskan")
}

func (sc *SchoolController) CloseSession(c *gin.Context) {
	_, schoolID := schoolOf(c)
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	s, err := findSchoolSession(schoolID, id)
	if err != nil {
		respondError(c, err)
		return
	}
	if err := config.DB.Model(s).Update("status", model.SessionClosed).Error; err != nil {
		respondError(c, err)
		return
	}
	s.Status = model.SessionClosed
	dataOK(c, http.StatusOK, sessionView(s, loadSessionStats(config.DB, []uuid.UUID{s.ID})[s.ID]), "Sesi asesmen ditutup")
}

func (sc *SchoolController) DeleteSession(c *gin.Context) {
	_, schoolID := schoolOf(c)
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	s, err := findSchoolSession(schoolID, id)
	if err != nil {
		respondError(c, err)
		return
	}
	var n int64
	config.DB.Model(&model.AssessmentAttempt{}).Where("session_id = ?", s.ID).Count(&n)
	if n > 0 {
		respondError(c, apperr.Conflict("IN_USE", "Sesi sudah dikerjakan siswa sehingga tidak dapat dihapus; tutup sesi sebagai gantinya"))
		return
	}
	err = config.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("session_id = ?", s.ID).Delete(&model.AssessmentSessionTarget{}).Error; err != nil {
			return err
		}
		tx.Where("session_id = ?", s.ID).Delete(&model.AssessmentConsent{})
		return tx.Delete(&model.AssessmentSession{}, "id = ?", s.ID).Error
	})
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Sesi asesmen dihapus"})
}

// ---------- Results ----------

func resultQuery(c *gin.Context, schoolID uuid.UUID) (*gorm.DB, bool) {
	q := config.DB.Model(&model.AssessmentResult{}).Where("school_id = ?", schoolID)
	sessionID, ok := queryUUID(c, "session_id")
	if !ok {
		return nil, false
	}
	classID, ok := queryUUID(c, "class_id")
	if !ok {
		return nil, false
	}
	if sessionID != nil {
		q = q.Where("session_id = ?", *sessionID)
	}
	if classID != nil {
		q = q.Where("class_id = ?", *classID)
	}
	if code := strings.ToUpper(c.Query("instrument_code")); code != "" {
		q = q.Where("instrument_code = ?", code)
	}
	if f := strings.TrimSpace(c.Query("flag")); f != "" {
		q = q.Where("(jsonb_exists(flags_json, ?) OR jsonb_exists(validity_flags, ?))", f, f)
	}
	if s := strings.TrimSpace(c.Query("search")); s != "" {
		like := likePattern(s)
		q = q.Where("(student_name ILIKE ? OR nis ILIKE ?)", like, like)
	}
	return q, true
}

func (sc *SchoolController) GetResults(c *gin.Context) {
	_, schoolID := schoolOf(c)
	q, ok := resultQuery(c, schoolID)
	if !ok {
		return
	}
	p := parsePage(c)
	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		respondError(c, err)
		return
	}
	rows := []model.AssessmentResult{}
	if err := q.Order("created_at DESC").Offset(p.Offset()).Limit(p.Limit).Find(&rows).Error; err != nil {
		respondError(c, err)
		return
	}
	out := make([]gin.H, 0, len(rows))
	for i := range rows {
		out = append(out, resultRow(&rows[i]))
	}
	listOK(c, out, p, total)
}

func (sc *SchoolController) GetResult(c *gin.Context) {
	p, schoolID := schoolOf(c)
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	var r model.AssessmentResult
	if err := config.DB.First(&r, "id = ? AND school_id = ?", id, schoolID).Error; err != nil {
		respondError(c, err)
		return
	}
	audit(c, p, "view_result", &r.StudentID, &r.ID)
	v := resultView(&r)
	v["student"] = gin.H{"id": r.StudentID, "full_name": r.StudentName, "nis": r.NIS, "class_id": uuidPtrString(r.ClassID), "class_name": r.ClassName}
	dataOK(c, http.StatusOK, v, "")
}

func flattenScores(m model.JSONMap, key string) string {
	v, ok := m[key].(map[string]any)
	if !ok {
		return ""
	}
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := []string{}
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v[k]))
	}
	return strings.Join(parts, "; ")
}

func (sc *SchoolController) ExportResults(c *gin.Context) {
	p, schoolID := schoolOf(c)
	q, ok := resultQuery(c, schoolID)
	if !ok {
		return
	}
	rows := []model.AssessmentResult{}
	if err := q.Order("class_name ASC, student_name ASC, instrument_code ASC").Limit(10000).Find(&rows).Error; err != nil {
		respondError(c, err)
		return
	}
	audit(c, p, "export_results", nil, nil)
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="hasil-asesmen-%s.csv"`, time.Now().Format("20060102-150405")))
	c.Status(http.StatusOK)
	_, _ = c.Writer.Write([]byte("\xEF\xBB\xBF"))
	w := csv.NewWriter(c.Writer)
	_ = w.Write([]string{"nama_siswa", "nis", "kelas", "instrumen", "kode_holland", "gaya_belajar", "rekomendasi_rumpun",
		"skor_utama", "persentase", "flags", "validity_flags", "ringkasan", "versi_instrumen", "versi_config", "tanggal"})
	mainKey := map[string]string{scoring.InstrumentRIA: "raw_scores", scoring.InstrumentPMK: "total_scores", scoring.InstrumentTMB: "rumpun_scores", scoring.InstrumentADN: "vark_scores"}
	pctKey := map[string]string{scoring.InstrumentRIA: "percentages", scoring.InstrumentPMK: "percentages", scoring.InstrumentTMB: "minat_scores", scoring.InstrumentADN: "socio_emotional_levels"}
	for _, r := range rows {
		_ = w.Write([]string{r.StudentName, r.NIS, r.ClassName, r.InstrumentCode, r.HollandCode, r.VarkCategory, r.RecommendedTrack,
			flattenScores(r.ScoresJSON, mainKey[r.InstrumentCode]), flattenScores(r.ScoresJSON, pctKey[r.InstrumentCode]),
			strings.Join(r.FlagsJSON, "|"), strings.Join(r.ValidityFlags, "|"), r.ReportSummary, r.InstrumentVersion, r.ConfigVersion,
			r.CreatedAt.Format(time.RFC3339)})
	}
	w.Flush()
}

// ---------- Analytics (computed from data, §13.4) ----------

func (sc *SchoolController) GetAnalytics(c *gin.Context) {
	_, schoolID := schoolOf(c)
	sessionID, ok := queryUUID(c, "session_id")
	if !ok {
		return
	}
	classID, ok := queryUUID(c, "class_id")
	if !ok {
		return
	}
	where := "school_id = ?"
	args := []any{schoolID}
	if sessionID != nil {
		where += " AND session_id = ?"
		args = append(args, *sessionID)
	}
	if classID != nil {
		where += " AND class_id = ?"
		args = append(args, *classID)
	}
	rows := []model.AssessmentResult{}
	// latest result per student and instrument
	if err := config.DB.Raw(`SELECT DISTINCT ON (student_id, instrument_code) * FROM assessment_results WHERE `+where+
		` ORDER BY student_id, instrument_code, created_at DESC`, args...).Scan(&rows).Error; err != nil {
		respondError(c, err)
		return
	}
	vark := map[string]int{"Visual": 0, "Auditori": 0, "Baca-Tulis": 0, "Kinestetik": 0, "Multimodal": 0, "Belum jelas": 0}
	riasec := map[string]int{"R": 0, "I": 0, "A": 0, "S": 0, "E": 0, "C": 0}
	codes := map[string]int{}
	tracks := map[string]int{}
	byInst := map[string]int{"TMB": 0, "RIA": 0, "PMK": 0, "ADN": 0}
	socio := map[string]map[string]int{}
	for _, k := range []string{scoring.SocioMotivasi, scoring.SocioEfikasi, scoring.SocioRegulasi, scoring.SocioKecemasan, scoring.SocioDukungan} {
		socio[k] = map[string]int{"kuat": 0, "berkembang": 0, "perlu_dukungan": 0}
	}
	students := map[uuid.UUID]bool{}
	for _, r := range rows {
		students[r.StudentID] = true
		byInst[r.InstrumentCode]++
		switch r.InstrumentCode {
		case scoring.InstrumentRIA, scoring.InstrumentPMK:
			if len(r.HollandCode) >= 1 {
				riasec[r.HollandCode[:1]]++
			}
			if len(r.HollandCode) == 3 {
				codes[r.HollandCode]++
			}
		case scoring.InstrumentTMB:
			t := r.RecommendedTrack
			if t == "" {
				t = "Belum jelas"
			}
			tracks[t]++
		case scoring.InstrumentADN:
			vt, _ := r.IndicesJSON["vark_type"].(string)
			dom, _ := r.IndicesJSON["vark_dominant"].([]any)
			switch {
			case vt == scoring.VarkUnimodal && len(dom) == 1:
				if m, ok := dom[0].(string); ok {
					vark[scoring.ModalityNames[m]]++
				}
			case vt == scoring.VarkUnclear:
				vark["Belum jelas"]++
			default:
				vark["Multimodal"]++
			}
			if lv, ok := r.ScoresJSON["socio_emotional_levels"].(map[string]any); ok {
				for sub, x := range lv {
					s, _ := x.(string)
					if socio[sub] == nil {
						continue
					}
					switch s {
					case scoring.LevelStrong:
						socio[sub]["kuat"]++
					case scoring.LevelGrowing:
						socio[sub]["berkembang"]++
					case scoring.LevelSupport:
						socio[sub]["perlu_dukungan"]++
					}
				}
			}
		}
	}
	top := []gin.H{}
	keys := make([]string, 0, len(codes))
	for k := range codes {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if codes[keys[i]] != codes[keys[j]] {
			return codes[keys[i]] > codes[keys[j]]
		}
		return keys[i] < keys[j]
	})
	for i, k := range keys {
		if i >= 5 {
			break
		}
		top = append(top, gin.H{"code": k, "count": codes[k]})
	}
	aq := config.DB.Model(&model.AssessmentWelfareAlert{}).Where("school_id = ? AND status <> ?", schoolID, model.AlertResolved)
	if sessionID != nil {
		aq = aq.Where("session_id = ?", *sessionID)
	}
	if classID != nil {
		aq = aq.Where("class_id = ?", *classID)
	}
	var openAlerts int64
	aq.Count(&openAlerts)
	mirrorOK(c, http.StatusOK, map[string]any{
		"total_participants": len(students), "by_instrument": byInst, "vark_distribution": vark, "riasec_primary": riasec,
		"top_codes": top, "track_distribution": tracks, "socio_emotional": socio, "welfare_alerts_open": openAlerts,
		"insights": insights(vark, socio, riasec),
	})
}

// insights are derived from the distributions (PRD §9.5), never hard-coded results.
func insights(vark map[string]int, socio map[string]map[string]int, riasec map[string]int) gin.H {
	total := 0
	best, bestN := "", -1
	for _, k := range []string{"Visual", "Auditori", "Baca-Tulis", "Kinestetik", "Multimodal", "Belum jelas"} {
		total += vark[k]
		if vark[k] > bestN {
			best, bestN = k, vark[k]
		}
	}
	if total == 0 {
		return gin.H{"primary_learning_style": "Belum ada data gaya belajar",
			"teaching_recommendation": "Jalankan Asesmen Diagnostik Non-Kognitif (ADN) untuk melihat sebaran preferensi belajar kelas."}
	}
	strategy := map[string]string{
		"Visual": "peta konsep, diagram alur, infografis, dan video", "Auditori": "diskusi kelompok, tanya jawab, dan penjelasan lisan",
		"Baca-Tulis": "ringkasan tertulis, glosarium, dan latihan soal tertulis", "Kinestetik": "praktikum, proyek, simulasi, dan role-play",
		"Multimodal": "kombinasi media (teks, diagram, diskusi, praktik)", "Belum jelas": "beragam cara belajar agar siswa menemukan yang paling membantu",
	}
	pct := math.Round(float64(bestN) / float64(total) * 100)
	rec := fmt.Sprintf("Sebanyak %.0f%% siswa (%d dari %d) cenderung %s; variasikan pembelajaran dengan %s, sambil tetap memadukan media lain (diferensiasi pembelajaran).",
		pct, bestN, total, strings.ToLower(best), strategy[best])
	support := 0
	for _, sub := range []string{scoring.SocioRegulasi, scoring.SocioMotivasi} {
		support += socio[sub]["perlu_dukungan"]
	}
	if support > 0 {
		rec += fmt.Sprintf(" Ada %d catatan regulasi/motivasi belajar yang perlu dukungan; jadwal belajar sederhana dan target kecil bertahap dapat membantu.", support)
	}
	topType, topN := "", 0
	for _, t := range scoring.RIASECTypes() {
		if riasec[t] > topN {
			topType, topN = t, riasec[t]
		}
	}
	out := gin.H{"primary_learning_style": fmt.Sprintf("%s (%.0f%%)", best, pct), "teaching_recommendation": rec}
	if topType != "" {
		out["dominant_interest"] = scoring.TypeLabel(topType)
	}
	return out
}

// ---------- Welfare alerts ----------

func (sc *SchoolController) GetAlerts(c *gin.Context) {
	_, schoolID := schoolOf(c)
	p := parsePage(c)
	q := config.DB.Model(&model.AssessmentWelfareAlert{}).Where("school_id = ?", schoolID)
	if st := c.Query("status"); st != "" {
		q = q.Where("status = ?", st)
	}
	if sid, ok := queryUUID(c, "session_id"); !ok {
		return
	} else if sid != nil {
		q = q.Where("session_id = ?", *sid)
	}
	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		respondError(c, err)
		return
	}
	rows := []model.AssessmentWelfareAlert{}
	if err := q.Order("CASE status WHEN 'open' THEN 0 WHEN 'acknowledged' THEN 1 ELSE 2 END, created_at DESC").Offset(p.Offset()).Limit(p.Limit).Find(&rows).Error; err != nil {
		respondError(c, err)
		return
	}
	out := make([]gin.H, 0, len(rows))
	for i := range rows {
		out = append(out, alertView(&rows[i]))
	}
	listOK(c, out, p, total)
}

func (sc *SchoolController) UpdateAlert(c *gin.Context) {
	p, schoolID := schoolOf(c)
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	var req struct {
		Status string  `json:"status"`
		Note   *string `json:"note"`
	}
	if !bindJSON(c, &req) {
		return
	}
	if req.Status != model.AlertOpen && req.Status != model.AlertAcknowledged && req.Status != model.AlertResolved {
		respondError(c, apperr.BadRequest("status harus open, acknowledged, atau resolved"))
		return
	}
	var a model.AssessmentWelfareAlert
	if err := config.DB.First(&a, "id = ? AND school_id = ?", id, schoolID).Error; err != nil {
		respondError(c, err)
		return
	}
	now := time.Now()
	uid := middleware.UserID(c)
	upd := map[string]any{"status": req.Status, "handled_by_user_id": uid, "handled_by_name": actorName(c, p), "handled_at": now}
	if req.Note != nil {
		n := strings.TrimSpace(*req.Note)
		if len(n) > 2000 {
			respondError(c, apperr.BadRequest("note maksimal 2000 karakter"))
			return
		}
		upd["note"] = n
	}
	if err := config.DB.Model(&a).Updates(upd).Error; err != nil {
		respondError(c, err)
		return
	}
	config.DB.First(&a, "id = ?", a.ID)
	dataOK(c, http.StatusOK, alertView(&a), "Status tindak lanjut diperbarui")
}

var _ = json.Marshal
