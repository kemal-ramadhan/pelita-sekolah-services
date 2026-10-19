package dto

import (
	"time"

	"github.com/google/uuid"
)

// EmployeePositionRequest is the body of POST/PUT /instantion/school/employee-positions.
// position_code is optional: when blank it is generated from the name.
type EmployeePositionRequest struct {
	Position     string `json:"position" binding:"required"`
	Category     string `json:"category" binding:"required"`
	PositionCode string `json:"position_code"`
}

// EmployeePositionResponse is one jabatan / tugas tambahan of the school.
type EmployeePositionResponse struct {
	ID              uuid.UUID `json:"id"`
	PositionCode    string    `json:"position_code"`
	Position        string    `json:"position"`
	Category        string    `json:"category"`
	AssignmentCount int64     `json:"assignment_count"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}
