package auth

import (
	"os"
	"strconv"
	"strings"

	"food-agent/pkg/defaults"

	"github.com/spf13/pflag"
)

type DefaultOption func(o *Options)

func SetDefaultJWTSecret(s string) DefaultOption {
	return func(o *Options) { o.JWTSecret = s }
}

func SetDefaultJWTExpiresInSeconds(n int) DefaultOption {
	return func(o *Options) { o.JWTExpiresInSeconds = n }
}

func SetDefaultIssuer(s string) DefaultOption {
	return func(o *Options) { o.Issuer = s }
}

// Options holds authentication-related settings.
type Options struct {
	JWTSecret           string `mapstructure:"jwt_secret" yaml:"jwt_secret"`
	JWTExpiresInSeconds int    `mapstructure:"jwt_expires_in_seconds" yaml:"jwt_expires_in_seconds"`
	Issuer              string `mapstructure:"issuer" yaml:"issuer"`
}

func NewOptions(opts ...DefaultOption) *Options {
	o := &Options{
		JWTSecret:           defaults.DefaultAuthJWTSecret,
		JWTExpiresInSeconds: defaults.DefaultAuthJWTExpiresInSeconds,
		Issuer:              defaults.DefaultAuthIssuer,
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

func (o *Options) AddFlagsWithPrefix(fs *pflag.FlagSet, prefix string) {
	p := strings.TrimSpace(prefix)
	if p != "" {
		p += "."
	}
	fs.StringVar(&o.JWTSecret, p+"jwt_secret", o.JWTSecret, "JWT signing secret (HMAC)")
	fs.IntVar(&o.JWTExpiresInSeconds, p+"jwt_expires_in_seconds", o.JWTExpiresInSeconds, "JWT expires in seconds")
	fs.StringVar(&o.Issuer, p+"issuer", o.Issuer, "JWT issuer")
}

// LoadEnv loads auth options from environment variables.
//
// Env naming: <envPrefix>_AUTH_*
// - AUTH_JWT_SECRET / AUTH_JWT_EXPIRES_IN_SECONDS / AUTH_ISSUER
//
// Priority is preserved by skipping fields whose corresponding flag is Changed.
// - flags: <flagPrefix>.jwt_secret / .jwt_expires_in_seconds / .issuer
func (o *Options) LoadEnv(envPrefix string, fs *pflag.FlagSet, flagPrefix string) []error {
	var errs []error
	envPrefix = strings.TrimSpace(envPrefix)
	flagPrefix = strings.TrimSpace(flagPrefix)

	secretFlag := "jwt_secret"
	expiresFlag := "jwt_expires_in_seconds"
	issuerFlag := "issuer"
	if flagPrefix != "" {
		secretFlag = flagPrefix + ".jwt_secret"
		expiresFlag = flagPrefix + ".jwt_expires_in_seconds"
		issuerFlag = flagPrefix + ".issuer"
	}

	if v, ok := os.LookupEnv(envPrefix + "_AUTH_JWT_SECRET"); ok && !flagChanged(fs, secretFlag) {
		o.JWTSecret = v
	}
	if v, ok := os.LookupEnv(envPrefix + "_AUTH_JWT_EXPIRES_IN_SECONDS"); ok && !flagChanged(fs, expiresFlag) {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			errs = append(errs, err)
		} else {
			o.JWTExpiresInSeconds = n
		}
	}
	if v, ok := os.LookupEnv(envPrefix + "_AUTH_ISSUER"); ok && !flagChanged(fs, issuerFlag) {
		o.Issuer = v
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
