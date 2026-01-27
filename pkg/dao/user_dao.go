package dao

import (
	"context"
	"fmt"
	"food-agent/pkg/model"

	"gorm.io/gorm"
)

// UserDAO 提供 User 的基础 CRUD 操作。
type UserDAO struct {
	db *gorm.DB
}

func NewUserDAO(db *gorm.DB) *UserDAO {
	return &UserDAO{db: db}
}

func (d *UserDAO) Create(ctx context.Context, user *model.User) error {
	if err := d.db.WithContext(ctx).Create(user).Error; err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	return nil
}

func (d *UserDAO) GetByID(ctx context.Context, id uint) (*model.User, error) {
	var user model.User
	if err := d.db.WithContext(ctx).First(&user, id).Error; err != nil {
		return nil, fmt.Errorf("get user by id=%d: %w", id, err)
	}
	return &user, nil
}

func (d *UserDAO) GetByUsername(ctx context.Context, username string) (*model.User, error) {
	var user model.User
	if err := d.db.WithContext(ctx).Where("username = ?", username).First(&user).Error; err != nil {
		return nil, fmt.Errorf("get user by username=%q: %w", username, err)
	}
	return &user, nil
}

func (d *UserDAO) Update(ctx context.Context, user *model.User) error {
	if err := d.db.WithContext(ctx).Save(user).Error; err != nil {
		return fmt.Errorf("update user id=%d: %w", user.ID, err)
	}
	return nil
}

func (d *UserDAO) Delete(ctx context.Context, id uint) error {
	if err := d.db.WithContext(ctx).Delete(&model.User{}, id).Error; err != nil {
		return fmt.Errorf("delete user id=%d: %w", id, err)
	}
	return nil
}

func (d *UserDAO) List(ctx context.Context, offset, limit int) ([]model.User, error) {
	if limit <= 0 {
		limit = 20
	}
	var users []model.User
	if err := d.db.WithContext(ctx).
		Order("id desc").
		Offset(offset).
		Limit(limit).
		Find(&users).Error; err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	return users, nil
}
