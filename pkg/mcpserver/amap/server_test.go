package amap

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	amapsdk "food-agent/pkg/amap"
	"food-agent/pkg/mcpserver/jsonrpc"
)

type fakeBackend struct{}

func (b fakeBackend) Geocode(ctx context.Context, req *amapsdk.GeocodeRequest) (*amapsdk.GeocodeResponse, error) {
	_ = ctx
	resp := &amapsdk.GeocodeResponse{
		Count: "1",
		Geocodes: []amapsdk.GeocodeItem{
			{FormattedAddress: req.Address, Location: "1,2"},
		},
	}
	resp.Status = "1"
	resp.Info = "OK"
	resp.Infocode = "10000"
	return resp, nil
}

func (b fakeBackend) Regeo(ctx context.Context, req *amapsdk.RegeoRequest) (*amapsdk.RegeoResponse, error) {
	_ = ctx
	resp := &amapsdk.RegeoResponse{
		Regeocode: map[string]any{"location": req.Location},
	}
	resp.Status = "1"
	resp.Info = "OK"
	resp.Infocode = "10000"
	return resp, nil
}

func (b fakeBackend) POI2Text(ctx context.Context, req *amapsdk.POI2TextRequest) (*amapsdk.POI2TextResponse, error) {
	_ = ctx
	resp := &amapsdk.POI2TextResponse{
		Count: "1",
		Pois: []amapsdk.POI2POI{
			{Name: req.Keywords, Location: "1,2"},
		},
	}
	resp.Status = "1"
	resp.Info = "OK"
	resp.Infocode = "10000"
	return resp, nil
}

func (b fakeBackend) POI2Around(ctx context.Context, req *amapsdk.POI2AroundRequest) (*amapsdk.POI2AroundResponse, error) {
	_ = ctx
	resp := &amapsdk.POI2AroundResponse{
		Count: "1",
		Pois: []amapsdk.POI2POI{
			{Name: req.Keywords, Location: req.Location, Distance: "10"},
		},
	}
	resp.Status = "1"
	resp.Info = "OK"
	resp.Infocode = "10000"
	return resp, nil
}

func TestToolsList(t *testing.T) {
	s := NewServer(fakeBackend{})
	resp := s.Handle(context.Background(), jsonrpc.Request{
		JSONRPC: "2.0",
		Method:  "tools/list",
	})
	if resp.Error != nil {
		t.Fatalf("expected nil error, got %+v", resp.Error)
	}

	b, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var out struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(out.Tools) == 0 {
		t.Fatalf("expected non-empty tool list")
	}
}

func TestToolCall_Geocode(t *testing.T) {
	s := NewServer(fakeBackend{})
	params := map[string]any{
		"name": "amap.geocode",
		"arguments": map[string]any{
			"address": "test-addr",
		},
	}
	raw, _ := json.Marshal(params)
	resp := s.Handle(context.Background(), jsonrpc.Request{
		JSONRPC: "2.0",
		Method:  "tools/call",
		Params:  raw,
	})
	if resp.Error != nil {
		t.Fatalf("expected nil error, got %+v", resp.Error)
	}

	b, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(out.Content) == 0 || out.Content[0].Type != "text" {
		t.Fatalf("unexpected content: %+v", out.Content)
	}
	if !strings.Contains(out.Content[0].Text, "\"status\": \"1\"") {
		t.Fatalf("expected response json, got: %s", out.Content[0].Text)
	}
}

func TestToolCall_UnknownTool(t *testing.T) {
	s := NewServer(fakeBackend{})
	params := map[string]any{"name": "amap.nope", "arguments": map[string]any{}}
	raw, _ := json.Marshal(params)
	resp := s.Handle(context.Background(), jsonrpc.Request{
		JSONRPC: "2.0",
		Method:  "tools/call",
		Params:  raw,
	})
	if resp.Error == nil {
		t.Fatalf("expected error")
	}
	if resp.Error.Code != jsonrpc.CodeInvalidParams {
		t.Fatalf("expected invalid params, got %d", resp.Error.Code)
	}
}
