package middleware

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/edupilot-instantion-service/internal/config"
	"github.com/edupilot-instantion-service/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

var errForbiddenRole = gin.H{"error": "Akses tidak diizinkan untuk peran Anda", "code": "FORBIDDEN_ROLE"}

// RejectStudentPrincipal blocks student accounts from /instantion/school/* and
// /instantion/admin/* (API_CONTRACT §10.1). Run right after AuthMiddleware.
func RejectStudentPrincipal() gin.HandlerFunc {
	return func(c *gin.Context) {
		if IsStudentPrincipal(c) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Akun siswa tidak dapat mengakses fitur ini", "code": "FORBIDDEN_ROLE"})
			return
		}
		c.Next()
	}
}

// RequireStudentPrincipal allows only student accounts (/instantion/student/*).
func RequireStudentPrincipal() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !IsStudentPrincipal(c) {
			c.AbortWithStatusJSON(http.StatusForbidden, errForbiddenRole)
			return
		}
		c.Next()
	}
}

// StudentContext resolves the student record linked to the account (may be
// none) and stores it plus its school in the context. The first authenticated
// student request of an invited account sets students.activated_at (§10.3).
func StudentContext() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := UserID(c)
		student, school, err := service.ResolveStudentPrincipal(c.Request.Context(), userID)
		if err != nil {
			config.Logger.Error("student resolution failed", zap.Error(err), zap.Uint("user_id", userID))
			abortInternal(c)
			return
		}
		if student != nil {
			if student.ActivatedAt == nil {
				if err := service.MarkStudentActivated(c.Request.Context(), student, userID); err != nil {
					config.Logger.Warn("failed to set students.activated_at", zap.Error(err), zap.String("student_id", student.ID.String()))
				}
			}
			c.Set(CtxStudent, student)
			if school != nil {
				c.Set(CtxSchool, school)
				c.Set(CtxSchoolID, school.ID)
			}
		}
		c.Next()
	}
}

// RequireEnrolledStudent: attendance routes need an active student of an active school.
func RequireEnrolledStudent() gin.HandlerFunc {
	return func(c *gin.Context) {
		st := LinkedStudent(c)
		school := TenantSchool(c)
		if st == nil || school == nil || !school.IsActive || st.Status != "active" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Akun Anda belum terdaftar sebagai siswa aktif di sekolah", "code": "NOT_ENROLLED"})
			return
		}
		c.Next()
	}
}

// UserRateLimiter is a fixed-window limit per authenticated user (Redis), used
// for student join (10/hour) and code claims (10/min). Fails open on Redis errors.
func UserRateLimiter(prefix string, requests int, window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		if os.Getenv("RATE_LIMIT_ENABLED") == "false" || config.RDB == nil {
			c.Next()
			return
		}
		key := fmt.Sprintf("%s:user:%d", prefix, UserID(c))
		ctx := c.Request.Context()
		current, err := config.RDB.Incr(ctx, key).Result()
		if err != nil {
			c.Next()
			return
		}
		if current == 1 {
			config.RDB.Expire(ctx, key, window)
		}
		ttl, _ := config.RDB.TTL(ctx, key).Result()
		if ttl < 0 {
			config.RDB.Expire(ctx, key, window)
			ttl = window
		}
		if current > int64(requests) {
			c.Header("Retry-After", fmt.Sprintf("%d", int(ttl.Seconds())))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "Terlalu banyak percobaan, silakan coba lagi nanti", "code": "RATE_LIMITED"})
			return
		}
		c.Next()
	}
}
