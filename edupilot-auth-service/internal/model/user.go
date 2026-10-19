package model

import "time"

type User struct {
	ID                     uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	Email                  string     `gorm:"uniqueIndex;not null" json:"email"`
	Phone                  string     `gorm:"index;null" json:"phone"`
	Password               string     `json:"-"`
	PhotoProfile           string     `gorm:"type:text" json:"photo_profile,omitempty"`
	Cover                  string     `gorm:"type:text" json:"cover,omitempty"`
	Role                   string     `gorm:"type:varchar(50);not null;default:'customer'" json:"role"`
	GoogleID               *string    `gorm:"uniqueIndex;null" json:"-"`
	GoogleAccessToken      *string    `gorm:"type:text;null" json:"-"`
	EmailVerifiedAt        *time.Time `json:"email_verified_at,omitempty"`
	TwoFactorSecret        *string    `gorm:"type:text;null" json:"-"`
	TwoFactorRecoveryCodes *string    `gorm:"type:text;null" json:"-"`
	TwoFactorConfirmedAt   *time.Time `json:"-"`
	RememberToken          *string    `gorm:"type:varchar(255);null" json:"-"`
	EmailOtp               *int       `json:"-"`
	EmailOtpExpiresAt      *time.Time `json:"-"`
	EmailOtpAttempt        *int16     `json:"-"`
	ResetPasswordToken     *string    `gorm:"type:text;null;index" json:"-"` // SHA-256 hex of the reset token
	ResetPasswordExpiresAt *time.Time `json:"-"`
	ActivationTokenHash    *string    `gorm:"type:text;null;index" json:"-"` // SHA-256 hex of the teacher activation token
	ActivationExpiresAt    *time.Time `json:"-"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`

	Customer Customer `gorm:"foreignKey:UserID" json:"customer,omitempty"`
}
