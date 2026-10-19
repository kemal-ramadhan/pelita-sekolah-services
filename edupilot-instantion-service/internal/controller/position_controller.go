package controller

import (
	"net/http"

	"github.com/edupilot-instantion-service/internal/dto"
	"github.com/edupilot-instantion-service/internal/service"
	"github.com/gin-gonic/gin"
)

var positionService = service.NewPositionService(nil)

// GetEmployeePositions godoc
// @Summary List the school's jabatan / tugas tambahan (seeds defaults on first call)
// @Tags EmployeePositions
// @Security Bearer
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /instantion/school/employee-positions [get]
func GetEmployeePositions(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	list, err := positionService.ListPositions(c.Request.Context(), schoolID)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

// CreateEmployeePosition godoc
// @Summary Create a jabatan (position_code generated from the name when blank)
// @Tags EmployeePositions
// @Security Bearer
// @Accept json
// @Produce json
// @Param body body dto.EmployeePositionRequest true "Position"
// @Success 201 {object} map[string]interface{}
// @Router /instantion/school/employee-positions [post]
func CreateEmployeePosition(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	var req dto.EmployeePositionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}
	pos, err := positionService.CreatePosition(c.Request.Context(), schoolID, req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": pos})
}

// UpdateEmployeePosition godoc
// @Summary Update a jabatan
// @Tags EmployeePositions
// @Security Bearer
// @Accept json
// @Produce json
// @Param id path string true "Position ID"
// @Param body body dto.EmployeePositionRequest true "Position"
// @Success 200 {object} map[string]interface{}
// @Router /instantion/school/employee-positions/{id} [put]
func UpdateEmployeePosition(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "id", "ID jabatan")
	if !ok {
		return
	}
	var req dto.EmployeePositionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}
	pos, err := positionService.UpdatePosition(c.Request.Context(), schoolID, id, req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": pos})
}

// DeleteEmployeePosition godoc
// @Summary Delete a jabatan (409 IN_USE while assigned)
// @Tags EmployeePositions
// @Security Bearer
// @Param id path string true "Position ID"
// @Success 204
// @Router /instantion/school/employee-positions/{id} [delete]
func DeleteEmployeePosition(c *gin.Context) {
	schoolID, ok := tenantSchoolID(c)
	if !ok {
		return
	}
	id, ok := parseUUIDParam(c, "id", "ID jabatan")
	if !ok {
		return
	}
	if err := positionService.DeletePosition(c.Request.Context(), schoolID, id); err != nil {
		respondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
