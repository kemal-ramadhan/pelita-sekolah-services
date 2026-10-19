package controller

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/edupilot-instantion-service/internal/apperr"
	"github.com/edupilot-instantion-service/internal/dto"
	"github.com/edupilot-instantion-service/internal/middleware"
	"github.com/edupilot-instantion-service/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// tenantSchoolID returns the tenant school id or writes a 404 response.
func tenantSchoolID(c *gin.Context) (uuid.UUID, bool) {
	id, ok := middleware.SchoolID(c)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Profil sekolah belum dibuat", "code": "SCHOOL_NOT_SET_UP"})
	}
	return id, ok
}

func tenantSchoolName(c *gin.Context) string {
	if s := middleware.TenantSchool(c); s != nil {
		return s.Name
	}
	return ""
}

func parseUUIDParam(c *gin.Context, name, label string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": label + " tidak valid", "code": "VALIDATION_ERROR"})
		return uuid.Nil, false
	}
	return id, true
}

func bindTeacherRequest(c *gin.Context, req *dto.TeacherRequest) bool {
	if err := c.ShouldBindJSON(req); err != nil {
		respondBindError(c, err)
		return false
	}
	if msg := req.Normalize(); msg != "" {
		respondError(c, apperr.BadRequest(msg))
		return false
	}
	return true
}

// GetTeachers godoc
// @Summary List teachers of the tenant school
// @Tags Teachers
// @Security Bearer
// @Produce json
// @Param page query int false "Page (default 1)"
// @Param limit query int false "Limit (default 10, max 100)"
// @Param search query string false "Search full_name / employee_code / email"
// @Success 200 {object} map[string]interface{}
// @Router /instantion/school/teachers [get]
func GetTeachers(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	q := dto.ParsePageQuery(c.Query("page"), c.Query("limit"), c.Query("search"))
	teachers, total, err := service.ListTeachers(c.Request.Context(), schoolID, q)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Success", "data": teachers, "meta": dto.NewPageMeta(q, total)})
}

// GetTeacher godoc
// @Summary Get a teacher (with subjects, assignments, schedules)
// @Tags Teachers
// @Security Bearer
// @Produce json
// @Param id path string true "Teacher ID"
// @Success 200 {object} map[string]interface{}
// @Router /instantion/school/teachers/{id} [get]
func GetTeacher(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "id", "ID guru")
	if !ok {
		return
	}
	teacher, err := service.GetTeacher(c.Request.Context(), schoolID, id)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Success", "data": teacher})
}

// CreateTeacher godoc
// @Summary Create a teacher
// @Tags Teachers
// @Security Bearer
// @Accept json
// @Produce json
// @Param body body dto.TeacherRequest true "Teacher"
// @Success 201 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Router /instantion/school/teachers [post]
func CreateTeacher(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	var req dto.TeacherRequest
	if !bindTeacherRequest(c, &req) {
		return
	}
	teacher, warning, err := service.CreateTeacher(c.Request.Context(), schoolID, tenantSchoolName(c), &req)
	if err != nil {
		respondError(c, err)
		return
	}
	body := gin.H{"message": "Teacher created successfully", "data": teacher}
	if warning != "" {
		body["warning"] = warning
	}
	c.JSON(http.StatusCreated, body)
}

// UpdateTeacher godoc
// @Summary Update a teacher
// @Tags Teachers
// @Security Bearer
// @Accept json
// @Produce json
// @Param id path string true "Teacher ID"
// @Param body body dto.TeacherRequest true "Teacher"
// @Success 200 {object} map[string]interface{}
// @Router /instantion/school/teachers/{id} [put]
func UpdateTeacher(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "id", "ID guru")
	if !ok {
		return
	}
	var req dto.TeacherRequest
	if !bindTeacherRequest(c, &req) {
		return
	}
	teacher, err := service.UpdateTeacher(c.Request.Context(), schoolID, id, &req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Teacher updated successfully", "data": teacher})
}

