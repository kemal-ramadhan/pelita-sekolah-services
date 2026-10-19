package controller

import (
	"net/http"

	"github.com/edupilot-auth-service/internal/service"
	"github.com/gin-gonic/gin"
)

// InternalCreateUser creates an institution/customer user for other services.
func InternalCreateUser(c *gin.Context) {
	var req service.CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBadRequest(c, err)
		return
	}
	user, err := service.CreateUser(req, "")
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "User created successfully", "data": user})
}

// InternalDeleteUser removes a user (compensation when a later step fails).
func InternalDeleteUser(c *gin.Context) {
	id, ok := paramUint(c, "id")
	if !ok {
		return
	}
	if err := service.DeleteUserInternal(id); err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "User deleted successfully"})
}

// InternalInviteUser creates a teacher account (no password) and emails an activation link.
func InternalInviteUser(c *gin.Context) {
	var req service.InviteUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBadRequest(c, err)
		return
	}
	user, err := service.InviteUser(req)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "User invited successfully", "data": user})
}

// InternalResendInvite issues a new activation token and re-sends the invite email.
func InternalResendInvite(c *gin.Context) {
	id, ok := paramUint(c, "id")
	if !ok {
		return
	}
	var req service.ResendInviteRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			respondBadRequest(c, err)
			return
		}
	}
	user, err := service.ResendInvite(id, req.SchoolName)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Invite re-sent", "data": user})
}
