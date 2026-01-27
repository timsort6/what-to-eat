package amap

import (
	"food-agent/pkg/defaults"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/pflag"
)

type DefaultOption func(o *Options)

func SetDefaultBaseURL(s string) DefaultOption {
	return func(o *Options) { o.BaseURL = s }
}

func SetDefaultTimeoutSeconds(n int) DefaultOption {
	return func(o *Options) { o.TimeoutSeconds = n }
}

type Options struct {
	Key            string `mapstructure:"key" yaml:"key"`
	BaseURL        string `mapstructure:"base_url" yaml:"base_url"`
	TimeoutSeconds int    `mapstructure:"timeout_seconds" yaml:"timeout_seconds"`
}

func NewOptions(opts ...DefaultOption) *Options {
	o := &Options{
		Key:            "",
		BaseURL:        defaults.DefaultAmapBaseURL,
		TimeoutSeconds: defaults.DefaultAmapTimeoutSeconds,
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

func (o *Options) AddFlagsWithPrefix(fs *pflag.FlagSet, prefix string) {
	p := prefix
	if p != "" {
		p += "."
	}
	fs.StringVar(&o.Key, p+"key", o.Key, "AMap API key")
	fs.StringVar(&o.BaseURL, p+"base_url", o.BaseURL, "AMap base URL")
	fs.IntVar(&o.TimeoutSeconds, p+"timeout_seconds", o.TimeoutSeconds, "AMap timeout seconds")
}

// LoadEnv loads AMap options from environment variables.
//
// Env naming: <envPrefix>_AMAP_KEY / _AMAP_BASE_URL / _AMAP_TIMEOUT_SECONDS
//
// Priority is preserved by skipping fields whose corresponding flag is Changed:
// - flags: <flagPrefix>.key / .base_url / .timeout_seconds
func (o *Options) LoadEnv(envPrefix string, fs *pflag.FlagSet, flagPrefix string) []error {
	var errs []error
	envPrefix = strings.TrimSpace(envPrefix)
	flagPrefix = strings.TrimSpace(flagPrefix)

	keyFlag := "key"
	baseURLFlag := "base_url"
	timeoutFlag := "timeout_seconds"
	if flagPrefix != "" {
		keyFlag = flagPrefix + ".key"
		baseURLFlag = flagPrefix + ".base_url"
		timeoutFlag = flagPrefix + ".timeout_seconds"
	}

	if v, ok := os.LookupEnv(envPrefix + "_AMAP_KEY"); ok && !flagChanged(fs, keyFlag) {
		o.Key = v
	}
	if v, ok := os.LookupEnv(envPrefix + "_AMAP_BASE_URL"); ok && !flagChanged(fs, baseURLFlag) {
		o.BaseURL = v
	}
	if v, ok := os.LookupEnv(envPrefix + "_AMAP_TIMEOUT_SECONDS"); ok && !flagChanged(fs, timeoutFlag) {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			errs = append(errs, err)
		} else {
			o.TimeoutSeconds = n
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
