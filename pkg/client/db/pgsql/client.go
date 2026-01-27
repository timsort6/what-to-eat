package pgsql

import (
	"fmt"
	"strings"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// NewPgsqlClient create a gorm pgsql client
func NewPgsqlClient(options *Options) (*gorm.DB, error) {
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%d sslmode=%s",
		options.RdbHost, options.RdbUser, options.RdbPassword, options.RdbDbname, options.RdbPort, options.RdbSSLMode)
	if tz := strings.TrimSpace(options.TimeZone); tz != "" {
		dsn += fmt.Sprintf(" TimeZone=%s", tz)
	}

	db, err := gorm.Open(postgres.Open(dsn))
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
