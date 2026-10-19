package dto

import (
	"net/mail"
	"strings"
	"time"
)

// SchoolLevels allowed by API_CONTRACT.md §6.
var SchoolLevels = []string{"TK", "SD", "SMP", "SMA", "SMK", "SLB", "Lainnya"}

type SchoolRequest struct {
	Code         string       `json:"code" binding:"required,max=255"`
	Name         string       `json:"name" binding:"required,max=255"`
	NPSN         *string      `json:"npsn" binding:"omitempty,max=255"`
	NSS          *string      `json:"nss" binding:"omitempty,max=255"`
	SchoolLevel  string       `json:"school_level" binding:"required,oneof=TK SD SMP SMA SMK SLB Lainnya"`
	SchoolStatus string       `json:"school_status" binding:"max=255"`
	Email        *string      `json:"email" binding:"omitempty,max=255"`
	Phone        *string      `json:"phone" binding:"omitempty,max=255"`
	Website      *string      `json:"website" binding:"omitempty,max=255"`
	Address      *string      `json:"address" binding:"omitempty,max=2000"`
	ProvinceID   OptionalUUID `json:"province_id"`
	CityID       OptionalUUID `json:"city_id"`
	DistrictID   OptionalUUID `json:"district_id"`
	PostalCode   *string      `json:"postal_code" binding:"omitempty,max=50"`
	Timezone     string       `json:"timezone" binding:"max=64"`
	// AllowStudentJoin (§10.4); omitted = unchanged (default true on create).
	AllowStudentJoin *bool `json:"allow_student_join"`
}

// Normalize trims values and turns empty optional strings into nil.
// It returns a validation message, or "" when valid.
func (r *SchoolRequest) Normalize() string {
	r.Code = strings.TrimSpace(r.Code)
	r.Name = strings.TrimSpace(r.Name)
	r.SchoolStatus = strings.TrimSpace(r.SchoolStatus)
	r.Timezone = strings.TrimSpace(r.Timezone)
	r.NPSN = TrimPtr(r.NPSN)
	r.NSS = TrimPtr(r.NSS)
	r.Email = TrimPtr(r.Email)
	r.Phone = TrimPtr(r.Phone)
	r.Website = TrimPtr(r.Website)
	r.Address = TrimPtr(r.Address)
	r.PostalCode = TrimPtr(r.PostalCode)
	if r.Code == "" {
		return "code wajib diisi"
	}
	if r.Name == "" {
		return "name wajib diisi"
	}
	if r.Timezone != "" {
		if _, err := time.LoadLocation(r.Timezone); err != nil {
			return "timezone tidak valid (contoh: Asia/Jakarta)"
		}
	}
	if r.Email != nil && !ValidEmail(*r.Email) {
		return "email harus berupa email yang valid"
	}
	return ""
}

// ValidEmail is a light RFC 5322 address check (no display names).
func ValidEmail(s string) bool {
	a, err := mail.ParseAddress(s)
	return err == nil && a.Address == s && strings.Contains(s, ".")
}

// AccountRequest creates an auth user (role institution) linked to a school.
type AccountRequest struct {
	Email    string  `json:"email" binding:"required,email,max=255"`
	Password string  `json:"password" binding:"required,min=8,max=72"`
	FullName string  `json:"full_name" binding:"required,max=255"`
	Phone    *string `json:"phone" binding:"omitempty,max=50"`
}

type AdminCreateSchoolRequest struct {
	SchoolRequest
	Account *AccountRequest `json:"account"`
}

type SchoolStatusRequest struct {
	IsActive *bool `json:"is_active" binding:"required"`
}

type LinkAccountRequest struct {
	UserID uint `json:"user_id" binding:"gt=0"`
}

type AdminStats struct {
	TotalSchools    int64 `json:"total_schools"`
	ActiveSchools   int64 `json:"active_schools"`
	InactiveSchools int64 `json:"inactive_schools"`
	TotalTeachers   int64 `json:"total_teachers"`
}
