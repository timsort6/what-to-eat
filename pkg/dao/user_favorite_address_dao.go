package dao

import (
	"context"
	"fmt"
	"food-agent/pkg/model"

	"gorm.io/gorm"
)

// UserFavoriteAddressDAO 提供 UserFavoriteAddress 的基础 CRUD 操作。
type UserFavoriteAddressDAO struct {
	db *gorm.DB
}

func NewUserFavoriteAddressDAO(db *gorm.DB) *UserFavoriteAddressDAO {
	return &UserFavoriteAddressDAO{db: db}
}

func (d *UserFavoriteAddressDAO) Create(ctx context.Context, addr *model.UserFavoriteAddress) error {
	if err := d.db.WithContext(ctx).Create(addr).Error; err != nil {
		return fmt.Errorf("create favorite address: %w", err)
	}
	return nil
}

func (d *UserFavoriteAddressDAO) GetByID(ctx context.Context, id uint) (*model.UserFavoriteAddress, error) {
	var addr model.UserFavoriteAddress
	if err := d.db.WithContext(ctx).First(&addr, id).Error; err != nil {
		return nil, fmt.Errorf("get favorite address by id=%d: %w", id, err)
	}
	return &addr, nil
}

func (d *UserFavoriteAddressDAO) Update(ctx context.Context, addr *model.UserFavoriteAddress) error {
	if err := d.db.WithContext(ctx).Save(addr).Error; err != nil {
		return fmt.Errorf("update favorite address id=%d: %w", addr.ID, err)
	}
	return nil
}

func (d *UserFavoriteAddressDAO) Delete(ctx context.Context, id uint) error {
	if err := d.db.WithContext(ctx).Delete(&model.UserFavoriteAddress{}, id).Error; err != nil {
		return fmt.Errorf("delete favorite address id=%d: %w", id, err)
	}
	return nil
}

func (d *UserFavoriteAddressDAO) ListByUserID(ctx context.Context, userID uint, offset, limit int) ([]model.UserFavoriteAddress, error) {
	if limit <= 0 {
		limit = 20
	}
	var addrs []model.UserFavoriteAddress
	if err := d.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("is_default desc, id desc").
		Offset(offset).
		Limit(limit).
		Find(&addrs).Error; err != nil {
		return nil, fmt.Errorf("list favorite addresses by user_id=%d: %w", userID, err)
	}
	return addrs, nil
}
