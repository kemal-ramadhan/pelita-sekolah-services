package controller

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/edupilot-instantion-service/internal/apperr"
	"github.com/edupilot-instantion-service/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Internal routes for edupilot-assesment (API_CONTRACT §13.1), guarded by
// middleware.RequireInternalGateway. Not reachable through the gateway.

// InternalAssessmentPrincipal: GET /internal/assessment/principal?user_id=&role=&customer_role=
// Responds with the principal at top level (not wrapped in data).
func InternalAssessmentPrincipal(c *gin.Context) {
	uid, err := strconv.ParseUint(strings.TrimSpace(c.Query("user_id")), 10, 64)
	if err != nil || uid == 0 || uid > uint64(^uint(0)) {
		respondError(c, apperr.BadRequest("user_id tidak valid"))
		return
	}
	p, err := service.ResolveAssessmentPrincipal(c.Request.Context(), uint(uid),
		strings.TrimSpace(c.Query("role")), strings.TrimSpace(c.Query("customer_role")))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, p)
}

// InternalAssessmentStudents: POST /internal/assessment/students
func InternalAssessmentStudents(c *gin.Context) {
	var req service.AssessmentStudentsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}
	rows, err := service.ListAssessmentStudents(c.Request.Context(), req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

// InternalAssessmentClasses: GET /internal/assessment/classes?school_id=
func InternalAssessmentClasses(c *gin.Context) {
	schoolID, err := uuid.Parse(strings.TrimSpace(c.Query("school_id")))
	if err != nil || schoolID == uuid.Nil {
		respondError(c, apperr.BadRequest("school_id tidak valid"))
		return
	}
	rows, err := service.ListAssessmentClasses(c.Request.Context(), schoolID)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

// InternalAssessmentNotify: POST /internal/assessment/notify
func InternalAssessmentNotify(c *gin.Context) {
	var req service.AssessmentNotifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}
	if err := req.Validate(); err != nil {
		respondError(c, err)
		return
	}
	n, err := service.AssessmentNotify(c.Request.Context(), req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"created": n}})
}
