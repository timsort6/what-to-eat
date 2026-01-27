package cache

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/pflag"
)

const (
	keyAddr     = "addr"
	keyPassword = "password"
	keyDB       = "db"
)

type DefaultOption func(o *Options)

func SetDefaultAddr(s string) DefaultOption     { return func(o *Options) { o.Host = s } }
func SetDefaultPassword(s string) DefaultOption { return func(o *Options) { o.Password = s } }
func SetDefaultDB(n int) DefaultOption          { return func(o *Options) { o.DB = n } }

type Options struct {
	Host     string `json:"host" mapstructure:"addr" yaml:"addr"`
	Password string `json:"password" mapstructure:"password" yaml:"password"`
	DB       int    `json:"db" mapstructure:"db" yaml:"db"`
}

// NewRedisOptions returns options points to nowhere,
// because redis is not required for some components
func NewRedisOptions(opts ...DefaultOption) *Options {
	o := &Options{
		Host:     "",
		Password: "",
		DB:       0,
	}

	for _, opt := range opts {
		opt(o)
	}
	return o
}

// Validate check options
func (r *Options) Validate() []error {
	errors := make([]error, 0)

	if r.DB > 15 || r.DB < 0 {
		errors = append(errors, fmt.Errorf("invalid redis db"))
	}

	return errors
}

// AddFlags add option flags to command line flags,
// if redis-host left empty, the following options will be ignored.
func (r *Options) AddFlags(fs *pflag.FlagSet) {
	// Legacy flag names (kept for compatibility).
	fs.StringVar(&r.Host, "redis-host", r.Host, "Redis connection URL. If left blank, redis will be disabled.")
	fs.StringVar(&r.Password, "redis-password", r.Password, "Redis password")
	fs.IntVar(&r.DB, "redis-db", r.DB, "Redis db")
}

func (r *Options) AddFlagsWithPrefix(fs *pflag.FlagSet, prefix string) {
	p := prefix
	if p != "" {
		p += "."
	}
	fs.StringVar(&r.Host, p+keyAddr, r.Host, "Redis address (host:port). If blank, redis is disabled.")
	fs.StringVar(&r.Password, p+keyPassword, r.Password, "Redis password")
	fs.IntVar(&r.DB, p+keyDB, r.DB, "Redis db")
}

// LoadEnv loads configuration items from environment variables.
//
// Env naming: <envPrefix>_REDIS_HOST / _REDIS_PASSWORD / _REDIS_DB
//
// Priority is preserved by skipping fields whose corresponding flag is Changed.
// - legacy flags: redis-host / redis-password / redis-db
// - prefixed flags: <flagPrefix>.addr / .password / .db
func (r *Options) LoadEnv(envPrefix string, fs *pflag.FlagSet, flagPrefix string) []error {
	var errs []error

	hostFlag := "redis-host"
	passFlag := "redis-password"
	dbFlag := "redis-db"
	if strings.TrimSpace(flagPrefix) != "" {
		hostFlag = strings.TrimSpace(flagPrefix) + "." + keyAddr
		passFlag = strings.TrimSpace(flagPrefix) + "." + keyPassword
		dbFlag = strings.TrimSpace(flagPrefix) + "." + keyDB
	}

	if v, ok := os.LookupEnv(strings.TrimSpace(envPrefix) + "_REDIS_HOST"); ok && !flagChanged(fs, hostFlag) {
		r.Host = v
	}
	if v, ok := os.LookupEnv(strings.TrimSpace(envPrefix) + "_REDIS_PASSWORD"); ok && !flagChanged(fs, passFlag) {
		r.Password = v
	}
	if v, ok := os.LookupEnv(strings.TrimSpace(envPrefix) + "_REDIS_DB"); ok && !flagChanged(fs, dbFlag) {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			errs = append(errs, fmt.Errorf("invalid %s_REDIS_DB: %w", strings.TrimSpace(envPrefix), err))
		} else {
			r.DB = n
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
