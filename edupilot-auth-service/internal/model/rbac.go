package model

import "time"

// Role represents a group/packet of abilities (e.g., "Guru BK", "Admin Sekolah")
type Role struct {
	ID          uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Name        string    `gorm:"uniqueIndex;type:varchar(100);not null" json:"name"`
	Description string    `gorm:"type:text" json:"description"`
	Abilities   []Ability `gorm:"many2many:role_abilities;" json:"abilities"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Ability represents a specific feature or endpoint permission
type Ability struct {
	ID          uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Name        string    `gorm:"uniqueIndex;type:varchar(100);not null" json:"name"` // e.g. "view_reports", "manage_teachers"
	Description string    `gorm:"type:text" json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// UserRole assigns a Role (paket permission) to a specific user
type UserRole struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID    uint      `gorm:"uniqueIndex:idx_user_role;not null" json:"user_id"`
	RoleID    uint      `gorm:"uniqueIndex:idx_user_role;not null" json:"role_id"`
	Role      Role      `gorm:"foreignKey:RoleID" json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

// UserAbility allows overriding abilities for a specific user (granting extra or revoking)
type UserAbility struct {
	ID          uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID      uint      `gorm:"uniqueIndex:idx_user_ability;not null" json:"user_id"`
	AbilityID   uint      `gorm:"uniqueIndex:idx_user_ability;not null" json:"ability_id"`
	Ability     Ability   `gorm:"foreignKey:AbilityID" json:"ability"`
	GrantedByID *uint     `json:"granted_by_id"`
	IsActive    bool      `gorm:"default:true" json:"is_active"` // if false, acts as revocation even if role has it
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
