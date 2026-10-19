package controller

import (
	"net/http"
	"time"

	"github.com/edupilot-instantion-service/internal/dto"
	"github.com/edupilot-instantion-service/internal/middleware"
	"github.com/edupilot-instantion-service/internal/service"
	"github.com/gin-gonic/gin"
)

// ==================== /instantion/student/* (API_CONTRACT §10.5) ====================

// GetStudentMe godoc
// @Summary Student account status, school, linked student and pending join request
// @Tags Student
// @Security Bearer
// @Router /instantion/student/me [get]
func GetStudentMe(c *gin.Context) {
	me, err := service.GetStudentMe(c.Request.Context(), middleware.UserID(c), middleware.LinkedStudent(c), middleware.TenantSchool(c))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Success", "data": me})
}

// StudentJoinSchool godoc
// @Summary Request to join a school with its code (10/hour/user)
// @Tags Student
// @Security Bearer
// @Router /instantion/student/join [post]
func StudentJoinSchool(c *gin.Context) {
	var req dto.JoinSchoolRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}
	jr, err := service.CreateJoinRequest(c.Request.Context(), middleware.UserID(c), c.GetString(middleware.CtxEmail), req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "Permintaan bergabung berhasil dikirim", "data": jr})
}

// StudentCancelJoin godoc
// @Summary Cancel the pending join request
// @Tags Student
// @Security Bearer
// @Router /instantion/student/join [delete]
func StudentCancelJoin(c *gin.Context) {
	if err := service.CancelJoinRequest(c.Request.Context(), middleware.UserID(c)); err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Permintaan bergabung dibatalkan"})
}

// StudentAttendanceToday godoc
// @Summary Today's school attendance, location, targets and class sessions
// @Tags Student
// @Security Bearer
// @Router /instantion/student/attendance/today [get]
func StudentAttendanceToday(c *gin.Context) {
	data, err := attendanceService.StudentToday(c.Request.Context(), middleware.LinkedStudent(c), middleware.TenantSchool(c), time.Now())
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Success", "data": data})
}

// StudentCheckIn godoc
// @Summary Student daily check-in (GPS geofence)
// @Tags Student
// @Security Bearer
// @Router /instantion/student/attendance/check-in [post]
func StudentCheckIn(c *gin.Context) {
	var req dto.StudentGPSRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}
	daily, err := attendanceService.StudentCheckIn(c.Request.Context(), middleware.LinkedStudent(c), middleware.TenantSchool(c), &req, time.Now())
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Presensi kedatangan berhasil dicatat", "data": daily})
}

// StudentCheckOut godoc
// @Summary Student daily check-out (GPS geofence)
// @Tags Student
// @Security Bearer
// @Router /instantion/student/attendance/check-out [post]
func StudentCheckOut(c *gin.Context) {
	var req dto.StudentGPSRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}
	daily, err := attendanceService.StudentCheckOut(c.Request.Context(), middleware.LinkedStudent(c), middleware.TenantSchool(c), &req, time.Now())
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Presensi kepulangan berhasil dicatat", "data": daily})
}

// StudentClaimSession godoc
// @Summary Claim class-session attendance with the 6-char code (10/min/user)
// @Tags Student
// @Security Bearer
// @Router /instantion/student/attendance/claim [post]
func StudentClaimSession(c *gin.Context) {
	var req dto.StudentClaimRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}
	data, err := attendanceService.StudentClaim(c.Request.Context(), middleware.LinkedStudent(c), middleware.TenantSchool(c), &req, time.Now())
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Kehadiran berhasil dikonfirmasi", "data": data})
}

// StudentAttendanceHistory godoc
// @Summary Monthly attendance history (?month=YYYY-MM)
// @Tags Student
// @Security Bearer
// @Router /instantion/student/attendance/history [get]
func StudentAttendanceHistory(c *gin.Context) {
	data, err := attendanceService.StudentHistory(c.Request.Context(), middleware.LinkedStudent(c), middleware.TenantSchool(c), c.Query("month"))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Success", "data": data})
}

// GetMyNotifications godoc
// @Summary Notifications of the logged-in student (paginated, newest first)
// @Tags Student
// @Security Bearer
// @Router /instantion/student/notifications [get]
func GetMyNotifications(c *gin.Context) {
	q := dto.ParsePageQuery(c.Query("page"), c.Query("limit"), "")
	list, meta, err := service.ListNotifications(c.Request.Context(), middleware.UserID(c), q)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Success", "data": list, "meta": meta})
}

