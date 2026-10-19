package controller

import (
	"net/http"
	"strconv"

	"github.com/edupilot-instantion-service/internal/dto"
	"github.com/edupilot-instantion-service/internal/middleware"
	"github.com/edupilot-instantion-service/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

var scheduleService = service.NewScheduleService(nil)

// ==================== Schedule Periods (Jam Pelajaran) ====================

// GetSchedulePeriods godoc
// @Summary List all schedule periods for tenant school
// @Tags Schedules
// @Security Bearer
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /instantion/school/schedule-periods [get]
func GetSchedulePeriods(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}

	periods, err := scheduleService.ListSchedulePeriods(c.Request.Context(), schoolID)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Success",
		"data":    periods,
	})
}

// CreateSchedulePeriod godoc
// @Summary Create a schedule period slot
// @Tags Schedules
// @Security Bearer
// @Accept json
// @Produce json
// @Param body body dto.SchedulePeriodRequest true "Schedule period data"
// @Success 201 {object} map[string]interface{}
// @Router /instantion/school/schedule-periods [post]
func CreateSchedulePeriod(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}

	var req dto.SchedulePeriodRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	period, err := scheduleService.CreateSchedulePeriod(c.Request.Context(), schoolID, req)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "Jam pelajaran berhasil ditambahkan",
		"data":    period,
	})
}

// UpdateSchedulePeriod godoc
// @Summary Update a schedule period slot
// @Tags Schedules
// @Security Bearer
// @Accept json
// @Produce json
// @Param id path string true "Schedule period ID"
// @Param body body dto.SchedulePeriodRequest true "Schedule period data"
// @Success 200 {object} map[string]interface{}
// @Router /instantion/school/schedule-periods/{id} [put]
func UpdateSchedulePeriod(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "id", "ID jam pelajaran")
	if !ok {
		return
	}

	var req dto.SchedulePeriodRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	period, err := scheduleService.UpdateSchedulePeriod(c.Request.Context(), schoolID, id, req)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Jam pelajaran berhasil diperbarui",
		"data":    period,
	})
}

// DeleteSchedulePeriod godoc
// @Summary Delete a schedule period slot
// @Tags Schedules
// @Security Bearer
// @Produce json
// @Param id path string true "Schedule period ID"
// @Success 200 {object} map[string]interface{}
// @Router /instantion/school/schedule-periods/{id} [delete]
func DeleteSchedulePeriod(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "id", "ID jam pelajaran")
	if !ok {
		return
	}

	if err := scheduleService.DeleteSchedulePeriod(c.Request.Context(), schoolID, id); err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Jam pelajaran berhasil dihapus",
	})
}

// ApplyPeriodPresets godoc
// @Summary Apply period presets (40min / 45min)
// @Tags Schedules
// @Security Bearer
// @Accept json
// @Produce json
// @Param body body dto.SchedulePresetRequest true "Preset: 35_MIN (SD/MI), 40_MIN (SMP/MTs), 45_MIN (SMA/MA/SMK)"
// @Success 200 {object} map[string]interface{}
// @Router /instantion/school/schedule-periods/preset [post]
func ApplyPeriodPresets(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}

	var req dto.SchedulePresetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	periods, err := scheduleService.ApplyPeriodPresets(c.Request.Context(), schoolID, req.Preset)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Template jam pelajaran berhasil diterapkan",
		"data":    periods,
	})
}

// ==================== Schedules (Jadwal Pelajaran & Timetable) ====================

// parseScheduleQuery reads the list/grid filters; invalid values → 400.
func parseScheduleQuery(c *gin.Context) (service.ScheduleQuery, bool) {
	var q service.ScheduleQuery
	params := []struct {
		name, label string
		dst         **uuid.UUID
	}{
		{"academic_year_id", "academic_year_id", &q.AcademicYearID},
		{"semester_id", "semester_id", &q.SemesterID},
		{"class_id", "class_id", &q.ClassID},
		{"subject_id", "subject_id", &q.SubjectID},
		{"teacher_id", "teacher_id", &q.TeacherID},
	}
	for _, p := range params {
		raw := c.Query(p.name)
		if raw == "" {
			continue
		}
		id, err := uuid.Parse(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": p.label + " tidak valid", "code": "VALIDATION_ERROR"})
			return q, false
		}
		*p.dst = &id
	}
	if dayStr := c.Query("day_of_week"); dayStr != "" {
		day, err := strconv.Atoi(dayStr)
		if err != nil || day < 1 || day > 7 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "day_of_week harus antara 1 dan 7", "code": "VALIDATION_ERROR"})
			return q, false
		}
		q.DayOfWeek = day
	}
	return q, true
}

// scopeToOwnTeacher limits role teacher to its own schedules: teacher_id is
// forced to the caller's employee id; asking for another teacher → 403.
func scopeToOwnTeacher(c *gin.Context, q *service.ScheduleQuery) bool {
	if c.GetString(middleware.CtxRole) != middleware.RoleTeacher {
		return true
	}
	own, ok := middleware.EmployeeID(c)
	if !ok || (q.TeacherID != nil && *q.TeacherID != own) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Guru hanya dapat melihat jadwal mengajarnya sendiri", "code": "FORBIDDEN_ROLE"})
		return false
	}
	q.TeacherID = &own
	return true
}

