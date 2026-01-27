package logger

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/pflag"
	"go.uber.org/zap/zapcore"
)

const (
	keyLevel         = "level"
	keyFilename      = "filename"
	keyMaxSize       = "max_size"
	keyMaxBackups    = "max_backups"
	keyMaxAge        = "max_age"
	keyCompress      = "compress"
	keyAddStackLevel = "add_stack_level"
)

type DefaultOption func(o *Options)

func SetDefaultLevel(s string) DefaultOption {
	return func(o *Options) { o.Level = s }
}

func SetDefaultAddStackLevel(s string) DefaultOption {
	return func(o *Options) { o.AddStackLevel = s }
}

func SetDefaultRotation(maxSize, maxBackups, maxAge int) DefaultOption {
	return func(o *Options) {
		o.MaxSize = maxSize
		o.MaxBackups = maxBackups
		o.MaxAge = maxAge
	}
}

type Options struct {
	Level         string `mapstructure:"level" yaml:"level"`
	AddStackLevel string `mapstructure:"add_stack_level" yaml:"add_stack_level"`

	Filename   string `mapstructure:"filename" yaml:"filename"`
	MaxSize    int    `mapstructure:"max_size" yaml:"max_size"`
	MaxBackups int    `mapstructure:"max_backups" yaml:"max_backups"`
	MaxAge     int    `mapstructure:"max_age" yaml:"max_age"`
	Compress   bool   `mapstructure:"compress" yaml:"compress"`
}

func NewLoggerOptions(opts ...DefaultOption) *Options {
	o := &Options{
		Level:         "info",
		AddStackLevel: "fatal",
		Filename:      "",
		MaxSize:       10,
		MaxBackups:    10,
		MaxAge:        30,
		Compress:      false,
	}

	for _, opt := range opts {
		opt(o)
	}
	return o
}

// Validate check options
func (o *Options) Validate() []error {
	errors := make([]error, 0)

	if _, err := parseZapLevel(o.Level); err != nil {
		errors = append(errors, fmt.Errorf("invalid log level"))
	}
	if _, err := parseZapLevel(o.AddStackLevel); err != nil {
		errors = append(errors, fmt.Errorf("invalid AddStackLevel"))
	}

	return errors
}

// AddFlags add option flags to command line flags,
func (o *Options) AddFlags(fs *pflag.FlagSet) {
	// Legacy flag names (kept for compatibility).
	fs.StringVar(&o.Level, "log-level", o.Level, "log level")
	fs.StringVar(&o.AddStackLevel, "log-add-stack-level", o.AddStackLevel, "add stacktrace at/above level")
	fs.StringVar(&o.Filename, "log-filename", o.Filename, "log filename")
	fs.IntVar(&o.MaxSize, "log-max-size", o.MaxSize, "log max size")
	fs.IntVar(&o.MaxBackups, "log-max-backups", o.MaxBackups, "log max backups")
	fs.IntVar(&o.MaxAge, "log-max-age", o.MaxAge, "log max age")
	fs.BoolVar(&o.Compress, "log-compress", o.Compress, "log compress")
}

func (o *Options) AddFlagsWithPrefix(fs *pflag.FlagSet, prefix string) {
	p := prefix
	if p != "" {
		p += "."
	}
	fs.StringVar(&o.Level, p+keyLevel, o.Level, "log level")
	fs.StringVar(&o.AddStackLevel, p+keyAddStackLevel, o.AddStackLevel, "add stacktrace at/above level")
	fs.StringVar(&o.Filename, p+keyFilename, o.Filename, "log filename")
	fs.IntVar(&o.MaxSize, p+keyMaxSize, o.MaxSize, "log max size")
	fs.IntVar(&o.MaxBackups, p+keyMaxBackups, o.MaxBackups, "log max backups")
	fs.IntVar(&o.MaxAge, p+keyMaxAge, o.MaxAge, "log max age")
	fs.BoolVar(&o.Compress, p+keyCompress, o.Compress, "log compress")
}

