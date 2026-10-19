package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"edupilot-assesment/internal/apperr"
	"edupilot-assesment/internal/client"
	"edupilot-assesment/internal/config"
	"edupilot-assesment/internal/middleware"
	"edupilot-assesment/internal/model"
	"edupilot-assesment/internal/rabbitmq"
	"edupilot-assesment/internal/service"
	"edupilot-assesment/internal/service/scoring"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// StudentController — enrolled student accounts only (contract §13.5).
type StudentController struct{}

func NewStudentController() *StudentController { return &StudentController{} }

const (
	termsVersion = "1.0"
	// SubmitGrace lets an attempt started before closes_at still be saved/submitted.
	submitGrace     = 30 * time.Minute
	bakatTimeLimit  = 20 * time.Minute
	bakatGrace      = 60 * time.Second
	bakatSectionKey = scoring.SubTMBBakat
)

var (
	errSessionNotFound = apperr.NotFound("Sesi asesmen tidak ditemukan")
	errAttemptNotFound = apperr.NotFound("Pengerjaan tidak ditemukan")
	errSessionNotOpen  = apperr.Conflict("SESSION_NOT_OPEN", "Sesi asesmen belum dibuka atau sudah ditutup")
	errAlreadySubmit   = apperr.Conflict("ALREADY_SUBMITTED", "Jawaban sudah dikirim")
)

func studentOf(c *gin.Context) (*client.Principal, uuid.UUID, uuid.UUID) {
	p := middleware.GetPrincipal(c)
	return p, *p.StudentID, *p.SchoolID
}

// targetSession returns a published session of the student's school in which the student is a target.
func targetSession(studentID, schoolID, sessionID uuid.UUID) (*model.AssessmentSession, *model.AssessmentSessionTarget, error) {
	var t model.AssessmentSessionTarget
	if err := config.DB.First(&t, "session_id = ? AND student_id = ?", sessionID, studentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, errSessionNotFound
		}
		return nil, nil, err
	}
	var s model.AssessmentSession
	if err := config.DB.First(&s, "id = ? AND school_id = ? AND status IN ?", sessionID, schoolID, []string{model.SessionPublished, model.SessionClosed}).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, errSessionNotFound
		}
		return nil, nil, err
	}
	return &s, &t, nil
}

func ownAttempt(c *gin.Context) (*model.AssessmentAttempt, bool) {
	_, studentID, _ := studentOf(c)
	id, ok := paramUUID(c, "attemptId")
	if !ok {
		return nil, false
	}
	var a model.AssessmentAttempt
	if err := config.DB.First(&a, "id = ? AND student_id = ?", id, studentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			err = errAttemptNotFound
		}
		respondError(c, err)
		return nil, false
	}
	return &a, true
}

type instrumentStatus struct {
	Code             string  `json:"code"`
	Name             string  `json:"name"`
	EstimatedMinutes int     `json:"estimated_minutes"`
	TotalQuestions   int     `json:"total_questions"`
	Status           string  `json:"status"`
	AttemptID        *string `json:"attempt_id"`
	ResultID         *string `json:"result_id"`
	AttemptsUsed     int     `json:"attempts_used"`
}

func instrumentStatuses(s *model.AssessmentSession, studentID uuid.UUID, counts map[string]int) []instrumentStatus {
	var atts []model.AssessmentAttempt
	config.DB.Select("id, instrument_code, status, created_at").Where("session_id = ? AND student_id = ?", s.ID, studentID).Order("created_at ASC").Find(&atts)
	latest := map[string]*model.AssessmentAttempt{}
	used := map[string]int{}
	ids := []uuid.UUID{}
	for i := range atts {
		latest[atts[i].InstrumentCode] = &atts[i]
		if atts[i].Status != model.AttemptFailed {
			used[atts[i].InstrumentCode]++
		}
		ids = append(ids, atts[i].ID)
	}
	results := map[uuid.UUID]uuid.UUID{}
	if len(ids) > 0 {
		var rs []model.AssessmentResult
		config.DB.Select("id, attempt_id").Where("attempt_id IN ?", ids).Find(&rs)
		for _, r := range rs {
			results[r.AttemptID] = r.ID
		}
	}
	out := []instrumentStatus{}
	for _, code := range s.InstrumentCodes {
		st := instrumentStatus{Code: code, Status: "not_started", TotalQuestions: counts[code], AttemptsUsed: used[code]}
		if info, err := scoring.Info(code); err == nil {
			st.Name, st.EstimatedMinutes = info.Name, info.EstimatedMinute
		}
		if a := latest[code]; a != nil {
			id := a.ID.String()
			st.Status, st.AttemptID = a.Status, &id
			if rid, ok := results[a.ID]; ok {
				r := rid.String()
				st.ResultID = &r
			}
		}
		out = append(out, st)
	}
	return out
}

