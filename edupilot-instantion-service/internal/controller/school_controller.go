package controller

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"

	"github.com/edupilot-instantion-service/internal/apperr"
	"github.com/edupilot-instantion-service/internal/dto"
	"github.com/edupilot-instantion-service/internal/middleware"
	"github.com/edupilot-instantion-service/internal/service"
	"github.com/gin-gonic/gin"
)

func bindSchoolRequest(c *gin.Context, req *dto.SchoolRequest) bool {
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

// GetSchoolProfile godoc
// @Summary Get tenant school profile
// @Tags School
// @Security Bearer
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{} "SCHOOL_NOT_SET_UP"
// @Router /instantion/school/profile [get]
func GetSchoolProfile(c *gin.Context) {
	school := middleware.TenantSchool(c)
	if school == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Profil sekolah belum dibuat", "code": "SCHOOL_NOT_SET_UP"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Success", "data": school})
}

// CreateSchoolProfile godoc
// @Summary Create tenant school profile (once)
// @Tags School
// @Security Bearer
// @Accept json
// @Produce json
// @Param body body dto.SchoolRequest true "School"
// @Success 201 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Router /instantion/school/profile [post]
func CreateSchoolProfile(c *gin.Context) {
	if middleware.TenantSchool(c) != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Profil sekolah sudah ada untuk akun ini", "code": "SCHOOL_ALREADY_EXISTS"})
		return
	}
	var req dto.SchoolRequest
	if !bindSchoolRequest(c, &req) {
		return
	}
	school, err := service.CreateSchool(c.Request.Context(), middleware.UserID(c), req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "School profile created successfully", "data": school})
}

// UpdateSchoolProfile godoc
// @Summary Update tenant school profile
// @Tags School
// @Security Bearer
// @Accept json
// @Produce json
// @Param body body dto.SchoolRequest true "School"
// @Success 200 {object} map[string]interface{}
// @Router /instantion/school/profile [put]
func UpdateSchoolProfile(c *gin.Context) {
	schoolID, ok := middleware.SchoolID(c)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Profil sekolah belum dibuat", "code": "SCHOOL_NOT_SET_UP"})
		return
	}
	var req dto.SchoolRequest
	if !bindSchoolRequest(c, &req) {
		return
	}
	school, err := service.UpdateSchool(c.Request.Context(), schoolID, req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "School profile updated successfully", "data": school})
}

const maxLogoSize = 2 << 20 // 2 MB

var allowedLogoTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/webp": true,
}

// readLogoDataURL validates the multipart "logo" (png/jpeg/webp by content sniffing,
// <= 2 MB) and returns it as a data URL.
func readLogoDataURL(c *gin.Context) (string, bool) {
	fh, err := c.FormFile("logo")
	if err != nil {
		if isBodyTooLarge(err) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "Ukuran file maksimal 2 MB", "code": "FILE_TOO_LARGE"})
			return "", false
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "File logo wajib diunggah (field: logo)", "code": "VALIDATION_ERROR"})
		return "", false
	}
	if fh.Size > maxLogoSize {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "Ukuran file maksimal 2 MB", "code": "FILE_TOO_LARGE"})
		return "", false
	}
	src, err := fh.Open()
	if err != nil {
		respondError(c, fmt.Errorf("open uploaded logo: %w", err))
		return "", false
	}
	defer src.Close()

	data, err := io.ReadAll(io.LimitReader(src, maxLogoSize+1))
	if err != nil {
		respondError(c, fmt.Errorf("read uploaded logo: %w", err))
		return "", false
	}
	if len(data) > maxLogoSize {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "Ukuran file maksimal 2 MB", "code": "FILE_TOO_LARGE"})
		return "", false
	}
	mimeType := http.DetectContentType(data)
	if !allowedLogoTypes[mimeType] {
		c.JSON(http.StatusUnsupportedMediaType, gin.H{"error": "Format gambar harus PNG, JPEG, atau WEBP", "code": "INVALID_FILE_TYPE"})
		return "", false
	}
	return fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(data)), true
}

// UploadSchoolLogo godoc
// @Summary Upload tenant school logo (multipart "logo", png/jpeg/webp <= 2MB)
// @Tags School
// @Security Bearer
// @Accept multipart/form-data
// @Produce json
// @Param logo formData file true "Logo image"
// @Success 200 {object} map[string]interface{}
// @Router /instantion/school/profile/logo [put]
func UploadSchoolLogo(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	dataURL, ok := readLogoDataURL(c)
	if !ok {
		return
	}
	school, err := service.UpdateSchoolLogo(c.Request.Context(), schoolID, &dataURL)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Logo sekolah berhasil diperbarui", "data": school})
}

// DeleteSchoolLogo godoc
// @Summary Remove tenant school logo
// @Tags School
// @Security Bearer
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /instantion/school/profile/logo [delete]
func DeleteSchoolLogo(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	school, err := service.UpdateSchoolLogo(c.Request.Context(), schoolID, nil)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Logo sekolah berhasil dihapus", "data": school})
}
