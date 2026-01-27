package app

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"food-agent/internal/options"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"food-agent/pkg/client/amap"
)

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
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
}

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

func Run(cmd *cobra.Command, opts *options.ServerRunOptions) error {
	// cmd/mcp-server: default config file is empty (optional).
	if err := opts.Complete(cmd, ""); err != nil {
		return err
	}

	amapKey := strings.TrimSpace(opts.Amap.Key)
	amapBaseURL := strings.TrimSpace(opts.Amap.BaseURL)
	amapTimeout := opts.Amap.TimeoutSeconds

	if amapKey == "" {
		writeError(os.Stdout, 0, -32000, "missing amap key (flag --amap.key, env FOOD_AGENT_AMAP_KEY, or config amap.key)")
		return nil
	}

	cliOpts := []amap.Option{
		amap.WithBaseURL(amapBaseURL),
	}
	if amapTimeout > 0 {
		cliOpts = append(cliOpts, amap.WithTimeout(time.Duration(amapTimeout)*time.Second))
	}
	client := amap.NewClient(amapKey, cliOpts...)

	in := bufio.NewReader(os.Stdin)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()

	return serve(in, out, client)
}

func serve(in *bufio.Reader, out *bufio.Writer, cli *amap.Client) error {
	for {
		raw, err := readMessage(in)
		if err != nil {
			return nil
		}

		var req jsonRPCRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			writeError(out, 0, -32700, fmt.Sprintf("parse error: %v", err))
			_ = out.Flush()
			continue
		}

		switch req.Method {
		case "initialize":
			resp := jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: map[string]any{
					"serverInfo": map[string]any{
						"name":    "amap-mcp",
						"version": "0.0.1",
					},
					"capabilities": map[string]any{
						"tools": map[string]any{},
					},
				},
			}
			_ = writeMessage(out, resp)
			_ = out.Flush()

		case "tools/list":
			resp := jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: map[string]any{
					"tools": []map[string]any{
						{
							"name":        "amap.search_around",
							"description": "高德周边 POI 搜索：根据 location + keywords 返回附近地点列表。",
							"inputSchema": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"location": map[string]any{"type": "string", "description": "经纬度，格式 lng,lat"},
									"keywords": map[string]any{"type": "string", "description": "关键词，例如 火锅/咖啡"},
									"radius":   map[string]any{"type": "integer", "description": "半径（米），默认 1000"},
									"page":     map[string]any{"type": "integer", "description": "页码，从 1 开始"},
									"offset":   map[string]any{"type": "integer", "description": "每页条数，<=25"},
								},
								"required": []string{"location"},
							},
						},
					},
				},
			}
			_ = writeMessage(out, resp)
			_ = out.Flush()

		case "tools/call":
			var params toolsCallParams
			if err := json.Unmarshal(req.Params, &params); err != nil {
				writeError(out, req.ID, -32602, "invalid params")
				_ = out.Flush()
				continue
			}
			if params.Name != "amap.search_around" {
				writeError(out, req.ID, -32601, "tool not found")
				_ = out.Flush()
				continue
			}

			location, _ := params.Arguments["location"].(string)
			keywords, _ := params.Arguments["keywords"].(string)
			radius := intFromAny(params.Arguments["radius"])
			page := intFromAny(params.Arguments["page"])
			offset := intFromAny(params.Arguments["offset"])

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			res, err := cli.Place().Around(ctx, &amap.PlaceAroundRequest{
				Location: location,
				Keywords: keywords,
				Radius:   radius,
				Page:     page,
				Offset:   offset,
			})
			if err != nil {
				writeError(out, req.ID, -32000, err.Error())
				_ = out.Flush()
				continue
			}

			b, _ := json.Marshal(res)
			resp := jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: toolsCallResult{
					Content: []toolContent{
						{Type: "text", Text: string(b)},
					},
				},
			}
			_ = writeMessage(out, resp)
			_ = out.Flush()

		default:
			writeError(out, req.ID, -32601, "method not found")
			_ = out.Flush()
		}
	}
}

func intFromAny(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case int32:
		return int(t)
	case int64:
		return int(t)
	default:
		return 0
	}
}

func writeError(w io.Writer, id int64, code int, message string) {
	_ = writeMessage(w, jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &jsonRPCError{
			Code:    code,
			Message: message,
		},
	})
}

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
				return nil, fmt.Errorf("invalid content-length %q: %w", v, err)
			}
			contentLength = n
		}
	}
	if contentLength <= 0 {
		return nil, fmt.Errorf("missing content-length")
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
	_, err = fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(b))
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}