func (stc *StudentController) GetMySessions(c *gin.Context) {
	_, studentID, schoolID := studentOf(c)
	sessions := []model.AssessmentSession{}
	if err := config.DB.Joins("JOIN assessment_session_targets t ON t.session_id = assessment_sessions.id").
		Where("t.student_id = ? AND assessment_sessions.school_id = ? AND assessment_sessions.status = ?", studentID, schoolID, model.SessionPublished).
		Order("assessment_sessions.opens_at DESC").Find(&sessions).Error; err != nil {
		respondError(c, err)
		return
	}
	counts := service.CountActive(config.DB)
	out := make([]gin.H, 0, len(sessions))
	for i := range sessions {
		s := &sessions[i]
		out = append(out, gin.H{
			"id": s.ID, "title": s.Title, "description": s.Description, "opens_at": s.OpensAt, "closes_at": s.ClosesAt,
			"state": sessionState(s, time.Now()), "max_attempts": s.MaxAttempts, "instruments": instrumentStatuses(s, studentID, counts),
		})
	}
	dataOK(c, http.StatusOK, out, "")
}

func termsAndConditions() gin.H {
	return gin.H{
		"title":   "Syarat, Ketentuan & Perlindungan Privasi Asesmen",
		"version": termsVersion,
		"points": []string{
			"Asesmen ini membantu mengenali minat, potensi, preferensi karier, dan gaya belajarmu. Ini bukan ujian kelulusan dan bukan penilaian kecerdasan.",
			"Tidak ada jawaban benar atau salah pada bagian minat, preferensi karier, dan gaya belajar. Jawablah secara jujur sesuai dirimu.",
			"Khusus subtes bakat pada Tes Minat dan Bakat (TMB) terdapat batas waktu 20 menit.",
			"Jawabanmu tersimpan otomatis dan dapat dilanjutkan bila koneksi terputus.",
			"Data asesmen dilindungi (UU PDP No. 27/2022) dan hanya dapat diakses olehmu, Guru BK, dan pengelola sekolah yang berwenang; setiap akses staf tercatat.",
			"Hasil adalah bahan skrining dan eksplorasi untuk didiskusikan bersama Guru BK dan orang tua.",
		},
		"disclaimer": scoring.Disclaimer,
	}
}

func interactiveExamples(code string) []gin.H {
	likert := gin.H{
		"type": "scale_preference", "title": "Contoh Soal Skala",
		"instruction": "Pilih nilai yang paling menggambarkan dirimu.", "sample_question": "Merakit atau memperbaiki perangkat elektronik sederhana di rumah",
		"options": []gin.H{{"key": "1", "text": "Sangat tidak suka"}, {"key": "2", "text": "Tidak suka"}, {"key": "3", "text": "Netral"}, {"key": "4", "text": "Suka"}, {"key": "5", "text": "Sangat suka"}},
		"hint":    "Tidak ada jawaban benar atau salah. Pilih sesuai seberapa kamu menyukainya.",
	}
	switch code {
	case scoring.InstrumentTMB:
		likert["options"] = []gin.H{{"key": "1", "text": "Tidak tertarik"}, {"key": "2", "text": "Kurang tertarik"}, {"key": "3", "text": "Tertarik"}, {"key": "4", "text": "Sangat tertarik"}}
		return []gin.H{likert, {
			"type": "aptitude_mini", "title": "Contoh Soal Subtes Bakat (Pilihan Ganda A–E)",
			"instruction":     "Pilih satu jawaban yang paling tepat. Subtes ini dibatasi waktu 20 menit.",
			"sample_question": "Kaki : Sepatu = Tangan : …",
			"options":         []gin.H{{"key": "A", "text": "Sarung tangan"}, {"key": "B", "text": "Jari"}, {"key": "C", "text": "Topi"}, {"key": "D", "text": "Kaus kaki"}, {"key": "E", "text": "Cincin"}},
			"correct_option":  "A", "hint": "Sepatu dipakai di kaki, sarung tangan dipakai di tangan.",
		}}
	case scoring.InstrumentPMK:
		return []gin.H{{
			"type": "boolean", "title": "Contoh Soal Suka/Tidak", "instruction": "Tandai SUKA bila kamu menyukai kegiatan ini, TIDAK bila tidak.",
			"sample_question": "Menanam dan merawat tanaman", "options": []gin.H{{"key": "true", "text": "Suka"}, {"key": "false", "text": "Tidak"}},
			"hint": "Jawab spontan sesuai dirimu.",
		}, {
			"type": "self_estimate", "title": "Contoh Estimasi Diri (1–7)", "instruction": "Bandingkan dirimu dengan teman seusiamu.",
			"sample_question": "Kemampuan bekerja sama dalam kelompok",
			"options":         []gin.H{{"key": "1", "text": "Paling rendah"}, {"key": "4", "text": "Rata-rata"}, {"key": "7", "text": "Paling tinggi"}},
			"hint":            "Angka 4 berarti sama dengan rata-rata teman seusiamu.",
		}}
	case scoring.InstrumentADN:
		likert["options"] = []gin.H{{"key": "1", "text": "Tidak pernah"}, {"key": "2", "text": "Jarang"}, {"key": "3", "text": "Sering"}, {"key": "4", "text": "Selalu"}}
		likert["sample_question"] = "Saya lebih mudah paham setelah melihat contoh gambar"
		likert["hint"] = "Pilih seberapa sering pernyataan ini sesuai dengan dirimu."
	}
	return []gin.H{likert}
}

