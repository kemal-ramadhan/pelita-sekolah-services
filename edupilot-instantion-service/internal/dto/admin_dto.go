package dto

import "github.com/edupilot-instantion-service/internal/model"

// AdminSchoolItem is a School plus the number of linked institution accounts.
type AdminSchoolItem struct {
	model.School
	AccountCount int64 `json:"account_count"`
}
