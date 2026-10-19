package service

import (
	"errors"
	"fmt"
	"net/url"
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
	otpTTL            = 5 * time.Minute
	otpMaxAttempts    = 5
	otpResendCooldown = 60 * time.Second
	resetTokenTTL     = 30 * time.Minute
)

type RegisterRequest struct {
	Email      string `json:"email" binding:"required,email,max=255"`
	Password   string `json:"password" binding:"required,min=8,max=72"`
	FullName   string `json:"full_name" binding:"required,max=255"`
	Phone      string `json:"phone" binding:"max=50"`
	SchoolName string `json:"school_name" binding:"max=255"`
	// App selects the account kind: "schools" (default) or "students" (API_CONTRACT §10.2).
	App string `json:"app" binding:"omitempty,oneof=schools students"`
}

// UpdateProfileRequest: nil pointer = field not provided (unchanged); "" = cleared.
type UpdateProfileRequest struct {
	FullName   string            `json:"full_name" binding:"required,max=255"`
	Phone      *string           `json:"phone" binding:"omitempty,max=50"`
	BirthPlace *string           `json:"birth_place" binding:"omitempty,max=255"`
	BirthDate  *string           `json:"birth_date"`
	Gender     *utils.FlexString `json:"gender"`
}

func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func whereEmail(db *gorm.DB, email string) *gorm.DB {
	return db.Where("LOWER(email) = ?", NormalizeEmail(email))
}

func logErr(msg string, err error, fields ...zap.Field) {
	if config.Logger != nil {
		config.Logger.Error(msg, append(fields, zap.Error(err))...)
	}
}

func loadUser(db *gorm.DB, userID uint) (*model.User, error) {
	var user model.User
	err := db.Preload("Customer", func(db *gorm.DB) *gorm.DB {
		return db.Order("id ASC").Limit(1)
	}).Where("id = ?", userID).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// issueSession creates an access + refresh token pair and stores the refresh jti.
func issueSession(user *model.User, message string) (*LoginResponse, error) {
	customerRole := customerRoleOf(user)
	access, err := utils.GenerateAccessToken(user.ID, user.Email, user.Role, customerRole)
	if err != nil {
		return nil, err
	}
	refresh, jti, err := utils.GenerateRefreshToken(user.ID, user.Email, user.Role, customerRole)
	if err != nil {
		return nil, err
	}
	if err := config.SaveRefreshToken(user.ID, jti, utils.RefreshTTL()); err != nil {
		return nil, err
	}
	return &LoginResponse{
		Message:      message,
		AccessToken:  access,
		RefreshToken: refresh,
		User:         ToUserDTO(user),
	}, nil
}

func strPtrOrNil(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}

// ---------------------------------------------------------------------------
// Register / OTP
// ---------------------------------------------------------------------------

// Register creates an unverified customer account and emails an OTP. No tokens are issued.
// app "students" creates a student account (customers.role "students", school_name ignored).
func Register(req RegisterRequest) (string, error) {
	email := NormalizeEmail(req.Email)
	customerRole := RoleCustomer
	schoolName := strPtrOrNil(req.SchoolName)
	switch strings.TrimSpace(req.App) {
	case "", AppSchools:
	case AppStudents:
		customerRole = CustomerRoleStudents
		schoolName = nil
	default:
		return "", badRequest("Nilai app tidak valid (schools | students)")
	}

	var count int64
	if err := whereEmail(config.DB.Model(&model.User{}), email).Count(&count).Error; err != nil {
		return "", err
	}
	if count > 0 {
		return "", ErrEmailTaken
	}

	hashed, err := utils.HashPassword(req.Password)
	if err != nil {
		return "", err
	}
	otp, err := utils.GenerateOTP()
	if err != nil {
		return "", err
	}
	exp := time.Now().Add(otpTTL)
	attempt := int16(0)

	user := model.User{
		Email:             email,
		Phone:             strings.TrimSpace(req.Phone),
		Password:          hashed,
		Role:              RoleCustomer,
		EmailOtp:          &otp,
		EmailOtpExpiresAt: &exp,
		EmailOtpAttempt:   &attempt,
	}

	err = config.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&user).Error; err != nil {
			return err
		}
		customer := model.Customer{
			UserID:     user.ID,
			FullName:   strPtrOrNil(req.FullName),
			SchoolName: schoolName,
			Role:       customerRole,
		}
		if err := tx.Create(&customer).Error; err != nil {
			return err
		}
		return assignSystemRBACRole(tx, user.ID, RoleCustomer)
	})
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return "", ErrEmailTaken
	}
	if err != nil {
		return "", err
	}

	sendOTPAsync(email, req.FullName, otp)
	return email, nil
}

