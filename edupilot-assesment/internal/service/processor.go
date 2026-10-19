package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"edupilot-assesment/internal/client"
	"edupilot-assesment/internal/config"
	"edupilot-assesment/internal/model"
	"edupilot-assesment/internal/service/scoring"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MaxProcessRetries: after this many failed scoring runs the attempt becomes `failed`.
const MaxProcessRetries = 3

// StaleProcessing: a processing/queued attempt untouched for this long is re-enqueued.
const StaleProcessing = 2 * time.Minute

// ErrSkip means the attempt is not (or no longer) eligible for processing.
var ErrSkip = errors.New("attempt not eligible for processing")

func toJSONMap(v any) model.JSONMap {
	b, err := json.Marshal(v)
	if err != nil {
		return model.JSONMap{}
	}
	m := model.JSONMap{}
	_ = json.Unmarshal(b, &m)
	return m
}

// medianDuration of completed attempts of an instrument (0 when the sample is too small).
func medianDuration(db *gorm.DB, code string, cfg *scoring.Config) float64 {
	var row struct {
		N int
		M *float64
	}
	db.Raw(`SELECT COUNT(*) AS n, percentile_cont(0.5) WITHIN GROUP (ORDER BY duration_sec) AS m
		FROM assessment_attempts WHERE instrument_code = ? AND status = 'completed' AND duration_sec > 0`, code).Scan(&row)
	if row.N < cfg.Validity.MinMedianSample || row.M == nil {
		return 0
	}
	return *row.M
}

// ProcessAttempt scores a submitted attempt. Idempotent: a completed attempt or
// an existing result is never scored twice, responses are never re-inserted.
func ProcessAttempt(ctx context.Context, attemptID uuid.UUID) error {
	db := config.DB.WithContext(ctx)
	cfg, err := scoring.LoadConfig()
	if err != nil {
		return err
	}
	// Claim: queued → processing (or a stale processing run).
	res := db.Model(&model.AssessmentAttempt{}).
		Where("id = ? AND (status = ? OR (status = ? AND updated_at < ?))", attemptID, model.AttemptQueued, model.AttemptProcessing, time.Now().Add(-StaleProcessing)).
		Updates(map[string]any{"status": model.AttemptProcessing, "updated_at": time.Now()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrSkip
	}
	var a model.AssessmentAttempt
	if err := db.First(&a, "id = ?", attemptID).Error; err != nil {
		return err
	}
	var existing int64
	db.Model(&model.AssessmentResult{}).Where("attempt_id = ?", a.ID).Count(&existing)
	if existing > 0 {
		return db.Model(&model.AssessmentAttempt{}).Where("id = ?", a.ID).Update("status", model.AttemptCompleted).Error
	}

	rows, err := RowsByCodes(db, a.QuestionOrder)
	if err != nil {
		return err
	}
	items := make([]scoring.Item, 0, len(a.QuestionOrder))
	for _, id := range a.QuestionOrder {
		if q, ok := rows[id]; ok {
			items = append(items, ToItem(q))
		}
	}
	var resp []model.AssessmentResponse
	if err := db.Where("attempt_id = ?", a.ID).Find(&resp).Error; err != nil {
		return err
	}
	answers := map[string]string{}
	for _, r := range resp {
		answers[r.ItemID] = r.Value
	}
	out, err := scoring.Evaluate(a.InstrumentCode, scoring.Input{
		Items: items, Answers: answers, DurationSec: a.DurationSec, MedianDurationSec: medianDuration(db, a.InstrumentCode, cfg),
	}, cfg)
	if err != nil {
		return err
	}
	version := ""
	if info, err := scoring.Info(a.InstrumentCode); err == nil {
		version = info.Version
	}

	var target model.AssessmentSessionTarget
	db.Where("session_id = ? AND student_id = ?", a.SessionID, a.StudentID).First(&target)

	result := model.AssessmentResult{
		ID: uuid.New(), AttemptID: a.ID, SessionID: a.SessionID, StudentID: a.StudentID, SchoolID: a.SchoolID,
		ClassID: a.ClassID, StudentName: target.FullName, NIS: target.NIS, ClassName: target.ClassName,
		InstrumentCode: a.InstrumentCode, HollandCode: out.HollandCode, VarkCategory: out.VarkCategory,
		RecommendedTrack: out.RecommendedTrack,
		ScoresJSON:       toJSONMap(out.Scores), IndicesJSON: toJSONMap(out.Indices),
		RecommendationsJSON: toJSONMap(out.Recommendations), Interpretation: toJSONMap(out.Interpretation),
		FlagsJSON: model.StringArray(out.Flags), ValidityFlags: model.StringArray(out.ValidityFlags),
		StaffNotes: model.StringArray(out.StaffNotes), ReportSummary: out.ReportSummary,
		InstrumentVersion: version, ConfigVersion: out.ConfigVersion,
	}
	var alert *model.AssessmentWelfareAlert
	err = db.Transaction(func(tx *gorm.DB) error {
		ins := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "attempt_id"}}, DoNothing: true}).Create(&result)
		if ins.Error != nil {
			return ins.Error
		}
		if ins.RowsAffected == 0 {
			return nil // another run stored it first
		}
		if err := tx.Model(&model.AssessmentAttempt{}).Where("id = ?", a.ID).Updates(map[string]any{
			"status": model.AttemptCompleted, "validity_flags": model.StringArray(out.ValidityFlags), "failure_reason": "",
		}).Error; err != nil {
			return err
		}
		if out.WelfareFlag {
			al := model.AssessmentWelfareAlert{
				ID: uuid.New(), SchoolID: a.SchoolID, StudentID: a.StudentID, StudentName: target.FullName,
				ClassID: a.ClassID, ClassName: target.ClassName, ResultID: result.ID, SessionID: a.SessionID,
				Flag: scoring.FlagWelfare, Status: model.AlertOpen,
			}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&al).Error; err != nil {
				return err
			}
			alert = &al
		}
		return refreshTargetStatus(tx, a.SessionID, a.StudentID)
	})
	if err != nil {
		return err
	}
	// Mark completed even if the result already existed.
	db.Model(&model.AssessmentAttempt{}).Where("id = ? AND status = ?", a.ID, model.AttemptProcessing).Update("status", model.AttemptCompleted)
	if alert != nil {
		NotifyWelfareAlert(ctx, alert)
	}
	return nil
}

