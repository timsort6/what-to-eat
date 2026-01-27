package model

import "gorm.io/gorm"

// GlobalDst 统一维护需要自动迁移（AutoMigrate）的数据模型列表。
//
// 参考 tiansuoVM/pkg/model/migrate.go 的组织方式：启动时由 cmd/server 显式调用 AutoMigrate。
var GlobalDst = []any{
	&User{},
	&UserFavoriteAddress{},
}

// AutoMigrate 自动迁移数据库表结构。
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(GlobalDst...)
}
