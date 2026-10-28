package config

import "testing"

func TestUnmarshalYAML_readsAgentSettings(t *testing.T) {
	var cfg Config
	err := UnmarshalYAML([]byte("agent:\n  prompt_file: prompts/custom.md\n  allowed_tools: [amap.poi2_text]\n  max_steps: 7\n  max_model_calls: 3\n  max_tool_calls: 2\n  timeout_seconds: 25\n"), &cfg)
	if err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	if cfg.Agent.PromptFile != "prompts/custom.md" || len(cfg.Agent.AllowedTools) != 1 || cfg.Agent.AllowedTools[0] != "amap.poi2_text" || cfg.Agent.MaxSteps != 7 || cfg.Agent.MaxModelCalls != 3 || cfg.Agent.MaxToolCalls != 2 || cfg.Agent.TimeoutSeconds != 25 {
		t.Fatalf("unexpected agent config: %+v", cfg.Agent)
	}
}
