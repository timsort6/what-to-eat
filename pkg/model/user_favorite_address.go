package model

import (
	"time"

	"gorm.io/gorm"
)

// UserFavoriteAddress 用户收藏地址（用于常用地址查询/推荐）。
//
// 这里会存储“用户语义名称 + 结构化地址 + 经纬度”等信息，便于后续：
// - 结合 Amap 做周边搜索
// - 结合用户偏好做推荐
type UserFavoriteAddress struct {
	ID        uint           `gorm:"primaryKey"`
	CreatedAt time.Time      `gorm:"not null"`
	UpdatedAt time.Time      `gorm:"not null"`
	DeletedAt gorm.DeletedAt `gorm:"index"`

	UserID uint `gorm:"index;not null"`

	Name    string `gorm:"size:64;not null"`  // 例如：家/公司/健身房
	Address string `gorm:"size:255;not null"` // 详细地址文本

	Longitude float64 `gorm:"not null"` // 经度
	Latitude  float64 `gorm:"not null"` // 纬度

	City   string `gorm:"size:64"`
	Adcode string `gorm:"size:32"` // 行政区划代码

	IsDefault bool `gorm:"not null;default:false"` // 是否默认地址
}
