package route

import (
	"net/http"

	"edupilot-assesment/internal/controller"
	"edupilot-assesment/internal/middleware"

	"github.com/gin-gonic/gin"
)

func SetupRoutes(r *gin.Engine) {
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "edupilot-assesment"})
	})

	adminCtrl := controller.NewAdminController()
	schoolCtrl := controller.NewSchoolController()
	studentCtrl := controller.NewStudentController()

	// Platform admin (§13.3)
	admin := r.Group("/assessment/admin", middleware.AuthMiddleware(), middleware.RequireRoles(middleware.RoleAdmin, middleware.RoleSuperAdmin))
	{
		admin.GET("/packages", adminCtrl.GetPackages)
		admin.POST("/packages", adminCtrl.CreatePackage)
		admin.PUT("/packages/:id", adminCtrl.UpdatePackage)
		admin.PATCH("/packages/:id/status", adminCtrl.TogglePackageStatus)

		admin.GET("/vouchers", adminCtrl.GetVouchers)
		admin.POST("/vouchers", adminCtrl.CreateVoucher)
		admin.GET("/vouchers/logs", adminCtrl.GetVoucherLogs)
		admin.PATCH("/vouchers/:id/status", adminCtrl.ToggleVoucherStatus)

		admin.GET("/subscriptions", adminCtrl.GetSubscriptions)
		admin.PATCH("/subscriptions/:id/status", adminCtrl.UpdateSubscriptionStatus)

		admin.GET("/stats", adminCtrl.GetAdminStats)

		admin.GET("/questions", adminCtrl.GetQuestions)
		admin.POST("/questions", adminCtrl.CreateQuestion)
		admin.GET("/questions/:id", adminCtrl.GetQuestion)
		admin.PUT("/questions/:id", adminCtrl.UpdateQuestion)
		admin.DELETE("/questions/:id", adminCtrl.DeleteQuestion)
	}

	// School: managers + Guru BK (§13.4)
	school := r.Group("/assessment/school", middleware.AuthMiddleware(), middleware.SchoolStaff())
	{
		school.GET("/me", schoolCtrl.GetMe)
		school.GET("/subscriptions", schoolCtrl.GetSubscriptions)
		school.GET("/packages", schoolCtrl.GetAvailablePackages)
		school.POST("/claim-voucher", middleware.ManagerOnly(), schoolCtrl.ClaimVoucher)
		school.POST("/purchase", middleware.ManagerOnly(), schoolCtrl.PurchasePackage)

		school.GET("/classes", schoolCtrl.GetClasses)
		school.GET("/students", schoolCtrl.GetStudents)

		school.GET("/sessions", schoolCtrl.GetSessions)
		school.POST("/sessions", schoolCtrl.CreateSession)
		school.GET("/sessions/:id", schoolCtrl.GetSession)
		school.PATCH("/sessions/:id/close", schoolCtrl.CloseSession)
		school.DELETE("/sessions/:id", schoolCtrl.DeleteSession)

		school.GET("/results", schoolCtrl.GetResults)
		school.GET("/results/export", schoolCtrl.ExportResults)
		school.GET("/results/:id", schoolCtrl.GetResult)
		school.GET("/analytics", schoolCtrl.GetAnalytics)

		school.GET("/alerts", schoolCtrl.GetAlerts)
		school.PATCH("/alerts/:id", schoolCtrl.UpdateAlert)
	}

	// Student: enrolled student accounts (§13.5)
	student := r.Group("/assessment/student", middleware.AuthMiddleware(), middleware.EnrolledStudent())
	{
		student.GET("/sessions", studentCtrl.GetMySessions)
		student.GET("/sessions/:id/onboarding", studentCtrl.GetSessionOnboarding)
		student.POST("/sessions/:id/start", studentCtrl.StartAttempt)
		student.GET("/attempts/:attemptId", studentCtrl.GetAttempt)
		student.PUT("/attempts/:attemptId/answers", studentCtrl.SaveAnswers)
		student.POST("/attempts/:attemptId/submit", studentCtrl.SubmitAttempt)
		student.GET("/attempts/:attemptId/status", studentCtrl.GetAttemptStatus)
		student.GET("/attempts/:attemptId/result", studentCtrl.GetMyResult)
		student.GET("/results", studentCtrl.GetMyResults)
	}
}
