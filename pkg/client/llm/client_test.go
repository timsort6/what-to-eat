package llm

import (
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChatModelConfig_PassesTimeout(t *testing.T) {
	opts := NewLLMOptions(SetDefaultBaseURL("http://fake.local/v1"), SetDefaultAPIKey("k"), SetDefaultModel("chat"), SetDefaultTimeoutSeconds(12))
	cfg := chatModelConfig(opts, "reasoner")
	assert.Equal(t, 12*time.Second, cfg.Timeout)
	assert.Equal(t, "reasoner", cfg.Model)
	assert.Equal(t, "http://fake.local/v1", cfg.BaseURL)
	assert.Equal(t, "k", cfg.APIKey)
	assert.Nil(t, cfg.HTTPClient, "HTTPClient 非空会让 Timeout 失效")
}

func TestNewClient_DecisionModelIsOptional(t *testing.T) {
	opts := NewLLMOptions(SetDefaultBaseURL("http://fake.local/v1"), SetDefaultModel("chat"))
	single, err := NewClient(opts)
	require.NoError(t, err)
	assert.Same(t, single.ChatModel, single.DecisionChatModel, "未配置 decision_model 时所有阶段共用 model")

	opts.DecisionModel = "reasoner"
	split, err := NewClient(opts)
	require.NoError(t, err)
	assert.NotSame(t, split.ChatModel, split.DecisionChatModel)
	assert.Equal(t, "reasoner", split.DecisionModel)

	t.Setenv("FOOD_AGENT_AI_DECISION_MODEL", "env-reasoner")
	fs := pflag.NewFlagSet("fake", pflag.ContinueOnError)
	opts.AddFlagsWithPrefix(fs, "llm")
	require.Empty(t, opts.LoadEnv("FOOD_AGENT", fs, "llm"))
	assert.Equal(t, "env-reasoner", opts.DecisionModel)
}

// N3：decision_model 去空白（纯空白视为未配置）；flag 前缀为空时用旧式 flag 名 ai-decision-model 判断优先级。
func TestDecisionModel_TrimAndLegacyFlag(t *testing.T) {
	opts := NewLLMOptions(SetDefaultBaseURL("http://fake.local/v1"), SetDefaultModel("chat"))
	opts.DecisionModel = "   "
	blank, err := NewClient(opts)
	require.NoError(t, err)
	assert.Same(t, blank.ChatModel, blank.DecisionChatModel)
	assert.Equal(t, "chat", blank.DecisionModel)
	opts.DecisionModel = " reasoner "
	padded, err := NewClient(opts)
	require.NoError(t, err)
	assert.Equal(t, "reasoner", padded.DecisionModel)

	t.Setenv("FOOD_AGENT_AI_DECISION_MODEL", " env-reasoner ")
	legacy := NewLLMOptions()
	fs := pflag.NewFlagSet("legacy", pflag.ContinueOnError)
	legacy.AddFlags(fs)
	require.Empty(t, legacy.LoadEnv("FOOD_AGENT", fs, ""))
	assert.Equal(t, "env-reasoner", legacy.DecisionModel, "环境变量值去空白")
	require.NoError(t, fs.Parse([]string{"--ai-decision-model=flag-reasoner"}))
	require.Empty(t, legacy.LoadEnv("FOOD_AGENT", fs, ""))
	assert.Equal(t, "flag-reasoner", legacy.DecisionModel, "旧式 flag 优先于环境变量")
}