// DeleteTeacher godoc
// @Summary Delete (soft) a teacher
// @Tags Teachers
// @Security Bearer
// @Produce json
// @Param id path string true "Teacher ID"
// @Success 200 {object} map[string]interface{}
// @Router /instantion/school/teachers/{id} [delete]
func DeleteTeacher(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "id", "ID guru")
	if !ok {
		return
	}
	if err := service.DeleteTeacher(c.Request.Context(), schoolID, id); err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Teacher deleted successfully"})
}

// InviteTeacher godoc
// @Summary Invite (or re-invite) a teacher to activate their account
// @Description status none → creates the account; invited → resends (new token); active → 409 ALREADY_ACTIVE. Max 1 per teacher per 60s (429).
// @Tags Teachers
// @Security Bearer
// @Produce json
// @Param id path string true "Teacher ID"
// @Success 200 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Failure 429 {object} map[string]interface{}
// @Router /instantion/school/teachers/{id}/invite [post]
func InviteTeacher(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "id", "ID guru")
	if !ok {
		return
	}
	teacher, warning, err := service.InviteTeacher(c.Request.Context(), schoolID, tenantSchoolName(c), id)
	if err != nil {
		respondError(c, err)
		return
	}
	body := gin.H{"message": "Undangan berhasil dikirim", "data": teacher}
	if warning != "" {
		body["warning"] = warning
	}
	c.JSON(http.StatusOK, body)
}

// GetMyTeacher godoc
// @Summary Get the logged-in teacher's own record (role teacher only)
// @Tags Teachers
// @Security Bearer
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Router /instantion/school/me [get]
func GetMyTeacher(c *gin.Context) {
	employeeID, ok := middleware.EmployeeID(c)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Data guru tidak ditemukan untuk akun ini", "code": "NOT_FOUND"})
		return
	}
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	teacher, err := service.GetTeacher(c.Request.Context(), schoolID, employeeID)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Success", "data": teacher})
}

// ImportTeachers godoc
// @Summary Import teachers from .xlsx (first sheet, header row 1)
// @Tags Teachers
// @Security Bearer
// @Accept multipart/form-data
// @Produce json
// @Param file formData file true ".xlsx file, max 5 MB"
// @Param dry_run query bool false "Validate only"
// @Success 200 {object} map[string]interface{}
// @Router /instantion/school/teachers/import [post]
func ImportTeachers(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	dryRun, _ := strconv.ParseBool(c.DefaultQuery("dry_run", "false"))

	fh, err := c.FormFile("file")
	if err != nil {
		var msg = "File wajib diunggah (field: file)"
		if isBodyTooLarge(err) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "Ukuran file maksimal 5 MB", "code": "FILE_TOO_LARGE"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": msg, "code": "VALIDATION_ERROR"})
		return
	}
	src, err := fh.Open()
	if err != nil {
		respondError(c, fmt.Errorf("open uploaded file: %w", err))
		return
	}
	defer src.Close()

	result, err := service.ImportTeachers(c.Request.Context(), schoolID, fh.Filename, fh.Size, src, dryRun)
	if err != nil {
		respondError(c, err)
		return
	}
	msg := "Import selesai"
	if dryRun {
		msg = "Validasi selesai (dry run)"
	}
	c.JSON(http.StatusOK, gin.H{"message": msg, "data": result})
}

// ImportTeachersTemplate godoc
// @Summary Download the teacher import .xlsx template
// @Tags Teachers
// @Security Bearer
// @Produce application/vnd.openxmlformats-officedocument.spreadsheetml.sheet
// @Success 200 {file} file
// @Router /instantion/school/teachers/import/template [get]
func ImportTeachersTemplate(c *gin.Context) {
	buf, err := service.BuildImportTemplate()
	if err != nil {
		respondError(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="template_import_guru.xlsx"`)
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", buf.Bytes())
}
