package route

import (
	"os"
	"time"

	"github.com/edupilot-instantion-service/internal/controller"
	"github.com/edupilot-instantion-service/internal/middleware"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// TeacherImportPath gets a larger body limit than the global 1 MB.
const TeacherImportPath = "/instantion/school/teachers/import"

// SchoolLogoPath gets a 3 MB body limit (2 MB image + multipart overhead).
const SchoolLogoPath = "/instantion/school/profile/logo"

// Student routes with larger bodies (API_CONTRACT §8.6, §8.7, §8.9).
const (
	StudentImportPath   = "/instantion/school/students/import"
	StudentPhotoPath    = "/instantion/school/students/:id/photo"
	StudentDocumentPath = "/instantion/school/students/:id/documents"
	// StudentsPath / StudentPath: the edit form may echo identity.photo (data URL, ignored).
	StudentsPath = "/instantion/school/students"
	StudentPath  = "/instantion/school/students/:id"
)

func SetupRoutes(r *gin.Engine) {
	if os.Getenv("APP_ENV") != "production" {
		r.GET("/docs/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	}
	r.GET("/health", controller.HealthCheck)

	// Tenant (school) routes: institution/customer roles, plus role teacher which is
	// limited to GET /profile and GET /me by TeacherRouteGuard (API_CONTRACT §7.1).
	school := r.Group("/instantion/school")
	school.Use(
		middleware.AuthMiddleware(),
		middleware.RejectStudentPrincipal(),
		middleware.RequireRoles(middleware.RoleInstitution, middleware.RoleCustomer, middleware.RoleTeacher),
		middleware.TenantMiddleware(),
		middleware.TeacherRouteGuard(),
	)
	{
		school.GET("/profile", controller.GetSchoolProfile)
		school.POST("/profile", controller.CreateSchoolProfile)
		school.PUT("/profile", controller.UpdateSchoolProfile)
		school.PUT("/profile/logo", controller.UploadSchoolLogo)
		school.DELETE("/profile/logo", controller.DeleteSchoolLogo)
		school.GET("/me", controller.GetMyTeacher)
		school.GET("/me/schedule", controller.GetMySchedule)

		school.GET("/teachers", controller.GetTeachers)
		school.POST("/teachers", controller.CreateTeacher)
		school.GET("/teachers/import/template", controller.ImportTeachersTemplate)
		school.POST("/teachers/import", controller.ImportTeachers)
		school.GET("/teachers/:id", controller.GetTeacher)
		school.PUT("/teachers/:id", controller.UpdateTeacher)
		school.DELETE("/teachers/:id", controller.DeleteTeacher)
		school.POST("/teachers/:id/invite", controller.InviteTeacher)

		// Jabatan / tugas tambahan (employee positions), managers only.
		school.GET("/employee-positions", controller.GetEmployeePositions)
		school.POST("/employee-positions", controller.CreateEmployeePosition)
		school.PUT("/employee-positions/:id", controller.UpdateEmployeePosition)
		school.DELETE("/employee-positions/:id", controller.DeleteEmployeePosition)

		// Student accounts & join requests (§10.3, §10.4)
		school.POST("/students/:id/invite", controller.InviteStudent)
		school.GET("/join-requests", controller.GetJoinRequests)
		school.GET("/join-requests/count", controller.GetJoinRequestCount)
		school.POST("/join-requests/:id/approve", controller.ApproveJoinRequest)
		school.POST("/join-requests/:id/reject", controller.RejectJoinRequest)

		// Reference data (§8.1)
		school.GET("/references", controller.GetReferences)
		school.GET("/document-types", controller.GetDocumentTypes)

		// Academic years & semesters (§8.2)
		school.GET("/academic-years", controller.GetAcademicYears)
		school.POST("/academic-years", controller.CreateAcademicYear)
		school.PUT("/academic-years/:id", controller.UpdateAcademicYear)
		school.DELETE("/academic-years/:id", controller.DeleteAcademicYear)
		school.POST("/academic-years/:id/activate", controller.ActivateAcademicYear)
		school.POST("/semesters/:id/activate", controller.ActivateSemester)

		// Grade levels (§8.3)
		school.GET("/grade-levels", controller.GetGradeLevels)
		school.POST("/grade-levels", controller.CreateGradeLevel)
		school.PUT("/grade-levels/:id", controller.UpdateGradeLevel)
		school.DELETE("/grade-levels/:id", controller.DeleteGradeLevel)
		school.POST("/grade-levels/preset", controller.ApplyGradePreset)

		// Majors (§8.4)
		school.GET("/majors", controller.GetMajors)
		school.POST("/majors", controller.CreateMajor)
		school.PUT("/majors/:id", controller.UpdateMajor)
		school.DELETE("/majors/:id", controller.DeleteMajor)

		// Classes (§8.5)
		school.GET("/classes", controller.GetClasses)
		school.POST("/classes", controller.CreateClass)
		school.POST("/classes/bulk", controller.CreateBulkClasses)
		school.POST("/classes/copy", controller.CopyClasses)
		school.GET("/classes/:id", controller.GetClass)
		school.PUT("/classes/:id", controller.UpdateClass)
		school.DELETE("/classes/:id", controller.DeleteClass)
		school.GET("/classes/:id/students", controller.GetClassStudents)
		school.POST("/classes/:id/members", controller.AddClassMembers)
		school.DELETE("/classes/:id/members", controller.RemoveClassMembers)

		// Students (§8.6)
		school.GET("/students", controller.GetStudents)
		school.POST("/students", controller.CreateStudent)
		school.GET("/students/stats", controller.GetStudentStats)
		school.GET("/students/import/template", controller.DownloadStudentTemplate)
		school.POST("/students/import", controller.ImportStudents)
		school.GET("/students/export", controller.ExportStudents)
		school.GET("/students/:id", controller.GetStudent)
		school.PUT("/students/:id", controller.UpdateStudent)
		school.DELETE("/students/:id", controller.DeleteStudent)
		school.PATCH("/students/:id/status", controller.UpdateStudentStatus)
		school.PUT("/students/:id/photo", controller.UploadStudentPhoto)
		school.DELETE("/students/:id/photo", controller.DeleteStudentPhoto)

		// Documents (§8.7)
		school.GET("/students/:id/documents", controller.GetStudentDocuments)
		school.POST("/students/:id/documents", controller.UploadStudentDocument)
		school.GET("/students/:id/documents/:docId/download", controller.DownloadStudentDocument)
		school.DELETE("/students/:id/documents/:docId", controller.DeleteStudentDocument)

		// History & Promotions (§8.8)
		school.GET("/students/:id/history", controller.GetStudentHistory)
		school.POST("/students/:id/placement", controller.PlaceStudent)
		school.GET("/promotions/preview", controller.PreviewPromotions)
		school.POST("/promotions", controller.ExecutePromotions)

		// Subjects & Teacher-Subject Mapping
		school.GET("/subjects", controller.GetSubjects)
		school.POST("/subjects", controller.CreateSubject)
		school.POST("/subjects/preset", controller.ApplySubjectPresets)
		school.GET("/subjects/:id", controller.GetSubject)
		school.PUT("/subjects/:id", controller.UpdateSubject)
		school.DELETE("/subjects/:id", controller.DeleteSubject)
		school.GET("/subjects/:id/teachers", controller.GetSubjectTeachers)
		school.POST("/subjects/:id/teachers", controller.AssignTeachersToSubject)
		school.GET("/teachers/:id/subjects", controller.GetTeacherSubjects)
		school.POST("/teachers/:id/subjects", controller.AssignSubjectsToTeacher)

		// Class Subjects (Mapel per Rombel)
		school.GET("/classes/:id/subjects", controller.GetClassSubjects)
		school.POST("/classes/:id/subjects", controller.AssignClassSubject)
		school.POST("/classes/:id/subjects/bulk", controller.BulkAssignClassSubjects)
		school.DELETE("/classes/:id/subjects/:subject_id", controller.DeleteClassSubject)

		// Schedule Periods (Jam Pelajaran)
		school.GET("/schedule-periods", controller.GetSchedulePeriods)
		school.POST("/schedule-periods", controller.CreateSchedulePeriod)
		school.POST("/schedule-periods/preset", controller.ApplyPeriodPresets)
		school.PUT("/schedule-periods/:id", controller.UpdateSchedulePeriod)
		school.DELETE("/schedule-periods/:id", controller.DeleteSchedulePeriod)

		// Schedules & Timetable Grid
		school.GET("/schedules", controller.GetSchedules)
		school.POST("/schedules", controller.CreateSchedule)
		school.PUT("/schedules/:id", controller.UpdateSchedule)
		school.DELETE("/schedules/:id", controller.DeleteSchedule)
		school.POST("/schedules/check-conflict", controller.CheckScheduleConflict)
		school.GET("/schedules/grid", controller.GetTimetableGrid)

		// Attendance System
		school.GET("/attendance/config", controller.GetAttendanceConfig)
		school.PUT("/attendance/config", controller.UpdateAttendanceConfig)
		school.GET("/attendance/shifts", controller.GetAttendanceShifts)
		school.POST("/attendance/shifts", controller.CreateAttendanceShift)
		school.PUT("/attendance/shifts/:id", controller.UpdateAttendanceShift)
		school.DELETE("/attendance/shifts/:id", controller.DeleteAttendanceShift)
		school.GET("/attendance/class-shifts", controller.GetClassAttendanceShifts)
		school.POST("/attendance/class-shifts", controller.AssignClassAttendanceShift)
		school.DELETE("/attendance/class-shifts/:class_id", controller.UnassignClassAttendanceShift)

		// Daily Attendance
		school.GET("/attendance/me/today", controller.GetTeacherTodayAttendance)
		school.POST("/attendance/me/check-in", controller.TeacherCheckIn)
		school.POST("/attendance/me/check-out", controller.TeacherCheckOut)
		school.GET("/attendance/daily", controller.ListDailyAttendance)
		school.GET("/attendance/daily/summary", controller.GetDailyAttendanceSummary)
		school.POST("/attendance/daily/manual", controller.RecordManualDailyAttendance)

		// Classroom Sessions & KBM
		school.GET("/attendance/teaching/today", controller.GetTodayTeachingSessions)
		school.POST("/attendance/teaching/start", controller.StartTeachingSession)
		school.POST("/attendance/teaching/:id/end", controller.EndTeachingSession)
		school.POST("/attendance/teaching/:id/token", controller.RefreshTokenTeachingSession)
		school.GET("/attendance/teaching/:id", controller.GetTeachingSessionDetail)
		school.PUT("/attendance/teaching/:id/students", controller.SaveTeachingSessionStudents)
		school.PUT("/attendance/teaching/:id/journal", controller.UpdateTeachingJournal)
		school.GET("/attendance/live-monitor", controller.GetLiveClassMonitor)

		// Bimbingan Konseling (BK), API_CONTRACT §12. Teachers reach these via the
		// allowlist, but the controller enforces the Guru BK flag (403 NOT_COUNSELOR).
		school.GET("/counseling/summary", controller.GetCounselingSummary)
		school.GET("/counseling/counselors", controller.ListCounselors)
		school.GET("/counseling/students", controller.SearchCounselingStudents)
		school.POST("/counseling/teachers/:id/toggle", controller.ToggleTeacherCounselor)
		school.GET("/counseling/sessions", controller.ListCounselingSessions)
		school.POST("/counseling/sessions", controller.CreateCounselingSession)
		school.GET("/counseling/sessions/:id", controller.GetCounselingSession)
		school.PUT("/counseling/sessions/:id/status", controller.UpdateCounselingSessionStatus)
		school.POST("/counseling/sessions/:id/outcome", controller.SaveCounselingOutcome)
		school.GET("/counseling/events", controller.ListCounselingEvents)
		school.POST("/counseling/events", controller.CreateCounselingEvent)
		school.GET("/counseling/events/:id", controller.GetCounselingEvent)
		school.POST("/counseling/events/:id/attend", controller.AttendCounselingEvent)
		school.POST("/counseling/events/:id/conclusion", controller.ConcludeCounselingEvent)
		school.GET("/counseling/journals", controller.ListCounselingJournals)
		school.POST("/counseling/journals", controller.CreateCounselingJournal)
		school.PUT("/counseling/journals/:id", controller.UpdateCounselingJournal)
		school.DELETE("/counseling/journals/:id", controller.DeleteCounselingJournal)
	}

	// Student app routes (§10.5): student principals only. /me, /join and
	// /notifications work without a school; attendance needs an enrolled student.
	student := r.Group("/instantion/student")
	student.Use(
		middleware.AuthMiddleware(),
		middleware.RequireStudentPrincipal(),
		middleware.StudentContext(),
	)
	{
		student.GET("/me", controller.GetStudentMe)
		student.POST("/join", middleware.UserRateLimiter("rl:instantion:student-join", 10, time.Hour), controller.StudentJoinSchool)
		student.DELETE("/join", controller.StudentCancelJoin)
		student.GET("/notifications", controller.GetMyNotifications)
		student.GET("/notifications/unread-count", controller.GetMyUnreadNotificationCount)
		student.POST("/notifications/read-all", controller.ReadAllMyNotifications)
		student.POST("/notifications/:id/read", controller.ReadMyNotification)

		att := student.Group("/attendance", middleware.RequireEnrolledStudent())
		att.GET("/today", controller.StudentAttendanceToday)
		att.POST("/check-in", controller.StudentCheckIn)
		att.POST("/check-out", controller.StudentCheckOut)
		att.POST("/claim", middleware.UserRateLimiter("rl:instantion:student-claim", 10, time.Minute), controller.StudentClaimSession)
		att.GET("/history", controller.StudentAttendanceHistory)

		counseling := student.Group("/counseling", middleware.RequireEnrolledStudent())
		counseling.GET("/counselors", controller.StudentListCounselors)
		counseling.GET("/peers", controller.StudentListPeers)
		counseling.GET("/sessions", controller.StudentListSessions)
		counseling.POST("/sessions", middleware.UserRateLimiter("rl:instantion:student-counseling", 10, time.Hour), controller.StudentCreateSession)
		counseling.GET("/sessions/:id", controller.StudentGetSession)
		counseling.GET("/events", controller.StudentListEvents)
		counseling.POST("/events/:id/attend", controller.StudentAttendEvent)
	}

	// Service-to-service routes for edupilot-assesment (API_CONTRACT §13.1).
	// The gateway 404s /internal/*; X-Gateway-Auth = GATEWAY_SECRET is required
	// (fails closed when the secret is unset).
	internal := r.Group("/internal/assessment", middleware.RequireInternalGateway())
	{
		internal.GET("/principal", controller.InternalAssessmentPrincipal)
		internal.POST("/students", controller.InternalAssessmentStudents)
		internal.GET("/classes", controller.InternalAssessmentClasses)
		internal.POST("/notify", controller.InternalAssessmentNotify)
	}

	// Platform admin routes.
	admin := r.Group("/instantion/admin")
	admin.Use(
		middleware.AuthMiddleware(),
		middleware.RejectStudentPrincipal(),
		middleware.RequireRoles(middleware.RoleAdmin, middleware.RoleSuperAdmin),
	)
	{
		admin.GET("/stats", controller.AdminGetStats)
		admin.GET("/schools", controller.AdminGetSchools)
		admin.POST("/schools", controller.AdminCreateSchool)
		admin.GET("/schools/:id", controller.AdminGetSchool)
		admin.PUT("/schools/:id", controller.AdminUpdateSchool)
		admin.PATCH("/schools/:id/status", controller.AdminToggleSchoolStatus)
		admin.GET("/schools/:id/accounts", controller.AdminGetLinkedAccounts)
		admin.POST("/schools/:id/accounts", controller.AdminLinkAccountToSchool)
		admin.DELETE("/schools/:id/accounts", controller.AdminUnlinkAccount)
	}
}
