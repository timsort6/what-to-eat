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
	opts    StdioClientOptions
	process *stdioProcess
	callMu  chan struct{}
	stateMu sync.Mutex
	closed  bool
	nextID  int64
}

type stdioProcess struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Reader
	output  io.ReadCloser
	done    chan struct{}
	waitErr error
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

	c := &StdioClient{
		opts:   opts,
		callMu: make(chan struct{}, 1),
		nextID: 1,
	}
	c.callMu <- struct{}{}

	if err := c.call(ctx, "initialize", nil, nil); err != nil {
		return nil, err
	}

	return c, nil
}

func (c *StdioClient) Close() error {
	if c.callMu == nil {
		return nil
	}
	c.stateMu.Lock()
	c.closed = true
	process := c.process
	c.stateMu.Unlock()
	if process != nil {
		_ = process.stdin.Close()
		select {
		case <-process.done:
		case <-time.After(2 * time.Second):
			_ = process.cmd.Process.Kill()
			<-process.done
		}
		_ = process.output.Close()
	}
	<-c.callMu
	defer func() { c.callMu <- struct{}{} }()
	if process != nil {
		return process.waitErr
	}
	return nil
}

func (c *StdioClient) startProcess() (*stdioProcess, error) {
	cmd := exec.Command(c.opts.Command, c.opts.Args...)
	if c.opts.WorkDir != "" {
		cmd.Dir = c.opts.WorkDir
	}
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	for key, value := range c.opts.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", key, value))
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp stdio client: stdin pipe: %w", err)
	}
	stdout, output, err := os.Pipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("mcp stdio client: stdout pipe: %w", err)
	}
	cmd.Stdout = output
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = output.Close()
		return nil, fmt.Errorf("mcp stdio client: start: %w", err)
	}
	_ = output.Close()
	process := &stdioProcess{cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdout), output: stdout, done: make(chan struct{})}
	// stdout 管道由本代持有，Wait 回收进程时不会提前关闭尚未读完的响应。
	go func() {
		process.waitErr = cmd.Wait()
		close(process.done)
	}()
	return process, nil
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

func (c *StdioClient) call(ctx context.Context, method string, params any, result any) error {
	// 串行化调用，避免并发下读写乱序。
	select {
	case <-ctx.Done():
		return ctx.Err()
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
		c.process = process
		c.stateMu.Unlock()
		if err != nil {
			return err
		}
		initCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := c.exchange(initCtx, process, "initialize", map[string]any{
			"clientInfo": map[string]any{"name": "food-agent", "version": "0.0.1"},
		}, nil); err != nil {
			c.discard(process)
			return err
		}
	} else {
		c.stateMu.Unlock()
	}
	if method == "initialize" {
		return nil
	}
	return c.exchange(ctx, process, method, params, result)
}

func (c *StdioClient) discard(process *stdioProcess) {
	c.stateMu.Lock()
	if c.process == process {
		c.process = nil
	}
	c.stateMu.Unlock()
	_ = process.cmd.Process.Kill()
	_ = process.stdin.Close()
	_ = process.output.Close()
	<-process.done
}

func (c *StdioClient) exchange(ctx context.Context, process *stdioProcess, method string, params any, result any) (err error) {
	watchStop := make(chan struct{})
	watchExited := make(chan struct{})
	go func() {
		defer close(watchExited)
		select {
		case <-ctx.Done():
			_ = process.cmd.Process.Kill()
			_ = process.stdin.Close()
			_ = process.output.Close()
		case <-watchStop:
		}
	}()
	defer func() {
		close(watchStop)
		<-watchExited
		if ctx.Err() != nil {
			c.discard(process)
			err = ctx.Err()
		}
	}()

	id := c.nextID
	c.nextID++

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	if err := writeMessage(process.stdin, req); err != nil {
		c.discard(process)
		return fmt.Errorf("mcp write request: %w", err)
	}

	// 读取响应（简单实现：串行调用，按顺序读取一条响应即可）。
	raw, err := readMessage(process.stdout)
	if err != nil {
		c.discard(process)
		return fmt.Errorf("mcp read response: %w", err)
	}
	if err := ctx.Err(); err != nil {
		c.discard(process)
		return err
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
