package options

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"food-agent/pkg/client/amap"
	"food-agent/pkg/config"
	"food-agent/pkg/defaults"
	"food-agent/pkg/logger"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// MCPServerRunOptions is the runtime options root for cmd/mcp-server.
//
// It intentionally uses an independent config file from the main web server:
// - flag: --config
// - env:  FOOD_AGENT_MCP_CONFIG
// - default: configs/mcp-amap.yaml
//
// Priority (high -> low): flag(Changed) > env > config file > defaults.
type MCPServerRunOptions struct {
	ConfigFile string

	Amap *amap.Options   `mapstructure:"amap" yaml:"amap"`
	Log  *logger.Options `mapstructure:"log" yaml:"log"`
}

func NewMCPServerRunOptions() *MCPServerRunOptions {
	return &MCPServerRunOptions{
		ConfigFile: "configs/mcp-amap.yaml",
		Amap: amap.NewOptions(
			amap.SetDefaultBaseURL(defaults.DefaultAmapBaseURL),
			amap.SetDefaultTimeoutSeconds(defaults.DefaultAmapTimeoutSeconds),
		),
		Log: logger.NewLoggerOptions(
			logger.SetDefaultLevel(defaults.DefaultLogLevel),
			logger.SetDefaultAddStackLevel(defaults.DefaultLogAddStackLevel),
			logger.SetDefaultRotation(defaults.DefaultLogMaxSize, defaults.DefaultLogMaxBackups, defaults.DefaultLogMaxAge),
		),
	}
}

func (o *MCPServerRunOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.ConfigFile, "config", o.ConfigFile, "mcp-server config file path. env FOOD_AGENT_MCP_CONFIG")
	if o.Amap != nil {
		o.Amap.AddFlagsWithPrefix(fs, "amap")
	}
	if o.Log != nil {
		o.Log.AddFlagsWithPrefix(fs, "log")
	}
}

func (o *MCPServerRunOptions) Complete(cmd *cobra.Command, defaultConfigFile string) error {
	if cmd == nil {
		return fmt.Errorf("nil command")
	}

	cfgPath := resolveMCPConfigPath(cmd, o.ConfigFile, defaultConfigFile)
	v := viper.New()
	_ = v.BindPFlags(cmd.Flags())

	if strings.TrimSpace(cfgPath) != "" {
		raw, err := os.ReadFile(cfgPath)
		if err != nil {
			return fmt.Errorf("read config file %q: %w", cfgPath, err)
		}
		var cfg config.MCPServerAppConfig
		if err := config.UnmarshalYAML(raw, &cfg); err != nil {
			return fmt.Errorf("parse config yaml %q: %w", cfgPath, err)
		}
		v.SetConfigType("yaml")
		if err := v.ReadConfig(bytes.NewReader(raw)); err != nil {
			return fmt.Errorf("read config into viper %q: %w", cfgPath, err)
		}
	}

	if err := v.Unmarshal(o); err != nil {
		return fmt.Errorf("unmarshal options: %w", err)
	}

	// Env overrides config (but should not override changed flags).
	if errs := o.LoadEnv("FOOD_AGENT", cmd.Flags()); len(errs) > 0 {
		return fmt.Errorf("invalid env options: %w", joinErrors(errs))
	}

	o.ConfigFile = cfgPath

	if errs := o.Validate(); len(errs) > 0 {
		return fmt.Errorf("invalid options: %w", joinErrors(errs))
	}
	return nil
}

func (o *MCPServerRunOptions) LoadEnv(envPrefix string, fs *pflag.FlagSet) []error {
	var errs []error
	if o.Amap != nil {
		errs = append(errs, o.Amap.LoadEnv(envPrefix, fs, "amap")...)
	}
	if o.Log != nil {
		errs = append(errs, o.Log.LoadEnv(envPrefix, fs, "log")...)
	}
	return errs
}

func (o *MCPServerRunOptions) Validate() []error {
	var errs []error
	if o.Amap == nil {
		errs = append(errs, fmt.Errorf("amap options is nil"))
	} else {
		if strings.TrimSpace(o.Amap.Key) == "" {
			errs = append(errs, fmt.Errorf("amap.key is required for mcp-server"))
		}
		if o.Amap.TimeoutSeconds < 0 {
			errs = append(errs, fmt.Errorf("amap.timeout_seconds is invalid"))
		}
	}
	if o.Log != nil {
		errs = append(errs, o.Log.Validate()...)
	}
	return errs
}

func resolveMCPConfigPath(cmd *cobra.Command, current string, defaultConfigFile string) string {
	if cmd.Flags().Changed("config") {
		return strings.TrimSpace(current)
	}
	if v, ok := os.LookupEnv("FOOD_AGENT_MCP_CONFIG"); ok {
		return strings.TrimSpace(v)
	}
	if defaultConfigFile == "" {
		return ""
	}
	return defaultConfigFile
}
