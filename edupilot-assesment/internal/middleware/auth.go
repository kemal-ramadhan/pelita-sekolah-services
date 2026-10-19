package middleware

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"edupilot-assesment/internal/client"
	"edupilot-assesment/internal/config"
	"edupilot-assesment/internal/utils"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	CtxUserID          = "user_id"
	CtxEmail           = "email"
	CtxRole            = "role"
	CtxCustomerRole    = "customer_role"
	CtxIssuedAt        = "iat"
	CtxPrincipal       = "principal"
	CtxGatewayVerified = "gateway_verified"
)

const (
	RoleAdmin            = "admin"
	RoleSuperAdmin       = "super_admin"
	RoleInstitution      = "institution"
	RoleCustomer         = "customer"
	RoleTeacher          = "teacher"
	CustomerRoleStudents = "students"

	KindManager   = "manager"
	KindCounselor = "counselor"
	KindTeacher   = "teacher"
	KindStudent   = "student"
	KindNone      = "none"
)

func abort(c *gin.Context, status int, code, msg string) {
	c.AbortWithStatusJSON(status, gin.H{"error": msg, "code": code})
}

// GatewayAuthMiddleware enforces X-Gateway-Auth (constant-time) when GATEWAY_SECRET
// is set (contract §4). /health is skipped.
func GatewayAuthMiddleware() gin.HandlerFunc {
	expected := []byte(os.Getenv("GATEWAY_SECRET"))
	return func(c *gin.Context) {
		if len(expected) == 0 || c.Request.URL.Path == "/health" {
			c.Next()
			return
		}
		if subtle.ConstantTimeCompare([]byte(c.GetHeader("X-Gateway-Auth")), expected) != 1 {
			// 502, not 401: a secret mismatch is a server misconfiguration and must not
			// make the apps treat the user's session as expired.
			abort(c, http.StatusBadGateway, "UNAUTHORIZED_GATEWAY", "unauthorized gateway")
			return
		}
		c.Set(CtxGatewayVerified, true)
		c.Next()
	}
}

// CORSMiddleware echoes the Origin only when it is in CORS_ALLOW_ORIGINS.
func CORSMiddleware() gin.HandlerFunc {
	allowed := map[string]bool{}
	for _, o := range strings.Split(config.Getenv("CORS_ALLOW_ORIGINS", "http://localhost:5173,http://localhost:5174,http://localhost:5175"), ",") {
		if o = strings.TrimRight(strings.TrimSpace(o), "/"); o != "" {
			allowed[o] = true
		}
	}
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Add("Vary", "Origin")
		origin := c.GetHeader("Origin")
		ok := allowed[origin]
		if origin != "" && ok {
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Requested-With, X-Request-ID")
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Set("Access-Control-Expose-Headers", "Content-Disposition")
			h.Set("Access-Control-Max-Age", "600")
		}
		if c.Request.Method == http.MethodOptions && origin != "" {
			if ok {
				c.AbortWithStatus(http.StatusNoContent)
			} else {
				c.AbortWithStatus(http.StatusForbidden)
			}
			return
		}
		c.Next()
	}
}

// AuthMiddleware validates the access token (HS256, typ=access, revocation).
// Client-supplied identity headers (X-School-ID / X-Student-ID) are ignored.
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
		if !ok || strings.TrimSpace(token) == "" {
			abort(c, http.StatusUnauthorized, "UNAUTHORIZED", "Token tidak ditemukan")
			return
		}
		claims, err := utils.ParseAccessToken(strings.TrimSpace(token), utils.JWTSecret())
		if err != nil {
			abort(c, http.StatusUnauthorized, "UNAUTHORIZED", "Token tidak valid atau kedaluwarsa")
			return
		}
		iat := int64(0)
		if claims.IssuedAt != nil {
			iat = claims.IssuedAt.Unix()
		}
		if isRevoked(c.Request.Context(), claims.UserID, iat) {
			abort(c, http.StatusUnauthorized, "TOKEN_REVOKED", "Sesi telah berakhir, silakan masuk kembali")
			return
		}
		c.Set(CtxUserID, claims.UserID)
		c.Set(CtxEmail, claims.Email)
		c.Set(CtxRole, claims.Role)
		c.Set(CtxCustomerRole, claims.CustomerRole)
		c.Set(CtxIssuedAt, iat)
		c.Next()
	}
}

func isRevoked(ctx context.Context, userID uint, issuedAt int64) bool {
	if config.RedisClient == nil {
		return false
	}
	val, err := config.RedisClient.Get(ctx, fmt.Sprintf("auth:revoked_before:user:%d", userID)).Result()
	if err != nil {
		return false
	}
	revokedBefore, err := strconv.ParseInt(val, 10, 64)
	return err == nil && issuedAt < revokedBefore
}

// UserID returns the authenticated user id.
func UserID(c *gin.Context) uint {
	v, _ := c.Get(CtxUserID)
	id, _ := v.(uint)
	return id
}

// IsStudentPrincipal: role customer + customer_role students (§10.1).
func IsStudentPrincipal(c *gin.Context) bool {
	return c.GetString(CtxRole) == RoleCustomer && c.GetString(CtxCustomerRole) == CustomerRoleStudents
}

