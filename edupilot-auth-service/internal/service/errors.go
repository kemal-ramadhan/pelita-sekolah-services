package service

import "net/http"

// AppError is a client-safe error: Message and Code may be returned to the caller.
// Any other error type is treated as internal (logged, generic 500 to the client).
type AppError struct {
	Status  int
	Code    string
	Message string
}

func (e *AppError) Error() string { return e.Message }

func newErr(status int, code, msg string) *AppError {
	return &AppError{Status: status, Code: code, Message: msg}
}

func badRequest(msg string) *AppError { return newErr(http.StatusBadRequest, "", msg) }
func forbidden(msg string) *AppError  { return newErr(http.StatusForbidden, "FORBIDDEN", msg) }
func notFound(msg string) *AppError   { return newErr(http.StatusNotFound, "NOT_FOUND", msg) }
func conflict(msg string) *AppError   { return newErr(http.StatusConflict, "CONFLICT", msg) }

var (
	ErrInvalidCredentials = newErr(http.StatusUnauthorized, "INVALID_CREDENTIALS", "Email atau password salah")
	ErrEmailNotVerified   = newErr(http.StatusForbidden, "EMAIL_NOT_VERIFIED", "Email belum diverifikasi")
	ErrEmailTaken         = newErr(http.StatusConflict, "EMAIL_TAKEN", "Email sudah terdaftar")
	ErrInvalidRefresh     = newErr(http.StatusUnauthorized, "INVALID_REFRESH_TOKEN", "Refresh token tidak valid atau kedaluwarsa")
	ErrOTPInvalid         = newErr(http.StatusBadRequest, "OTP_INVALID", "Kode OTP tidak valid atau sudah kedaluwarsa")
	ErrOTPExpired         = newErr(http.StatusBadRequest, "OTP_EXPIRED", "Kode OTP sudah kedaluwarsa, silakan minta kode baru")
	ErrOTPTooManyAttempts = newErr(http.StatusTooManyRequests, "OTP_ATTEMPTS_EXCEEDED", "Terlalu banyak percobaan OTP, silakan minta kode baru")
	ErrResetTokenInvalid  = newErr(http.StatusBadRequest, "RESET_TOKEN_INVALID", "Token reset password tidak valid atau sudah kedaluwarsa")
	ErrActivationInvalid  = newErr(http.StatusBadRequest, "INVALID_OR_EXPIRED_TOKEN", "Tautan aktivasi tidak valid atau sudah kedaluwarsa")
	ErrAlreadyActive      = newErr(http.StatusConflict, "ALREADY_ACTIVE", "Akun sudah diaktifkan")
	ErrUserNotFound       = notFound("User tidak ditemukan")
	ErrInvalidRole        = newErr(http.StatusBadRequest, "INVALID_ROLE", "Role tidak valid")
)
