package options

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"food-agent/pkg/config"
	"food-agent/pkg/defaults"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Complete loads config file (if any), env (FOOD_AGENT_*), and flags into the same options struct.
// Priority (high -> low): flag(Changed) > env > config file > defaults.
func (o *ServerRunOptions) Complete(cmd *cobra.Command, defaultConfigFile string) error {
	if cmd == nil {
		return fmt.Errorf("nil command")
	}

	cfgPath := resolveConfigPath(cmd, o.ConfigFile, defaultConfigFile)
	v := newViper(cmd)

	// Parse config file schema once (file only) to satisfy the "config.Config reads file" requirement.
	// We do NOT validate here, because invalid config fields could be overridden by env/flags.
	if strings.TrimSpace(cfgPath) != "" {
		raw, err := os.ReadFile(cfgPath)
		if err != nil {
			return fmt.Errorf("read config file %q: %w", cfgPath, err)
		}
		var cfg config.Config
		if err := config.UnmarshalYAML(raw, &cfg); err != nil {
			return fmt.Errorf("parse config yaml %q: %w", cfgPath, err)
		}

		v.SetConfigType("yaml")
		if err := v.ReadConfig(bytes.NewReader(raw)); err != nil {
			return fmt.Errorf("read config into viper %q: %w", cfgPath, err)
		}
	}

	// Let viper compute final values by priority and fill the same options struct.
	if err := v.Unmarshal(o); err != nil {
		return fmt.Errorf("unmarshal options: %w", err)
	}

	// Apply environment variables after config is loaded, while keeping flag(Changed) highest priority.
	if errs := o.LoadEnv("FOOD_AGENT", cmd.Flags()); len(errs) > 0 {
		return fmt.Errorf("invalid env options: %w", joinErrors(errs))
	}

	// Keep ConfigFile resolved for later (logs/debug).
	o.ConfigFile = cfgPath
	o.Agent.PromptFile = resolvePromptPath(cfgPath, o.Agent.PromptFile)

	// Final validation should be done on the fully-resolved options.
	if errs := o.Validate(); len(errs) > 0 {
		return fmt.Errorf("invalid options: %w", joinErrors(errs))
	}
	return nil
}

func newViper(cmd *cobra.Command) *viper.Viper {
	v := viper.New()
	_ = v.BindPFlags(cmd.Flags())
	return v
}

func resolveConfigPath(cmd *cobra.Command, current string, defaultConfigFile string) string {
	if cmd.Flags().Changed("config") {
		return strings.TrimSpace(current)
	}
	if v, ok := os.LookupEnv("FOOD_AGENT_CONFIG"); ok {
		return strings.TrimSpace(v)
	}
	if defaultConfigFile == "" {
		return ""
	}
	return defaultConfigFile
}

func defaultConfigFileForCmd(use string) string {
	// For future use if we want to distinguish cmd/server and cmd/mcp-server defaults.
	_ = use
	return defaults.DefaultConfigFile
}
