package service

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/edupilot-auth-service/internal/config"
	"github.com/edupilot-auth-service/internal/model"
	"github.com/edupilot-auth-service/internal/utils"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

const (
	defaultActivationTTLHours = 72
	// inviteEmailWait bounds how long an invite request waits for SMTP so the calling
	// service (10s client timeout) always gets an answer; a slower send continues in
	// the background and is reported as email_sent=false.
	inviteEmailWait = 7 * time.Second
)

// InviteUserRequest is the body of POST /internal/users/invite (API_CONTRACT §7.3, §10.2).
// role "teacher" → users.role teacher; role "student" → users.role customer +
// customers.role students. school_name is required for teachers, optional for students.
type InviteUserRequest struct {
	Email      string `json:"email" binding:"required,email,max=255"`
	FullName   string `json:"full_name" binding:"required,max=255"`
	Phone      string `json:"phone" binding:"max=50"`
	Role       string `json:"role" binding:"required,oneof=teacher student"`
	SchoolName string `json:"school_name" binding:"required_if=Role teacher,max=255"`
}

type ResendInviteRequest struct {
	SchoolName string `json:"school_name" binding:"max=255"`
}

type InvitedUser struct {
	ID        uint   `json:"id"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	EmailSent bool   `json:"email_sent"`
}

type ActivationInfo struct {
	Email    string `json:"email"`
	FullName string `json:"full_name"`
}

// ActivationTTL returns ACTIVATION_TOKEN_TTL_HOURS (default 72h).
func ActivationTTL() time.Duration {
	return time.Duration(activationTTLHours()) * time.Hour
}

func activationTTLHours() int {
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv("ACTIVATION_TOKEN_TTL_HOURS"))); err == nil && v > 0 {
		return v
	}
	return defaultActivationTTLHours
}

// newActivationToken returns the raw token (for the email) and its SHA-256 hash (for storage).
func newActivationToken() (raw, hash string, err error) {
	raw, err = utils.RandomToken(32)
	if err != nil {
		return "", "", err
	}
	return raw, utils.HashToken(raw), nil
}

// activationLink points at the students app for student accounts, else FRONTEND_URL.
func activationLink(token, customerRole string) string {
	return fmt.Sprintf("%s/activate?token=%s", frontendBaseURL(customerRole), url.QueryEscape(token))
}

// sendInviteEmail sends the invite and reports whether it was delivered within inviteEmailWait.
// customerRole "students" sends the student ("akun siswa") template, else the teacher one.
func sendInviteEmail(userID uint, email, name, schoolName, token, customerRole string) bool {
	link := activationLink(token, customerRole)
	send := utils.SendTeacherInviteEmail
	if isStudentAccount(customerRole) {
		send = utils.SendStudentInviteEmail
	}
	done := make(chan error, 1)
	go func() {
		done <- send(email, name, schoolName, link, activationTTLHours())
	}()
	select {
	case err := <-done:
		if err != nil {
			logErr("failed to send invite email", err, zap.Uint("user_id", userID))
			return false
		}
		return true
	case <-time.After(inviteEmailWait):
		if config.Logger != nil {
			config.Logger.Warn("invite email still pending, reporting email_sent=false", zap.Uint("user_id", userID))
		}
		return false
	}
}

// inviteRoles maps an invite role to (users.role, customers.role).
func inviteRoles(inviteRole string) (userRole, customerRole string, ok bool) {
	switch strings.TrimSpace(inviteRole) {
	case RoleTeacher:
		return RoleTeacher, RoleTeacher, true
	case InviteRoleStudent:
		return RoleCustomer, CustomerRoleStudents, true
	}
	return "", "", false
}

// InviteUser creates a teacher or student account with no usable password and emails
// an activation link.
func InviteUser(req InviteUserRequest) (*InvitedUser, error) {
	userRole, customerRole, ok := inviteRoles(req.Role)
	if !ok {
		return nil, ErrInvalidRole
	}
	if userRole == RoleTeacher && strings.TrimSpace(req.SchoolName) == "" {
		return nil, badRequest("school_name wajib diisi untuk undangan guru")
	}
	email := NormalizeEmail(req.Email)

	var count int64
	if err := whereEmail(config.DB.Model(&model.User{}), email).Count(&count).Error; err != nil {
		return nil, err
	}
	if count > 0 {
		return nil, ErrEmailTaken
	}

	raw, hash, err := newActivationToken()
	if err != nil {
		return nil, err
	}
	exp := time.Now().Add(ActivationTTL())
	user := model.User{
		Email:               email,
		Phone:               strings.TrimSpace(req.Phone),
		Password:            "", // no usable password until activation
		Role:                userRole,
		ActivationTokenHash: &hash,
		ActivationExpiresAt: &exp,
	}

	err = config.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&user).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.Customer{
			UserID:     user.ID,
			FullName:   strPtrOrNil(req.FullName),
			SchoolName: strPtrOrNil(req.SchoolName),
			Role:       customerRole,
		}).Error; err != nil {
			return err
		}
		return assignSystemRBACRole(tx, user.ID, userRole)
	})
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return nil, ErrEmailTaken
	}
	if err != nil {
		return nil, err
	}

	sent := sendInviteEmail(user.ID, user.Email, req.FullName, req.SchoolName, raw, customerRole)
	return &InvitedUser{ID: user.ID, Email: user.Email, Role: user.Role, EmailSent: sent}, nil
}

// isInvitableAccount: teacher accounts, and student accounts (customer + customers.role students).
func isInvitableAccount(userRole, customerRole string) bool {
	return userRole == RoleTeacher || (userRole == RoleCustomer && isStudentAccount(customerRole))
}

// ResendInvite issues a new activation token (the old one is invalidated) and re-sends the email.
func ResendInvite(userID uint, schoolName string) (*InvitedUser, error) {
	user, err := loadUser(config.DB, userID)
	if err != nil {
		return nil, err
	}
	customerRole := customerRoleOf(user)
	if !isInvitableAccount(user.Role, customerRole) {
		return nil, forbidden("Hanya akun guru atau siswa yang dapat diundang ulang")
	}
	if user.EmailVerifiedAt != nil {
		return nil, ErrAlreadyActive
	}

	raw, hash, err := newActivationToken()
	if err != nil {
		return nil, err
	}
	res := config.DB.Model(&model.User{}).
		Where("id = ? AND email_verified_at IS NULL", user.ID).
		Updates(map[string]interface{}{
			"activation_token_hash": hash,
			"activation_expires_at": time.Now().Add(ActivationTTL()),
		})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrAlreadyActive
	}

	name := ""
	if user.Customer.FullName != nil {
		name = *user.Customer.FullName
	}
	if strings.TrimSpace(schoolName) == "" && user.Customer.SchoolName != nil {
		schoolName = *user.Customer.SchoolName
	}
	sent := sendInviteEmail(user.ID, user.Email, name, schoolName, raw, customerRole)
	return &InvitedUser{ID: user.ID, Email: user.Email, Role: user.Role, EmailSent: sent}, nil
}

type activationState struct {
	ID                  uint
	Email               string
	EmailVerifiedAt     *time.Time
	ActivationExpiresAt *time.Time
}

// checkActivation validates a looked-up activation record against the clock.
func checkActivation(st *activationState, now time.Time) error {
	if st == nil || st.ID == 0 || st.EmailVerifiedAt != nil {
		return ErrActivationInvalid
	}
	if st.ActivationExpiresAt == nil || !now.Before(*st.ActivationExpiresAt) {
		return ErrActivationInvalid
	}
	return nil
}

func normalizeActivationToken(token string) (string, bool) {
	token = strings.TrimSpace(token)
	// RandomToken(32) yields 64 hex chars; reject anything else without a DB lookup.
	if len(token) != 64 {
		return "", false
	}
	for _, r := range token {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return "", false
		}
	}
	return strings.ToLower(token), true
}

func findActivation(token string) (*activationState, string, error) {
	tok, ok := normalizeActivationToken(token)
	if !ok {
		return nil, "", ErrActivationInvalid
	}
	hash := utils.HashToken(tok)
	var st activationState
	err := config.DB.Model(&model.User{}).
		Select("id", "email", "email_verified_at", "activation_expires_at").
		Where("activation_token_hash = ?", hash).Take(&st).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, "", ErrActivationInvalid
	}
	if err != nil {
		return nil, "", err
	}
	if err := checkActivation(&st, time.Now()); err != nil {
		return nil, "", err
	}
	return &st, hash, nil
}

// CheckActivationToken returns the invitee's email and name for the activation screen.
func CheckActivationToken(token string) (*ActivationInfo, error) {
	st, _, err := findActivation(token)
	if err != nil {
		return nil, err
	}
	user, err := loadUser(config.DB, st.ID)
	if err != nil {
		return nil, err
	}
	info := &ActivationInfo{Email: user.Email}
	if user.Customer.FullName != nil {
		info.FullName = *user.Customer.FullName
	}
	return info, nil
}

// ActivateAccount sets the password, marks the email verified, consumes the token
// (single use) and logs the user in.
func ActivateAccount(token, password string) (*LoginResponse, error) {
	st, hash, err := findActivation(token)
	if err != nil {
		return nil, err
	}
	hashed, err := utils.HashPassword(password)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	res := config.DB.Model(&model.User{}).
		Where("id = ? AND activation_token_hash = ? AND activation_expires_at > ? AND email_verified_at IS NULL",
			st.ID, hash, now).
		Updates(map[string]interface{}{
			"password":              hashed,
			"email_verified_at":     now,
			"activation_token_hash": nil,
			"activation_expires_at": nil,
		})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrActivationInvalid
	}

	user, err := loadUser(config.DB, st.ID)
	if err != nil {
		return nil, err
	}
	return issueSession(user, "account activated successfully")
}
