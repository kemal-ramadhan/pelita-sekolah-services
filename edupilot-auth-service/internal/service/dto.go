package service

import (
	"time"

	"github.com/edupilot-auth-service/internal/model"
)

// UserDTO is the only user representation sent to clients (API_CONTRACT §5).
type UserDTO struct {
	ID              uint         `json:"id"`
	Email           string       `json:"email"`
	Role            string       `json:"role"`
	Phone           string       `json:"phone"`
	PhotoProfile    string       `json:"photo_profile"`
	Cover           string       `json:"cover"`
	EmailVerifiedAt *time.Time   `json:"email_verified_at"`
	Customer        *CustomerDTO `json:"customer"`
}

type CustomerDTO struct {
	ID         uint    `json:"id"`
	FullName   *string `json:"full_name"`
	SchoolName *string `json:"school_name"`
	BirthPlace *string `json:"birth_place"`
	BirthDate  *string `json:"birth_date"` // YYYY-MM-DD
	Gender     *int8   `json:"gender"`
	Role       string  `json:"role"`
}

type LoginResponse struct {
	Message      string   `json:"message"`
	AccessToken  string   `json:"access_token"`
	RefreshToken string   `json:"refresh_token"`
	User         *UserDTO `json:"user"`
}

func ToUserDTO(u *model.User) *UserDTO {
	if u == nil {
		return nil
	}
	dto := &UserDTO{
		ID:              u.ID,
		Email:           u.Email,
		Role:            u.Role,
		Phone:           u.Phone,
		PhotoProfile:    u.PhotoProfile,
		Cover:           u.Cover,
		EmailVerifiedAt: u.EmailVerifiedAt,
	}
	if u.Customer.ID != 0 {
		c := u.Customer
		var bd *string
		if c.BirthDate != nil {
			s := c.BirthDate.Format("2006-01-02")
			bd = &s
		}
		dto.Customer = &CustomerDTO{
			ID:         c.ID,
			FullName:   c.FullName,
			SchoolName: c.SchoolName,
			BirthPlace: c.BirthPlace,
			BirthDate:  bd,
			Gender:     c.Gender,
			Role:       c.Role,
		}
	}
	return dto
}
