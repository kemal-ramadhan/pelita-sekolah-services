package controller

import (
	"net/http"
	"time"

	"github.com/edupilot-instantion-service/internal/dto"
	"github.com/edupilot-instantion-service/internal/middleware"
	"github.com/edupilot-instantion-service/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

var attendanceService = service.NewAttendanceService(nil)

// ==================== Configuration & Shifts ====================

// GetAttendanceConfig godoc
// @Summary Get school attendance configuration
// @Tags Attendance
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /instantion/school/attendance/config [get]
func GetAttendanceConfig(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}

	cfg, err := attendanceService.GetOrCreateConfig(c.Request.Context(), schoolID)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Success",
		"data":    cfg,
	})
}

// UpdateAttendanceConfig godoc
// @Summary Update school attendance configuration
// @Tags Attendance
// @Accept json
// @Produce json
// @Router /instantion/school/attendance/config [put]
func UpdateAttendanceConfig(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}

	var req dto.AttendanceConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	cfg, err := attendanceService.UpdateConfig(c.Request.Context(), schoolID, &req)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Konfigurasi presensi berhasil diperbarui",
		"data":    cfg,
	})
}

// GetAttendanceShifts lists shifts
func GetAttendanceShifts(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}

	shifts, err := attendanceService.ListShifts(c.Request.Context(), schoolID)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Success",
		"data":    shifts,
	})
}

// CreateAttendanceShift creates shift
func CreateAttendanceShift(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}

	var req dto.AttendanceShiftRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	shift, err := attendanceService.CreateShift(c.Request.Context(), schoolID, &req)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "Shift berhasil ditambahkan",
		"data":    shift,
	})
}

// UpdateAttendanceShift updates shift
func UpdateAttendanceShift(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}

	shiftID, ok := parseUUIDParam(c, "id", "ID Shift")
	if !ok {
		return
	}

	var req dto.AttendanceShiftRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	shift, err := attendanceService.UpdateShift(c.Request.Context(), schoolID, shiftID, &req)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Shift berhasil diperbarui",
		"data":    shift,
	})
}

// DeleteAttendanceShift deletes shift
func DeleteAttendanceShift(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}

	shiftID, ok := parseUUIDParam(c, "id", "ID Shift")
	if !ok {
		return
	}

	if err := attendanceService.DeleteShift(c.Request.Context(), schoolID, shiftID); err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Shift berhasil dihapus",
	})
}

// GetClassAttendanceShifts lists class to shift mappings
func GetClassAttendanceShifts(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}

	mappings, err := attendanceService.ListClassShifts(c.Request.Context(), schoolID)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Success",
		"data":    mappings,
	})
}

// AssignClassAttendanceShift assigns a class to a shift
func AssignClassAttendanceShift(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}

	var req dto.AssignClassShiftRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	mapping, err := attendanceService.AssignClassShift(c.Request.Context(), schoolID, &req)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Shift kelas berhasil disimpan",
		"data":    mapping,
	})
}

// ==================== Teacher Habit & Daily Self Attendance ====================

// GetTeacherTodayAttendance godoc
// @Summary Get teacher's daily attendance status today
func GetTeacherTodayAttendance(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}

	teacherID, ok := resolveTeacherID(c)
	if !ok {
		return
	}

	dateStr := c.Query("date") // empty = today in the school time zone
	status, err := attendanceService.GetTeacherTodayStatus(c.Request.Context(), schoolID, teacherID, dateStr)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Success",
		"data":    status,
	})
}

// TeacherCheckIn godoc
// @Summary Daily arrival check-in for teacher
func TeacherCheckIn(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}

	teacherID, ok := resolveTeacherID(c)
	if !ok {
		return
	}

	var req dto.CheckInRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	daily, err := attendanceService.TeacherCheckIn(c.Request.Context(), schoolID, teacherID, &req, time.Now())
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Presensi kedatangan berhasil dicatat",
		"data":    daily,
	})
}

