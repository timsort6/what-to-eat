package mcp

// 这里实现的是 MCP 常用的 JSON-RPC 2.0 形态（与 LSP 类似的 Content-Length framing）。

type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      int64         `json:"id,omitempty"`
	Result  any           `json:"result,omitempty"`
	Error   *jsonRPCError `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// tools/list
type ToolsListResult struct {
	Tools []ToolDef `json:"tools"`
}

type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
}

// tools/call
type toolsCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type toolsCallResult struct {
	Content []toolContent `json:"content"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}