func (stc *StudentController) GetSessionOnboarding(c *gin.Context) {
	_, studentID, schoolID := studentOf(c)
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	s, _, err := targetSession(studentID, schoolID, id)
	if err != nil {
		respondError(c, err)
		return
	}
	code := strings.ToUpper(strings.TrimSpace(c.Query("instrument")))
	if code == "" && len(s.InstrumentCodes) > 0 {
		code = s.InstrumentCodes[0]
	}
	if !contains(s.InstrumentCodes, code) {
		respondError(c, apperr.Invalid("INVALID_INSTRUMENT", "Instrumen tidak termasuk dalam sesi ini"))
		return
	}
	info, err := scoring.Info(code)
	if err != nil {
		respondError(c, err)
		return
	}
	var consent model.AssessmentConsent
	consentView := gin.H{"required": true, "accepted_at": nil}
	if config.DB.Where("session_id = ? AND student_id = ? AND instrument_code = ?", s.ID, studentID, code).First(&consent).Error == nil {
		consentView["accepted_at"] = consent.AcceptedAt
	}
	counts := service.CountActive(config.DB)
	dataOK(c, http.StatusOK, gin.H{
		"session": gin.H{"id": s.ID, "title": s.Title, "description": s.Description, "opens_at": s.OpensAt, "closes_at": s.ClosesAt, "state": sessionState(s, time.Now())},
		"instrument": gin.H{"code": code, "name": info.Name, "description": info.Description, "estimated_minutes": info.EstimatedMinute,
			"total_questions": counts[code], "sections": info.Sections, "version": info.Version},
		"terms_and_conditions": termsAndConditions(),
		"consent":              consentView,
		"interactive_examples": interactiveExamples(code),
	}, "")
}

