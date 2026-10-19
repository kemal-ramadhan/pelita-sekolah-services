package route

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/edupilot-auth-service/internal/config"
	"github.com/edupilot-auth-service/internal/controller"
	"github.com/edupilot-auth-service/internal/middleware"
	"github.com/edupilot-auth-service/internal/service"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// emailKey extracts "email" from a JSON body for per-email rate limiting.
func emailKey(c *gin.Context) string {
	body, err := middleware.ReadBodyOnce(c)
	if err != nil || len(body) == 0 {
		return ""
	}
	var p struct {
		Email string `json:"email"`
	}
	if json.Unmarshal(body, &p) != nil {
		return ""
	}
	return "email:" + strings.ToLower(strings.TrimSpace(p.Email))
}

func limiter(prefix string, requests int, window time.Duration, perEmail bool) gin.HandlerFunc {
	opts := middleware.RateLimitOptions{
		Prefix:    prefix,
		Requests:  requests,
		Window:    window,
		UseIPOnly: !perEmail,
	}
	if perEmail {
		opts.KeyGetter = emailKey
	}
	return middleware.RateLimiter(opts)
}

func SetupRoutes(r *gin.Engine) {
	if !config.IsProduction() {
		r.GET("/docs/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	}
	r.GET("/health", controller.HealthCheck)

	registerLimiter := limiter("rl:register", 10, time.Hour, false)
	loginLimiter := limiter("rl:login", 10, 5*time.Minute, true)
	verifyLimiter := limiter("rl:verify", 10, 10*time.Minute, true)
	resendLimiter := limiter("rl:resend-otp", 5, 15*time.Minute, true)
	forgotLimiter := limiter("rl:forgot", 5, 15*time.Minute, true)
	resetLimiter := limiter("rl:reset", 10, 15*time.Minute, false)
	activateLimiter := limiter("rl:activate", 10, 15*time.Minute, false)
	activateCheckLimiter := limiter("rl:activate-check", 10, 15*time.Minute, false)
	refreshLimiter := limiter("rl:refresh", 30, 5*time.Minute, false)
	googleLimiter := limiter("rl:google", 20, 10*time.Minute, false)

	adminOnly := middleware.RequireRole(service.RoleAdmin, service.RoleSuperAdmin)

	auth := r.Group("/auth")
	{
		// Public
		auth.POST("/register", registerLimiter, controller.Register)
		auth.POST("/verify", verifyLimiter, controller.VerifyEmail)
		auth.POST("/resend-otp", resendLimiter, controller.SendOtp)
		auth.POST("/login", loginLimiter, controller.Login)
		auth.POST("/refresh", refreshLimiter, controller.Refresh)
		auth.POST("/forgot-password", forgotLimiter, controller.ForgotPassword)
		auth.POST("/reset-password", resetLimiter, controller.ResetPassword)
		auth.GET("/activate", activateCheckLimiter, controller.CheckActivation)
		auth.POST("/activate", activateLimiter, controller.Activate)
		auth.GET("/google/login", googleLimiter, controller.GoogleLogin)
		auth.GET("/google/callback", googleLimiter, controller.GoogleCallback)
	}

	authProtected := auth.Group("")
	authProtected.Use(middleware.AuthMiddleware())
	{
		authProtected.GET("/me", controller.Me)
		authProtected.PUT("/profile", controller.UpdateProfile)
		authProtected.PUT("/photo-profile", controller.PhotoProfile)
		authProtected.PUT("/cover", controller.Cover)
		authProtected.POST("/logout", controller.Logout)
	}

	rbac := auth.Group("/rbac")
	rbac.Use(middleware.AuthMiddleware(), adminOnly)
	{
		rbac.POST("/grant", controller.GrantAbility)
		rbac.POST("/role/assign", controller.AssignRole)
		rbac.POST("/role/permission", middleware.RequireRole(service.RoleSuperAdmin), controller.AddAbilityToRole)
	}

	admin := auth.Group("/admin")
	admin.Use(middleware.AuthMiddleware(), adminOnly)
	{
		admin.POST("/users", controller.AdminCreateUser)
		admin.PUT("/users/:id/profile", controller.AdminUpdateUserProfile)
		admin.PUT("/users/:id/role", controller.AdminChangeUserRole)
	}

	other := r.Group("/other")
	other.Use(middleware.AuthMiddleware(), adminOnly)
	{
		other.GET("/statistik", controller.GetStatistikCustomers)
		other.GET("/customers", controller.GetCustomersUsers)
		other.GET("/customers/:customer_id", controller.GetCustomerUserDetail)
	}

	internal := r.Group("/internal")
	internal.Use(middleware.InternalAuthMiddleware())
	{
		internal.POST("/users", controller.InternalCreateUser)
		internal.DELETE("/users/:id", controller.InternalDeleteUser)
		internal.POST("/users/invite", controller.InternalInviteUser)
		internal.POST("/users/:id/resend-invite", controller.InternalResendInvite)
	}
}
