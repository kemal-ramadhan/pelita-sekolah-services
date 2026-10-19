package controller

import (
	"net/http"
	"strconv"

	"github.com/edupilot-instantion-service/internal/apperr"
	"github.com/edupilot-instantion-service/internal/dto"
	"github.com/edupilot-instantion-service/internal/service"
	"github.com/gin-gonic/gin"
)

// AdminGetSchools godoc
// @Summary List schools (admin)
// @Tags Admin
// @Security Bearer
// @Produce json
// @Param page query int false "Page"
// @Param limit query int false "Limit (max 100)"
// @Param search query string false "Search name / code / npsn"
// @Param status query string false "active | inactive"
// @Success 200 {object} map[string]interface{}
// @Router /instantion/admin/schools [get]
func AdminGetSchools(c *gin.Context) {
	status := c.Query("status")
	if status != "" && status != "active" && status != "inactive" {
		respondError(c, apperr.BadRequest("status harus active atau inactive"))
		return
	}
	q := dto.ParsePageQuery(c.Query("page"), c.Query("limit"), c.Query("search"))
	items, total, err := service.AdminListSchools(c.Request.Context(), q, status)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Success", "data": items, "meta": dto.NewPageMeta(q, total)})
}

// AdminGetSchool godoc
// @Summary Get a school (admin)
// @Tags Admin
// @Security Bearer
// @Produce json
// @Param id path string true "School ID"
// @Success 200 {object} map[string]interface{}
// @Router /instantion/admin/schools/{id} [get]
func AdminGetSchool(c *gin.Context) {
	id, ok := parseUUIDParam(c, "id", "ID sekolah")
	if !ok {
		return
	}
	school, err := service.AdminGetSchool(c.Request.Context(), id)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Success", "data": school})
}

// AdminCreateSchool godoc
// @Summary Create a school, optionally with an institution account (admin)
// @Tags Admin
// @Security Bearer
// @Accept json
// @Produce json
// @Param body body dto.AdminCreateSchoolRequest true "School (+ optional account)"
// @Success 201 {object} map[string]interface{}
// @Router /instantion/admin/schools [post]
func AdminCreateSchool(c *gin.Context) {
	var req dto.AdminCreateSchoolRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}
	if msg := req.SchoolRequest.Normalize(); msg != "" {
		respondError(c, apperr.BadRequest(msg))
		return
	}
	school, err := service.AdminCreateSchool(c.Request.Context(), req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "School created successfully", "data": school})
}

// AdminUpdateSchool godoc
// @Summary Update a school (admin)
// @Tags Admin
// @Security Bearer
// @Accept json
// @Produce json
// @Param id path string true "School ID"
// @Param body body dto.SchoolRequest true "School"
// @Success 200 {object} map[string]interface{}
// @Router /instantion/admin/schools/{id} [put]
func AdminUpdateSchool(c *gin.Context) {
	id, ok := parseUUIDParam(c, "id", "ID sekolah")
	if !ok {
		return
	}
	var req dto.SchoolRequest
	if !bindSchoolRequest(c, &req) {
		return
	}
	school, err := service.AdminUpdateSchool(c.Request.Context(), id, req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "School updated successfully", "data": school})
}

// AdminToggleSchoolStatus godoc
// @Summary Activate / deactivate a school (admin)
// @Tags Admin
// @Security Bearer
// @Accept json
// @Produce json
// @Param id path string true "School ID"
// @Param body body dto.SchoolStatusRequest true "is_active (required)"
// @Success 200 {object} map[string]interface{}
// @Router /instantion/admin/schools/{id}/status [patch]
func AdminToggleSchoolStatus(c *gin.Context) {
	id, ok := parseUUIDParam(c, "id", "ID sekolah")
	if !ok {
		return
	}
	var req dto.SchoolStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}
	school, err := service.AdminSetSchoolStatus(c.Request.Context(), id, *req.IsActive)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "School status updated successfully", "data": school})
}

// bindUserID reads user_id from the JSON body, falling back to ?user_id= (for DELETE clients that cannot send a body).
func bindUserID(c *gin.Context) (uint, bool) {
	if c.Request.Method == http.MethodDelete && c.Request.ContentLength <= 0 {
		if v := c.Query("user_id"); v != "" {
			id, err := strconv.ParseUint(v, 10, 32)
			if err != nil || id == 0 {
				respondError(c, apperr.BadRequest("user_id harus lebih besar dari 0"))
				return 0, false
			}
			return uint(id), true
		}
	}
	var req dto.LinkAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return 0, false
	}
	return req.UserID, true
}

// AdminLinkAccountToSchool godoc
// @Summary Link an auth user to a school (admin)
// @Tags Admin
// @Security Bearer
// @Accept json
// @Produce json
// @Param id path string true "School ID"
// @Param body body dto.LinkAccountRequest true "user_id"
// @Success 201 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Router /instantion/admin/schools/{id}/accounts [post]
func AdminLinkAccountToSchool(c *gin.Context) {
	schoolID, ok := parseUUIDParam(c, "id", "ID sekolah")
	if !ok {
		return
	}
	userID, ok := bindUserID(c)
	if !ok {
		return
	}
	account, err := service.AdminLinkAccount(c.Request.Context(), schoolID, userID)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "Account linked successfully", "data": account})
}

// AdminUnlinkAccount godoc
// @Summary Unlink an auth user from a school (admin)
// @Tags Admin
// @Security Bearer
// @Accept json
// @Produce json
// @Param id path string true "School ID"
// @Param body body dto.LinkAccountRequest true "user_id"
// @Success 200 {object} map[string]interface{}
// @Router /instantion/admin/schools/{id}/accounts [delete]
func AdminUnlinkAccount(c *gin.Context) {
	schoolID, ok := parseUUIDParam(c, "id", "ID sekolah")
	if !ok {
		return
	}
	userID, ok := bindUserID(c)
	if !ok {
		return
	}
	if err := service.AdminUnlinkAccount(c.Request.Context(), schoolID, userID); err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Account unlinked successfully"})
}

// AdminGetLinkedAccounts godoc
// @Summary List accounts linked to a school (admin)
// @Tags Admin
// @Security Bearer
// @Produce json
// @Param id path string true "School ID"
// @Success 200 {object} map[string]interface{}
// @Router /instantion/admin/schools/{id}/accounts [get]
func AdminGetLinkedAccounts(c *gin.Context) {
	schoolID, ok := parseUUIDParam(c, "id", "ID sekolah")
	if !ok {
		return
	}
	accounts, err := service.AdminGetLinkedAccounts(c.Request.Context(), schoolID)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Success", "data": accounts})
}

// AdminGetStats godoc
// @Summary Platform statistics (admin)
// @Tags Admin
// @Security Bearer
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /instantion/admin/stats [get]
func AdminGetStats(c *gin.Context) {
	stats, err := service.AdminGetStats(c.Request.Context())
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Success", "data": stats})
}