func (stc *StudentController) StartAttempt(c *gin.Context) {
	p, studentID, schoolID := studentOf(c)
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	var req struct {
		InstrumentCode  string `json:"instrument_code"`
		ConsentAccepted bool   `json:"consent_accepted"`
	}
	if !bindJSON(c, &req) {
		return
	}
	code := strings.ToUpper(strings.TrimSpace(req.InstrumentCode))
	s, target, err := targetSession(studentID, schoolID, id)
	if err != nil {
		respondError(c, err)
		return
	}
	now := time.Now()
	if sessionState(s, now) != "open" {
		respondError(c, errSessionNotOpen)
		return
	}
	if !contains(s.InstrumentCodes, code) {
		respondError(c, apperr.Invalid("INVALID_INSTRUMENT", "Instrumen tidak termasuk dalam sesi ini"))
		return
	}
	var consents int64
	config.DB.Model(&model.AssessmentConsent{}).Where("session_id = ? AND student_id = ? AND instrument_code = ?", s.ID, studentID, code).Count(&consents)
	if !req.ConsentAccepted && consents == 0 {
		respondError(c, apperr.Invalid("CONSENT_REQUIRED", "Persetujuan syarat & ketentuan wajib diberikan sebelum memulai"))
		return
	}
	// Resume an in-progress attempt (no duplicates).
	if a, err := inProgress(s.ID, studentID, code); err == nil {
		writeState(c, http.StatusOK, a)
		return
	}
	var used int64
	config.DB.Model(&model.AssessmentAttempt{}).Where("session_id = ? AND student_id = ? AND instrument_code = ? AND status <> ?", s.ID, studentID, code, model.AttemptFailed).Count(&used)
	if int(used) >= s.MaxAttempts {
		respondError(c, apperr.Conflict("MAX_ATTEMPTS", "Batas pengerjaan untuk instrumen ini sudah tercapai"))
		return
	}
	rows, err := service.ActiveBank(config.DB, code)
	if err != nil {
		respondError(c, err)
		return
	}
	if len(rows) == 0 {
		respondError(c, apperr.Conflict("INSTRUMENT_UNAVAILABLE", "Butir soal instrumen ini belum tersedia"))
		return
	}
	seed := rand.New(rand.NewSource(now.UnixNano())).Int63n(1<<31-1) + 1
	attempt := model.AssessmentAttempt{
		ID: uuid.New(), SessionID: s.ID, StudentID: studentID, UserID: middleware.UserID(c), SchoolID: schoolID, ClassID: p.ClassID,
		InstrumentCode: code, Status: model.AttemptInProgress, Seed: seed, QuestionOrder: service.QuestionOrder(code, rows, seed),
		SectionStarts: model.JSONMap{}, StartedAt: now, ValidityFlags: model.StringArray{},
	}
	err = config.DB.Transaction(func(tx *gorm.DB) error {
		if req.ConsentAccepted {
			cons := model.AssessmentConsent{ID: uuid.New(), SessionID: s.ID, StudentID: studentID, UserID: middleware.UserID(c),
				InstrumentCode: code, TermsVersion: termsVersion, IP: c.ClientIP(), AcceptedAt: now}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&cons).Error; err != nil {
				return err
			}
		}
		// Quota: consumed once per student per session (atomic conditional UPDATEs).
		res := tx.Model(&model.AssessmentSessionTarget{}).Where("id = ? AND quota_consumed_at IS NULL", target.ID).Update("quota_consumed_at", now)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 1 {
			q := tx.Model(&model.SchoolAssessmentSubscription{}).
				Where("id = ? AND status = ? AND used_quota < total_quota AND expires_at > ?", s.SubscriptionID, model.SubStatusActive, now).
				Update("used_quota", gorm.Expr("used_quota + 1"))
			if q.Error != nil {
				return q.Error
			}
			if q.RowsAffected == 0 {
				return apperr.Conflict("QUOTA_EXHAUSTED", "Kuota paket asesmen sekolah sudah habis. Hubungi Guru BK.")
			}
		}
		if err := tx.Create(&attempt).Error; err != nil {
			return err
		}
		return tx.Model(&model.AssessmentSessionTarget{}).Where("id = ? AND status = ?", target.ID, model.TargetAssigned).Update("status", model.TargetInProgress).Error
	})
	if err != nil {
		if isUniqueViolation(err) { // concurrent start: return the attempt that won
			if a, e := inProgress(s.ID, studentID, code); e == nil {
				writeState(c, http.StatusOK, a)
				return
			}
		}
		respondError(c, err)
		return
	}
	writeState(c, http.StatusCreated, &attempt)
}

func inProgress(sessionID, studentID uuid.UUID, code string) (*model.AssessmentAttempt, error) {
	var a model.AssessmentAttempt
	err := config.DB.Where("session_id = ? AND student_id = ? AND instrument_code = ? AND status = ?", sessionID, studentID, code, model.AttemptInProgress).First(&a).Error
	return &a, err
}

func writeState(c *gin.Context, status int, a *model.AssessmentAttempt) {
	st, err := service.BuildAttemptState(config.DB, a)
	if err != nil {
		respondError(c, err)
		return
	}
	dataOK(c, status, st, "")
}

func (stc *StudentController) GetAttempt(c *gin.Context) {
	a, ok := ownAttempt(c)
	if !ok {
		return
	}
	writeState(c, http.StatusOK, a)
}

type saveReq struct {
	Answers        map[string]json.RawMessage `json:"answers"`
	RtMs           map[string]int             `json:"rt_ms"`
	SectionStarted string                     `json:"section_started"`
	DurationSec    int                        `json:"duration_sec"`
}

