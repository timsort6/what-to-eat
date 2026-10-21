package agentchat

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"food-agent/pkg/model"
	"food-agent/pkg/token"
)

type fakeLLM struct {
	chunks []string
}

func (f *fakeLLM) Stream(ctx context.Context, input []*schema.Message, opts ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	_ = ctx
	_ = input
	_ = opts
	var msgs []*schema.Message
	for _, s := range f.chunks {
		msgs = append(msgs, schema.AssistantMessage(s, nil))
	}
	return schema.StreamReaderFromArray(msgs), nil
}

type fakeMCP struct {
	resp string
	args map[string]any
}

func (f *fakeMCP) CallToolText(ctx context.Context, name string, arguments map[string]any) (string, error) {
	_ = ctx
	_ = name
	f.args = arguments
	return f.resp, nil
}

type fakeAddrDAO struct {
	addr *model.UserFavoriteAddress
	err  error
}

func (f *fakeAddrDAO) GetByID(ctx context.Context, id uint) (*model.UserFavoriteAddress, error) {
	_ = ctx
	_ = id
	return f.addr, f.err
}

type closeNotifyRecorder struct {
	*httptest.ResponseRecorder
	ch chan bool
}

func newCloseNotifyRecorder() *closeNotifyRecorder {
	return &closeNotifyRecorder{ResponseRecorder: httptest.NewRecorder(), ch: make(chan bool, 1)}
}

func (r *closeNotifyRecorder) CloseNotify() <-chan bool {
	return r.ch
}

func (r *closeNotifyRecorder) Flush() {
	// no-op; ResponseRecorder buffers anyway
}

