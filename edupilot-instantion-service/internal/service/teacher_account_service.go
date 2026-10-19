package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/edupilot-instantion-service/internal/apperr"
	"github.com/edupilot-instantion-service/internal/client"
	"github.com/edupilot-instantion-service/internal/config"
	"github.com/edupilot-instantion-service/internal/model"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// TeacherAccounts manages teacher auth accounts (implemented by client.AuthClient).
type TeacherAccounts interface {
	InviteUser(ctx context.Context, req client.InviteUserRequest) (*client.InvitedUser, error)
	ResendInvite(ctx context.Context, id uint, schoolName string) (*client.InvitedUser, error)
	DeleteUser(ctx context.Context, id uint) error
}

// NewTeacherAccounts is a variable so tests can swap the auth client.
var NewTeacherAccounts = func() TeacherAccounts { return client.NewAuthClient() }

const (
	teacherInviteCooldown = 60 * time.Second
	authCallTimeout       = 10 * time.Second
	// InviteEmailWarning is returned as top-level `warning` when the account was
	// created/re-invited but the email could not be sent.
	InviteEmailWarning = "Akun guru berhasil dibuat, tetapi email undangan gagal dikirim. Silakan kirim ulang undangan."
)

var (
	errEmailInUse        = apperr.Conflict("EMAIL_IN_USE", "Email sudah digunakan oleh akun lain")
	errEmailLocked       = apperr.Conflict("EMAIL_LOCKED", "Email guru yang sudah memiliki akun tidak dapat diubah")
	errAlreadyActive     = apperr.Conflict("ALREADY_ACTIVE", "Akun guru sudah aktif")
	errTeacherNoEmail    = apperr.New(http.StatusBadRequest, "EMAIL_REQUIRED", "Guru belum memiliki email")
	errInviteRateLimited = apperr.New(http.StatusTooManyRequests, "RATE_LIMITED", "Undangan baru saja dikirim, coba lagi dalam 60 detik")
	errInviteConflict    = apperr.Conflict("INVITE_CONFLICT", "Status akun guru berubah, silakan muat ulang data")
)

// authCallContext detaches from client cancellation so a created account id is
// always learned (needed for compensation); bounded by the auth client timeout.
func authCallContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), authCallTimeout)
}

// emailChangeLocked reports whether an update would change the email of a teacher
// that already has an auth account (invited/active): the account email is fixed.
func emailChangeLocked(e *model.Employees, newEmail *string) bool {
	if model.ComputeAccountStatus(e.UserID, e.ActivatedAt) == model.AccountStatusNone {
		return false
	}
	oldVal, newVal := "", ""
	if e.Email != nil {
		oldVal = strings.TrimSpace(*e.Email)
	}
	if newEmail != nil {
		newVal = strings.TrimSpace(*newEmail)
	}
	return !strings.EqualFold(oldVal, newVal)
}

// mapInviteError: 409 (email used by another account) → EMAIL_IN_USE; 400 → validation;
// anything else → 502 (logged).
func mapInviteError(err error) error {
	var ae *client.AuthError
	if errors.As(err, &ae) {
		switch ae.Status {
		case http.StatusConflict:
			return errEmailInUse
		case http.StatusBadRequest, http.StatusUnprocessableEntity:
			msg := ae.Message
			if msg == "" {
				msg = "Data akun guru tidak valid"
			}
			return apperr.BadRequest(msg)
		}
	}
	config.Logger.Error("auth service teacher invite failed", zap.Error(err))
	return apperr.New(http.StatusBadGateway, "AUTH_SERVICE_ERROR", "Gagal membuat akun guru di layanan autentikasi")
}

func inviteRequest(t *model.Employees, schoolName string) client.InviteUserRequest {
	email := ""
	if t.Email != nil {
		email = *t.Email
	}
	return client.InviteUserRequest{
		Email:      email,
		FullName:   t.FullName,
		Phone:      t.Phone,
		Role:       "teacher",
		SchoolName: schoolName,
	}
}

func inviteWarning(u *client.InvitedUser) string {
	if u != nil && !u.EmailSent {
		return InviteEmailWarning
	}
	return ""
}

func teacherInviteKey(id uuid.UUID) string {
	return fmt.Sprintf("instantion:teacher-invite:%s", id)
}

// acquireInviteSlot enforces 1 invite/resend per teacher per 60s. Fails open on Redis errors.
func acquireInviteSlot(ctx context.Context, id uuid.UUID) (bool, func()) {
	noop := func() {}
	if config.RDB == nil {
		return true, noop
	}
	key := teacherInviteKey(id)
	ok, err := config.RDB.SetNX(ctx, key, 1, teacherInviteCooldown).Result()
	if err != nil {
		config.Logger.Warn("teacher invite rate limit check failed, allowing", zap.Error(err))
		return true, noop
	}
	if !ok {
		return false, noop
	}
	return true, func() { config.RDB.Del(context.WithoutCancel(ctx), key) }
}