// writable checks the attempt can still receive answers.
func writable(a *model.AssessmentAttempt) error {
	if a.Status != model.AttemptInProgress {
		return errAlreadySubmit
	}
	var s model.AssessmentSession
	if err := config.DB.Select("id, status, closes_at").First(&s, "id = ?", a.SessionID).Error; err != nil {
		return err
	}
	if s.Status == model.SessionClosed || time.Now().After(s.ClosesAt.Add(submitGrace)) {
		return errSessionNotOpen
	}
	return nil
}

// saveAnswers validates and upserts answers (idempotent). Aptitude answers after
// the 20-minute limit (+60 s grace) are ignored and reported.
func saveAnswers(a *model.AssessmentAttempt, req saveReq) (int, []string, error) {
	rows, err := service.RowsByCodes(config.DB, a.QuestionOrder)
	if err != nil {
		return 0, nil, err
	}
	now := time.Now()
	starts := model.JSONMap{}
	for k, v := range a.SectionStarts {
		starts[k] = v
	}
	changed := false
	markStart := func(key string) {
		if _, ok := starts[key]; !ok {
			starts[key] = now.UTC().Format(time.RFC3339Nano)
			changed = true
		}
	}
	if sec := strings.TrimSpace(req.SectionStarted); sec != "" {
		if !contains(scoring.SubtestsOf(a.InstrumentCode), sec) {
			return 0, nil, apperr.BadRequest("section_started tidak valid")
		}
		markStart(sec)
	}
	upserts := []model.AssessmentResponse{}
	ignored := []string{}
	for itemID, raw := range req.Answers {
		q, ok := rows[itemID]
		if !ok {
			return 0, nil, apperr.Invalid("INVALID_ANSWER", "Butir "+itemID+" tidak termasuk dalam pengerjaan ini")
		}
		val, err := scoring.NormalizeAnswer(service.ToItem(q), raw)
		if err != nil {
			return 0, nil, apperr.Invalid("INVALID_ANSWER", strings.TrimPrefix(err.Error(), scoring.ErrInvalidAnswer.Error()+": "))
		}
		if q.Subtest == bakatSectionKey {
			markStart(bakatSectionKey)
			started, _ := time.Parse(time.RFC3339Nano, starts[bakatSectionKey].(string))
			if now.After(started.Add(bakatTimeLimit + bakatGrace)) {
				ignored = append(ignored, itemID)
				continue
			}
		}
		rt := req.RtMs[itemID]
		if rt < 0 || rt > 3_600_000 {
			rt = 0
		}
		upserts = append(upserts, model.AssessmentResponse{ID: uuid.New(), AttemptID: a.ID, ItemID: itemID, Value: val, ReactionTimeMs: rt, CreatedAt: now, UpdatedAt: now})
	}
	err = config.DB.Transaction(func(tx *gorm.DB) error {
		if len(upserts) > 0 {
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "attempt_id"}, {Name: "item_id"}},
				DoUpdates: clause.AssignmentColumns([]string{"value", "reaction_time_ms", "updated_at"}),
			}).Create(&upserts).Error; err != nil {
				return err
			}
		}
		if changed {
			if err := tx.Model(&model.AssessmentAttempt{}).Where("id = ?", a.ID).Update("section_starts", starts).Error; err != nil {
				return err
			}
			a.SectionStarts = starts
		}
		return nil
	})
	return len(upserts), ignored, err
}

func (stc *StudentController) SaveAnswers(c *gin.Context) {
	a, ok := ownAttempt(c)
	if !ok {
		return
	}
	var req saveReq
	if !bindJSON(c, &req) {
		return
	}
	if err := writable(a); err != nil {
		respondError(c, err)
		return
	}
	n, ignored, err := saveAnswers(a, req)
	if err != nil {
		respondError(c, err)
		return
	}
	mirrorOK(c, http.StatusOK, map[string]any{"saved": n, "ignored": ignored, "server_time": time.Now()})
}

