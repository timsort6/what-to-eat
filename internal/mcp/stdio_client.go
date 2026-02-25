package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// StdioClient 通过启动 MCP server 子进程并使用 stdin/stdout 通信来调用工具。
//
// 说明：
// - 该实现只覆盖当前阶段需要的最小能力（initialize / tools/list / tools/call）。
// - 未来可以扩展为连接多个 server、支持 resources 等。
type StdioClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader

	mu     sync.Mutex
	callMu sync.Mutex
	nextID int64
}

type StdioClientOptions struct {
	Command string
	Args    []string
	// WorkDir sets the working directory for the spawned MCP server process.
	// If empty, it inherits the current process working directory.
	WorkDir string
	Env     map[string]string
}

func NewStdioClient(ctx context.Context, opts StdioClientOptions) (*StdioClient, error) {
	if opts.Command == "" {
		return nil, fmt.Errorf("mcp stdio client: empty command")
	}

	cmd := exec.CommandContext(ctx, opts.Command, opts.Args...)
	if opts.WorkDir != "" {
		cmd.Dir = opts.WorkDir
	}
	cmd.Stderr = os.Stderr

	// 继承环境变量，并追加自定义 env。
	cmd.Env = os.Environ()
	for k, v := range opts.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp stdio client: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("mcp stdio client: stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("mcp stdio client: start: %w", err)
	}

	c := &StdioClient{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
		nextID: 1,
	}

	// 最小握手：initialize
	initCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := c.initialize(initCtx); err != nil {
		_ = c.Close()
		return nil, err
	}

	return c, nil
}

func (c *StdioClient) Close() error {
	// 尝试优雅结束子进程；如已退出则忽略错误。
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.cmd == nil || c.cmd.Process == nil {
		return nil
	}

	done := make(chan error, 1)
	go func() {
		done <- c.cmd.Wait()
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		_ = c.cmd.Process.Kill()
		<-done
		return nil
	}
}

func (c *StdioClient) ListTools(ctx context.Context) (ToolsListResult, error) {
	var out ToolsListResult
	if err := c.call(ctx, "tools/list", nil, &out); err != nil {
		return ToolsListResult{}, err
	}
	return out, nil
}

func (c *StdioClient) CallTool(ctx context.Context, name string, arguments map[string]any) (toolsCallResult, error) {
	params := toolsCallParams{
		Name:      name,
		Arguments: arguments,
	}
	var out toolsCallResult
	if err := c.call(ctx, "tools/call", params, &out); err != nil {
		return toolsCallResult{}, err
	}
	return out, nil
}

// CallToolText 是一个便捷方法：调用工具并返回第一段 text 内容。
func (c *StdioClient) CallToolText(ctx context.Context, name string, arguments map[string]any) (string, error) {
	res, err := c.CallTool(ctx, name, arguments)
	if err != nil {
		return "", err
	}
	for _, c := range res.Content {
		if c.Type == "text" && c.Text != "" {
			return c.Text, nil
		}
	}
	return "", nil
}

func (c *StdioClient) initialize(ctx context.Context) error {
	// MCP initialize 的 params 在不同实现中可能略有差异，这里使用最小字段，保持兼容。
	params := map[string]any{
		"clientInfo": map[string]any{
			"name":    "food-agent",
			"version": "0.0.1",
		},
	}
	var ignore any
	return c.call(ctx, "initialize", params, &ignore)
}

func (c *StdioClient) call(ctx context.Context, method string, params any, result any) error {
	// 串行化调用，避免并发下读写乱序。
	c.callMu.Lock()
	defer c.callMu.Unlock()

	c.mu.Lock()
	id := c.nextID
	c.nextID++
	c.mu.Unlock()

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	if err := writeMessage(c.stdin, req); err != nil {
		return fmt.Errorf("mcp write request: %w", err)
	}

	// 读取响应（简单实现：串行调用，按顺序读取一条响应即可）。
	raw, err := readMessage(c.stdout)
	if err != nil {
		return fmt.Errorf("mcp read response: %w", err)
	}

	var resp jsonRPCResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("mcp unmarshal response: %w", err)
	}
	if resp.Error != nil {
		return fmt.Errorf("mcp error code=%d message=%s", resp.Error.Code, resp.Error.Message)
	}

	// 将 resp.Result 再次 marshal/unmarshal 到具体结构体，避免使用 interface{} 直接断言。
	if result == nil {
		return nil
	}
	b, err := json.Marshal(resp.Result)
	if err != nil {
		return fmt.Errorf("mcp marshal result: %w", err)
	}
	if err := json.Unmarshal(b, result); err != nil {
		return fmt.Errorf("mcp unmarshal result: %w", err)
	}
	return nil
}
