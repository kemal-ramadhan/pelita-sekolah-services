package controller

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"

	"github.com/edupilot-auth-service/internal/service"
	"github.com/edupilot-auth-service/internal/utils"
	"github.com/gin-gonic/gin"
)

const maxImageSize = 2 << 20 // 2 MB

var allowedImageTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/webp": true,
}

type RegisterRequest = service.RegisterRequest
type UpdateProfileRequest = service.UpdateProfileRequest

type EmailRequest struct {
	Email string `json:"email" binding:"required,email"`
}

type VerifyEmailRequest struct {
	Email string           `json:"email" binding:"required,email"`
	Otp   utils.FlexString `json:"otp" binding:"required"`
}

type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

type LogoutRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type ResetPasswordRequest struct {
	Token    string `json:"token" binding:"required"`
	Password string `json:"password" binding:"required,min=8,max=72"`
}

// Register godoc
// @Summary Register new user (role customer, OTP emailed, no tokens)
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body RegisterRequest true "Register request"
// @Success 201 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Router /auth/register [post]
func Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBadRequest(c, err)
		return
	}
	email, err := service.Register(req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"message": "Registrasi berhasil. Kode OTP telah dikirim ke email Anda.",
		"data": gin.H{
			"email":                 email,
			"requires_verification": true,
		},
	})
}

// VerifyEmail godoc
// @Summary Verify email with OTP (logs the user in)
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body VerifyEmailRequest true "Verify email request"
// @Success 200 {object} service.LoginResponse
// @Failure 400 {object} map[string]string
// @Router /auth/verify [post]
func VerifyEmail(c *gin.Context) {
	var req VerifyEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBadRequest(c, err)
		return
	}
	resp, err := service.VerifyEmail(req.Email, req.Otp.String())
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// SendOtp godoc
// @Summary Resend OTP (generic response)
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body EmailRequest true "Email"
// @Success 200 {object} map[string]string
// @Router /auth/resend-otp [post]
func SendOtp(c *gin.Context) {
	var req EmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBadRequest(c, err)
		return
	}
	if err := service.ResendOTP(req.Email); err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Jika email terdaftar dan belum diverifikasi, kode OTP baru telah dikirim."})
}

// Login godoc
// @Summary Login user
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body LoginRequest true "Login request"
// @Success 200 {object} service.LoginResponse
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Router /auth/login [post]
func Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBadRequest(c, err)
		return
	}
	resp, err := service.Login(req.Email, req.Password)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// Me godoc
// @Summary Get current logged-in user
// @Tags Auth
// @Security Bearer
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /auth/me [get]
func Me(c *gin.Context) {
	userID, _, ok := currentUser(c)
	if !ok {
		return
	}
	user, abilities, err := service.Me(userID)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message":   "Get Me successfully",
		"data":      user,
		"abilities": abilities,
	})
}

// UpdateProfile godoc
// @Summary Update own profile
// @Tags Auth
// @Security Bearer
// @Accept json
// @Produce json
// @Param request body UpdateProfileRequest true "Update profile request"
// @Success 200 {object} map[string]interface{}
// @Router /auth/profile [put]
func UpdateProfile(c *gin.Context) {
	userID, _, ok := currentUser(c)
	if !ok {
		return
	}
	var req UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBadRequest(c, err)
		return
	}
	user, err := service.UpdateOwnProfile(userID, req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "profile updated successfully", "data": user})
}

// readImageDataURL validates an uploaded image (png/jpeg/webp, <= 2MB) and returns a data URL.
func readImageDataURL(c *gin.Context, field string) (string, bool) {
	file, err := c.FormFile(field)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("File '%s' wajib diunggah", field), "code": "VALIDATION_ERROR"})
		return "", false
	}
	if file.Size > maxImageSize {
		c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": "Ukuran file maksimal 2 MB", "code": "FILE_TOO_LARGE"})
		return "", false
	}
	src, err := file.Open()
	if err != nil {
		respondError(c, err)
		return "", false
	}
	defer src.Close()

	data, err := io.ReadAll(io.LimitReader(src, maxImageSize+1))
	if err != nil {
		respondError(c, err)
		return "", false
	}
	if len(data) > maxImageSize {
		c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": "Ukuran file maksimal 2 MB", "code": "FILE_TOO_LARGE"})
		return "", false
	}
	mimeType := http.DetectContentType(data)
	if !allowedImageTypes[mimeType] {
		c.AbortWithStatusJSON(http.StatusUnsupportedMediaType, gin.H{"error": "Format gambar harus PNG, JPEG, atau WEBP", "code": "INVALID_FILE_TYPE"})
		return "", false
	}
	return fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(data)), true
}

// PhotoProfile godoc
// @Summary Upload profile photo (multipart "photo", png/jpeg/webp <= 2MB)
// @Tags Auth
// @Security Bearer
// @Router /auth/photo-profile [put]
func PhotoProfile(c *gin.Context) {
	userID, _, ok := currentUser(c)
	if !ok {
		return
	}
	dataURL, ok := readImageDataURL(c, "photo")
	if !ok {
		return
	}
	user, err := service.UpdateUserImage(userID, "photo_profile", dataURL)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Profile has been changed", "data": user})
}

// Cover godoc
// @Summary Upload cover image (multipart "cover", png/jpeg/webp <= 2MB)
// @Tags Auth
// @Security Bearer
// @Router /auth/cover [put]
func Cover(c *gin.Context) {
	userID, _, ok := currentUser(c)
	if !ok {
		return
	}
	dataURL, ok := readImageDataURL(c, "cover")
	if !ok {
		return
	}
	user, err := service.UpdateUserImage(userID, "cover", dataURL)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Cover updated successfully", "data": user})
}

// Refresh godoc
// @Summary Rotate refresh token
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body RefreshRequest true "Refresh request"
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string
// @Router /auth/refresh [post]
func Refresh(c *gin.Context) {
	var req RefreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBadRequest(c, err)
		return
	}
	access, refresh, err := service.Refresh(req.RefreshToken)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message":       "token refreshed successfully",
		"access_token":  access,
		"refresh_token": refresh,
	})
}

// Logout godoc
// @Summary Logout (revokes all sessions of the user)
// @Tags Auth
// @Security Bearer
// @Produce json
// @Success 200 {object} map[string]string
// @Router /auth/logout [post]
func Logout(c *gin.Context) {
	userID, _, ok := currentUser(c)
	if !ok {
		return
	}
	// Body {refresh_token?} is accepted for compatibility; all sessions are revoked anyway.
	var req LogoutRequest
	_ = c.ShouldBindJSON(&req)

	if err := service.RevokeSessions(userID); err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "logout success"})
}

// ForgotPassword godoc
// @Summary Request password reset email (generic response)
// @Tags Auth
// @Accept json
// @Param request body EmailRequest true "Email"
// @Success 200 {object} map[string]string
// @Router /auth/forgot-password [post]
func ForgotPassword(c *gin.Context) {
	var req EmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBadRequest(c, err)
		return
	}
	if err := service.ForgotPassword(req.Email); err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message": "Jika email terdaftar, tautan reset password telah dikirim. Silakan periksa email Anda.",
	})
}

// ResetPassword godoc
// @Summary Reset password with token (single use, revokes all sessions)
// @Tags Auth
// @Accept json
// @Param request body ResetPasswordRequest true "Reset request"
// @Success 200 {object} map[string]string
// @Router /auth/reset-password [post]
func ResetPassword(c *gin.Context) {
	var req ResetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBadRequest(c, err)
		return
	}
	if err := service.ResetPassword(req.Token, req.Password); err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Password berhasil diubah"})
}
