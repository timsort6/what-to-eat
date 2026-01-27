package llm

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/pflag"
)

const (
	keyAPIKey         = "api_key"
	keyBaseURL        = "base_url"
	keyModel          = "model"
	keyLanguage       = "language"
	keyTimeoutSeconds = "timeout_seconds"
	keyMaxRetries     = "max_retries"
)

type DefaultOption func(o *Options)

func SetDefaultAPIKey(s string) DefaultOption {
	return func(o *Options) { o.APIKey = s }
}

func SetDefaultBaseURL(s string) DefaultOption {
	return func(o *Options) { o.BaseURL = s }
}

func SetDefaultModel(s string) DefaultOption {
	return func(o *Options) { o.Model = s }
}

func SetDefaultLanguage(s string) DefaultOption {
	return func(o *Options) { o.Language = s }
}

func SetDefaultTimeoutSeconds(n int) DefaultOption {
	return func(o *Options) { o.TimeoutSeconds = n }
}

func SetDefaultMaxRetries(n int) DefaultOption {
	return func(o *Options) { o.MaxRetries = n }
}

type Options struct {
	// APIKey 是 OpenAI 兼容网关的鉴权 Token（可能为空，取决于你的网关策略）。
	// 环境变量：AI_API_KEY
	APIKey string `mapstructure:"api_key" yaml:"api_key"`
	// BaseURL 是 OpenAI 兼容协议的 API 地址（例如 https://api.openai.com/v1）。
	// 环境变量：AI_BASE_URL
	BaseURL string `mapstructure:"base_url" yaml:"base_url"`
	// Model 是模型名称（例如 gpt-4o-mini / deepseek-chat / qwen-max）。
	// 环境变量：AI_MODEL
	Model string `mapstructure:"model" yaml:"model"`
	// Language 是一个“偏好语言”提示（例如 zh/en），方便上层在 prompt 模板中使用。
	// 环境变量：AI_LANGUAGE
	Language string `mapstructure:"language" yaml:"language"`

	// TimeoutSeconds 是请求超时秒数（实际是否生效取决于底层组件/HTTP client 的实现）。
	// 环境变量：AI_TIMEOUT_SECONDS
	TimeoutSeconds int `mapstructure:"timeout_seconds" yaml:"timeout_seconds"`

	// MaxRetries 是重试次数（实际是否生效取决于底层组件的实现）。
	// 环境变量：AI_MAX_RETRIES
	MaxRetries int `mapstructure:"max_retries" yaml:"max_retries"`
}

func NewLLMOptions(opts ...DefaultOption) *Options {
	o := &Options{
		APIKey:         "",
		BaseURL:        "https://api.openai.com/v1",
		Model:          "",
		Language:       "zh",
		TimeoutSeconds: 60,
		MaxRetries:     2,
	}

	for _, opt := range opts {
		opt(o)
	}

	return o
}

func (o *Options) Validate() []error {
	errs := make([]error, 0)

	if o.BaseURL == "" {
		errs = append(errs, fmt.Errorf("ai base url is empty"))
	}
	if o.TimeoutSeconds <= 0 {
		errs = append(errs, fmt.Errorf("ai timeout seconds is invalid"))
	}
	if o.MaxRetries < 0 {
		errs = append(errs, fmt.Errorf("ai max retries is invalid"))
	}
	if o.Language == "" {
		errs = append(errs, fmt.Errorf("ai language is empty"))
	}

	// APIKey/Model 在 Options 层保持可选：
	// - 某些兼容网关可能不需要 APIKey
	// - 某些上层调用方会在运行时动态指定模型
	return errs
}

func (o *Options) AddFlags(fs *pflag.FlagSet) {
	// Legacy flag names (kept for compatibility).
	fs.StringVar(&o.APIKey, "ai-api-key", o.APIKey, "OpenAI-compatible API key")
	fs.StringVar(&o.BaseURL, "ai-base-url", o.BaseURL, "OpenAI-compatible base URL")
	fs.StringVar(&o.Model, "ai-model", o.Model, "Remote model name")
	fs.StringVar(&o.Language, "ai-language", o.Language, "Preferred language hint (zh/en)")
	fs.IntVar(&o.TimeoutSeconds, "ai-timeout-seconds", o.TimeoutSeconds, "HTTP timeout in seconds")
	fs.IntVar(&o.MaxRetries, "ai-max-retries", o.MaxRetries, "Max retries for transient failures")
}

