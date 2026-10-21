package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func newTestClient(t *testing.T, env map[string]string) *StdioClient {
	t.Helper()
	childEnv := map[string]string{"MCP_HELPER": "1", "GORACE": "atexit_sleep_ms=0"}
	for key, value := range env {
		childEnv[key] = value
	}
	client, err := NewStdioClient(testContext(t), StdioClientOptions{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestMCPHelperProcess", "--"},
		Env:     childEnv,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestStdioClient_ImmediateCancellationPreservesSuccessfulProcess(t *testing.T) {
	client := newTestClient(t, nil)
	process := client.process
	callCtx := testContext(t)
	for range 100 {
		ctx, cancel := context.WithCancel(callCtx)
		_, err := client.CallTool(ctx, "ok", nil)
		cancel()
		if err != nil {
			t.Fatalf("successful call: %v", err)
		}
		if _, err := client.CallTool(callCtx, "ok", nil); err != nil {
			t.Fatalf("client was killed after successful call: %v", err)
		}
		if client.process != process {
			t.Fatal("successful call caused an unexpected restart")
		}
	}
}

func TestStdioClient_CancelledCallRestartsProcessForNextCall(t *testing.T) {
	for _, mode := range []string{"read", "write"} {
		t.Run(mode, func(t *testing.T) {
			client := newTestClient(t, nil)
			callCtx := testContext(t)
			var arguments map[string]any
			if mode == "write" {
				if _, err := client.CallTool(callCtx, "stop_reading", nil); err != nil {
					t.Fatalf("stop reading: %v", err)
				}
				arguments = map[string]any{"payload": strings.Repeat("x", 1<<20)}
			}
			ctx, cancel := context.WithTimeout(callCtx, 100*time.Millisecond)
			defer cancel()
			started := time.Now()
			_, err := client.CallTool(ctx, "blocked", arguments)
			if err != context.DeadlineExceeded {
				t.Fatalf("expected deadline exceeded, got %v", err)
			}
			if elapsed := time.Since(started); elapsed > time.Second {
				t.Fatalf("cancellation took too long: %s", elapsed)
			}
			if _, err := client.CallTool(callCtx, "ok", nil); err != nil {
				t.Fatalf("call after recovery: %v", err)
			}
		})
	}
}

func TestStdioClient_FinalBufferedResponseSurvivesProcessExit(t *testing.T) {
	client := newTestClient(t, nil)
	text, err := client.CallToolText(testContext(t), "exit", nil)
	if err != nil || text != strings.Repeat("x", 1<<20) {
		t.Fatalf("final response lost: length=%d, error=%v", len(text), err)
	}
}

func TestStdioClient_CloseRemainsTerminal(t *testing.T) {
	client := newTestClient(t, nil)
	_ = client.Close()
	if _, err := client.CallTool(testContext(t), "ok", nil); err == nil {
		t.Fatal("closed client restarted")
	}
}

func TestStdioClient_QueuedCancellationDoesNotKillActiveCall(t *testing.T) {
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := listener.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	client := newTestClient(t, map[string]string{"MCP_GATE": listener.Addr().String()})
	callCtx := testContext(t)
	firstResult := make(chan error, 1)
	go func() {
		_, err := client.CallTool(callCtx, "blocked", nil)
		firstResult <- err
	}()
	connection, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(connection, make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
	baseCtx, cancel := context.WithCancel(callCtx)
	defer cancel()
	ctx := &notifyingContext{Context: baseCtx, ready: make(chan struct{})}
	secondResult := make(chan error, 1)
	go func() {
		_, err := client.CallTool(ctx, "ok", nil)
		secondResult <- err
	}()
	select {
	case <-ctx.ready:
	case <-time.After(time.Second):
		t.Fatal("queued call did not begin waiting")
	}
	cancel()
	select {
	case err := <-secondResult:
		if err != context.Canceled {
			t.Fatalf("queued call error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("queued call did not cancel promptly")
	}
	if _, err := connection.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-firstResult:
		if err != nil {
			t.Fatalf("active call was interrupted: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("active call did not finish")
	}
}

type notifyingContext struct {
	context.Context
	ready chan struct{}
}

func (ctx *notifyingContext) Done() <-chan struct{} {
	select {
	case <-ctx.ready:
	default:
		close(ctx.ready)
	}
	return ctx.Context.Done()
}

func TestMCPHelperProcess(t *testing.T) {
	if os.Getenv("MCP_HELPER") != "1" {
		return
	}
	reader := bufio.NewReader(os.Stdin)
	initialized := false
	for {
		raw, err := readMessage(reader)
		if err != nil {
			os.Exit(0)
		}
		var request jsonRPCRequest
		if err := json.Unmarshal(raw, &request); err != nil {
			os.Exit(1)
		}
		if request.Method == "initialize" {
			initialized = true
		} else if !initialized {
			os.Exit(1)
		}
		var params toolsCallParams
		if request.Method == "tools/call" {
			paramsRaw, err := json.Marshal(request.Params)
			if err != nil || json.Unmarshal(paramsRaw, &params) != nil {
				os.Exit(1)
			}
			if params.Name == "blocked" {
				if gate := os.Getenv("MCP_GATE"); gate != "" {
					connection, err := net.DialTimeout("tcp", gate, 3*time.Second)
					if err != nil {
						os.Exit(1)
					}
					if _, err := connection.Write([]byte{1}); err != nil {
						os.Exit(1)
					}
					if _, err := io.ReadFull(connection, make([]byte, 1)); err != nil {
						os.Exit(1)
					}
					_ = connection.Close()
				} else {
					_, _ = reader.ReadByte()
				}
			}
		}
		text := "ok"
		if params.Name == "exit" {
			text = strings.Repeat("x", 1<<20)
		}
		result := toolsCallResult{Content: []toolContent{{Type: "text", Text: text}}}
		if err := writeMessage(os.Stdout, jsonRPCResponse{JSONRPC: "2.0", ID: request.ID, Result: result}); err != nil {
			os.Exit(1)
		}
		switch params.Name {
		case "exit":
			os.Exit(0)
		case "stop_reading":
			<-time.After(time.Hour)
		}
	}
}
