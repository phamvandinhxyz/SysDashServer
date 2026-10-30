package models

import "time"

const (
	RoleAdmin  = "admin"
	RoleViewer = "viewer"
)

type User struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Username  string    `gorm:"uniqueIndex;not null;size:64" json:"username"`
	Password  string    `gorm:"not null" json:"-"`
	FullName  string    `gorm:"size:128" json:"full_name"`
	Role      string    `gorm:"size:32;default:'viewer'" json:"role"` // "admin" or "viewer"
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type LoginResponse struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
	User      User   `json:"user"`
}

type RegisterRequest struct {
	Username string `json:"username" binding:"required,min=3,max=32"`
	Password string `json:"password" binding:"required,min=6"`
	FullName string `json:"full_name"`
	Role     string `json:"role"` // Admin can specify role, default viewer
}

type UpdateUserRequest struct {
	FullName string `json:"full_name"`
	Role     string `json:"role"`
	Password string `json:"password"` // optional, only if changing
}