func (stc *StudentController) SubmitAttempt(c *gin.Context) {
	a, ok := ownAttempt(c)
	if !ok {
		return
	}
	var req saveReq
	if c.Request.ContentLength != 0 && !bindJSON(c, &req) {
		return
	}
	if err := writable(a); err != nil {
		respondError(c, err)
		return
	}
	if len(req.Answers) > 0 || req.SectionStarted != "" {
		if _, _, err := saveAnswers(a, req); err != nil {
			respondError(c, err)
			return
		}
	}
	rows, err := service.RowsByCodes(config.DB, a.QuestionOrder)
	if err != nil {
		respondError(c, err)
		return
	}
	items := []scoring.Item{}
	for _, id := range a.QuestionOrder {
		if q, ok := rows[id]; ok {
			items = append(items, service.ToItem(q))
		}
	}
	var resp []model.AssessmentResponse
	config.DB.Select("item_id, value").Where("attempt_id = ?", a.ID).Find(&resp)
	answers := map[string]string{}
	for _, r := range resp {
		answers[r.ItemID] = r.Value
	}
	cfg := scoring.MustConfig()
	missing := scoring.MissingItems(items, answers)
	if total := scoring.ScoredCount(items); total > 0 && float64(len(missing))/float64(total) > cfg.Validity.MaxMissingRatio {
		respondError(c, apperr.Unprocessable("INCOMPLETE", fmt.Sprintf("Masih ada %d butir yang belum dijawab", len(missing))).With("missing", missing))
		return
	}
	now := time.Now()
	elapsed := int(now.Sub(a.StartedAt).Seconds())
	duration := elapsed
	if req.DurationSec > 0 && req.DurationSec <= elapsed+60 {
		duration = req.DurationSec
	}
	res := config.DB.Model(&model.AssessmentAttempt{}).Where("id = ? AND status = ?", a.ID, model.AttemptInProgress).
		Updates(map[string]any{"status": model.AttemptQueued, "submitted_at": now, "duration_sec": duration})
	if res.Error != nil {
		respondError(c, res.Error)
		return
	}
	if res.RowsAffected == 0 {
		respondError(c, errAlreadySubmit)
		return
	}
	rabbitmq.Enqueue(a.ID)
	mirrorOK(c, http.StatusAccepted, map[string]any{"attempt_id": a.ID, "status": model.AttemptQueued,
		"message": "Jawaban diterima dan sedang diproses."})
}

func (stc *StudentController) GetAttemptStatus(c *gin.Context) {
	a, ok := ownAttempt(c)
	if !ok {
		return
	}
	out := map[string]any{"attempt_id": a.ID, "status": a.Status, "result_id": nil}
	var r model.AssessmentResult
	if config.DB.Select("id").First(&r, "attempt_id = ?", a.ID).Error == nil {
		out["result_id"] = r.ID
	}
	mirrorOK(c, http.StatusOK, out)
}

func (stc *StudentController) GetMyResult(c *gin.Context) {
	a, ok := ownAttempt(c)
	if !ok {
		return
	}
	var r model.AssessmentResult
	if err := config.DB.First(&r, "attempt_id = ? AND student_id = ?", a.ID, a.StudentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respondError(c, apperr.New(http.StatusNotFound, "RESULT_NOT_READY", "Hasil asesmen belum selesai diproses"))
			return
		}
		respondError(c, err)
		return
	}
	dataOK(c, http.StatusOK, studentResultView(&r), "")
}

func (stc *StudentController) GetMyResults(c *gin.Context) {
	_, studentID, schoolID := studentOf(c)
	p := parsePage(c)
	q := config.DB.Table("assessment_results r").Joins("JOIN assessment_sessions s ON s.id = r.session_id").
		Where("r.student_id = ? AND r.school_id = ?", studentID, schoolID)
	if code := strings.ToUpper(c.Query("instrument_code")); code != "" {
		q = q.Where("r.instrument_code = ?", code)
	}
	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		respondError(c, err)
		return
	}
	var rows []struct {
		ID, AttemptID, SessionID                                    uuid.UUID
		SessionTitle, InstrumentCode, HollandCode, RecommendedTrack string
		VarkCategory, ReportSummary                                 string
		CreatedAt                                                   time.Time
	}
	if err := q.Select("r.id, r.attempt_id, r.session_id, s.title AS session_title, r.instrument_code, COALESCE(r.holland_code,'') AS holland_code, COALESCE(r.recommended_track,'') AS recommended_track, COALESCE(r.vark_category,'') AS vark_category, COALESCE(r.report_summary,'') AS report_summary, r.created_at").
		Order("r.created_at DESC").Offset(p.Offset()).Limit(p.Limit).Scan(&rows).Error; err != nil {
		respondError(c, err)
		return
	}
	out := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		out = append(out, gin.H{"id": r.ID, "attempt_id": r.AttemptID, "session_id": r.SessionID, "session_title": r.SessionTitle,
			"instrument_code": r.InstrumentCode, "holland_code": r.HollandCode, "recommended_track": r.RecommendedTrack,
			"vark_category": r.VarkCategory, "report_summary": r.ReportSummary, "created_at": r.CreatedAt})
	}
	listOK(c, out, p, total)
}