// TeacherCheckOut godoc
// @Summary Daily departure check-out for teacher
func TeacherCheckOut(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}

	teacherID, ok := resolveTeacherID(c)
	if !ok {
		return
	}

	var req dto.CheckOutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	daily, err := attendanceService.TeacherCheckOut(c.Request.Context(), schoolID, teacherID, &req, time.Now())
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Presensi kepulangan berhasil dicatat",
		"data":    daily,
	})
}

func resolveTeacherID(c *gin.Context) (uuid.UUID, bool) {
	// If teacher role, use authenticated employee id
	if c.GetString(middleware.CtxRole) == middleware.RoleTeacher {
		id, ok := middleware.EmployeeID(c)
		if !ok || id == uuid.Nil {
			c.JSON(http.StatusForbidden, gin.H{"error": "Data profil guru tidak ditemukan", "code": "FORBIDDEN_ROLE"})
			return uuid.Nil, false
		}
		return id, true
	}

	// For manager role, allow query param ?employee_id= (must be a teacher of the tenant school).
	empParam := c.Query("employee_id")
	if empParam != "" {
		id, err := uuid.Parse(empParam)
		if err == nil && id != uuid.Nil {
			schoolID, ok := tenantSchoolID(c)
			if !ok {
				return uuid.Nil, false
			}
			if err := attendanceService.EnsureTeacher(c.Request.Context(), schoolID, id); err != nil {
				respondError(c, err)
				return uuid.Nil, false
			}
			return id, true
		}
	}

	c.JSON(http.StatusBadRequest, gin.H{"error": "Parameter employee_id diperlukan", "code": "VALIDATION_ERROR"})
	return uuid.Nil, false
}

// sessionTeacherScope returns the caller's employee id for role teacher (own
// sessions only) and nil for managers (any session of the school).
func sessionTeacherScope(c *gin.Context) (*uuid.UUID, bool) {
	if c.GetString(middleware.CtxRole) != middleware.RoleTeacher {
		return nil, true
	}
	id, ok := middleware.EmployeeID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "Data profil guru tidak ditemukan", "code": "FORBIDDEN_ROLE"})
		return nil, false
	}
	return &id, true
}

// ==================== Daily Attendance Management (Manager) ====================

// ListDailyAttendance godoc
// @Summary List daily attendance records with filters
func ListDailyAttendance(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}

	date := c.Query("date")
	userType := c.Query("user_type")
	status := c.Query("status")
	var classID *uuid.UUID
	if cid := c.Query("class_id"); cid != "" {
		parsed, err := uuid.Parse(cid)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Format class_id tidak valid", "code": "VALIDATION_ERROR"})
			return
		}
		classID = &parsed
	}

	list, err := attendanceService.ListDailyAttendance(c.Request.Context(), schoolID, date, userType, classID, status)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Success",
		"data":    list,
	})
}

// GetDailyAttendanceSummary godoc
// @Summary Get high-level attendance summary counts
func GetDailyAttendanceSummary(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}

	date := c.Query("date") // empty = today in the school time zone
	userType := c.Query("user_type")

	summary, err := attendanceService.GetDailyAttendanceSummary(c.Request.Context(), schoolID, date, userType)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Success",
		"data":    summary,
	})
}

// RecordManualDailyAttendance godoc
// @Summary Record or correct daily attendance manually
func RecordManualDailyAttendance(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}

	var req dto.ManualDailyAttendanceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	record, err := attendanceService.RecordManualDailyAttendance(c.Request.Context(), schoolID, middleware.UserID(c), &req)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Data presensi berhasil disimpan",
		"data":    record,
	})
}

// ==================== Classroom Teaching Sessions (KBM) ====================

// GetTodayTeachingSessions godoc
// @Summary Get teaching sessions for today
func GetTodayTeachingSessions(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}

	teacherID, ok := resolveTeacherID(c)
	if !ok {
		return
	}

	dateStr := c.Query("date") // empty = today in the school time zone
	sessions, err := attendanceService.GetTeacherTodayTeachingSessions(c.Request.Context(), schoolID, teacherID, dateStr)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Success",
		"data":    sessions,
	})
}

