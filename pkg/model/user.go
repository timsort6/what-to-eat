package model

import (
	"time"

	"gorm.io/gorm"
)

// User 用户表（用于登录/鉴权的基础数据）。
//
// 注意：这里只存 PasswordHash（推荐 bcrypt/argon2 的 hash），严禁存明文密码。
type User struct {
	ID        uint           `gorm:"primaryKey"`
	CreatedAt time.Time      `gorm:"not null"`
	UpdatedAt time.Time      `gorm:"not null"`
	DeletedAt gorm.DeletedAt `gorm:"index"`

	Username     string `gorm:"size:64;uniqueIndex;not null"`
	PasswordHash string `gorm:"size:255;not null"`

	// 可选信息
	Nickname string `gorm:"size:64"`
	Email    string `gorm:"size:128;index"`
}
