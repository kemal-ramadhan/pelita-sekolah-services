package middleware

import (
	"net/http"

	"github.com/edupilot-instantion-service/internal/config"
	"github.com/edupilot-instantion-service/internal/model"
	"github.com/edupilot-instantion-service/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// SchoolProfilePath is reachable before a school exists (GET/POST only).
const SchoolProfilePath = "/instantion/school/profile"

// SchoolMePath returns the teacher's own employee record.
const SchoolMePath = "/instantion/school/me"

// SchoolMySchedulePath returns the teacher's own weekly timetable grid.
const SchoolMySchedulePath = "/instantion/school/me/schedule"

// teacherAllowedRoutes are the only tenant routes a teacher may call (API_CONTRACT §7.1).
// Everything else under /instantion/school/* is 403 FORBIDDEN_ROLE for teachers.
// Handlers further restrict teachers to their own data: GET /schedules and
// /schedules/grid are forced to teacher_id = own employee, and
// GET /teachers/:id/subjects requires :id = own employee (else 403).
var teacherAllowedRoutes = map[string]bool{
	http.MethodGet + " " + SchoolProfilePath:                                 true,
	http.MethodGet + " " + SchoolMePath:                                      true,
	http.MethodGet + " " + SchoolMySchedulePath:                              true,
	http.MethodGet + " /instantion/school/schedules":                         true,
	http.MethodGet + " /instantion/school/schedules/grid":                    true,
	http.MethodGet + " /instantion/school/schedule-periods":                  true,
	http.MethodGet + " /instantion/school/subjects":                          true,
	http.MethodGet + " /instantion/school/teachers/:id/subjects":             true,
	http.MethodGet + " /instantion/school/attendance/config":                 true,
	http.MethodGet + " /instantion/school/attendance/me/today":               true,
	http.MethodPost + " /instantion/school/attendance/me/check-in":           true,
	http.MethodPost + " /instantion/school/attendance/me/check-out":          true,
	http.MethodGet + " /instantion/school/attendance/teaching/today":         true,
	http.MethodPost + " /instantion/school/attendance/teaching/start":        true,
	http.MethodPost + " /instantion/school/attendance/teaching/:id/end":      true,
	http.MethodPost + " /instantion/school/attendance/teaching/:id/token":    true,
	http.MethodGet + " /instantion/school/attendance/teaching/:id":           true,
	http.MethodPut + " /instantion/school/attendance/teaching/:id/students":  true,
	http.MethodPut + " /instantion/school/attendance/teaching/:id/journal":   true,
	http.MethodGet + " /instantion/school/counseling/summary":                true,
	http.MethodGet + " /instantion/school/counseling/students":               true,
	http.MethodGet + " /instantion/school/counseling/counselors":             true,
	http.MethodGet + " /instantion/school/counseling/sessions":               true,
	http.MethodPost + " /instantion/school/counseling/sessions":              true,
	http.MethodGet + " /instantion/school/counseling/sessions/:id":           true,
	http.MethodPut + " /instantion/school/counseling/sessions/:id/status":    true,
	http.MethodPost + " /instantion/school/counseling/sessions/:id/outcome":  true,
	http.MethodGet + " /instantion/school/counseling/events":                 true,
	http.MethodPost + " /instantion/school/counseling/events":                true,
	http.MethodGet + " /instantion/school/counseling/events/:id":             true,
	http.MethodPost + " /instantion/school/counseling/events/:id/attend":     true,
	http.MethodPost + " /instantion/school/counseling/events/:id/conclusion": true,
	http.MethodGet + " /instantion/school/counseling/journals":               true,
	http.MethodPost + " /instantion/school/counseling/journals":              true,
	http.MethodPut + " /instantion/school/counseling/journals/:id":           true,
	http.MethodDelete + " /instantion/school/counseling/journals/:id":        true,
}

func abortInternal(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Terjadi kesalahan pada server", "code": "INTERNAL_ERROR"})
}

// TenantMiddleware resolves the tenant school (API_CONTRACT.md §6, §7.1) and stores
// school_id / school (and employee_id for teachers) in the gin context.
// Must run after AuthMiddleware.
func TenantMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := UserID(c)
		if userID == 0 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "code": "UNAUTHORIZED"})
			return
		}

		if c.GetString(CtxRole) == RoleTeacher {
			resolveTeacherTenant(c, userID)
			return
		}

		school, err := service.ResolveTenantSchool(c.Request.Context(), userID)
		if err != nil {
			config.Logger.Error("tenant resolution failed", zap.Error(err), zap.Uint("user_id", userID))
			abortInternal(c)
			return
		}

		if school == nil {
			method := c.Request.Method
			if c.FullPath() == SchoolProfilePath && (method == http.MethodGet || method == http.MethodPost) {
				c.Next()
				return
			}
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "Profil sekolah belum dibuat", "code": "SCHOOL_NOT_SET_UP"})
			return
		}

		if !setSchool(c, school) {
			return
		}
		c.Next()
	}
}

// resolveTeacherTenant: school = employees.user_id → employee.school_id.
func resolveTeacherTenant(c *gin.Context, userID uint) {
	school, employee, err := service.ResolveTeacherTenant(c.Request.Context(), userID)
	if err != nil {
		config.Logger.Error("teacher tenant resolution failed", zap.Error(err), zap.Uint("user_id", userID))
		abortInternal(c)
		return
	}
	if school == nil || employee == nil {
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "Akun guru tidak terhubung ke sekolah", "code": "SCHOOL_NOT_SET_UP"})
		return
	}

	if employee.ActivatedAt == nil {
		if err := service.MarkTeacherActivated(c.Request.Context(), employee.ID); err != nil {
			// Not fatal for the request; it is retried on the next one.
			config.Logger.Warn("failed to set employees.activated_at", zap.Error(err), zap.String("employee_id", employee.ID.String()))
		}
	}

	if !setSchool(c, school) {
		return
	}
	c.Set(CtxEmployeeID, employee.ID)
	c.Next()
}

func setSchool(c *gin.Context, school *model.School) bool {
	if !school.IsActive {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Sekolah tidak aktif", "code": "SCHOOL_INACTIVE"})
		return false
	}
	c.Set(CtxSchoolID, school.ID)
	c.Set(CtxSchool, school)
	return true
}

// TeacherRouteGuard limits role teacher to the read-only routes in
// teacherAllowedRoutes; other roles pass through. Run after TenantMiddleware.
func TeacherRouteGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetString(CtxRole) == RoleTeacher && !teacherAllowedRoutes[c.Request.Method+" "+c.FullPath()] {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Akses tidak diizinkan untuk role guru", "code": "FORBIDDEN_ROLE"})
			return
		}
		c.Next()
	}
}
