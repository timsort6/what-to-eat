package options

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/pflag"
)

const (
	bindAddress   = "bind_address"
	port          = "port"
	tlsCertFile   = "tls_cert_file"
	tlsPrivateKey = "tls_private_key"
	mode          = "mode"
)

type DefaultOption func(o *ServerRunOptions)

func WithDefaultBindAddress(s string) DefaultOption {
	return func(o *ServerRunOptions) { o.BindAddress = s }
}

func WithDefaultPort(n int) DefaultOption {
	return func(o *ServerRunOptions) { o.Port = n }
}

func WithDefaultMode(s string) DefaultOption {
	return func(o *ServerRunOptions) { o.Mode = s }
}

type ServerRunOptions struct {
	// server bind address
	BindAddress string `mapstructure:"bind_address" yaml:"bind_address"`
	// server port number
	Port int `mapstructure:"port" yaml:"port"`
	// tls cert file
	TlsCertFile string `mapstructure:"tls_cert_file" yaml:"tls_cert_file"`
	// tls private key file
	TlsPrivateKey string `mapstructure:"tls_private_key" yaml:"tls_private_key"`
	// gin mode: debug/release/test
	Mode string `mapstructure:"mode" yaml:"mode"`
}

func NewServerRunOptions(opts ...DefaultOption) *ServerRunOptions {
	// create default server run options
	s := &ServerRunOptions{
		BindAddress:   "0.0.0.0",
		Port:          9090,
		TlsCertFile:   "",
		TlsPrivateKey: "",
		Mode:          "release",
	}

	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *ServerRunOptions) Validate() []error {
	errs := make([]error, 0)

	if s.Port < 0 || s.Port > 65535 {
		errs = append(errs, fmt.Errorf("port invalid"))
	}

	if s.TlsPrivateKey != "" && s.TlsCertFile != "" {
		if _, err := os.Stat(s.TlsCertFile); err != nil {
			errs = append(errs, err)
		}

		if _, err := os.Stat(s.TlsPrivateKey); err != nil {
			errs = append(errs, err)
		}
	}

	return errs
}

func (s *ServerRunOptions) AddFlags(fs *pflag.FlagSet) {
	// Legacy flag names are intentionally not registered in the unified CLI.
	// Keep this method for backwards compatibility in other packages.
	fs.StringVar(&s.BindAddress, "bind-address", s.BindAddress, "server bind address")
	fs.IntVar(&s.Port, "port", s.Port, "server port number")
	fs.StringVar(&s.TlsCertFile, "tls-cert-file", s.TlsCertFile, "tls cert file")
	fs.StringVar(&s.TlsPrivateKey, "tls-private-key", s.TlsPrivateKey, "tls private key")
	fs.StringVar(&s.Mode, "mode", s.Mode, "gin mode: debug/release/test")
}

// AddFlagsWithPrefix registers dot-style flags (recommended for this project).
// Example prefix "server": server.bind_address, server.port, ...
func (s *ServerRunOptions) AddFlagsWithPrefix(fs *pflag.FlagSet, prefix string) {
	p := prefix
	if p != "" {
		p += "."
	}
	fs.StringVar(&s.BindAddress, p+bindAddress, s.BindAddress, "server bind address")
	fs.IntVar(&s.Port, p+port, s.Port, "server port number")
	fs.StringVar(&s.TlsCertFile, p+tlsCertFile, s.TlsCertFile, "tls cert file")
	fs.StringVar(&s.TlsPrivateKey, p+tlsPrivateKey, s.TlsPrivateKey, "tls private key")
	fs.StringVar(&s.Mode, p+mode, s.Mode, "gin mode: debug/release/test")
}

// LoadEnv loads server options from environment variables.
//
// Env naming: <envPrefix>_SERVER_*
// - SERVER_BIND_ADDRESS / SERVER_PORT / SERVER_TLS_CERT_FILE / SERVER_TLS_PRIVATE_KEY / SERVER_MODE
//
// Priority is preserved by skipping fields whose corresponding flag is Changed.
// - legacy flags: bind-address / port / tls-cert-file / tls-private-key / mode
// - prefixed flags: <flagPrefix>.bind_address / .port / .tls_cert_file / .tls_private_key / .mode
func (s *ServerRunOptions) LoadEnv(envPrefix string, fs *pflag.FlagSet, flagPrefix string) []error {
	var errs []error
	envPrefix = strings.TrimSpace(envPrefix)
	flagPrefix = strings.TrimSpace(flagPrefix)

	bindFlag := "bind-address"
	portFlag := "port"
	certFlag := "tls-cert-file"
	keyFlag := "tls-private-key"
	modeFlag := "mode"
	if flagPrefix != "" {
		bindFlag = flagPrefix + "." + bindAddress
		portFlag = flagPrefix + "." + port
		certFlag = flagPrefix + "." + tlsCertFile
		keyFlag = flagPrefix + "." + tlsPrivateKey
		modeFlag = flagPrefix + "." + mode
	}

	if v, ok := os.LookupEnv(envPrefix + "_SERVER_BIND_ADDRESS"); ok && !flagChanged(fs, bindFlag) {
		s.BindAddress = v
	}
	if v, ok := os.LookupEnv(envPrefix + "_SERVER_PORT"); ok && !flagChanged(fs, portFlag) {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			errs = append(errs, fmt.Errorf("invalid %s_SERVER_PORT: %w", envPrefix, err))
		} else {
			s.Port = n
		}
	}
	if v, ok := os.LookupEnv(envPrefix + "_SERVER_TLS_CERT_FILE"); ok && !flagChanged(fs, certFlag) {
		s.TlsCertFile = v
	}
	if v, ok := os.LookupEnv(envPrefix + "_SERVER_TLS_PRIVATE_KEY"); ok && !flagChanged(fs, keyFlag) {
		s.TlsPrivateKey = v
	}
	if v, ok := os.LookupEnv(envPrefix + "_SERVER_MODE"); ok && !flagChanged(fs, modeFlag) {
		s.Mode = v
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
