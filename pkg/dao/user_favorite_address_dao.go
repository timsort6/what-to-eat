package dao

import (
	"context"
	"errors"
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

func (d *UserFavoriteAddressDAO) CountByUserID(ctx context.Context, userID uint) (int64, error) {
	var cnt int64
	if err := d.db.WithContext(ctx).
		Model(&model.UserFavoriteAddress{}).
		Where("user_id = ?", userID).
		Count(&cnt).Error; err != nil {
		return 0, fmt.Errorf("count favorite address by user_id=%d: %w", userID, err)
	}
	return cnt, nil
}

func (d *UserFavoriteAddressDAO) ListByUserID(ctx context.Context, userID uint, offset, limit int) ([]model.UserFavoriteAddress, error) {
	if limit <= 0 {
		limit = 20
	}
	var addrs []model.UserFavoriteAddress
	if err := d.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("is_default desc, id asc").
		Offset(offset).
		Limit(limit).
		Find(&addrs).Error; err != nil {
		return nil, fmt.Errorf("list favorite addresses by user_id=%d: %w", userID, err)
	}
	return addrs, nil
}

func (d *UserFavoriteAddressDAO) ListAllByUserID(ctx context.Context, userID uint) ([]model.UserFavoriteAddress, error) {
	var addrs []model.UserFavoriteAddress
	if err := d.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("is_default desc, id asc").
		Find(&addrs).Error; err != nil {
		return nil, fmt.Errorf("list all favorite addresses by user_id=%d: %w", userID, err)
	}
	return addrs, nil
}

func (d *UserFavoriteAddressDAO) ClearDefaultByUserID(ctx context.Context, userID uint) error {
	if err := d.db.WithContext(ctx).
		Model(&model.UserFavoriteAddress{}).
		Where("user_id = ? AND is_default = ?", userID, true).
		Update("is_default", false).Error; err != nil {
		return fmt.Errorf("clear default favorite address by user_id=%d: %w", userID, err)
	}
	return nil
}

func (d *UserFavoriteAddressDAO) FindMinIDByUserID(ctx context.Context, userID uint) (uint, bool, error) {
	var addr model.UserFavoriteAddress
	if err := d.db.WithContext(ctx).
		Select("id").
		Where("user_id = ?", userID).
		Order("id asc").
		First(&addr).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("find min favorite address id by user_id=%d: %w", userID, err)
	}
	return addr.ID, true, nil
}

func (d *UserFavoriteAddressDAO) SetDefaultByID(ctx context.Context, id uint, isDefault bool) error {
	if err := d.db.WithContext(ctx).
		Model(&model.UserFavoriteAddress{}).
		Where("id = ?", id).
		Update("is_default", isDefault).Error; err != nil {
		return fmt.Errorf("set favorite address default id=%d: %w", id, err)
	}
	return nil
}
