package model

import (
	"time"

	"gorm.io/gorm"
)

// UserFavoriteAddress 用户常用/收藏地址。
//
// 设计目标：
// - 关联用户（UserID）
// - 存储尽可能完整的地图信息（来源于 AMap/高德前端 SDK 或 WebService 返回）
// - 额外保留 RawPOIJSON 以便后续作为 chat 上下文输入
type UserFavoriteAddress struct {
	ID        uint           `gorm:"primaryKey"`
	CreatedAt time.Time      `gorm:"not null"`
	UpdatedAt time.Time      `gorm:"not null"`
	DeletedAt gorm.DeletedAt `gorm:"index"`

	UserID uint `gorm:"index;not null"`

	// Label 是用户对该地址的语义标注，例如：家/公司/健身房。
	Label string `gorm:"size:64;not null"`

	// POI 关键信息（尽量对齐 AMap POI 字段，便于前端展示与后续检索）。
	AmapID   string `gorm:"size:64;index"` // 高德 POI id（如有）
	Name     string `gorm:"size:128"`      // POI 名称（如有）
	Type     string `gorm:"size:128"`      // POI type（如有）
	TypeCode string `gorm:"size:64"`       // POI typecode（如有）
	Tel      string `gorm:"size:128"`      // 电话（如有）
	Tag      string `gorm:"size:128"`      // 标签（如有）

	// Address/FormattedAddress 兼容不同来源字段（PlaceSearch/Geocode/JS SDK 等）。
	FormattedAddress string `gorm:"size:255"` // 常用于展示（如有）
	Address          string `gorm:"size:255"` // POI address（如有）

	// Location 为 "lng,lat"（建议前端直接透传）。
	Location  string  `gorm:"size:64;not null"`
	Longitude float64 `gorm:"not null"` // 经度（从 Location 解析，便于排序/计算）
	Latitude  float64 `gorm:"not null"` // 纬度（从 Location 解析，便于排序/计算）

	// 行政区划/城市信息（可选）
	Pname    string `gorm:"size:64"` // 省名
	Cityname string `gorm:"size:64"` // 市名
	Adname   string `gorm:"size:64"` // 区名
	Citycode string `gorm:"size:32"`
	Adcode   string `gorm:"size:32"`

	// RawPOIJSON 存储前端/SDK 返回的原始 POI JSON（尽可能完整）。
	// 使用 text 以保证跨 pgsql/mysql 的兼容性。
	RawPOIJSON string `gorm:"type:text"`

	IsDefault bool `gorm:"not null;default:false"` // 是否默认地址（每个用户最多一个）
}
