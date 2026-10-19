package controller

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/edupilot-instantion-service/internal/dto"
	"github.com/edupilot-instantion-service/internal/middleware"
	"github.com/edupilot-instantion-service/internal/service"
	"github.com/gin-gonic/gin"
)

var subjectService = service.NewSubjectService(nil)

// ==================== Subjects (Master Mata Pelajaran) ====================

// GetSubjects godoc
// @Summary List subjects with filter & pagination
// @Tags Subjects
// @Security Bearer
// @Produce json
// @Param search query string false "Search name, code, short name"
// @Param subject_type query string false "Subject type: wajib_umum, peminatan, kejuruan, muatan_lokal, ekstrakurikuler"
// @Param is_active query boolean false "Filter active status"
// @Param page query int false "Page number"
// @Param limit query int false "Items per page"
// @Param all query boolean false "Get all items without pagination"
// @Success 200 {object} map[string]interface{}
// @Router /instantion/school/subjects [get]
func GetSubjects(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	all := c.Query("all") == "true"

	var isActive *bool
	if activeStr := c.Query("is_active"); activeStr != "" {
		val := activeStr == "true" || activeStr == "1"
		isActive = &val
	}

	if c.Query("subject_type") != "" && !dto.ValidEnum(dto.RefSubjectTypes, strings.TrimSpace(c.Query("subject_type"))) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "subject_type tidak valid", "code": "VALIDATION_ERROR"})
		return
	}

	q := service.SubjectQuery{
		Search:      strings.TrimSpace(c.Query("search")),
		SubjectType: strings.TrimSpace(c.Query("subject_type")),
		IsActive:    isActive,
		Page:        page,
		Limit:       limit,
		All:         all,
	}

	subjects, meta, err := subjectService.ListSubjects(c.Request.Context(), schoolID, q)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Success",
		"data":    subjects,
		"meta":    meta,
	})
}

// GetSubject godoc
// @Summary Get subject detail by ID
// @Tags Subjects
// @Security Bearer
// @Produce json
// @Param id path string true "Subject ID"
// @Success 200 {object} map[string]interface{}
// @Router /instantion/school/subjects/{id} [get]
func GetSubject(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "id", "ID mata pelajaran")
	if !ok {
		return
	}

	subject, err := subjectService.GetSubject(c.Request.Context(), schoolID, id)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Success",
		"data":    subject,
	})
}

// CreateSubject godoc
// @Summary Create a new subject
// @Tags Subjects
// @Security Bearer
// @Accept json
// @Produce json
// @Param body body dto.SubjectRequest true "Subject data"
// @Success 201 {object} map[string]interface{}
// @Router /instantion/school/subjects [post]
func CreateSubject(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}

	var req dto.SubjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	subject, err := subjectService.CreateSubject(c.Request.Context(), schoolID, req)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "Mata pelajaran berhasil ditambahkan",
		"data":    subject,
	})
}

// UpdateSubject godoc
// @Summary Update subject by ID
// @Tags Subjects
// @Security Bearer
// @Accept json
// @Produce json
// @Param id path string true "Subject ID"
// @Param body body dto.SubjectRequest true "Subject data"
// @Success 200 {object} map[string]interface{}
// @Router /instantion/school/subjects/{id} [put]
func UpdateSubject(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "id", "ID mata pelajaran")
	if !ok {
		return
	}

	var req dto.SubjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	subject, err := subjectService.UpdateSubject(c.Request.Context(), schoolID, id, req)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Mata pelajaran berhasil diperbarui",
		"data":    subject,
	})
}

// DeleteSubject godoc
// @Summary Delete subject by ID
// @Tags Subjects
// @Security Bearer
// @Produce json
// @Param id path string true "Subject ID"
// @Success 200 {object} map[string]interface{}
// @Router /instantion/school/subjects/{id} [delete]
func DeleteSubject(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "id", "ID mata pelajaran")
	if !ok {
		return
	}

	if err := subjectService.DeleteSubject(c.Request.Context(), schoolID, id); err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Mata pelajaran berhasil dihapus",
	})
}

// ApplySubjectPresets godoc
// @Summary Apply Kurikulum Merdeka subject presets (SD, MI, SMP, MTS, SMA, MA, SMK)
// @Tags Subjects
// @Security Bearer
// @Accept json
// @Produce json
// @Param body body dto.SubjectPresetRequest true "Preset type: sd, smp, sma, smk"
// @Success 200 {object} map[string]interface{}
// @Router /instantion/school/subjects/preset [post]
func ApplySubjectPresets(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}

	var req dto.SubjectPresetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	created, err := subjectService.ApplySubjectPresets(c.Request.Context(), schoolID, req.Preset)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Template kurikulum berhasil diterapkan",
		"data":    created,
	})
}

// ==================== Teacher Subject Assignments (Many-to-Many) ====================

// GetSubjectTeachers godoc
// @Summary Get list of teachers assigned to a subject
// @Tags Subjects
// @Security Bearer
// @Produce json
// @Param id path string true "Subject ID"
// @Success 200 {object} map[string]interface{}
// @Router /instantion/school/subjects/{id}/teachers [get]
func GetSubjectTeachers(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	subjectID, ok := parseUUIDParam(c, "id", "ID mata pelajaran")
	if !ok {
		return
	}

	teachers, err := subjectService.GetSubjectTeachers(c.Request.Context(), schoolID, subjectID)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Success",
		"data":    teachers,
	})
}

