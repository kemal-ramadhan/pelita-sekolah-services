package controller

import (
	"net/http"

	"github.com/edupilot-auth-service/internal/service"
	"github.com/gin-gonic/gin"
)

type ChangeRoleRequest struct {
	Role string `json:"role" binding:"required"`
}

// AdminCreateUser godoc
// @Summary Create a pre-verified user (admin only)
// @Tags Admin
// @Security Bearer
// @Accept json
// @Param request body service.CreateUserRequest true "User"
// @Success 201 {object} map[string]interface{}
// @Router /auth/admin/users [post]
func AdminCreateUser(c *gin.Context) {
	_, callerRole, ok := currentUser(c)
	if !ok {
		return
	}
	var req service.CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBadRequest(c, err)
		return
	}
	user, err := service.CreateUser(req, callerRole)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "User created successfully", "data": user})
}

// AdminUpdateUserProfile godoc
// @Summary Update a user's profile (admin only)
// @Tags Admin
// @Security Bearer
// @Param id path int true "User ID"
// @Param request body service.UpdateProfileRequest true "Profile"
// @Router /auth/admin/users/{id}/profile [put]
func AdminUpdateUserProfile(c *gin.Context) {
	callerID, callerRole, ok := currentUser(c)
	if !ok {
		return
	}
	targetID, ok := paramUint(c, "id")
	if !ok {
		return
	}
	var req service.UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBadRequest(c, err)
		return
	}
	user, err := service.AdminUpdateProfile(callerID, callerRole, targetID, req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "profile updated successfully", "data": user})
}

// AdminChangeUserRole godoc
// @Summary Change a user's role (admin only, cannot escalate above caller)
// @Tags Admin
// @Security Bearer
// @Param id path int true "User ID"
// @Param request body ChangeRoleRequest true "Role"
// @Router /auth/admin/users/{id}/role [put]
func AdminChangeUserRole(c *gin.Context) {
	callerID, callerRole, ok := currentUser(c)
	if !ok {
		return
	}
	targetID, ok := paramUint(c, "id")
	if !ok {
		return
	}
	var req ChangeRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBadRequest(c, err)
		return
	}
	user, err := service.ChangeUserRole(callerID, callerRole, targetID, req.Role)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "role updated successfully", "data": user})
}