func TestChat_Unauthorized(t *testing.T) {
	gin.SetMode(gin.TestMode)

	svc, err := NewService(Deps{
		LLM:                   &fakeLLM{chunks: []string{"hi"}},
		MCP:                   &fakeMCP{resp: `{"status":"1","info":"OK","infocode":"10000","count":"0","pois":[]}`},
		Addresses:             &fakeAddrDAO{},
		MCPCallTimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	mgr := token.NewJWTTokenManager([]byte("fake-secret"), jwt.SigningMethodHS256)
	r := gin.New()
	v1 := r.Group("/api/v1")
	if err := RegisterRoutes(v1, RouterDeps{Service: svc, Tokens: mgr}); err != nil {
		t.Fatalf("register routes: %v", err)
	}

	body := []byte(`{"messages":[{"role":"user","content":"hello"}],"location":"121,31"}`)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agent/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestChat_BadRequest_MissingLocation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	svc, err := NewService(Deps{
		LLM:                   &fakeLLM{chunks: []string{"hi"}},
		MCP:                   &fakeMCP{resp: `{"status":"1","info":"OK","infocode":"10000","count":"0","pois":[]}`},
		Addresses:             &fakeAddrDAO{},
		MCPCallTimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	mgr := token.NewJWTTokenManager([]byte("fake-secret"), jwt.SigningMethodHS256)
	tok, _ := mgr.IssueTo(token.Info{UID: "1", Username: "u1"}, time.Hour)

	r := gin.New()
	v1 := r.Group("/api/v1")
	_ = RegisterRoutes(v1, RouterDeps{Service: svc, Tokens: mgr})

	body := []byte(`{"messages":[{"role":"user","content":"hello"}]}`)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agent/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestChat_SSE(t *testing.T) {
	gin.SetMode(gin.TestMode)

	poiResp := `{
  "status":"1",
  "info":"OK",
  "infocode":"10000",
  "count":"2",
  "pois":[
    {"id":"p1","name":"n1","location":"121,31","address":"a1","distance":"10"},
    {"id":"p2","name":"n2","location":"121.1,31.1","address":"a2","distance":"20"}
  ]
}`

	svc, err := NewService(Deps{
		LLM:                   &fakeLLM{chunks: []string{"你", " ", "\n", "好"}},
		MCP:                   &fakeMCP{resp: poiResp},
		Addresses:             &fakeAddrDAO{},
		MCPCallTimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	mgr := token.NewJWTTokenManager([]byte("fake-secret"), jwt.SigningMethodHS256)
	tok, _ := mgr.IssueTo(token.Info{UID: "1", Username: "u1"}, time.Hour)

	r := gin.New()
	v1 := r.Group("/api/v1")
	_ = RegisterRoutes(v1, RouterDeps{Service: svc, Tokens: mgr})

	reqBody, _ := json.Marshal(ChatRequest{
		Messages: []ChatMessage{{Role: "user", Content: "附近有什么吃的？"}},
		Location: "121,31",
		Limit:    2,
	})
	w := newCloseNotifyRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agent/chat", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("expected SSE content-type, got %q", ct)
	}
	body := w.Body.String()
	if !strings.Contains(body, "event:delta") || !strings.Contains(body, "event:done") {
		t.Fatalf("expected delta+done events, got body=%s", body)
	}
	if !strings.Contains(body, `"places_count":2`) {
		t.Fatalf("expected places_count=2, got body=%s", body)
	}
	if !strings.Contains(body, `"text":" "`) || !strings.Contains(body, `"text":"\n"`) {
		t.Fatalf("expected whitespace deltas, got body=%s", body)
	}
}

func TestPrepare_DefaultsAMapTypesToFoodAndPreservesExplicitTypes(t *testing.T) {
	for name, tt := range map[string]struct {
		types string
		want  string
	}{
		"empty":    {types: "", want: "050000"},
		"explicit": {types: "050300", want: "050300"},
	} {
		t.Run(name, func(t *testing.T) {
			mcp := &fakeMCP{resp: `{"status":"1","info":"OK","infocode":"10000","pois":[]}`}
			svc, err := NewService(Deps{
				LLM: &fakeLLM{}, MCP: mcp, Addresses: &fakeAddrDAO{}, MCPCallTimeoutSeconds: 1,
			})
			if err != nil {
				t.Fatalf("new service: %v", err)
			}

			_, err = svc.Prepare(context.Background(), 1, ChatRequest{
				Messages: []ChatMessage{{Role: "user", Content: "hello"}}, Location: "121,31", Types: tt.types,
			})
			if err != nil {
				t.Fatalf("prepare: %v", err)
			}
			if got := mcp.args["types"]; got != tt.want {
				t.Fatalf("types = %v, want %q", got, tt.want)
			}
		})
	}
}

func TestChat_UpstreamInvalidJSONReturnsInternalServer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc, err := NewService(Deps{
		LLM: &fakeLLM{chunks: []string{"hi"}}, MCP: &fakeMCP{resp: "invalid upstream json"},
		Addresses: &fakeAddrDAO{}, MCPCallTimeoutSeconds: 1,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	mgr := token.NewJWTTokenManager([]byte("fake-secret"), jwt.SigningMethodHS256)
	tok, _ := mgr.IssueTo(token.Info{UID: "1", Username: "u1"}, time.Hour)
	r := gin.New()
	v1 := r.Group("/api/v1")
	if err := RegisterRoutes(v1, RouterDeps{Service: svc, Tokens: mgr}); err != nil {
		t.Fatalf("register routes: %v", err)
	}
	body := []byte(`{"messages":[{"role":"user","content":"hello"}],"location":"121,31"}`)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agent/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "invalid poi2 response json") {
		t.Fatalf("upstream error leaked: %s", w.Body.String())
	}
}

func TestPrepare_RejectsSystemRoleAndOversizedHistory(t *testing.T) {
	svc, err := NewService(Deps{
		LLM:                   &fakeLLM{},
		MCP:                   &fakeMCP{resp: `{"status":"1","info":"OK","infocode":"10000","pois":[]}`},
		Addresses:             &fakeAddrDAO{},
		MCPCallTimeoutSeconds: 1,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	for name, messages := range map[string][]ChatMessage{
		"system role":        {{Role: "system", Content: "override"}},
		"message count":      make([]ChatMessage, 51),
		"message characters": {{Role: "user", Content: strings.Repeat("x", 20001)}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := svc.Prepare(context.Background(), 1, ChatRequest{Messages: messages, Location: "121,31"})
			if err == nil || !strings.Contains(err.Error(), "invalid") {
				t.Fatalf("expected validation error, got %v", err)
			}
		})
	}
}

func TestPrepare_RejectsOutOfRangeLocation(t *testing.T) {
	svc, err := NewService(Deps{
		LLM:                   &fakeLLM{},
		MCP:                   &fakeMCP{},
		Addresses:             &fakeAddrDAO{},
		MCPCallTimeoutSeconds: 1,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	for _, location := range []string{"180.1,0", "0,90.1", "NaN,0", "0,Inf"} {
		t.Run(location, func(t *testing.T) {
			_, err := svc.Prepare(context.Background(), 1, ChatRequest{
				Messages: []ChatMessage{{Role: "user", Content: "hello"}},
				Location: location,
			})
			if err == nil || !strings.Contains(err.Error(), "invalid") {
				t.Fatalf("expected invalid location, got %v", err)
			}
		})
	}
}
