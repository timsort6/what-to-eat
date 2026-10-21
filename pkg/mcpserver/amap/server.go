package amap

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	amapsdk "food-agent/pkg/amap"
	"food-agent/pkg/mcpserver/jsonrpc"
)

// Backend abstracts AMap WebService calls so the MCP layer is testable.
type Backend interface {
	Geocode(ctx context.Context, req *amapsdk.GeocodeRequest) (*amapsdk.GeocodeResponse, error)
	Regeo(ctx context.Context, req *amapsdk.RegeoRequest) (*amapsdk.RegeoResponse, error)
	POI2Text(ctx context.Context, req *amapsdk.POI2TextRequest) (*amapsdk.POI2TextResponse, error)
	POI2Around(ctx context.Context, req *amapsdk.POI2AroundRequest) (*amapsdk.POI2AroundResponse, error)
}

// SDKBackend adapts food-agent/pkg/amap Client to Backend.
type SDKBackend struct {
	Client *amapsdk.Client
}

func NewSDKBackend(cli *amapsdk.Client) *SDKBackend { return &SDKBackend{Client: cli} }

func (b *SDKBackend) Geocode(ctx context.Context, req *amapsdk.GeocodeRequest) (*amapsdk.GeocodeResponse, error) {
	if b == nil || b.Client == nil {
		return nil, fmt.Errorf("nil sdk client")
	}
	return b.Client.Geo().Geocode(ctx, req)
}

func (b *SDKBackend) Regeo(ctx context.Context, req *amapsdk.RegeoRequest) (*amapsdk.RegeoResponse, error) {
	if b == nil || b.Client == nil {
		return nil, fmt.Errorf("nil sdk client")
	}
	return b.Client.Geo().Regeo(ctx, req)
}

func (b *SDKBackend) POI2Text(ctx context.Context, req *amapsdk.POI2TextRequest) (*amapsdk.POI2TextResponse, error) {
	if b == nil || b.Client == nil {
		return nil, fmt.Errorf("nil sdk client")
	}
	return b.Client.POI2().Text(ctx, req)
}

func (b *SDKBackend) POI2Around(ctx context.Context, req *amapsdk.POI2AroundRequest) (*amapsdk.POI2AroundResponse, error) {
	if b == nil || b.Client == nil {
		return nil, fmt.Errorf("nil sdk client")
	}
	return b.Client.POI2().Around(ctx, req)
}

// Server implements MCP JSON-RPC methods for AMap tools.
type Server struct {
	backend Backend
}

func NewServer(b Backend) *Server { return &Server{backend: b} }

func (s *Server) Handle(ctx context.Context, req jsonrpc.Request) jsonrpc.Response {
	switch strings.TrimSpace(req.Method) {
	case "initialize":
		return s.handleInitialize()
	case "tools/list":
		return s.handleToolsList()
	case "tools/call":
		return s.handleToolsCall(ctx, req.Params)
	default:
		return jsonrpc.Response{
			JSONRPC: "2.0",
			Error: &jsonrpc.Error{
				Code:    jsonrpc.CodeMethodNotFound,
				Message: "method not found",
				Data:    req.Method,
			},
		}
	}
}

func (s *Server) handleInitialize() jsonrpc.Response {
	// Minimal handshake: enough for clients that just need tools/call.
	return jsonrpc.Response{
		JSONRPC: "2.0",
		Result: map[string]any{
			"serverInfo": map[string]any{
				"name":    "food-agent-amap-mcp",
				"version": "0.0.1",
			},
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
		},
	}
}

func (s *Server) handleToolsList() jsonrpc.Response {
	return jsonrpc.Response{
		JSONRPC: "2.0",
		Result: toolsListResult{
			Tools: toolDefs(),
		},
	}
}

