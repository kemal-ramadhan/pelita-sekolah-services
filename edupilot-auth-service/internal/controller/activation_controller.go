package controller

import (
	"net/http"

	"github.com/edupilot-auth-service/internal/service"
	"github.com/gin-gonic/gin"
)

type ActivateRequest struct {
	Token    string `json:"token" binding:"required"`
	Password string `json:"password" binding:"required,min=8,max=72"`
}

// CheckActivation godoc
// @Summary Validate a teacher activation token
// @Tags Auth
// @Produce json
// @Param token query string true "Activation token"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Router /auth/activate [get]
func CheckActivation(c *gin.Context) {
	info, err := service.CheckActivationToken(c.Query("token"))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Token aktivasi valid", "data": info})
}

// Activate godoc
// @Summary Activate a teacher account (sets password, single-use token, logs in)
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body ActivateRequest true "Activation request"
// @Success 200 {object} service.LoginResponse
// @Failure 400 {object} map[string]string
// @Router /auth/activate [post]
func Activate(c *gin.Context) {
	var req ActivateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBadRequest(c, err)
		return
	}
	resp, err := service.ActivateAccount(req.Token, req.Password)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}