// AssignTeachersToSubject godoc
// @Summary Assign multiple teachers to a subject
// @Tags Subjects
// @Security Bearer
// @Accept json
// @Produce json
// @Param id path string true "Subject ID"
// @Param body body dto.SubjectTeacherAssignRequest true "Teacher IDs list"
// @Success 200 {object} map[string]interface{}
// @Router /instantion/school/subjects/{id}/teachers [post]
func AssignTeachersToSubject(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	subjectID, ok := parseUUIDParam(c, "id", "ID mata pelajaran")
	if !ok {
		return
	}

	var req dto.SubjectTeacherAssignRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	if err := subjectService.AssignTeachersToSubject(c.Request.Context(), schoolID, subjectID, req.TeacherIDs, req.IsPrimary); err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Guru pengampu mata pelajaran berhasil diperbarui",
	})
}

// GetTeacherSubjects godoc
// @Summary Get list of subjects assigned to a teacher
// @Tags Teachers
// @Security Bearer
// @Produce json
// @Param id path string true "Teacher ID"
// @Success 200 {object} map[string]interface{}
// @Router /instantion/school/teachers/{id}/subjects [get]
func GetTeacherSubjects(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	teacherID, ok := parseUUIDParam(c, "id", "ID guru")
	if !ok {
		return
	}
	// Role teacher may only read its own subjects.
	if c.GetString(middleware.CtxRole) == middleware.RoleTeacher {
		if own, ok := middleware.EmployeeID(c); !ok || own != teacherID {
			c.JSON(http.StatusForbidden, gin.H{"error": "Guru hanya dapat melihat mata pelajarannya sendiri", "code": "FORBIDDEN_ROLE"})
			return
		}
	}

	subjects, err := subjectService.GetTeacherSubjects(c.Request.Context(), schoolID, teacherID)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Success",
		"data":    subjects,
	})
}

// AssignSubjectsToTeacher godoc
// @Summary Assign multiple subjects to a teacher
// @Tags Teachers
// @Security Bearer
// @Accept json
// @Produce json
// @Param id path string true "Teacher ID"
// @Param body body dto.TeacherSubjectAssignRequest true "Subject IDs list"
// @Success 200 {object} map[string]interface{}
// @Router /instantion/school/teachers/{id}/subjects [post]
func AssignSubjectsToTeacher(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	teacherID, ok := parseUUIDParam(c, "id", "ID guru")
	if !ok {
		return
	}

	var req dto.TeacherSubjectAssignRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	if err := subjectService.AssignSubjectsToTeacher(c.Request.Context(), schoolID, teacherID, req.SubjectIDs, req.IsPrimary); err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Mata pelajaran guru berhasil diperbarui",
	})
}

// ==================== Class Subjects (Mapel per Rombel) ====================

// GetClassSubjects godoc
// @Summary Get subjects assigned to a class
// @Tags Classes
// @Security Bearer
// @Produce json
// @Param id path string true "Class ID"
// @Success 200 {object} map[string]interface{}
// @Router /instantion/school/classes/{id}/subjects [get]
func GetClassSubjects(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	classID, ok := parseUUIDParam(c, "id", "ID kelas")
	if !ok {
		return
	}

	classSubjects, err := subjectService.ListClassSubjects(c.Request.Context(), schoolID, classID)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Success",
		"data":    classSubjects,
	})
}

// AssignClassSubject godoc
// @Summary Assign a single subject to a class
// @Tags Classes
// @Security Bearer
// @Accept json
// @Produce json
// @Param id path string true "Class ID"
// @Param body body dto.ClassSubjectRequest true "Class subject data"
// @Success 201 {object} map[string]interface{}
// @Router /instantion/school/classes/{id}/subjects [post]
func AssignClassSubject(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	classID, ok := parseUUIDParam(c, "id", "ID kelas")
	if !ok {
		return
	}

	var req dto.ClassSubjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	cs, err := subjectService.AssignClassSubject(c.Request.Context(), schoolID, classID, req)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "Mata pelajaran berhasil ditambahkan ke kelas",
		"data":    cs,
	})
}

// BulkAssignClassSubjects godoc
// @Summary Bulk assign subjects to a class
// @Tags Classes
// @Security Bearer
// @Accept json
// @Produce json
// @Param id path string true "Class ID"
// @Param body body dto.ClassSubjectBulkRequest true "Bulk class subjects"
// @Success 200 {object} map[string]interface{}
// @Router /instantion/school/classes/{id}/subjects/bulk [post]
func BulkAssignClassSubjects(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	classID, ok := parseUUIDParam(c, "id", "ID kelas")
	if !ok {
		return
	}

	var req dto.ClassSubjectBulkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	count, err := subjectService.BulkAssignClassSubjects(c.Request.Context(), schoolID, classID, req)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Mata pelajaran berhasil ditetapkan ke kelas",
		"data": gin.H{
			"assigned_count": count,
		},
	})
}

// DeleteClassSubject godoc
// @Summary Remove a subject assignment from a class
// @Tags Classes
// @Security Bearer
// @Produce json
// @Param id path string true "Class ID"
// @Param subject_id path string true "Subject ID or ClassSubject ID"
// @Success 200 {object} map[string]interface{}
// @Router /instantion/school/classes/{id}/subjects/{subject_id} [delete]
func DeleteClassSubject(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	classID, ok := parseUUIDParam(c, "id", "ID kelas")
	if !ok {
		return
	}
	subjectID, ok := parseUUIDParam(c, "subject_id", "ID mata pelajaran")
	if !ok {
		return
	}

	if err := subjectService.DeleteClassSubject(c.Request.Context(), schoolID, classID, subjectID); err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Mata pelajaran berhasil dihapus dari rombel",
	})
}
