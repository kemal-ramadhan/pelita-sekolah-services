package middleware

import (
	"github.com/edupilot-instantion-service/internal/model"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Gin context keys set by the middlewares in this package.
const (
	CtxUserID          = "user_id"
	CtxEmail           = "email"
	CtxRole            = "role"
	CtxSchoolID        = "school_id"
	CtxSchool          = "school"
	CtxEmployeeID      = "employee_id"
	CtxGatewayVerified = "gateway_verified"
	CtxCustomerRole    = "customer_role"
	CtxStudent         = "student"
)

// Role names (API_CONTRACT.md §2).
const (
	RoleSuperAdmin  = "super_admin"
	RoleAdmin       = "admin"
	RoleInstitution = "institution"
	RoleCustomer    = "customer"
	RoleTeacher     = "teacher"

	// CustomerRoleStudents marks a student account (customers.role, API_CONTRACT §10.1).
	CustomerRoleStudents = "students"
)

// IsStudentPrincipal: role customer + customer_role students (§10.1).
func IsStudentPrincipal(c *gin.Context) bool {
	return c.GetString(CtxRole) == RoleCustomer && c.GetString(CtxCustomerRole) == CustomerRoleStudents
}

// LinkedStudent returns the student record linked to the authenticated student
// account (nil when not linked). Set by StudentContext.
func LinkedStudent(c *gin.Context) *model.Students {
	if v, ok := c.Get(CtxStudent); ok {
		if s, ok := v.(*model.Students); ok {
			return s
		}
	}
	return nil
}

// UserID returns the authenticated user's id (0 when unauthenticated).
func UserID(c *gin.Context) uint {
	if v, ok := c.Get(CtxUserID); ok {
		if id, ok := v.(uint); ok {
			return id
		}
	}
	return 0
}

// SchoolID returns the tenant school id resolved by TenantMiddleware.
func SchoolID(c *gin.Context) (uuid.UUID, bool) {
	if v, ok := c.Get(CtxSchoolID); ok {
		if id, ok := v.(uuid.UUID); ok && id != uuid.Nil {
			return id, true
		}
	}
	return uuid.Nil, false
}

// TenantSchool returns the tenant school resolved by TenantMiddleware (nil if not set up).
func TenantSchool(c *gin.Context) *model.School {
	if v, ok := c.Get(CtxSchool); ok {
		if s, ok := v.(*model.School); ok {
			return s
		}
	}
	return nil
}

// EmployeeID returns the teacher's own employee id (set by TenantMiddleware for role teacher).
func EmployeeID(c *gin.Context) (uuid.UUID, bool) {
	if v, ok := c.Get(CtxEmployeeID); ok {
		if id, ok := v.(uuid.UUID); ok && id != uuid.Nil {
			return id, true
		}
	}
	return uuid.Nil, false
}
