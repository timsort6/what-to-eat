package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/model"

	openai "github.com/cloudwego/eino-ext/components/model/openai"
)

// Client 是一个轻量的 LLM 连接管理器（更偏“配置/构建”而不是“编排/调用”）。
//
// 设计目标：
// - 通过 CLI flag / 环境变量统一管理远程大模型连接参数；
// - 返回 Eino 的模型实例，供 internal/agent 或 service 层注入使用；
// - 保持 pkg/client/llm 尽量薄：只关心“怎么连”，不关心“怎么用”。
type Client struct {
	// ChatModel 是通过 eino-ext OpenAI 组件创建的聊天模型。
	//
	// 这里用 any 的原因：
	// - Eino 的接口/类型在不同版本中可能有调整；
	// - 上层如果需要强类型（例如 model.ChatModel），可在上层做断言；
	// - pkg/client/llm 只承担“构建”和“保存配置”的职责。
	ChatModel model.BaseChatModel
	// DecisionChatModel 是决策阶段模型；未配置 decision_model 时与 ChatModel 为同一实例。
	DecisionChatModel model.BaseChatModel

	// 以下字段用于透传/诊断（例如日志打印、debug 输出）。
	Model         string
	DecisionModel string
	Language      string
	BaseURL       string
	APIKey        string
}

// NewClient 基于默认 context 构建 LLM Client。
// 如果你希望在初始化时可控超时/取消，请使用 NewClientWithContext。
func NewClient(opts *Options) (*Client, error) {
	return NewClientWithContext(context.Background(), opts)
}

// NewClientWithContext 根据 Options 创建远程大模型连接（Eino ChatModel）。
func NewClientWithContext(ctx context.Context, opts *Options) (*Client, error) {
	if opts == nil {
		return nil, fmt.Errorf("llm options is nil")
	}
	if errs := opts.Validate(); len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	chatModel, err := NewOpenAIChatModel(ctx, opts)
	if err != nil {
		return nil, err
	}
	decisionModel, decisionChatModel := opts.Model, chatModel
	if name := strings.TrimSpace(opts.DecisionModel); name != "" {
		decisionModel = name
		if decisionChatModel, err = openai.NewChatModel(ctx, chatModelConfig(opts, decisionModel)); err != nil {
			return nil, err
		}
	}

	return &Client{
		ChatModel:         chatModel,
		DecisionChatModel: decisionChatModel,
		Model:             opts.Model,
		DecisionModel:     decisionModel,
		Language:          opts.Language,
		BaseURL:           opts.BaseURL,
		APIKey:            opts.APIKey,
	}, nil
}

// NewOpenAIChatModel 使用 `github.com/cloudwego/eino-ext/components/model/openai` 构建 ChatModel。
func NewOpenAIChatModel(ctx context.Context, opts *Options) (model.BaseChatModel, error) {
	if opts == nil {
		return nil, fmt.Errorf("llm options is nil")
	}
	return openai.NewChatModel(ctx, chatModelConfig(opts, opts.Model))
}

// chatModelConfig 透传超时（单次 HTTP 请求含流式读取）。eino-ext openai v0.1.7 无重试配置项，max_retries 不接入，保持不自动重试。
func chatModelConfig(opts *Options, modelName string) *openai.ChatModelConfig {
	return &openai.ChatModelConfig{
		APIKey:  opts.APIKey,
		BaseURL: opts.BaseURL,
		Model:   modelName,
		Timeout: time.Duration(opts.TimeoutSeconds) * time.Second,
	}
}