// GetMyUnreadNotificationCount godoc
// @Router /instantion/student/notifications/unread-count [get]
func GetMyUnreadNotificationCount(c *gin.Context) {
	n, err := service.UnreadNotificationCount(c.Request.Context(), middleware.UserID(c))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Success", "data": gin.H{"count": n}})
}

// ReadMyNotification godoc
// @Router /instantion/student/notifications/{id}/read [post]
func ReadMyNotification(c *gin.Context) {
	id, ok := parseUUIDParam(c, "id", "ID notifikasi")
	if !ok {
		return
	}
	n, err := service.MarkNotificationRead(c.Request.Context(), middleware.UserID(c), id)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Success", "data": n})
}

// ReadAllMyNotifications godoc
// @Router /instantion/student/notifications/read-all [post]
func ReadAllMyNotifications(c *gin.Context) {
	n, err := service.MarkAllNotificationsRead(c.Request.Context(), middleware.UserID(c))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Success", "data": gin.H{"updated": n}})
}

// ==================== School side: join requests & student invite (§10.3, §10.4) ====================

// GetJoinRequests godoc
// @Summary Join requests of the school with matching-student suggestions
// @Tags JoinRequests
// @Security Bearer
// @Router /instantion/school/join-requests [get]
func GetJoinRequests(c *gin.Context) {
	school := middleware.TenantSchool(c)
	if school == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Profil sekolah belum dibuat", "code": "SCHOOL_NOT_SET_UP"})
		return
	}
	q := dto.ParsePageQuery(c.Query("page"), c.Query("limit"), c.Query("search"))
	list, meta, err := service.ListJoinRequests(c.Request.Context(), school, c.Query("status"), q)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Success", "data": list, "meta": meta})
}

// GetJoinRequestCount godoc
// @Router /instantion/school/join-requests/count [get]
func GetJoinRequestCount(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	n, err := service.CountJoinRequests(c.Request.Context(), schoolID, c.Query("status"))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Success", "data": gin.H{"count": n}})
}

// ApproveJoinRequest godoc
// @Router /instantion/school/join-requests/{id}/approve [post]
func ApproveJoinRequest(c *gin.Context) {
	school := middleware.TenantSchool(c)
	if school == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Profil sekolah belum dibuat", "code": "SCHOOL_NOT_SET_UP"})
		return
	}
	id, ok := parseUUIDParam(c, "id", "ID permintaan")
	if !ok {
		return
	}
	var req dto.JoinApproveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}
	jr, err := service.ApproveJoinRequest(c.Request.Context(), school, middleware.UserID(c), id, req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Permintaan bergabung disetujui", "data": jr})
}

// RejectJoinRequest godoc
// @Router /instantion/school/join-requests/{id}/reject [post]
func RejectJoinRequest(c *gin.Context) {
	school := middleware.TenantSchool(c)
	if school == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Profil sekolah belum dibuat", "code": "SCHOOL_NOT_SET_UP"})
		return
	}
	id, ok := parseUUIDParam(c, "id", "ID permintaan")
	if !ok {
		return
	}
	var req dto.JoinRejectRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			respondBindError(c, err)
			return
		}
	}
	jr, err := service.RejectJoinRequest(c.Request.Context(), school, middleware.UserID(c), id, req.Reason)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Permintaan bergabung ditolak", "data": jr})
}

// InviteStudent godoc
// @Summary Invite (or re-invite) a student to activate a student account
// @Description none → creates the account; invited → resends; active → 409 ALREADY_ACTIVE. Max 1 per student per 60s (429).
// @Tags Students
// @Security Bearer
// @Router /instantion/school/students/{id}/invite [post]
func InviteStudent(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "id", "ID siswa")
	if !ok {
		return
	}
	st, warning, err := service.InviteStudent(c.Request.Context(), schoolID, tenantSchoolName(c), id)
	if err != nil {
		respondError(c, err)
		return
	}
	body := gin.H{"message": "Undangan berhasil dikirim", "data": st}
	if warning != "" {
		body["warning"] = warning
	}
	c.JSON(http.StatusOK, body)
}
