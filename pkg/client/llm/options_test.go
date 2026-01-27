package llm

import (
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
)

func TestNewLLMOptions(t *testing.T) {
	opts := NewLLMOptions()
	assert.Equal(t, "", opts.APIKey)
	assert.Equal(t, "https://api.openai.com/v1", opts.BaseURL)
	assert.Equal(t, "", opts.Model)
	assert.Equal(t, "zh", opts.Language)
	assert.Equal(t, 60, opts.TimeoutSeconds)
	assert.Equal(t, 2, opts.MaxRetries)

	// 环境变量覆盖
	t.Setenv("FOOD_AGENT_AI_API_KEY", "fake")
	t.Setenv("FOOD_AGENT_AI_BASE_URL", "http://fake.local/v1")
	t.Setenv("FOOD_AGENT_AI_MODEL", "fake-model")
	t.Setenv("FOOD_AGENT_AI_LANGUAGE", "en")
	t.Setenv("FOOD_AGENT_AI_TIMEOUT_SECONDS", "12")
	t.Setenv("FOOD_AGENT_AI_MAX_RETRIES", "3")

	fs := pflag.NewFlagSet("fake", pflag.ExitOnError)
	opts.AddFlags(fs)
	loadErrs := opts.LoadEnv("FOOD_AGENT", fs, "")
	assert.Equal(t, 0, len(loadErrs))
	errs := opts.Validate()
	assert.Equal(t, 0, len(errs))

	assert.Equal(t, "fake", opts.APIKey)
	assert.Equal(t, "http://fake.local/v1", opts.BaseURL)
	assert.Equal(t, "fake-model", opts.Model)
	assert.Equal(t, "en", opts.Language)
	assert.Equal(t, 12, opts.TimeoutSeconds)
	assert.Equal(t, 3, opts.MaxRetries)

	// 命令行参数覆盖（优先级高于 env）
	err := fs.Parse([]string{
		"--ai-api-key=fake1",
		"--ai-base-url=http://fake1.local/v1",
		"--ai-model=fake-model-1",
		"--ai-language=zh",
		"--ai-timeout-seconds=30",
		"--ai-max-retries=5",
	})
	assert.NoError(t, err)
	_ = opts.LoadEnv("FOOD_AGENT", fs, "")

	assert.Equal(t, "fake1", opts.APIKey)
	assert.Equal(t, "http://fake1.local/v1", opts.BaseURL)
	assert.Equal(t, "fake-model-1", opts.Model)
	assert.Equal(t, "zh", opts.Language)
	assert.Equal(t, 30, opts.TimeoutSeconds)
	assert.Equal(t, 5, opts.MaxRetries)
}
