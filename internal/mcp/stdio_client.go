package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"
)

// StdioClient 通过启动 MCP server 子进程并使用 stdin/stdout 通信来调用工具。
//
// 说明：
// - 该实现只覆盖当前阶段需要的最小能力（initialize / tools/list / tools/call）。
// - 未来可以扩展为连接多个 server、支持 resources 等。
type StdioClient struct {
	opts       StdioClientOptions
	process    *stdioProcess
	callMu     chan struct{}
	stateMu    sync.Mutex
	closed     bool
	nextID     int64
	closeOnce  sync.Once
	closeStart chan struct{}
	closeDone  chan struct{}
	closeErr   error
}

type StdioClientOptions struct {
	Command string
	Args    []string
	// WorkDir sets the working directory for the spawned MCP server process.
	// If empty, it inherits the current process working directory.
	WorkDir string
	Env     map[string]string
}

type stdioExchange struct {
	method string
	params any
	result any
}

type stdioRPCError struct {
	code    int
	message string
}

func (e *stdioRPCError) Error() string {
	return fmt.Sprintf("mcp error code=%d message=%s", e.code, e.message)
}

func NewStdioClient(ctx context.Context, opts StdioClientOptions) (*StdioClient, error) {
	if opts.Command == "" {
		return nil, fmt.Errorf("mcp stdio client: empty command")
	}
	opts.Args = slices.Clone(opts.Args)
	opts.Env = maps.Clone(opts.Env)

	c := &StdioClient{
		opts:       opts,
		callMu:     make(chan struct{}, 1),
		nextID:     1,
		closeStart: make(chan struct{}),
		closeDone:  make(chan struct{}),
	}
	c.callMu <- struct{}{}

	// 最小握手：initialize
	if err := c.initialize(ctx); err != nil {
		_ = c.Close()
		return nil, err
	}

	return c, nil
}

func (c *StdioClient) Close() error {
	c.closeOnce.Do(func() {
		defer close(c.closeDone)
		close(c.closeStart)
		c.stateMu.Lock()
		c.closed = true
		process := c.process
		c.stateMu.Unlock()
		if process != nil {
			c.terminate(process)
		}
		<-c.callMu
		c.callMu <- struct{}{}
		if process != nil {
			c.closeErr = process.waitErr
		}
	})
	<-c.closeDone
	return c.closeErr
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
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closeStart:
		return fmt.Errorf("mcp client is closed")
	case <-c.callMu:
	}
	defer func() { c.callMu <- struct{}{} }()
	if err := ctx.Err(); err != nil {
		return err
	}

	c.stateMu.Lock()
	if c.closed {
		c.stateMu.Unlock()
		return fmt.Errorf("mcp client is closed")
	}
	process := c.process
	if process == nil {
		var err error
		process, err = c.startProcess()
		if err == nil {
			c.process = process
		}
		c.stateMu.Unlock()
		if err != nil {
			return err
		}
		initCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := c.exchange(initCtx, process, stdioExchange{
			method: "initialize",
			params: map[string]any{
				"clientInfo": map[string]any{"name": "food-agent", "version": "0.0.1"},
			},
		}); err != nil {
			return err
		}
	} else {
		c.stateMu.Unlock()
	}
	if method == "initialize" {
		return nil
	}

	return c.exchange(ctx, process, stdioExchange{method: method, params: params, result: result})
}

func (c *StdioClient) exchange(ctx context.Context, process *stdioProcess, request stdioExchange) (err error) {
	watchStop := make(chan struct{})
	watchExited := make(chan struct{})
	go func() {
		defer close(watchExited)
		select {
		case <-ctx.Done():
			c.terminate(process)
		case <-watchStop:
		}
	}()
	defer func() {
		close(watchStop)
		<-watchExited
		if ctxErr := ctx.Err(); ctxErr != nil {
			c.discard(process)
			err = ctxErr
			return
		}
		if err != nil {
			var rpcErr *stdioRPCError
			if errors.As(err, &rpcErr) {
				return
			}
			c.discard(process)
		}
	}()

	c.stateMu.Lock()
	id := c.nextID
	c.nextID++
	c.stateMu.Unlock()

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  request.method,
		Params:  request.params,
	}

	if err := writeMessage(process.stdin, req); err != nil {
		return fmt.Errorf("mcp write request: %w", err)
	}

	// 读取响应（简单实现：串行调用，按顺序读取一条响应即可）。
	raw, err := readMessage(process.stdout)
	if err != nil {
		return fmt.Errorf("mcp read response: %w", err)
	}

	var resp jsonRPCResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("mcp unmarshal response: %w", err)
	}
	if resp.Error != nil {
		return &stdioRPCError{code: resp.Error.Code, message: resp.Error.Message}
	}

	// 将 resp.Result 再次 marshal/unmarshal 到具体结构体，避免使用 interface{} 直接断言。
	if request.result == nil {
		return nil
	}
	b, err := json.Marshal(resp.Result)
	if err != nil {
		return fmt.Errorf("mcp marshal result: %w", err)
	}
	if err := json.Unmarshal(b, request.result); err != nil {
		return fmt.Errorf("mcp unmarshal result: %w", err)
	}
	return nil
}
