package mysql

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/pflag"
	"gorm.io/gorm/logger"
)

const (
	keyHost     = "host"
	keyPort     = "port"
	keyUser     = "user"
	keyPassword = "password"
	keyDBName   = "dbname"
	keyParams   = "params"
	keyLogLevel = "log_level"
)

type DefaultOption func(o *Options)

// SetDefaultRdbUser returns a DefaultOption that specifies default RdbUser parameters
func SetDefaultRdbUser(s string) DefaultOption {
	return func(o *Options) {
		o.RdbUser = s
	}
}

// SetDefaultRdbPassword returns a DefaultOption that specifies default RdbPassword parameters
func SetDefaultRdbPassword(s string) DefaultOption {
	return func(o *Options) {
		o.RdbPassword = s
	}
}

// SetDefaultRdbHost returns a DefaultOption that specifies default RdbHost parameters
func SetDefaultRdbHost(s string) DefaultOption {
	return func(o *Options) {
		o.RdbHost = s
	}
}

// SetDefaultRdbPort returns a DefaultOption that specifies default RdbPort parameters
func SetDefaultRdbPort(n int) DefaultOption {
	return func(o *Options) {
		o.RdbPort = n
	}
}

// SetDefaultRdbDbname returns a DefaultOption that specifies default RdbDbname parameters
func SetDefaultRdbDbname(s string) DefaultOption {
	return func(o *Options) {
		o.RdbDbname = s
	}
}

// SetDefaultRdbLogLevel returns a DefaultOption that specifies default RdbLogLevel parameters
func SetDefaultRdbLogLevel(n logger.LogLevel) DefaultOption {
	return func(o *Options) {
		o.RdbLogLevel = int(n)
	}
}

// New-style default setters (for config/flags with dot keys).
func SetDefaultHost(s string) DefaultOption     { return SetDefaultRdbHost(s) }
func SetDefaultPort(n int) DefaultOption        { return SetDefaultRdbPort(n) }
func SetDefaultUser(s string) DefaultOption     { return SetDefaultRdbUser(s) }
func SetDefaultPassword(s string) DefaultOption { return SetDefaultRdbPassword(s) }
func SetDefaultDBName(s string) DefaultOption   { return SetDefaultRdbDbname(s) }
func SetDefaultParams(s string) DefaultOption {
	return func(o *Options) { o.Params = s }
}
func SetDefaultLogLevel(s string) DefaultOption {
	return func(o *Options) { o.LogLevel = s }
}

type Options struct {
	RdbUser     string `mapstructure:"user" yaml:"user"`
	RdbPassword string `mapstructure:"password" yaml:"password"`
	RdbHost     string `mapstructure:"host" yaml:"host"`
	RdbPort     int    `mapstructure:"port" yaml:"port"`
	RdbDbname   string `mapstructure:"dbname" yaml:"dbname"`

	Params string `mapstructure:"params" yaml:"params"`

	LogLevel    string `mapstructure:"log_level" yaml:"log_level"`
	RdbLogLevel int    `mapstructure:"-" yaml:"-"`
}

func NewMysqlOptions(opts ...DefaultOption) *Options {
	o := &Options{
		RdbUser:     "root",
		RdbPassword: "123456",
		RdbHost:     "localhost",
		RdbPort:     3306,
		RdbDbname:   "async_vm",
		Params:      "charset=utf8mb4&parseTime=True&loc=Local",
		LogLevel:    "info",
		RdbLogLevel: int(logger.Info),
	}

	for _, opt := range opts {
		opt(o)
	}

	return o
}

// Validate check options
func (o *Options) Validate() []error {
	errors := make([]error, 0)

	if o.RdbUser == "" {
		errors = append(errors, fmt.Errorf("rdb user is empty"))
	}
	if o.RdbPassword == "" {
		errors = append(errors, fmt.Errorf("rdb password is empty"))
	}
	if o.RdbHost == "" {
		errors = append(errors, fmt.Errorf("rdb host is empty"))
	}
	if o.RdbPort < 0 || o.RdbPort > 65535 {
		errors = append(errors, fmt.Errorf("rdb port is invaild"))
	}
	if o.RdbDbname == "" {
		errors = append(errors, fmt.Errorf("rdb dbname is empty"))
	}

	return errors
}

// AddFlags keeps legacy flag names for compatibility in other packages.
// The unified CLI uses AddFlagsWithPrefix instead.
func (o *Options) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.RdbHost, "rdb-host", o.RdbHost, "mysql host")
	fs.IntVar(&o.RdbPort, "rdb-port", o.RdbPort, "mysql port")
	fs.StringVar(&o.RdbUser, "rdb-user", o.RdbUser, "mysql user")
	fs.StringVar(&o.RdbPassword, "rdb-password", o.RdbPassword, "mysql password")
	fs.StringVar(&o.RdbDbname, "rdb-dbname", o.RdbDbname, "mysql dbname")
	fs.StringVar(&o.Params, "rdb-params", o.Params, "mysql params query string")
	// Keep legacy flag name, but accept both:
	// - numeric gorm level (sets RdbLogLevel, clears LogLevel)
	// - text level (sets LogLevel)
	fs.Var(&gormLogLevelFlag{logLevel: &o.LogLevel, rdbLogLevel: &o.RdbLogLevel}, "rdb-log-level", "gorm log level (silent/error/warn/info) or integer level")
}

