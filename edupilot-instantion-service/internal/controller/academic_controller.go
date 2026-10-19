package controller

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/edupilot-instantion-service/internal/dto"
	"github.com/edupilot-instantion-service/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

var academicService = service.NewAcademicService(nil)

// GetReferences returns all enum options for client forms
func GetReferences(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	refs, err := academicService.GetReferences(c.Request.Context(), schoolID)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": refs})
}

// ==================== Academic Years ====================

func GetAcademicYears(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	years, err := academicService.GetAcademicYears(c.Request.Context(), schoolID)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": years})
}

func CreateAcademicYear(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	var req dto.AcademicYearRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	year, err := academicService.CreateAcademicYear(c.Request.Context(), schoolID, req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": year})
}

func UpdateAcademicYear(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "id", "ID tahun ajaran")
	if !ok {
		return
	}

	var req dto.AcademicYearRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	year, err := academicService.UpdateAcademicYear(c.Request.Context(), schoolID, id, req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": year})
}

func ActivateAcademicYear(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "id", "ID tahun ajaran")
	if !ok {
		return
	}

	if err := academicService.ActivateAcademicYear(c.Request.Context(), schoolID, id); err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Tahun ajaran berhasil diaktifkan"})
}

func ActivateSemester(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "id", "ID semester")
	if !ok {
		return
	}

	if err := academicService.ActivateSemester(c.Request.Context(), schoolID, id); err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Semester berhasil diaktifkan"})
}

func DeleteAcademicYear(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "id", "ID tahun ajaran")
	if !ok {
		return
	}

	if err := academicService.DeleteAcademicYear(c.Request.Context(), schoolID, id); err != nil {
		respondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ==================== Grade Levels ====================

func GetGradeLevels(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	levels, err := academicService.GetGradeLevels(c.Request.Context(), schoolID)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": levels})
}

func CreateGradeLevel(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	var req dto.GradeLevelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	level, err := academicService.CreateGradeLevel(c.Request.Context(), schoolID, req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": level})
}

func UpdateGradeLevel(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "id", "ID tingkat")
	if !ok {
		return
	}

	var req dto.GradeLevelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	level, err := academicService.UpdateGradeLevel(c.Request.Context(), schoolID, id, req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": level})
}

func DeleteGradeLevel(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "id", "ID tingkat")
	if !ok {
		return
	}

	if err := academicService.DeleteGradeLevel(c.Request.Context(), schoolID, id); err != nil {
		respondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func ApplyGradePreset(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	var req dto.GradePresetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	levels, err := academicService.ApplyGradePreset(c.Request.Context(), schoolID, req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": levels})
}

// ==================== Majors ====================

func GetMajors(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	majors, err := academicService.GetMajors(c.Request.Context(), schoolID)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": majors})
}

func CreateMajor(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	var req dto.MajorRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	major, err := academicService.CreateMajor(c.Request.Context(), schoolID, req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": major})
}

func UpdateMajor(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "id", "ID jurusan")
	if !ok {
		return
	}

	var req dto.MajorRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	major, err := academicService.UpdateMajor(c.Request.Context(), schoolID, id, req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": major})
}

func DeleteMajor(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "id", "ID jurusan")
	if !ok {
		return
	}

	if err := academicService.DeleteMajor(c.Request.Context(), schoolID, id); err != nil {
		respondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ==================== Classes ====================

func GetClasses(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}

	yearID, ok := optionalUUIDQuery(c, "academic_year_id")
	if !ok {
		return
	}
	gradeID, ok := optionalUUIDQuery(c, "grade_level_id")
	if !ok {
		return
	}
	majorID, ok := optionalUUIDQuery(c, "major_id")
	if !ok {
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	all := c.Query("all") == "true"

	classes, meta, err := academicService.ListClasses(c.Request.Context(), schoolID, service.ClassQuery{
		AcademicYearID: yearID,
		GradeLevelID:   gradeID,
		MajorID:        majorID,
		Search:         c.Query("search"),
		Page:           page,
		Limit:          limit,
		All:            all,
	})
	if err != nil {
		respondError(c, err)
		return
	}

	if all {
		c.JSON(http.StatusOK, gin.H{"data": classes})
	} else {
		c.JSON(http.StatusOK, gin.H{"data": classes, "meta": meta})
	}
}

func GetClass(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "id", "ID kelas")
	if !ok {
		return
	}

	cls, err := academicService.GetClass(c.Request.Context(), schoolID, id)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": cls})
}

func CreateClass(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	var req dto.ClassRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	cls, err := academicService.CreateClass(c.Request.Context(), schoolID, req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": cls})
}

func UpdateClass(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "id", "ID kelas")
	if !ok {
		return
	}

	var req dto.ClassRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	cls, err := academicService.UpdateClass(c.Request.Context(), schoolID, id, req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": cls})
}

func DeleteClass(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "id", "ID kelas")
	if !ok {
		return
	}

	if err := academicService.DeleteClass(c.Request.Context(), schoolID, id); err != nil {
		respondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func CreateBulkClasses(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	var req dto.ClassBulkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	created, skipped, err := academicService.CreateBulkClasses(c.Request.Context(), schoolID, req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": gin.H{"created": created, "skipped": skipped}})
}

func CopyClasses(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	var req dto.ClassCopyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	res, err := academicService.CopyClasses(c.Request.Context(), schoolID, req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": res})
}

func GetDocumentTypes(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	types, err := academicService.GetOrSeedDocumentTypes(c.Request.Context(), schoolID)
	if err != nil {
		respondError(c, err)
		return
	}

	result := make([]dto.DocumentTypeResponse, 0, len(types))
	for _, t := range types {
		result = append(result, dto.DocumentTypeResponse{
			ID:          t.ID,
			Name:        t.Name,
			Description: t.Description,
			IsRequired:  t.IsRequired,
			SortOrder:   t.SortOrder,
		})
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

// optionalUUIDQuery parses an optional uuid query parameter; an invalid value
// is a 400 instead of being silently ignored.
func optionalUUIDQuery(c *gin.Context, name string) (*uuid.UUID, bool) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return nil, true
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": name + " tidak valid", "code": "VALIDATION_ERROR",
			"details": gin.H{name: "Format UUID tidak valid"}})
		return nil, false
	}
	return &id, true
}
