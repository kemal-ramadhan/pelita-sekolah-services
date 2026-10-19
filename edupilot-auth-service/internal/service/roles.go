package service

const (
	RoleSuperAdmin  = "super_admin"
	RoleAdmin       = "admin"
	RoleInstitution = "institution"
	RoleCustomer    = "customer"
	// RoleTeacher accounts are created only through the teacher-invite flow
	// (POST /internal/users/invite), never via register or /auth/admin/users.
	RoleTeacher = "teacher"
)

// roleRank orders users.role values. Unknown roles rank 0.
var roleRank = map[string]int{
	RoleTeacher:     1,
	RoleCustomer:    1,
	RoleInstitution: 1,
	RoleAdmin:       2,
	RoleSuperAdmin:  3,
}

func RoleRank(role string) int { return roleRank[role] }

func IsValidRole(role string) bool { _, ok := roleRank[role]; return ok }

func IsAdminRole(role string) bool { return role == RoleAdmin || role == RoleSuperAdmin }

// canAssignRole reports whether a caller may create/assign role. callerRole "" means an
// internal service call. RoleTeacher is never assignable here (invite flow only).
func canAssignRole(callerRole, role string) bool {
	switch role {
	case RoleInstitution, RoleCustomer:
		return callerRole == "" || IsAdminRole(callerRole)
	case RoleAdmin, RoleSuperAdmin:
		return callerRole == RoleSuperAdmin
	}
	return false
}

// canManageUser: super_admin manages anyone; admin manages only users ranked below admin.
func canManageUser(callerRole, targetRole string) bool {
	if callerRole == RoleSuperAdmin {
		return true
	}
	return RoleRank(targetRole) < RoleRank(callerRole)
}

// CustomerRoleStudents marks a student account: users.role "customer" + customers.role
// "students" (API_CONTRACT §10.1). It is exposed as the JWT claim customer_role.
const CustomerRoleStudents = "students"

// Register `app` values (API_CONTRACT §10.2).
const (
	AppSchools  = "schools"
	AppStudents = "students"
)

// InviteRoleStudent is the internal invite role for student accounts; it maps to
// users.role "customer" + customers.role "students".
const InviteRoleStudent = "student"
