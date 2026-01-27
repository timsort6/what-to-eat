package mysql

import (
	"fmt"
	"strings"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// NewMysqlClient create a gorm mysql client
func NewMysqlClient(options *Options) (*gorm.DB, error) {
	params := strings.TrimSpace(options.Params)
	if params == "" {
		params = "charset=utf8mb4&parseTime=True&loc=Local"
	}
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?%s",
		options.RdbUser, options.RdbPassword, options.RdbHost, options.RdbPort, options.RdbDbname, params)

	db, err := gorm.Open(mysql.Open(dsn))
	if err != nil {
		return nil, err
	}

	lvl := logger.LogLevel(options.RdbLogLevel)
	if strings.TrimSpace(options.LogLevel) != "" {
		switch strings.ToLower(strings.TrimSpace(options.LogLevel)) {
		case "silent":
			lvl = logger.Silent
		case "error":
			lvl = logger.Error
		case "warn", "warning":
			lvl = logger.Warn
		case "info":
			lvl = logger.Info
		}
	}
	db.Logger = db.Logger.LogMode(lvl)

	return db, nil
}