// InviteTeacher creates (status none) or re-sends (status invited) the teacher's
// account invitation. Returns the updated teacher and an optional email warning.
func InviteTeacher(ctx context.Context, schoolID uuid.UUID, schoolName string, id uuid.UUID) (*model.Employees, string, error) {
	db := config.DB.WithContext(ctx)
	t, err := findTeacher(db, schoolID, id)
	if err != nil {
		return nil, "", err
	}
	if t.Email == nil || strings.TrimSpace(*t.Email) == "" {
		return nil, "", errTeacherNoEmail
	}
	if t.AccountStatus == model.AccountStatusActive {
		return nil, "", errAlreadyActive
	}

	ok, release := acquireInviteSlot(ctx, id)
	if !ok {
		return nil, "", errInviteRateLimited
	}

	accounts := NewTeacherAccounts()
	warning, err := sendTeacherInvite(ctx, accounts, schoolID, schoolName, t)
	if err != nil {
		release() // allow an immediate retry after a failure
		return nil, "", err
	}
	t, err = findTeacher(db, schoolID, id)
	if err != nil {
		return nil, "", err
	}
	return t, warning, nil
}

func sendTeacherInvite(ctx context.Context, accounts TeacherAccounts, schoolID uuid.UUID, schoolName string, t *model.Employees) (string, error) {
	db := config.DB.WithContext(ctx)
	callCtx, cancel := authCallContext(ctx)
	defer cancel()

	if t.UserID != nil {
		u, err := accounts.ResendInvite(callCtx, *t.UserID, schoolName)
		if err == nil {
			if err := db.Model(&model.Employees{}).
				Where("id = ? AND school_id = ?", t.ID, schoolID).
				Updates(map[string]interface{}{"invited_at": time.Now(), "updated_at": time.Now()}).Error; err != nil {
				return "", err
			}
			return inviteWarning(u), nil
		}
		var ae *client.AuthError
		if !errors.As(err, &ae) {
			config.Logger.Error("auth service resend invite failed", zap.Error(err))
			return "", apperr.New(http.StatusBadGateway, "AUTH_SERVICE_ERROR", "Gagal mengirim ulang undangan")
		}
		switch ae.Status {
		case http.StatusConflict:
			return "", errAlreadyActive
		case http.StatusNotFound:
			// Stale link (auth user removed elsewhere): unlink and invite afresh.
			config.Logger.Warn("teacher auth account missing, re-inviting",
				zap.String("employee_id", t.ID.String()), zap.Uint("user_id", *t.UserID))
			res := db.Model(&model.Employees{}).
				Where("id = ? AND school_id = ? AND user_id = ?", t.ID, schoolID, *t.UserID).
				Updates(map[string]interface{}{"user_id": nil, "invited_at": nil, "activated_at": nil, "updated_at": time.Now()})
			if res.Error != nil {
				return "", res.Error
			}
			t.UserID = nil
		default:
			config.Logger.Error("auth service resend invite failed", zap.Error(err))
			return "", apperr.New(http.StatusBadGateway, "AUTH_SERVICE_ERROR", "Gagal mengirim ulang undangan")
		}
	}

	u, err := accounts.InviteUser(callCtx, inviteRequest(t, schoolName))
	if err != nil {
		return "", mapInviteError(err)
	}
	now := time.Now()
	res := db.Model(&model.Employees{}).
		Where("id = ? AND school_id = ? AND user_id IS NULL", t.ID, schoolID).
		Updates(map[string]interface{}{"user_id": u.ID, "invited_at": now, "activated_at": nil, "updated_at": now})
	if res.Error != nil || res.RowsAffected == 0 {
		compensateUser(accounts, u.ID)
		if res.Error != nil {
			return "", res.Error
		}
		return "", errInviteConflict
	}
	return inviteWarning(u), nil
}

// deleteTeacherAccount removes a deleted teacher's auth account (best effort, logged).
func deleteTeacherAccount(ctx context.Context, userID uint) {
	callCtx, cancel := authCallContext(ctx)
	defer cancel()
	if err := NewTeacherAccounts().DeleteUser(callCtx, userID); err != nil {
		var ae *client.AuthError
		if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
			return
		}
		config.Logger.Error("failed to delete teacher auth account (orphaned)",
			zap.Uint("user_id", userID), zap.Error(err))
		return
	}
	config.Logger.Info("deleted teacher auth account", zap.Uint("user_id", userID))
}
