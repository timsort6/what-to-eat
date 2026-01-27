package agent

import (
	"food-agent/internal/mcp"

	openai "github.com/sashabaranov/go-openai"
)

// Agent 是 AI Agent 的最小骨架（当前阶段聚焦 ReAct + MCP 工具调用）。
//
// 设计说明：
// - 通过依赖注入传入 LLM client（OpenAI-compatible）与工具（通过 MCP server 调用）。
// - 本阶段先把初始化骨架落稳：可注入、可扩展、可替换。
// - 后续会在这里接入 Eino 的 Graph/Tool 机制，实现真正的 ReAct 编排。
type Agent struct {
	llmClient *openai.Client
	model     string

	// amapMCP 用于调用 Amap MCP Server 提供的工具（例如 amap.search_around）。
	amapMCP *mcp.StdioClient
}

func NewAgent(llmClient *openai.Client, model string, amapMCP *mcp.StdioClient) *Agent {
	return &Agent{
		llmClient: llmClient,
		model:     model,
		amapMCP:   amapMCP,
	}
}

// BindTools 预留：后续在这里把 MCP 工具绑定到 Eino Graph。
func (a *Agent) BindTools() {
	// TODO: implement with eino tool registry (MCP -> Tool adapter).
}