func (o *Options) AddFlagsWithPrefix(fs *pflag.FlagSet, prefix string) {
	p := prefix
	if p != "" {
		p += "."
	}
	fs.StringVar(&o.RdbHost, p+keyHost, o.RdbHost, "mysql host")
	fs.IntVar(&o.RdbPort, p+keyPort, o.RdbPort, "mysql port")
	fs.StringVar(&o.RdbUser, p+keyUser, o.RdbUser, "mysql user")
	fs.StringVar(&o.RdbPassword, p+keyPassword, o.RdbPassword, "mysql password")
	fs.StringVar(&o.RdbDbname, p+keyDBName, o.RdbDbname, "mysql dbname")
	fs.StringVar(&o.Params, p+keyParams, o.Params, "mysql params query string")
	fs.Var(&gormLogLevelFlag{logLevel: &o.LogLevel, rdbLogLevel: &o.RdbLogLevel}, p+keyLogLevel, "gorm log level (silent/error/warn/info) or integer level")
}

// LoadEnv loads configuration items from environment variables.
//
// Env naming: <envPrefix>_RDB_*
// - RDB_USER / RDB_PASSWORD / RDB_HOST / RDB_PORT / RDB_DBNAME
// - RDB_PARAMS
// - RDB_LOG_LEVEL (supports integer gorm level; or "silent/error/warn/info" for LogLevel)
//
// Priority is preserved by skipping fields whose corresponding flag is Changed.
// - legacy flags: rdb-user / rdb-password / rdb-host / rdb-port / rdb-dbname / rdb-params / rdb-log-level
// - prefixed flags: <flagPrefix>.user / .password / .host / .port / .dbname / .params / .log_level
func (o *Options) LoadEnv(envPrefix string, fs *pflag.FlagSet, flagPrefix string) []error {
	var errs []error
	envPrefix = strings.TrimSpace(envPrefix)
	flagPrefix = strings.TrimSpace(flagPrefix)

	userFlag := "rdb-user"
	passFlag := "rdb-password"
	hostFlag := "rdb-host"
	portFlag := "rdb-port"
	dbFlag := "rdb-dbname"
	paramsFlag := "rdb-params"
	logFlag := "rdb-log-level"
	if flagPrefix != "" {
		userFlag = flagPrefix + "." + keyUser
		passFlag = flagPrefix + "." + keyPassword
		hostFlag = flagPrefix + "." + keyHost
		portFlag = flagPrefix + "." + keyPort
		dbFlag = flagPrefix + "." + keyDBName
		paramsFlag = flagPrefix + "." + keyParams
		logFlag = flagPrefix + "." + keyLogLevel
	}

	if v, ok := os.LookupEnv(envPrefix + "_RDB_USER"); ok && !flagChanged(fs, userFlag) {
		o.RdbUser = v
	}
	if v, ok := os.LookupEnv(envPrefix + "_RDB_PASSWORD"); ok && !flagChanged(fs, passFlag) {
		o.RdbPassword = v
	}
	if v, ok := os.LookupEnv(envPrefix + "_RDB_HOST"); ok && !flagChanged(fs, hostFlag) {
		o.RdbHost = v
	}
	if v, ok := os.LookupEnv(envPrefix + "_RDB_PORT"); ok && !flagChanged(fs, portFlag) {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			errs = append(errs, fmt.Errorf("invalid %s_RDB_PORT: %w", envPrefix, err))
		} else {
			o.RdbPort = n
		}
	}
	if v, ok := os.LookupEnv(envPrefix + "_RDB_DBNAME"); ok && !flagChanged(fs, dbFlag) {
		o.RdbDbname = v
	}
	if v, ok := os.LookupEnv(envPrefix + "_RDB_PARAMS"); ok && !flagChanged(fs, paramsFlag) {
		o.Params = v
	}
	if v, ok := os.LookupEnv(envPrefix + "_RDB_LOG_LEVEL"); ok && !flagChanged(fs, logFlag) {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			o.RdbLogLevel = n
			// Numeric env should win over textual defaults/config.
			o.LogLevel = ""
		} else {
			// Accept "silent/error/warn/info" as preferred config surface.
			o.LogLevel = v
		}
	}

	return errs
}

func flagChanged(fs *pflag.FlagSet, name string) bool {
	if fs == nil {
		return false
	}
	f := fs.Lookup(name)
	if f == nil {
		return false
	}
	return f.Changed
}

type gormLogLevelFlag struct {
	logLevel    *string
	rdbLogLevel *int
}

func (f *gormLogLevelFlag) String() string {
	if f == nil {
		return ""
	}
	if f.logLevel != nil && strings.TrimSpace(*f.logLevel) != "" {
		return *f.logLevel
	}
	if f.rdbLogLevel != nil {
		return strconv.Itoa(*f.rdbLogLevel)
	}
	return ""
}

func (f *gormLogLevelFlag) Type() string { return "gormLogLevel" }

func (f *gormLogLevelFlag) Set(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if n, err := strconv.Atoi(s); err == nil {
		if f.rdbLogLevel != nil {
			*f.rdbLogLevel = n
		}
		if f.logLevel != nil {
			*f.logLevel = ""
		}
		return nil
	}
	if f.logLevel != nil {
		*f.logLevel = s
	}
	return nil
}