func sendOTPAsync(email, name string, otp int) {
	if strings.TrimSpace(name) == "" {
		name = "Pengguna"
	}
	go func() {
		if err := utils.SendOtpEmail(email, name, otp); err != nil {
			logErr("failed to send OTP email", err)
		}
	}()
}

type otpState struct {
	ID                uint
	EmailOtp          *int
	EmailOtpExpiresAt *time.Time
	EmailOtpAttempt   int16
}

// checkOTP validates an OTP after the attempt counter has already been incremented.
func checkOTP(st otpState, provided string, now time.Time) error {
	if st.EmailOtp == nil {
		return ErrOTPInvalid
	}
	if st.EmailOtpAttempt > otpMaxAttempts {
		return ErrOTPTooManyAttempts
	}
	if st.EmailOtpExpiresAt == nil || now.After(*st.EmailOtpExpiresAt) {
		return ErrOTPExpired
	}
	if !utils.SecureCompare(utils.FormatOTP(*st.EmailOtp), strings.TrimSpace(provided)) {
		return ErrOTPInvalid
	}
	return nil
}

// VerifyEmail checks the OTP (attempts incremented atomically before comparing)
// and logs the user in on success.
func VerifyEmail(email, otp string) (*LoginResponse, error) {
	var st otpState
	res := config.DB.Raw(`
		UPDATE users
		SET email_otp_attempt = COALESCE(email_otp_attempt, 0) + 1, updated_at = NOW()
		WHERE LOWER(email) = ? AND email_verified_at IS NULL AND email_otp IS NOT NULL
		RETURNING id, email_otp, email_otp_expires_at, email_otp_attempt`,
		NormalizeEmail(email)).Scan(&st)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 || st.ID == 0 {
		return nil, ErrOTPInvalid
	}
	if err := checkOTP(st, otp, time.Now()); err != nil {
		return nil, err
	}

	upd := config.DB.Model(&model.User{}).
		Where("id = ? AND email_verified_at IS NULL", st.ID).
		Updates(map[string]interface{}{
			"email_verified_at":    time.Now(),
			"email_otp":            nil,
			"email_otp_expires_at": nil,
			"email_otp_attempt":    nil,
		})
	if upd.Error != nil {
		return nil, upd.Error
	}
	if upd.RowsAffected == 0 {
		return nil, ErrOTPInvalid
	}

	user, err := loadUser(config.DB, st.ID)
	if err != nil {
		return nil, err
	}
	return issueSession(user, "email verified successfully")
}

// ResendOTP generates a new OTP for an unverified account. Always returns nil for
// unknown/verified emails (no enumeration); errors are only internal failures.
func ResendOTP(email string) error {
	email = NormalizeEmail(email)

	// per-email cooldown
	ok, err := config.RDB.SetNX(config.Ctx(), "otp:resend:"+email, 1, otpResendCooldown).Result()
	if err != nil {
		logErr("resend otp cooldown check failed", err)
	} else if !ok {
		return nil
	}

	var user model.User
	err = whereEmail(config.DB.Preload("Customer"), email).
		Where("email_verified_at IS NULL").First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}

	otp, err := utils.GenerateOTP()
	if err != nil {
		return err
	}
	if err := config.DB.Model(&model.User{}).Where("id = ?", user.ID).Updates(map[string]interface{}{
		"email_otp":            otp,
		"email_otp_expires_at": time.Now().Add(otpTTL),
		"email_otp_attempt":    0,
	}).Error; err != nil {
		return err
	}

	name := ""
	if user.Customer.FullName != nil {
		name = *user.Customer.FullName
	}
	sendOTPAsync(user.Email, name, otp)
	return nil
}

// ---------------------------------------------------------------------------
// Login / refresh / logout
// ---------------------------------------------------------------------------

func Login(email, password string) (*LoginResponse, error) {
	var user model.User
	err := whereEmail(config.DB.Preload("Customer", func(db *gorm.DB) *gorm.DB {
		return db.Order("id ASC").Limit(1)
	}), email).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		utils.CheckPassword("", password) // equalize timing
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}

	if !utils.CheckPassword(user.Password, password) {
		return nil, ErrInvalidCredentials
	}
	if user.EmailVerifiedAt == nil {
		return nil, ErrEmailNotVerified
	}
	return issueSession(&user, "login success")
}