// GetSchedules godoc
// @Summary List schedules with filter
// @Tags Schedules
// @Security Bearer
// @Produce json
// @Param academic_year_id query string false "Academic Year ID"
// @Param semester_id query string false "Semester ID"
// @Param class_id query string false "Class ID"
// @Param subject_id query string false "Subject ID"
// @Param teacher_id query string false "Teacher ID"
// @Param day_of_week query int false "Day of week (1=Monday...7=Sunday)"
// @Success 200 {object} map[string]interface{}
// @Router /instantion/school/schedules [get]
func GetSchedules(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}

	q, ok := parseScheduleQuery(c)
	if !ok || !scopeToOwnTeacher(c, &q) {
		return
	}
	schedules, err := scheduleService.ListSchedules(c.Request.Context(), schoolID, q)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Success",
		"data":    schedules,
	})
}

// CheckScheduleConflict godoc
// @Summary Check for teacher, class, and room schedule conflicts
// @Tags Schedules
// @Security Bearer
// @Accept json
// @Produce json
// @Param body body dto.ScheduleRequest true "Schedule slot candidate"
// @Param exclude_id query string false "Exclude schedule ID if updating"
// @Success 200 {object} map[string]interface{}
// @Router /instantion/school/schedules/check-conflict [post]
func CheckScheduleConflict(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}

	var req dto.ScheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	var excludeID *uuid.UUID
	if exStr := c.Query("exclude_id"); exStr != "" {
		parsed, err := uuid.Parse(exStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "exclude_id tidak valid", "code": "VALIDATION_ERROR"})
			return
		}
		excludeID = &parsed
	}

	result, err := scheduleService.CheckScheduleConflict(c.Request.Context(), schoolID, req, excludeID)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Success",
		"data":    result,
	})
}

// CreateSchedule godoc
// @Summary Create a timetable schedule slot
// @Tags Schedules
// @Security Bearer
// @Accept json
// @Produce json
// @Param body body dto.ScheduleRequest true "Schedule data"
// @Success 201 {object} map[string]interface{}
// @Router /instantion/school/schedules [post]
func CreateSchedule(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}

	var req dto.ScheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	schedule, warnings, err := scheduleService.CreateSchedule(c.Request.Context(), schoolID, req)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message":  "Jadwal pelajaran berhasil ditambahkan",
		"data":     schedule,
		"warnings": warnings,
	})
}

// UpdateSchedule godoc
// @Summary Update a timetable schedule slot
// @Tags Schedules
// @Security Bearer
// @Accept json
// @Produce json
// @Param id path string true "Schedule ID"
// @Param body body dto.ScheduleRequest true "Schedule data"
// @Success 200 {object} map[string]interface{}
// @Router /instantion/school/schedules/{id} [put]
func UpdateSchedule(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "id", "ID jadwal pelajaran")
	if !ok {
		return
	}

	var req dto.ScheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	schedule, warnings, err := scheduleService.UpdateSchedule(c.Request.Context(), schoolID, id, req)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":  "Jadwal pelajaran berhasil diperbarui",
		"data":     schedule,
		"warnings": warnings,
	})
}

// DeleteSchedule godoc
// @Summary Delete a timetable schedule slot
// @Tags Schedules
// @Security Bearer
// @Produce json
// @Param id path string true "Schedule ID"
// @Success 200 {object} map[string]interface{}
// @Router /instantion/school/schedules/{id} [delete]
func DeleteSchedule(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "id", "ID jadwal pelajaran")
	if !ok {
		return
	}

	if err := scheduleService.DeleteSchedule(c.Request.Context(), schoolID, id); err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Jadwal pelajaran berhasil dihapus",
	})
}

// GetTimetableGrid godoc
// @Summary Get weekly timetable grid (by class, subject, or teacher)
// @Tags Schedules
// @Security Bearer
// @Produce json
// @Param academic_year_id query string false "Academic Year ID"
// @Param semester_id query string false "Semester ID"
// @Param class_id query string false "Class ID"
// @Param subject_id query string false "Subject ID"
// @Param teacher_id query string false "Teacher ID"
// @Success 200 {object} map[string]interface{}
// @Router /instantion/school/schedules/grid [get]
func GetTimetableGrid(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}

	q, ok := parseScheduleQuery(c)
	if !ok || !scopeToOwnTeacher(c, &q) {
		return
	}
	grid, err := scheduleService.GetTimetableGrid(c.Request.Context(), schoolID, q)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Success",
		"data":    grid,
	})
}

// GetMySchedule godoc
// @Summary Weekly timetable grid of the logged-in teacher (role teacher only)
// @Tags Schedules
// @Security Bearer
// @Produce json
// @Param academic_year_id query string false "Academic Year ID (default: active)"
// @Param semester_id query string false "Semester ID (default: active semester)"
// @Success 200 {object} map[string]interface{}
// @Router /instantion/school/me/schedule [get]
func GetMySchedule(c *gin.Context) {
	employeeID, ok := middleware.EmployeeID(c)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Data guru tidak ditemukan untuk akun ini", "code": "NOT_FOUND"})
		return
	}
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	q, ok := parseScheduleQuery(c)
	if !ok {
		return
	}
	q.ClassID, q.SubjectID, q.DayOfWeek = nil, nil, 0
	q.TeacherID = &employeeID
	grid, err := scheduleService.GetTimetableGrid(c.Request.Context(), schoolID, q)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Success", "data": grid})
}