// refreshTargetStatus: completed once every instrument of the session has a completed attempt.
func refreshTargetStatus(tx *gorm.DB, sessionID, studentID uuid.UUID) error {
	var s model.AssessmentSession
	if err := tx.First(&s, "id = ?", sessionID).Error; err != nil {
		return nil
	}
	var done int64
	tx.Model(&model.AssessmentAttempt{}).Where("session_id = ? AND student_id = ? AND status = ?", sessionID, studentID, model.AttemptCompleted).
		Distinct("instrument_code").Count(&done)
	status := model.TargetInProgress
	if int(done) >= len(s.InstrumentCodes) {
		status = model.TargetCompleted
	}
	return tx.Model(&model.AssessmentSessionTarget{}).Where("session_id = ? AND student_id = ?", sessionID, studentID).Update("status", status).Error
}

// NotifyWelfareAlert notifies every Guru BK of the school (title/body never
// contain answers or names). Best effort; the sweeper retries unsent ones.
func NotifyWelfareAlert(ctx context.Context, al *model.AssessmentWelfareAlert) {
	if client.Default == nil {
		return
	}
	err := client.Default.Notify(context.WithoutCancel(ctx), client.Notification{
		SchoolID: al.SchoolID, ToCounselors: true, Type: "assessment_welfare_alert",
		Title: "Hasil asesmen perlu tindak lanjut",
		Body:  "Ada hasil asesmen siswa yang memerlukan tindak lanjut Guru BK (maks. 1×24 jam kerja). Buka menu Asesmen › Peringatan.",
		Data:  map[string]any{"alert_id": al.ID.String(), "session_id": al.SessionID.String()},
	})
	if err != nil {
		config.Logger.Warn("welfare alert notification failed", zap.Error(err), zap.String("alert_id", al.ID.String()))
		return
	}
	now := time.Now()
	config.DB.Model(&model.AssessmentWelfareAlert{}).Where("id = ?", al.ID).Update("notified_at", now)
}

// RecordFailure counts a failed scoring run; after MaxProcessRetries the attempt
// is marked failed (no endless requeue). Returns true when it should be retried.
func RecordFailure(attemptID uuid.UUID, cause error) bool {
	db := config.DB
	var a model.AssessmentAttempt
	if err := db.Select("id, retry_count, status").First(&a, "id = ?", attemptID).Error; err != nil {
		return false
	}
	n := a.RetryCount + 1
	upd := map[string]any{"retry_count": n, "failure_reason": truncate(cause.Error(), 500)}
	retry := n < MaxProcessRetries
	if retry {
		upd["status"] = model.AttemptQueued
	} else {
		upd["status"] = model.AttemptFailed
	}
	db.Model(&model.AssessmentAttempt{}).Where("id = ? AND status IN ?", attemptID, []string{model.AttemptQueued, model.AttemptProcessing}).Updates(upd)
	config.Logger.Error("attempt scoring failed", zap.Error(cause), zap.String("attempt_id", attemptID.String()), zap.Int("retry", n), zap.Bool("will_retry", retry))
	return retry
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// StaleAttempts lists queued/processing attempts that nobody is working on.
func StaleAttempts(limit int) []uuid.UUID {
	var ids []uuid.UUID
	config.DB.Model(&model.AssessmentAttempt{}).
		Where("status IN ? AND updated_at < ?", []string{model.AttemptQueued, model.AttemptProcessing}, time.Now().Add(-StaleProcessing)).
		Order("updated_at").Limit(limit).Pluck("id", &ids)
	return ids
}

// RetryAlertNotifications resends notifications that failed earlier.
func RetryAlertNotifications(ctx context.Context) {
	var alerts []model.AssessmentWelfareAlert
	config.DB.Where("notified_at IS NULL AND created_at < ?", time.Now().Add(-time.Minute)).Limit(50).Find(&alerts)
	for i := range alerts {
		NotifyWelfareAlert(ctx, &alerts[i])
	}
}