// Refresh rotates a refresh token: the old jti is consumed, a new pair is issued.
func Refresh(refreshToken string) (string, string, error) {
	claims, err := utils.ValidateToken(refreshToken, utils.TokenTypeRefresh)
	if err != nil {
		return "", "", ErrInvalidRefresh
	}
	revokedBefore, err := config.GetRevokedBefore(claims.UserID)
	if err != nil {
		return "", "", err
	}
	if utils.IsRevoked(claims.IssuedAt.Time, revokedBefore) {
		return "", "", ErrInvalidRefresh
	}
	ok, err := config.ConsumeRefreshToken(claims.UserID, claims.ID)
	if err != nil {
		return "", "", err
	}
	if !ok {
		return "", "", ErrInvalidRefresh
	}

	user, err := loadUser(config.DB, claims.UserID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return "", "", ErrInvalidRefresh
		}
		return "", "", err
	}

	customerRole := customerRoleOf(user)
	access, err := utils.GenerateAccessToken(user.ID, user.Email, user.Role, customerRole)
	if err != nil {
		return "", "", err
	}
	refresh, jti, err := utils.GenerateRefreshToken(user.ID, user.Email, user.Role, customerRole)
	if err != nil {
		return "", "", err
	}
	if err := config.SaveRefreshToken(user.ID, jti, utils.RefreshTTL()); err != nil {
		return "", "", err
	}
	return access, refresh, nil
}

// RevokeSessions invalidates every access/refresh token of a user.
func RevokeSessions(userID uint) error {
	return config.RevokeUserSessions(userID, utils.SessionRevocationTTL())
}

// ---------------------------------------------------------------------------
// Forgot / reset password
// ---------------------------------------------------------------------------

// ForgotPassword never reveals whether the email exists; only internal errors are returned.
func ForgotPassword(email string) error {
	var user model.User
	err := whereEmail(config.DB.Preload("Customer", func(db *gorm.DB) *gorm.DB {
		return db.Order("id ASC").Limit(1)
	}), email).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}

	token, err := utils.RandomToken(32)
	if err != nil {
		return err
	}
	if err := config.DB.Model(&model.User{}).Where("id = ?", user.ID).Updates(map[string]interface{}{
		"reset_password_token":      utils.HashToken(token),
		"reset_password_expires_at": time.Now().Add(resetTokenTTL),
	}).Error; err != nil {
		return err
	}

	customerRole := customerRoleOf(&user)
	link := fmt.Sprintf("%s/reset-password?token=%s", frontendBaseURL(customerRole), url.QueryEscape(token))
	to := user.Email
	student := isStudentAccount(customerRole)
	go func() {
		if err := utils.SendResetPasswordEmail(to, link, student); err != nil {
			logErr("failed to send reset password email", err)
		}
	}()
	return nil
}

func ResetPassword(token, newPassword string) error {
	hash := utils.HashToken(strings.TrimSpace(token))

	var user model.User
	err := config.DB.Where("reset_password_token = ? AND reset_password_expires_at > ?", hash, time.Now()).
		First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrResetTokenInvalid
	}
	if err != nil {
		return err
	}

	hashed, err := utils.HashPassword(newPassword)
	if err != nil {
		return err
	}

	// Single use: conditional update on the token hash.
	res := config.DB.Model(&model.User{}).
		Where("id = ? AND reset_password_token = ?", user.ID, hash).
		Updates(map[string]interface{}{
			"password":                  hashed,
			"reset_password_token":      nil,
			"reset_password_expires_at": nil,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrResetTokenInvalid
	}

	if err := RevokeSessions(user.ID); err != nil {
		logErr("failed to revoke sessions after password reset", err, zap.Uint("user_id", user.ID))
	}
	return nil
}

// ---------------------------------------------------------------------------
// Me / profile
// ---------------------------------------------------------------------------

func Me(userID uint) (*UserDTO, []string, error) {
	user, err := loadUser(config.DB, userID)
	if err != nil {
		return nil, nil, err
	}
	abilities, err := UserAbilities(userID)
	if err != nil {
		return nil, nil, err
	}
	return ToUserDTO(user), abilities, nil
}

