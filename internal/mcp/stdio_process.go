package mcp

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

type stdioProcess struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	stdout   *bufio.Reader
	output   io.ReadCloser
	done     chan struct{}
	stopOnce sync.Once
	waitErr  error
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
	process := &stdioProcess{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
		output: stdout,
		done:   make(chan struct{}),
	}
	go func() {
		process.waitErr = cmd.Wait()
		close(process.done)
	}()
	return process, nil
}

func (c *StdioClient) terminate(process *stdioProcess) {
	process.stopOnce.Do(func() {
		_ = process.stdin.Close()
		_ = process.cmd.Process.Kill()
		_ = process.output.Close()
	})
	<-process.done
}

func (c *StdioClient) discard(process *stdioProcess) {
	c.stateMu.Lock()
	if c.process == process {
		c.process = nil
	}
	c.stateMu.Unlock()
	c.terminate(process)
}
