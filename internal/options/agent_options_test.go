package options

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestAgentOptions_Defaults(t *testing.T) {
	options := NewServerRunOptions().Agent
	if options.PromptFile != "prompts/food-agent.md" || options.MaxSteps != 10 || options.MaxModelCalls != 5 || options.MaxToolCalls != 6 || options.TimeoutSeconds != 90 {
		t.Fatalf("unexpected agent defaults: %+v", options)
	}
	if len(options.AllowedTools) != 2 || options.AllowedTools[0] != "amap.poi2_around" || options.AllowedTools[1] != "amap.poi2_text" {
		t.Fatalf("unexpected allowed tools: %v", options.AllowedTools)
	}
}

func TestServerRunOptions_Complete_resolvesPromptAndPriorities(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "prompts"), 0o700); err != nil {
		t.Fatal(err)
	}
	promptPath := filepath.Join(dir, "prompts", "agent.md")
	if err := os.WriteFile(promptPath, []byte("prompt"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "config.yaml")
	config := "agent:\n  prompt_file: prompts/agent.md\n  max_steps: 9\n"
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FOOD_AGENT_AGENT_MAX_STEPS", "10")

	options := NewServerRunOptions()
	options.Server.Mode = "debug"
	options.MCP.Servers.Amap = MCPAmapServerOptions{Enabled: true, Transport: "stdio", Command: "go", ConfigFile: "mcp.yaml"}
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	options.AddFlags(flags)
	if err := flags.Parse([]string{"--config", configPath, "--agent.max_steps", "11"}); err != nil {
		t.Fatal(err)
	}
	cmd := &cobra.Command{}
	cmd.Flags().AddFlagSet(flags)

	if err := options.Complete(cmd, ""); err != nil {
		t.Fatalf("complete options: %v", err)
	}
	if options.Agent.MaxSteps != 11 {
		t.Fatalf("flag max_steps = %d, want 11", options.Agent.MaxSteps)
	}
	if options.Agent.PromptFile != promptPath {
		t.Fatalf("resolved prompt path = %q, want %q", options.Agent.PromptFile, promptPath)
	}

	envOptions := NewServerRunOptions()
	envOptions.Server.Mode = "debug"
	envOptions.MCP.Servers.Amap = MCPAmapServerOptions{Enabled: true, Transport: "stdio", Command: "go", ConfigFile: "mcp.yaml"}
	envFlags := pflag.NewFlagSet("env-test", pflag.ContinueOnError)
	envOptions.AddFlags(envFlags)
	envCmd := &cobra.Command{}
	envCmd.Flags().AddFlagSet(envFlags)
	if err := envOptions.Complete(envCmd, configPath); err != nil {
		t.Fatalf("complete options with env: %v", err)
	}
	if envOptions.Agent.MaxSteps != 10 {
		t.Fatalf("environment max_steps = %d, want 10", envOptions.Agent.MaxSteps)
	}
}

func TestAgentOptions_Validate_rejectsInvalidBudgetsAndPrompt(t *testing.T) {
	options := NewServerRunOptions()
	options.Agent.MaxSteps = 0
	options.Agent.AllowedTools = []string{"amap.geocode"}
	options.Agent.PromptFile = filepath.Join(t.TempDir(), "missing.md")
	var messages []string
	for _, err := range options.Validate() {
		messages = append(messages, err.Error())
	}
	joined := strings.Join(messages, "; ")
	for _, expected := range []string{"agent call budgets", "unknown tool", "read agent prompt"} {
		if !strings.Contains(joined, expected) {
			t.Errorf("validation errors %q do not include %q", joined, expected)
		}
	}

	emptyPrompt := filepath.Join(t.TempDir(), "empty.md")
	if err := os.WriteFile(emptyPrompt, []byte(" \n\t"), 0o600); err != nil {
		t.Fatal(err)
	}
	options.Agent.PromptFile = emptyPrompt
	for _, err := range options.Validate() {
		if strings.Contains(err.Error(), "agent.prompt_file") && strings.Contains(err.Error(), "is empty") {
			return
		}
	}
	t.Fatal("empty prompt was not rejected")
}

func TestAgentOptions_LoadEnv_preservesChangedFlags(t *testing.T) {
	options := NewServerRunOptions().Agent
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.IntVar(&options.MaxSteps, "agent.max_steps", options.MaxSteps, "")
	if err := flags.Parse([]string{"--agent.max_steps", "12"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FOOD_AGENT_AGENT_MAX_STEPS", "13")
	if errs := options.LoadEnv("FOOD_AGENT", flags); len(errs) != 0 {
		t.Fatalf("load env errors: %v", errs)
	}
	if options.MaxSteps != 12 {
		t.Fatalf("max_steps = %d, want changed flag 12", options.MaxSteps)
	}
}