// StartTeachingSession godoc
// @Summary Teacher starts teaching in class
func StartTeachingSession(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}

	teacherID, ok := resolveTeacherID(c)
	if !ok {
		return
	}

	var req dto.TeachingSessionStartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	sess, err := attendanceService.StartTeachingSession(c.Request.Context(), schoolID, teacherID, &req, time.Now())
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Sesi mengajar berhasil dimulai",
		"data":    sess,
	})
}

// EndTeachingSession godoc
// @Summary Teacher completes teaching session
func EndTeachingSession(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}

	teacherID, ok := resolveTeacherID(c)
	if !ok {
		return
	}

	sessionID, ok := parseUUIDParam(c, "id", "ID Sesi")
	if !ok {
		return
	}

	var req dto.TeachingSessionEndRequest
	_ = c.ShouldBindJSON(&req)

	sess, err := attendanceService.EndTeachingSession(c.Request.Context(), schoolID, teacherID, sessionID, &req, time.Now())
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Sesi mengajar telah selesai",
		"data":    sess,
	})
}

// RefreshTokenTeachingSession godoc
// @Summary Generate a new 6-char token for session
func RefreshTokenTeachingSession(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}

	teacherID, ok := resolveTeacherID(c)
	if !ok {
		return
	}

	sessionID, ok := parseUUIDParam(c, "id", "ID Sesi")
	if !ok {
		return
	}

	sess, err := attendanceService.RefreshSessionToken(c.Request.Context(), schoolID, teacherID, sessionID, time.Now())
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Kode presensi berhasil diperbarui",
		"data":    sess,
	})
}

// GetTeachingSessionDetail godoc
// @Summary Get teaching session details and student attendance list
func GetTeachingSessionDetail(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}

	sessionID, ok := parseUUIDParam(c, "id", "ID Sesi")
	if !ok {
		return
	}

	teacherID, ok := sessionTeacherScope(c)
	if !ok {
		return
	}

	detail, err := attendanceService.GetTeachingSessionDetail(c.Request.Context(), schoolID, sessionID, teacherID)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Success",
		"data":    detail,
	})
}

// SaveTeachingSessionStudents godoc
// @Summary Batch save student attendance for a session
func SaveTeachingSessionStudents(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}

	sessionID, ok := parseUUIDParam(c, "id", "ID Sesi")
	if !ok {
		return
	}

	var req dto.StudentSessionAttendanceBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	teacherID, ok := sessionTeacherScope(c)
	if !ok {
		return
	}

	err := attendanceService.SaveBatchStudentSessionAttendance(c.Request.Context(), schoolID, sessionID, teacherID, middleware.UserID(c), &req)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Daftar presensi siswa berhasil disimpan",
	})
}

// GetLiveClassMonitor godoc
// @Summary Get live radar monitor of all classes
func GetLiveClassMonitor(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}

	dateStr := c.Query("date") // empty = today in the school time zone
	items, err := attendanceService.GetLiveClassMonitor(c.Request.Context(), schoolID, dateStr)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Success",
		"data":    items,
	})
}

// UnassignClassAttendanceShift removes a class shift mapping (managers).
// @Router /instantion/school/attendance/class-shifts/{class_id} [delete]
func UnassignClassAttendanceShift(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	classID, ok := parseUUIDParam(c, "class_id", "ID Kelas")
	if !ok {
		return
	}
	if err := attendanceService.UnassignClassShift(c.Request.Context(), schoolID, classID); err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Shift kelas berhasil dihapus"})
}

// UpdateTeachingJournal updates topic/notes of a session (own session for teachers).
// @Router /instantion/school/attendance/teaching/{id}/journal [put]
func UpdateTeachingJournal(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	sessionID, ok := parseUUIDParam(c, "id", "ID Sesi")
	if !ok {
		return
	}
	teacherID, ok := sessionTeacherScope(c)
	if !ok {
		return
	}
	var req dto.TeachingSessionEndRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}
	sess, err := attendanceService.UpdateTeachingJournal(c.Request.Context(), schoolID, sessionID, teacherID, &req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Jurnal mengajar berhasil disimpan", "data": sess})
}
