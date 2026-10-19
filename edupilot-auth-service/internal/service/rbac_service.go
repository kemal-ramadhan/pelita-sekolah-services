package service

import (
	"errors"
	"os"
	"strings"
	"time"

	"github.com/edupilot-auth-service/internal/config"
	"github.com/edupilot-auth-service/internal/model"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const AbilityManageRoles = "manage_roles"

// GrantAbility upserts a per-user ability override.
func GrantAbility(callerID uint, callerRole string, targetID, abilityID uint, isActive bool) (*model.UserAbility, error) {
	if targetID == callerID {
		return nil, badRequest("Tidak dapat mengubah ability akun sendiri")
	}
	var ability model.Ability
	if err := config.DB.First(&ability, abilityID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, notFound("Ability tidak ditemukan")
		}
		return nil, err
	}
	if ability.Name == AbilityManageRoles && callerRole != RoleSuperAdmin {
		return nil, forbidden("Hanya super_admin yang dapat memberikan manage_roles")
	}
	targetRole, err := getUserRole(targetID)
	if err != nil {
		return nil, err
	}
	if !canManageUser(callerRole, targetRole) {
		return nil, forbidden("Anda tidak berhak mengubah user ini")
	}

	ua := model.UserAbility{
		UserID:      targetID,
		AbilityID:   abilityID,
		GrantedByID: &callerID,
		IsActive:    isActive,
	}
	if err := config.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "ability_id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{"is_active": isActive, "granted_by_id": callerID, "updated_at": time.Now()}),
	}).Create(&ua).Error; err != nil {
		return nil, err
	}
	if err := config.DB.Preload("Ability").Where("user_id = ? AND ability_id = ?", targetID, abilityID).First(&ua).Error; err != nil {
		return nil, err
	}
	return &ua, nil
}

// AssignRole links an RBAC role to a user. Returns created=false if it already existed.
func AssignRole(callerID uint, callerRole string, targetID, roleID uint) (bool, error) {
	if targetID == callerID {
		return false, badRequest("Tidak dapat mengubah role akun sendiri")
	}
	var role model.Role
	if err := config.DB.Preload("Abilities").First(&role, roleID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, notFound("Role tidak ditemukan")
		}
		return false, err
	}
	if callerRole != RoleSuperAdmin {
		if RoleRank(role.Name) >= RoleRank(callerRole) {
			return false, forbidden("Tidak dapat memberikan role setara atau lebih tinggi dari role Anda")
		}
		for _, ab := range role.Abilities {
			if ab.Name == AbilityManageRoles {
				return false, forbidden("Hanya super_admin yang dapat memberikan role dengan manage_roles")
			}
		}
	}
	targetRole, err := getUserRole(targetID)
	if err != nil {
		return false, err
	}
	if !canManageUser(callerRole, targetRole) {
		return false, forbidden("Anda tidak berhak mengubah user ini")
	}

	res := config.DB.Clauses(clause.OnConflict{DoNothing: true}).
		Create(&model.UserRole{UserID: targetID, RoleID: roleID})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// AddAbilityToRole appends an ability to a role packet (idempotent).
func AddAbilityToRole(roleID, abilityID uint) error {
	var role model.Role
	if err := config.DB.First(&role, roleID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return notFound("Role tidak ditemukan")
		}
		return err
	}
	var ability model.Ability
	if err := config.DB.First(&ability, abilityID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return notFound("Ability tidak ditemukan")
		}
		return err
	}
	return config.DB.Model(&role).Association("Abilities").Append(&ability)
}

// ---------------------------------------------------------------------------
// Seeding (idempotent)
// ---------------------------------------------------------------------------

var seedAbilities = map[string]string{
	"manage_teachers":  "Kelola data guru",
	AbilityManageRoles: "Kelola role dan ability",
	"view_reports":     "Lihat laporan",
	"manage_schools":   "Kelola data sekolah",
}

var seedRoles = []struct {
	Name        string
	Description string
	Abilities   []string
}{
	{RoleSuperAdmin, "Pemilik platform", []string{"manage_teachers", AbilityManageRoles, "view_reports", "manage_schools"}},
	{RoleAdmin, "Staf platform", []string{"manage_teachers", "view_reports", "manage_schools"}},
	{RoleInstitution, "Akun sekolah", []string{"manage_teachers", "view_reports"}},
	{RoleCustomer, "Pemilik sekolah (legacy)", []string{"manage_teachers", "view_reports"}},
	{RoleTeacher, "Guru (akun undangan sekolah)", nil},
}

func SeedRBAC() error {
	return config.DB.Transaction(func(tx *gorm.DB) error {
		abilities := map[string]model.Ability{}
		for name, desc := range seedAbilities {
			ab := model.Ability{Name: name, Description: desc}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&ab).Error; err != nil {
				return err
			}
			if err := tx.Where("name = ?", name).First(&ab).Error; err != nil {
				return err
			}
			abilities[name] = ab
		}
		for _, r := range seedRoles {
			role := model.Role{Name: r.Name, Description: r.Description}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Omit("Abilities").Create(&role).Error; err != nil {
				return err
			}
			if err := tx.Where("name = ?", r.Name).First(&role).Error; err != nil {
				return err
			}
			if len(r.Abilities) == 0 {
				continue
			}
			list := make([]model.Ability, 0, len(r.Abilities))
			for _, a := range r.Abilities {
				list = append(list, abilities[a])
			}
			if err := tx.Model(&role).Association("Abilities").Append(list); err != nil {
				return err
			}
		}
		return nil
	})
}

// SeedSuperAdmin creates the super_admin from SEED_ADMIN_EMAIL / SEED_ADMIN_PASSWORD
// when both are set and the email does not exist yet.
func SeedSuperAdmin() error {
	email := NormalizeEmail(os.Getenv("SEED_ADMIN_EMAIL"))
	password := os.Getenv("SEED_ADMIN_PASSWORD")
	if email == "" || password == "" {
		return nil
	}
	if len(password) < 8 {
		return errors.New("SEED_ADMIN_PASSWORD must be at least 8 characters")
	}
	_, err := CreateUser(CreateUserRequest{
		Email:    email,
		Password: password,
		FullName: "Super Admin",
		Role:     RoleSuperAdmin,
	}, RoleSuperAdmin)
	var appErr *AppError
	if errors.As(err, &appErr) && appErr == ErrEmailTaken {
		return nil
	}
	if err == nil && config.Logger != nil {
		config.Logger.Info("seeded super_admin user", zap.String("email", strings.ToLower(email)))
	}
	return err
}
