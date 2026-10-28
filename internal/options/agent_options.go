package options

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/pflag"
)

type AgentOptions struct {
	PromptFile     string   `mapstructure:"prompt_file" yaml:"prompt_file"`
	AllowedTools   []string `mapstructure:"allowed_tools" yaml:"allowed_tools"`
	MaxSteps       int      `mapstructure:"max_steps" yaml:"max_steps"`
	MaxModelCalls  int      `mapstructure:"max_model_calls" yaml:"max_model_calls"`
	MaxToolCalls   int      `mapstructure:"max_tool_calls" yaml:"max_tool_calls"`
	TimeoutSeconds int      `mapstructure:"timeout_seconds" yaml:"timeout_seconds"`
}

func (o *AgentOptions) LoadEnv(envPrefix string, fs *pflag.FlagSet) []error {
	var errs []error
	for _, field := range []struct {
		name string
		dst  *int
	}{
		{"MAX_STEPS", &o.MaxSteps},
		{"MAX_MODEL_CALLS", &o.MaxModelCalls},
		{"MAX_TOOL_CALLS", &o.MaxToolCalls},
		{"TIMEOUT_SECONDS", &o.TimeoutSeconds},
	} {
		flagName := "agent." + strings.ToLower(field.name)
		envName := envPrefix + "_AGENT_" + field.name
		if value, ok := os.LookupEnv(envName); ok && !flagChanged(fs, flagName) {
			parsed, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				errs = append(errs, fmt.Errorf("invalid %s: %w", envName, err))
			} else {
				*field.dst = parsed
			}
		}
	}
	if value, ok := os.LookupEnv(envPrefix + "_AGENT_PROMPT_FILE"); ok && !flagChanged(fs, "agent.prompt_file") {
		o.PromptFile = value
	}
	if value, ok := os.LookupEnv(envPrefix + "_AGENT_ALLOWED_TOOLS"); ok && !flagChanged(fs, "agent.allowed_tools") {
		o.AllowedTools = strings.Split(value, ",")
		for index := range o.AllowedTools {
			o.AllowedTools[index] = strings.TrimSpace(o.AllowedTools[index])
		}
	}
	return errs
}

func flagChanged(fs *pflag.FlagSet, name string) bool {
	if fs == nil {
		return false
	}
	flag := fs.Lookup(name)
	return flag != nil && flag.Changed
}