func (o *Options) AddFlagsWithPrefix(fs *pflag.FlagSet, prefix string) {
	p := prefix
	if p != "" {
		p += "."
	}
	fs.StringVar(&o.APIKey, p+keyAPIKey, o.APIKey, "OpenAI-compatible API key")
	fs.StringVar(&o.BaseURL, p+keyBaseURL, o.BaseURL, "OpenAI-compatible base URL")
	fs.StringVar(&o.Model, p+keyModel, o.Model, "Remote model name")
	fs.StringVar(&o.Language, p+keyLanguage, o.Language, "Preferred language hint (zh/en)")
	fs.IntVar(&o.TimeoutSeconds, p+keyTimeoutSeconds, o.TimeoutSeconds, "HTTP timeout in seconds")
	fs.IntVar(&o.MaxRetries, p+keyMaxRetries, o.MaxRetries, "Max retries for transient failures")
}

// LoadEnv loads configuration items from environment variables.
//
// Env naming: <envPrefix>_AI_*
// - AI_API_KEY / AI_BASE_URL / AI_MODEL / AI_LANGUAGE
// - AI_TIMEOUT_SECONDS / AI_MAX_RETRIES
//
// Priority is preserved by skipping fields whose corresponding flag is Changed.
// - legacy flags: ai-api-key / ai-base-url / ai-model / ai-language / ai-timeout-seconds / ai-max-retries
// - prefixed flags: <flagPrefix>.api_key / .base_url / .model / .language / .timeout_seconds / .max_retries
func (o *Options) LoadEnv(envPrefix string, fs *pflag.FlagSet, flagPrefix string) []error {
	var errs []error
	envPrefix = strings.TrimSpace(envPrefix)
	flagPrefix = strings.TrimSpace(flagPrefix)

	apiKeyFlag := "ai-api-key"
	baseURLFlag := "ai-base-url"
	modelFlag := "ai-model"
	langFlag := "ai-language"
	timeoutFlag := "ai-timeout-seconds"
	retriesFlag := "ai-max-retries"
	if flagPrefix != "" {
		apiKeyFlag = flagPrefix + "." + keyAPIKey
		baseURLFlag = flagPrefix + "." + keyBaseURL
		modelFlag = flagPrefix + "." + keyModel
		langFlag = flagPrefix + "." + keyLanguage
		timeoutFlag = flagPrefix + "." + keyTimeoutSeconds
		retriesFlag = flagPrefix + "." + keyMaxRetries
	}

	if v, ok := os.LookupEnv(envPrefix + "_AI_API_KEY"); ok && !flagChanged(fs, apiKeyFlag) {
		o.APIKey = v
	}
	if v, ok := os.LookupEnv(envPrefix + "_AI_BASE_URL"); ok && !flagChanged(fs, baseURLFlag) {
		o.BaseURL = v
	}
	if v, ok := os.LookupEnv(envPrefix + "_AI_MODEL"); ok && !flagChanged(fs, modelFlag) {
		o.Model = v
	}
	if v, ok := os.LookupEnv(envPrefix + "_AI_LANGUAGE"); ok && !flagChanged(fs, langFlag) {
		o.Language = v
	}
	if v, ok := os.LookupEnv(envPrefix + "_AI_TIMEOUT_SECONDS"); ok && !flagChanged(fs, timeoutFlag) {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			errs = append(errs, fmt.Errorf("invalid %s_AI_TIMEOUT_SECONDS: %w", envPrefix, err))
		} else {
			o.TimeoutSeconds = n
		}
	}
	if v, ok := os.LookupEnv(envPrefix + "_AI_MAX_RETRIES"); ok && !flagChanged(fs, retriesFlag) {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			errs = append(errs, fmt.Errorf("invalid %s_AI_MAX_RETRIES: %w", envPrefix, err))
		} else {
			o.MaxRetries = n
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
