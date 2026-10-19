package service

import (
	"errors"
	"strings"
	"time"

	"github.com/edupilot-auth-service/internal/config"
	"github.com/edupilot-auth-service/internal/model"
	"github.com/edupilot-auth-service/internal/utils"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type CreateUserRequest struct {
	Email    string `json:"email" binding:"required,email,max=255"`
	Password string `json:"password" binding:"required,min=8,max=72"`
	FullName string `json:"full_name" binding:"required,max=255"`
	Phone    string `json:"phone" binding:"max=50"`
	Role     string `json:"role" binding:"required"`
}

type CreatedUser struct {
	ID    uint   `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

// assignSystemRBACRole links a user to the RBAC role named like users.role, if that
// RBAC role exists (it is seeded at startup). Duplicates are ignored.
func assignSystemRBACRole(tx *gorm.DB, userID uint, roleName string) error {
	var rbacRole model.Role
	err := tx.Where("name = ?", roleName).First(&rbacRole).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if config.Logger != nil {
			config.Logger.Warn("rbac role not found, skipping assignment", zap.String("role", roleName))
		}
		return nil
	}
	if err != nil {
		return err
	}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).
		Create(&model.UserRole{UserID: userID, RoleID: rbacRole.ID}).Error
}

// CreateUser creates a pre-verified user. callerRole is the admin's role, or "" for
// internal service-to-service calls (which may only create institution/customer).
func CreateUser(req CreateUserRequest, callerRole string) (*CreatedUser, error) {
	role := strings.TrimSpace(req.Role)
	if !IsValidRole(role) {
		return nil, ErrInvalidRole
	}
	if !canAssignRole(callerRole, role) {
		return nil, forbidden("Anda tidak berhak membuat user dengan role ini")
	}

	email := NormalizeEmail(req.Email)
	var count int64
	if err := whereEmail(config.DB.Model(&model.User{}), email).Count(&count).Error; err != nil {
		return nil, err
	}
	if count > 0 {
		return nil, ErrEmailTaken
	}

	hashed, err := utils.HashPassword(req.Password)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	user := model.User{
		Email:           email,
		Password:        hashed,
		Phone:           strings.TrimSpace(req.Phone),
		Role:            role,
		EmailVerifiedAt: &now, // admin/internal-created accounts are pre-verified
	}

	err = config.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&user).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.Customer{
			UserID:   user.ID,
			FullName: strPtrOrNil(req.FullName),
			Role:     role,
		}).Error; err != nil {
			return err
		}
		return assignSystemRBACRole(tx, user.ID, role)
	})
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return nil, ErrEmailTaken
	}
	if err != nil {
		return nil, err
	}
	return &CreatedUser{ID: user.ID, Email: user.Email, Role: user.Role}, nil
}

func getUserRole(userID uint) (string, error) {
	var user model.User
	err := config.DB.Select("id", "role").Where("id = ?", userID).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", ErrUserNotFound
	}
	return user.Role, err
}

// AdminUpdateProfile updates another user's profile (replaces PUT /auth/profile/:id).
func AdminUpdateProfile(callerID uint, callerRole string, targetID uint, req UpdateProfileRequest) (*UserDTO, error) {
	targetRole, err := getUserRole(targetID)
	if err != nil {
		return nil, err
	}
	if targetID != callerID && !canManageUser(callerRole, targetRole) {
		return nil, forbidden("Anda tidak berhak mengubah user ini")
	}
	return applyProfile(targetID, req)
}

// ChangeUserRole changes users.role and the matching system RBAC role, then revokes
// the target's sessions (the role is embedded in their JWTs).
func ChangeUserRole(callerID uint, callerRole string, targetID uint, newRole string) (*UserDTO, error) {
	newRole = strings.TrimSpace(newRole)
	if !IsValidRole(newRole) {
		return nil, ErrInvalidRole
	}
	if targetID == callerID {
		return nil, badRequest("Tidak dapat mengubah role akun sendiri")
	}
	targetRole, err := getUserRole(targetID)
	if err != nil {
		return nil, err
	}
	if !canManageUser(callerRole, targetRole) || !canAssignRole(callerRole, newRole) {
		return nil, forbidden("Anda tidak berhak memberikan role ini")
	}

	systemRoles := []string{RoleSuperAdmin, RoleAdmin, RoleInstitution, RoleCustomer, RoleTeacher}
	err = config.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.User{}).Where("id = ?", targetID).Update("role", newRole).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ? AND role_id IN (?)", targetID,
			tx.Model(&model.Role{}).Select("id").Where("name IN ?", systemRoles)).
			Delete(&model.UserRole{}).Error; err != nil {
			return err
		}
		return assignSystemRBACRole(tx, targetID, newRole)
	})
	if err != nil {
		return nil, err
	}

	if err := RevokeSessions(targetID); err != nil {
		logErr("failed to revoke sessions after role change", err, zap.Uint("user_id", targetID))
	}

	user, err := loadUser(config.DB, targetID)
	if err != nil {
		return nil, err
	}
	return ToUserDTO(user), nil
}

// DeleteUserInternal removes a user with its customer, roles and abilities
// (compensation for a failed multi-service flow, or removal of a teacher account).
// Admin accounts cannot be deleted here.
func DeleteUserInternal(userID uint) error {
	role, err := getUserRole(userID)
	if err != nil {
		return err
	}
	if IsAdminRole(role) {
		return forbidden("Akun admin tidak dapat dihapus melalui endpoint internal")
	}

	err = config.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", userID).Delete(&model.UserAbility{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&model.UserRole{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&model.Customer{}).Error; err != nil {
			return err
		}
		res := tx.Where("id = ?", userID).Delete(&model.User{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrUserNotFound
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := RevokeSessions(userID); err != nil {
		logErr("failed to revoke sessions after user delete", err, zap.Uint("user_id", userID))
	}
	return nil
}