func parseZapLevel(s string) (zapcore.Level, error) {
	var lvl zapcore.Level
	if err := lvl.Set(strings.TrimSpace(s)); err != nil {
		return 0, err
	}
	return lvl, nil
}

// LoadEnv loads logger options from environment variables.
//
// Env naming: <envPrefix>_LOG_*
// - LOG_LEVEL / LOG_ADD_STACK_LEVEL / LOG_FILENAME / LOG_MAX_SIZE / LOG_MAX_BACKUPS / LOG_MAX_AGE / LOG_COMPRESS
//
// Priority is preserved by skipping fields whose corresponding flag is Changed.
// - legacy flags: log-level / log-add-stack-level / log-filename / log-max-size / log-max-backups / log-max-age / log-compress
// - prefixed flags: <flagPrefix>.level / .add_stack_level / .filename / .max_size / .max_backups / .max_age / .compress
func (o *Options) LoadEnv(envPrefix string, fs *pflag.FlagSet, flagPrefix string) []error {
	var errs []error
	envPrefix = strings.TrimSpace(envPrefix)
	flagPrefix = strings.TrimSpace(flagPrefix)

	levelFlag := "log-level"
	addStackFlag := "log-add-stack-level"
	filenameFlag := "log-filename"
	maxSizeFlag := "log-max-size"
	maxBackupsFlag := "log-max-backups"
	maxAgeFlag := "log-max-age"
	compressFlag := "log-compress"
	if flagPrefix != "" {
		levelFlag = flagPrefix + "." + keyLevel
		addStackFlag = flagPrefix + "." + keyAddStackLevel
		filenameFlag = flagPrefix + "." + keyFilename
		maxSizeFlag = flagPrefix + "." + keyMaxSize
		maxBackupsFlag = flagPrefix + "." + keyMaxBackups
		maxAgeFlag = flagPrefix + "." + keyMaxAge
		compressFlag = flagPrefix + "." + keyCompress
	}

	if v, ok := os.LookupEnv(envPrefix + "_LOG_LEVEL"); ok && !flagChanged(fs, levelFlag) {
		o.Level = v
	}
	if v, ok := os.LookupEnv(envPrefix + "_LOG_ADD_STACK_LEVEL"); ok && !flagChanged(fs, addStackFlag) {
		o.AddStackLevel = v
	}
	if v, ok := os.LookupEnv(envPrefix + "_LOG_FILENAME"); ok && !flagChanged(fs, filenameFlag) {
		o.Filename = v
	}
	if v, ok := os.LookupEnv(envPrefix + "_LOG_MAX_SIZE"); ok && !flagChanged(fs, maxSizeFlag) {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			o.MaxSize = n
		} else {
			errs = append(errs, fmt.Errorf("invalid %s_LOG_MAX_SIZE: %w", envPrefix, err))
		}
	}
	if v, ok := os.LookupEnv(envPrefix + "_LOG_MAX_BACKUPS"); ok && !flagChanged(fs, maxBackupsFlag) {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			o.MaxBackups = n
		} else {
			errs = append(errs, fmt.Errorf("invalid %s_LOG_MAX_BACKUPS: %w", envPrefix, err))
		}
	}
	if v, ok := os.LookupEnv(envPrefix + "_LOG_MAX_AGE"); ok && !flagChanged(fs, maxAgeFlag) {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			o.MaxAge = n
		} else {
			errs = append(errs, fmt.Errorf("invalid %s_LOG_MAX_AGE: %w", envPrefix, err))
		}
	}
	if v, ok := os.LookupEnv(envPrefix + "_LOG_COMPRESS"); ok && !flagChanged(fs, compressFlag) {
		if b, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil {
			o.Compress = b
		} else {
			errs = append(errs, fmt.Errorf("invalid %s_LOG_COMPRESS: %w", envPrefix, err))
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