// RequireRoles allows only the listed JWT roles (admin group: 403 FORBIDDEN).
func RequireRoles(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		role := c.GetString(CtxRole)
		if !IsStudentPrincipal(c) {
			for _, r := range roles {
				if role == r {
					c.Next()
					return
				}
			}
		}
		abort(c, http.StatusForbidden, "FORBIDDEN", "Akses ditolak: peran tidak berwenang")
	}
}

// GetPrincipal returns the instantion principal resolved for this request.
func GetPrincipal(c *gin.Context) *client.Principal {
	v, _ := c.Get(CtxPrincipal)
	p, _ := v.(*client.Principal)
	return p
}

const principalTTL = 60 * time.Second

// resolvePrincipal asks instantion (cached in Redis 60 s per user+iat, §13.1).
func resolvePrincipal(c *gin.Context) (*client.Principal, error) {
	userID := UserID(c)
	iat, _ := c.Get(CtxIssuedAt)
	key := fmt.Sprintf("asm:principal:%d:%v", userID, iat)
	ctx := c.Request.Context()
	if config.RedisClient != nil {
		if s, err := config.RedisClient.Get(ctx, key).Result(); err == nil {
			var p client.Principal
			if json.Unmarshal([]byte(s), &p) == nil {
				return &p, nil
			}
		} else if !errors.Is(err, redis.Nil) {
			config.Logger.Warn("principal cache read failed", zap.Error(err))
		}
	}
	if client.Default == nil {
		return nil, client.ErrUnavailable
	}
	p, err := client.Default.Principal(ctx, userID, c.GetString(CtxRole), c.GetString(CtxCustomerRole))
	if err != nil {
		return nil, err
	}
	if config.RedisClient != nil {
		if b, err := json.Marshal(p); err == nil {
			_ = config.RedisClient.Set(ctx, key, b, principalTTL).Err()
		}
	}
	return p, nil
}

func principalOrAbort(c *gin.Context) *client.Principal {
	p, err := resolvePrincipal(c)
	if err != nil {
		config.Logger.Error("principal resolution failed", zap.Error(err), zap.Uint("user_id", UserID(c)))
		abort(c, http.StatusServiceUnavailable, "UPSTREAM_UNAVAILABLE", "Layanan data sekolah sedang tidak tersedia, coba lagi")
		return nil
	}
	return p
}

// SchoolStaff guards /assessment/school/*: managers and Guru BK only (§13.2).
func SchoolStaff() gin.HandlerFunc {
	return func(c *gin.Context) {
		if IsStudentPrincipal(c) {
			abort(c, http.StatusForbidden, "FORBIDDEN_ROLE", "Akun siswa tidak dapat mengakses menu ini")
			return
		}
		switch c.GetString(CtxRole) {
		case RoleInstitution, RoleCustomer, RoleTeacher:
		default:
			abort(c, http.StatusForbidden, "FORBIDDEN", "Akses ditolak: peran tidak berwenang")
			return
		}
		p := principalOrAbort(c)
		if p == nil {
			return
		}
		switch {
		case p.Kind == KindNone || p.SchoolID == nil:
			abort(c, http.StatusForbidden, "SCHOOL_NOT_SET_UP", "Akun belum terhubung ke sekolah")
			return
		case !p.SchoolActive:
			abort(c, http.StatusForbidden, "SCHOOL_INACTIVE", "Sekolah tidak aktif")
			return
		case p.Kind == KindTeacher:
			abort(c, http.StatusForbidden, "NOT_COUNSELOR", "Menu asesmen hanya untuk Guru BK dan pengelola sekolah")
			return
		case p.Kind != KindManager && p.Kind != KindCounselor:
			abort(c, http.StatusForbidden, "FORBIDDEN", "Akses ditolak: peran tidak berwenang")
			return
		}
		c.Set(CtxPrincipal, p)
		c.Next()
	}
}

// ManagerOnly: billing routes (claim/purchase), §13.2.
func ManagerOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		if p := GetPrincipal(c); p == nil || p.Kind != KindManager {
			abort(c, http.StatusForbidden, "MANAGER_ONLY", "Hanya pengelola sekolah yang dapat melakukan aksi ini")
			return
		}
		c.Next()
	}
}

// EnrolledStudent guards /assessment/student/* (§13.2).
func EnrolledStudent() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !IsStudentPrincipal(c) {
			abort(c, http.StatusForbidden, "STUDENT_ONLY", "Hanya akun siswa yang dapat mengakses menu ini")
			return
		}
		p := principalOrAbort(c)
		if p == nil {
			return
		}
		if p.Kind != KindStudent || p.StudentID == nil || p.SchoolID == nil || !p.SchoolActive || p.StudentStatus != "active" {
			abort(c, http.StatusForbidden, "NOT_ENROLLED", "Akun Anda belum terdaftar sebagai siswa aktif di sekolah")
			return
		}
		c.Set(CtxPrincipal, p)
		c.Next()
	}
}
