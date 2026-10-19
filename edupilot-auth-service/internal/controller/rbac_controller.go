package controller

import (
	"net/http"

	"github.com/edupilot-auth-service/internal/service"
	"github.com/gin-gonic/gin"
)

type GrantAbilityRequest struct {
	TargetUserID uint `json:"target_user_id" binding:"required"`
	AbilityID    uint `json:"ability_id" binding:"required"`
	IsActive     bool `json:"is_active"`
}

// GrantAbility grants/revokes a per-user ability override (admin only; manage_roles needs super_admin).
func GrantAbility(c *gin.Context) {
	callerID, callerRole, ok := currentUser(c)
	if !ok {
		return
	}
	var req GrantAbilityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBadRequest(c, err)
		return
	}
	ua, err := service.GrantAbility(callerID, callerRole, req.TargetUserID, req.AbilityID, req.IsActive)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message": "Ability granted/updated successfully",
		"data": gin.H{
			"user_id":    ua.UserID,
			"ability_id": ua.AbilityID,
			"ability":    ua.Ability.Name,
			"is_active":  ua.IsActive,
		},
	})
}

type AssignRoleRequest struct {
	UserID uint `json:"user_id" binding:"required"`
	RoleID uint `json:"role_id" binding:"required"`
}

// AssignRole assigns an RBAC role packet to a user (cannot assign above caller).
func AssignRole(c *gin.Context) {
	callerID, callerRole, ok := currentUser(c)
	if !ok {
		return
	}
	var req AssignRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBadRequest(c, err)
		return
	}
	created, err := service.AssignRole(callerID, callerRole, req.UserID, req.RoleID)
	if err != nil {
		respondError(c, err)
		return
	}
	msg := "Role assigned successfully"
	if !created {
		msg = "Role already assigned"
	}
	c.JSON(http.StatusOK, gin.H{"message": msg, "data": gin.H{"user_id": req.UserID, "role_id": req.RoleID}})
}

type AddAbilityToRoleRequest struct {
	RoleID    uint `json:"role_id" binding:"required"`
	AbilityID uint `json:"ability_id" binding:"required"`
}

// AddAbilityToRole adds an ability to a role packet (super_admin only).
func AddAbilityToRole(c *gin.Context) {
	var req AddAbilityToRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBadRequest(c, err)
		return
	}
	if err := service.AddAbilityToRole(req.RoleID, req.AbilityID); err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Ability added to role successfully"})
}