func (s *Server) handleToolsCall(ctx context.Context, rawParams json.RawMessage) jsonrpc.Response {
	if s == nil || s.backend == nil {
		return jsonrpc.Response{
			JSONRPC: "2.0",
			Error: &jsonrpc.Error{
				Code:    jsonrpc.CodeInternalError,
				Message: "nil backend",
			},
		}
	}

	var params toolsCallParams
	if len(rawParams) > 0 {
		if err := json.Unmarshal(rawParams, &params); err != nil {
			return jsonrpc.Response{
				JSONRPC: "2.0",
				Error: &jsonrpc.Error{
					Code:    jsonrpc.CodeInvalidParams,
					Message: "invalid params",
					Data:    err.Error(),
				},
			}
		}
	}

	name := strings.TrimSpace(params.Name)
	if name == "" {
		return jsonrpc.Response{
			JSONRPC: "2.0",
			Error: &jsonrpc.Error{
				Code:    jsonrpc.CodeInvalidParams,
				Message: "missing tool name",
			},
		}
	}

	// Tool aliases for backward compatibility.
	switch name {
	case "amap.search_around":
		name = "amap.poi2_around"
	}

	var (
		out any
		err error
	)

	switch name {
	case "amap.geocode":
		out, err = s.callGeocode(ctx, params.Arguments)
	case "amap.regeo":
		out, err = s.callRegeo(ctx, params.Arguments)
	case "amap.poi2_text":
		out, err = s.callPOI2Text(ctx, params.Arguments)
	case "amap.poi2_around":
		out, err = s.callPOI2Around(ctx, params.Arguments)
	default:
		return jsonrpc.Response{
			JSONRPC: "2.0",
			Error: &jsonrpc.Error{
				Code:    jsonrpc.CodeInvalidParams,
				Message: "unknown tool",
				Data:    name,
			},
		}
	}
	if err != nil {
		return jsonrpc.Response{
			JSONRPC: "2.0",
			Error: &jsonrpc.Error{
				Code:    jsonrpc.CodeInternalError,
				Message: err.Error(),
			},
		}
	}

	text := mustJSONPretty(out)
	return jsonrpc.Response{
		JSONRPC: "2.0",
		Result: toolsCallResult{
			Content: []toolContent{{Type: "text", Text: text}},
		},
	}
}

func (s *Server) callGeocode(ctx context.Context, args map[string]any) (*amapsdk.GeocodeResponse, error) {
	address := getString(args, "address")
	city := getString(args, "city")
	return s.backend.Geocode(ctx, &amapsdk.GeocodeRequest{
		Address: address,
		City:    city,
	})
}

func (s *Server) callRegeo(ctx context.Context, args map[string]any) (*amapsdk.RegeoResponse, error) {
	location := getString(args, "location")
	radius := getInt(args, "radius")
	extensions := getString(args, "extensions")
	return s.backend.Regeo(ctx, &amapsdk.RegeoRequest{
		Location:   location,
		Radius:     radius,
		Extensions: extensions,
	})
}

func (s *Server) callPOI2Text(ctx context.Context, args map[string]any) (*amapsdk.POI2TextResponse, error) {
	keywords := getString(args, "keywords")
	types := getString(args, "types")
	region := getString(args, "region")
	showFields := getString(args, "show_fields")
	pageSize := getInt(args, "page_size")
	pageNum := getInt(args, "page_num")
	var cityLimit *bool
	if v, ok := getBoolPtr(args, "city_limit"); ok {
		cityLimit = &v
	}

	return s.backend.POI2Text(ctx, &amapsdk.POI2TextRequest{
		Keywords:   keywords,
		Types:      types,
		Region:     region,
		CityLimit:  cityLimit,
		ShowFields: showFields,
		PageSize:   pageSize,
		PageNum:    pageNum,
	})
}

func (s *Server) callPOI2Around(ctx context.Context, args map[string]any) (*amapsdk.POI2AroundResponse, error) {
	location := getString(args, "location")
	keywords := getString(args, "keywords")
	types := getString(args, "types")
	radius := getInt(args, "radius")
	sortRule := getString(args, "sortrule")
	region := getString(args, "region")
	showFields := getString(args, "show_fields")
	pageSize := getInt(args, "page_size")
	pageNum := getInt(args, "page_num")
	var cityLimit *bool
	if v, ok := getBoolPtr(args, "city_limit"); ok {
		cityLimit = &v
	}

	return s.backend.POI2Around(ctx, &amapsdk.POI2AroundRequest{
		Location:   location,
		Keywords:   keywords,
		Types:      types,
		Radius:     radius,
		SortRule:   sortRule,
		Region:     region,
		CityLimit:  cityLimit,
		ShowFields: showFields,
		PageSize:   pageSize,
		PageNum:    pageNum,
	})
}

func getString(m map[string]any, k string) string {
	if m == nil {
		return ""
	}
	v, ok := m[k]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case fmt.Stringer:
		return strings.TrimSpace(t.String())
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

func getInt(m map[string]any, k string) int {
	if m == nil {
		return 0
	}
	v, ok := m[k]
	if !ok || v == nil {
		return 0
	}
	switch t := v.(type) {
	case int:
		return t
	case int32:
		return int(t)
	case int64:
		return int(t)
	case float32:
		return int(t)
	case float64:
		return int(t)
	case json.Number:
		n, _ := t.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(t))
		return n
	default:
		return 0
	}
}

func getBoolPtr(m map[string]any, k string) (bool, bool) {
	if m == nil {
		return false, false
	}
	v, ok := m[k]
	if !ok || v == nil {
		return false, false
	}
	switch t := v.(type) {
	case bool:
		return t, true
	case string:
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "1", "true", "yes", "y":
			return true, true
		case "0", "false", "no", "n":
			return false, true
		default:
			return false, false
		}
	default:
		return false, false
	}
}

func mustJSONPretty(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		// Last resort: string formatting.
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}
