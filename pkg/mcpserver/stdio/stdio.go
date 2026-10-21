package stdio

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"food-agent/pkg/mcpserver/jsonrpc"
)

// Handler handles a parsed JSON-RPC request and returns a JSON-RPC response.
// If req.ID is empty (notification), the response will be ignored by Serve().
type Handler func(ctx context.Context, req jsonrpc.Request) jsonrpc.Response

// Serve runs an MCP stdio loop using LSP-like Content-Length framing.
// It blocks until EOF, an unrecoverable read/write error, or ctx is done.
func Serve(ctx context.Context, r io.Reader, w io.Writer, h Handler) error {
	if ctx == nil {
		return fmt.Errorf("stdio serve: nil context")
	}
	if r == nil || w == nil {
		return fmt.Errorf("stdio serve: nil reader/writer")
	}
	if h == nil {
		return fmt.Errorf("stdio serve: nil handler")
	}

	br := bufio.NewReader(r)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		raw, err := readMessage(br)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		var req jsonrpc.Request
		if err := json.Unmarshal(raw, &req); err != nil {
			resp := jsonrpc.Response{
				JSONRPC: "2.0",
				Error: &jsonrpc.Error{
					Code:    jsonrpc.CodeParseError,
					Message: "parse error",
					Data:    err.Error(),
				},
			}
			_ = writeMessage(w, resp)
			continue
		}

		// Basic validation.
		if strings.TrimSpace(req.JSONRPC) == "" {
			req.JSONRPC = "2.0"
		}
		if req.JSONRPC != "2.0" || strings.TrimSpace(req.Method) == "" {
			resp := jsonrpc.Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: &jsonrpc.Error{
					Code:    jsonrpc.CodeInvalidRequest,
					Message: "invalid request",
				},
			}
			_ = writeMessage(w, resp)
			continue
		}

		resp := h(ctx, req)
		if strings.TrimSpace(resp.JSONRPC) == "" {
			resp.JSONRPC = "2.0"
		}
		resp.ID = req.ID

		// Notification: do not write a response.
		if len(req.ID) == 0 {
			continue
		}
		if err := writeMessage(w, resp); err != nil {
			return err
		}
	}
}

// readMessage reads one Content-Length framed JSON message.
func readMessage(r *bufio.Reader) (json.RawMessage, error) {
	var contentLength int
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		const prefix = "Content-Length:"
		if strings.HasPrefix(strings.ToLower(line), strings.ToLower(prefix)) {
			v := strings.TrimSpace(line[len(prefix):])
			n, err := strconv.Atoi(v)
			if err != nil {
				return nil, fmt.Errorf("stdio framing: invalid content-length %q: %w", v, err)
			}
			contentLength = n
		}
	}
	if contentLength <= 0 {
		return nil, fmt.Errorf("stdio framing: missing content-length")
	}

	buf := make([]byte, contentLength)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return json.RawMessage(buf), nil
}

func writeMessage(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var header bytes.Buffer
	_, _ = fmt.Fprintf(&header, "Content-Length: %d\r\n\r\n", len(b))
	if _, err := w.Write(header.Bytes()); err != nil {
		return err
	}
	if _, err := w.Write(b); err != nil {
		return err
	}
	return nil
}