// UserAbilities = abilities of the user's RBAC roles, with per-user overrides applied.
func UserAbilities(userID uint) ([]string, error) {
	var userRoles []model.UserRole
	if err := config.DB.Preload("Role.Abilities").Where("user_id = ?", userID).Find(&userRoles).Error; err != nil {
		return nil, err
	}
	var overrides []model.UserAbility
	if err := config.DB.Preload("Ability").Where("user_id = ?", userID).Find(&overrides).Error; err != nil {
		return nil, err
	}

	set := map[string]bool{}
	for _, ur := range userRoles {
		for _, ab := range ur.Role.Abilities {
			set[ab.Name] = true
		}
	}
	for _, o := range overrides {
		if o.IsActive {
			set[o.Ability.Name] = true
		} else {
			delete(set, o.Ability.Name)
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	return out, nil
}

// applyProfile updates customer + user profile fields in a transaction.
func applyProfile(userID uint, req UpdateProfileRequest) (*UserDTO, error) {
	var birthDate *time.Time
	if req.BirthDate != nil && strings.TrimSpace(*req.BirthDate) != "" {
		bd, err := time.Parse("2006-01-02", strings.TrimSpace(*req.BirthDate))
		if err != nil {
			return nil, badRequest("Format tanggal lahir tidak valid, gunakan YYYY-MM-DD")
		}
		birthDate = &bd
	}
	var gender *int8
	if req.Gender != nil && req.Gender.String() != "" && req.Gender.String() != "0" {
		g, err := strconv.ParseInt(req.Gender.String(), 10, 8)
		if err != nil || (g != 1 && g != 2) {
			return nil, badRequest("Nilai gender tidak valid (1 = laki-laki, 2 = perempuan)")
		}
		gi := int8(g)
		gender = &gi
	}

	err := config.DB.Transaction(func(tx *gorm.DB) error {
		var user model.User
		if err := tx.Where("id = ?", userID).First(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrUserNotFound
			}
			return err
		}

		var customer model.Customer
		err := tx.Where("user_id = ?", userID).Order("id ASC").First(&customer).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			customer = model.Customer{UserID: userID, Role: user.Role}
		} else if err != nil {
			return err
		}

		customer.FullName = strPtrOrNil(req.FullName)
		if req.BirthPlace != nil {
			customer.BirthPlace = strPtrOrNil(*req.BirthPlace)
		}
		if req.BirthDate != nil {
			customer.BirthDate = birthDate
		}
		if req.Gender != nil {
			customer.Gender = gender
		}
		if err := tx.Save(&customer).Error; err != nil {
			return err
		}

		if req.Phone != nil {
			if err := tx.Model(&model.User{}).Where("id = ?", userID).
				Update("phone", strings.TrimSpace(*req.Phone)).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	user, err := loadUser(config.DB, userID)
	if err != nil {
		return nil, err
	}
	return ToUserDTO(user), nil
}

func UpdateOwnProfile(userID uint, req UpdateProfileRequest) (*UserDTO, error) {
	return applyProfile(userID, req)
}

// UpdateUserImage stores a data URL into photo_profile or cover.
func UpdateUserImage(userID uint, column, dataURL string) (*UserDTO, error) {
	if column != "photo_profile" && column != "cover" {
		return nil, errors.New("invalid image column")
	}
	res := config.DB.Model(&model.User{}).Where("id = ?", userID).Update(column, dataURL)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrUserNotFound
	}
	user, err := loadUser(config.DB, userID)
	if err != nil {
		return nil, err
	}
	return ToUserDTO(user), nil
}

// ---------------------------------------------------------------------------
// Google OAuth
// ---------------------------------------------------------------------------

type GoogleUserInfo struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	VerifiedEmail bool   `json:"verified_email"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
}

var ErrGoogleEmailNotVerified = newErr(403, "GOOGLE_EMAIL_NOT_VERIFIED", "Email Google belum terverifikasi")

func GoogleLoginCallback(info GoogleUserInfo) (*LoginResponse, error) {
	if !info.VerifiedEmail || info.ID == "" || info.Email == "" {
		return nil, ErrGoogleEmailNotVerified
	}
	email := NormalizeEmail(info.Email)
	googleID := info.ID

	var userID uint
	err := config.DB.Transaction(func(tx *gorm.DB) error {
		var user model.User
		err := tx.Where("google_id = ?", googleID).First(&user).Error
		if err == nil {
			userID = user.ID
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		err = whereEmail(tx, email).First(&user).Error
		if err == nil {
			// Link existing account. Google has verified ownership of this email.
			updates := map[string]interface{}{"google_id": googleID}
			if user.EmailVerifiedAt == nil {
				// The local account was never verified: whoever set its password did not
				// prove email ownership, so drop that password (pre-hijack protection).
				updates["email_verified_at"] = time.Now()
				updates["password"] = ""
				updates["email_otp"] = nil
				updates["email_otp_expires_at"] = nil
				updates["email_otp_attempt"] = nil
			}
			if user.PhotoProfile == "" && info.Picture != "" {
				updates["photo_profile"] = info.Picture
			}
			if err := tx.Model(&model.User{}).Where("id = ?", user.ID).Updates(updates).Error; err != nil {
				return err
			}
			userID = user.ID
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		now := time.Now()
		user = model.User{
			Email:           email,
			PhotoProfile:    info.Picture,
			Role:            RoleCustomer,
			GoogleID:        &googleID,
			EmailVerifiedAt: &now,
		}
		if err := tx.Create(&user).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.Customer{
			UserID:   user.ID,
			FullName: strPtrOrNil(info.Name),
			Role:     RoleCustomer,
		}).Error; err != nil {
			return err
		}
		if err := assignSystemRBACRole(tx, user.ID, RoleCustomer); err != nil {
			return err
		}
		userID = user.ID
		return nil
	})
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return nil, ErrEmailTaken
	}
	if err != nil {
		return nil, err
	}

	user, err := loadUser(config.DB, userID)
	if err != nil {
		return nil, err
	}
	return issueSession(user, "google login success")
}
