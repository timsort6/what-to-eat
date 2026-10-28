package agentchat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"food-agent/internal/agent"
	"food-agent/pkg/config"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"food-agent/pkg/model"
	"food-agent/pkg/token"
)

type fakeLLM struct {
	calls             int
	failFinalCall     bool
	decisionArguments string
	tools             []*schema.ToolInfo
}

func (f *fakeLLM) Stream(ctx context.Context, input []*schema.Message, opts ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	_ = ctx
	f.calls++
	// 最终正文阶段禁止工具；绑定状态会残留决策工具，只能按 ToolChoiceForbidden 识别。
	if choice := einomodel.GetCommonOptions(nil, opts...).ToolChoice; choice != nil && *choice == schema.ToolChoiceForbidden {
		if f.failFinalCall {
			return nil, errors.New("model stream failed")
		}
		return schema.StreamReaderFromArray([]*schema.Message{schema.AssistantMessage("推荐面馆", nil)}), nil
	}
	if len(f.tools) == 1 && f.tools[0].Name == "finalize_recommendation" {
		var decisionInput struct {
			Decision agent.RecommendationDecision `json:"decision"`
		}
		if len(input) == 0 || json.Unmarshal([]byte(input[len(input)-1].Content), &decisionInput) != nil {
			return nil, errors.New("decision input missing")
		}
		decision, err := json.Marshal(decisionInput.Decision)
		if err != nil {
			return nil, err
		}
		if f.decisionArguments != "" {
			decision = []byte(f.decisionArguments)
		}
		return schema.StreamReaderFromArray([]*schema.Message{{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{
			ID: "decision-1", Type: "function", Function: schema.FunctionCall{Name: "finalize_recommendation", Arguments: string(decision)},
		}}}}), nil
	}
	if f.calls == 1 {
		return schema.StreamReaderFromArray([]*schema.Message{{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{
			ID: "search-1", Type: "function", Function: schema.FunctionCall{Name: "amap_poi2_around", Arguments: `{"keywords":"面馆"}`},
		}}}}), nil
	}
	return schema.StreamReaderFromArray([]*schema.Message{{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{
		ID: "search-2", Type: "function", Function: schema.FunctionCall{Name: "amap_poi2_around", Arguments: `{"keywords":"面馆"}`},
	}}}}), nil
}

func (f *fakeLLM) WithTools(tools []*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	f.tools = tools
	return f, nil
}

func (f *fakeLLM) Generate(context.Context, []*schema.Message, ...einomodel.Option) (*schema.Message, error) {
	return nil, errors.New("unexpected generate")
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

func newTestService(t *testing.T, llm einomodel.BaseChatModel, mcp *fakeMCP, addresses FavoriteAddressDAO) *Service {
	t.Helper()
	svc, err := NewService(Deps{
		LLM: llm, MCP: mcp, Addresses: addresses, Prompt: "system",
		Budgets: config.AgentConfig{
			AllowedTools: []string{agent.ToolNamePOI2Around, agent.ToolNamePOI2Text},
			MaxSteps:     8, MaxModelCalls: 4, MaxToolCalls: 3, TimeoutSeconds: 5,
		},
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return svc
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

	svc := newTestService(t, &fakeLLM{}, &fakeMCP{resp: `{"status":"1","pois":[]}`}, &fakeAddrDAO{})

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

	svc := newTestService(t, &fakeLLM{}, &fakeMCP{resp: `{"status":"1","pois":[]}`}, &fakeAddrDAO{})

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

	llm := &fakeLLM{}
	svc := newTestService(t, llm, &fakeMCP{resp: poiResp}, &fakeAddrDAO{})

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
	if !strings.Contains(body, `"places_count":1`) || !strings.Contains(body, `"name":"n1"`) {
		t.Fatalf("expected tool places in done event, got body=%s", body)
	}
	if llm.calls != 4 {
		t.Fatalf("model calls = %d, want two search, decision, and final response", llm.calls)
	}
}

func TestChat_StreamErrorDoesNotEmitDone(t *testing.T) {
	gin.SetMode(gin.TestMode)
	llm := &fakeLLM{failFinalCall: true}
	svc := newTestService(t, llm, &fakeMCP{resp: `{"status":"1","pois":[]}`}, &fakeAddrDAO{})
	mgr := token.NewJWTTokenManager([]byte("fake-secret"), jwt.SigningMethodHS256)
	tok, err := mgr.IssueTo(token.Info{UID: "1", Username: "u1"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	if err := RegisterRoutes(r.Group("/api/v1"), RouterDeps{Service: svc, Tokens: mgr}); err != nil {
		t.Fatal(err)
	}
	// Runner 在搜索前即返回流，最终正文失败只能以 SSE error 事件呈现（响应头已提交为 200）。
	w := newCloseNotifyRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agent/chat", strings.NewReader(`{"messages":[{"role":"user","content":"附近吃什么"}],"location":"121,31"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	r.ServeHTTP(w, req)
	body := w.Body.String()
	if !strings.Contains(body, "event:error") || strings.Contains(body, "event:done") || strings.Contains(body, "event:delta") || strings.Contains(body, "model stream failed") {
		t.Fatalf("stream failure response = %d %s, want generic error and no done", w.Code, body)
	}
	if llm.calls != 4 {
		t.Fatalf("model calls = %d, want failure at final response after two search and decision", llm.calls)
	}
}

func TestChat_CanceledRequestDoesNotEmitDone(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newTestService(t, &fakeLLM{}, &fakeMCP{resp: `{"status":"1","pois":[]}`}, &fakeAddrDAO{})
	mgr := token.NewJWTTokenManager([]byte("fake-secret"), jwt.SigningMethodHS256)
	tok, err := mgr.IssueTo(token.Info{UID: "1", Username: "u1"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	if err := RegisterRoutes(r.Group("/api/v1"), RouterDeps{Service: svc, Tokens: mgr}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agent/chat", strings.NewReader(`{"messages":[{"role":"user","content":"附近吃什么"}],"location":"121,31"}`)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if strings.Contains(w.Body.String(), "event:done") {
		t.Fatalf("canceled request emitted done: %s", w.Body.String())
	}
}

func TestPrepare_UsesHistoryWithoutSearchingOrAcceptingSystemMessages(t *testing.T) {
	mcp := &fakeMCP{resp: `{"status":"1","pois":[]}`}
	svc := newTestService(t, &fakeLLM{}, mcp, &fakeAddrDAO{})
	prepared, err := svc.Prepare(context.Background(), 1, ChatRequest{
		Messages: []ChatMessage{{Role: "user", Content: "hello"}, {Role: "assistant", Content: "hi"}},
		Location: "121,31",
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if len(prepared.ModelInput) != 2 || prepared.ModelInput[0].Role != schema.User || prepared.ModelInput[1].Role != schema.Assistant {
		t.Fatalf("model input = %#v, want full user/assistant history", prepared.ModelInput)
	}
	if mcp.args != nil {
		t.Fatalf("Prepare called MCP eagerly: %#v", mcp.args)
	}
}

func TestChat_UpstreamInvalidJSONEmitsSSEErrorAfter200(t *testing.T) {
	gin.SetMode(gin.TestMode)
	llm, mcp := &fakeLLM{}, &fakeMCP{resp: "invalid upstream json"}
	svc := newTestService(t, llm, mcp, &fakeAddrDAO{})
	mgr := token.NewJWTTokenManager([]byte("fake-secret"), jwt.SigningMethodHS256)
	tok, _ := mgr.IssueTo(token.Info{UID: "1", Username: "u1"}, time.Hour)
	r := gin.New()
	v1 := r.Group("/api/v1")
	if err := RegisterRoutes(v1, RouterDeps{Service: svc, Tokens: mgr}); err != nil {
		t.Fatalf("register routes: %v", err)
	}
	body := []byte(`{"messages":[{"role":"user","content":"hello"}],"location":"121,31"}`)
	// 上游失败发生在已返回流后的首轮搜索，只能以通用 SSE error 呈现（响应头已提交为 200）。
	w := newCloseNotifyRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agent/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	r.ServeHTTP(w, req)
	got := w.Body.String()
	if !strings.Contains(got, "event:error") || strings.Contains(got, "event:done") || strings.Contains(got, "event:delta") {
		t.Fatalf("expected generic error event without done, got %d body=%s", w.Code, got)
	}
	if strings.Contains(got, "invalid poi2 response json") || strings.Contains(got, "invalid upstream json") {
		t.Fatalf("upstream error leaked: %s", got)
	}
	if mcp.args == nil || llm.calls != 1 {
		t.Fatalf("mcp called=%v model calls=%d, want failure at first search tool before decision", mcp.args != nil, llm.calls)
	}
}

func TestPrepare_RejectsSystemRoleAndOversizedHistory(t *testing.T) {
	svc := newTestService(t, &fakeLLM{}, &fakeMCP{resp: `{"status":"1","pois":[]}`}, &fakeAddrDAO{})

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
	svc := newTestService(t, &fakeLLM{}, &fakeMCP{}, &fakeAddrDAO{})

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

func TestPrepare_RejectsFavoriteAddressOwnedByAnotherUser(t *testing.T) {
	addressID := uint(7)
	svc := newTestService(t, &fakeLLM{}, &fakeMCP{}, &fakeAddrDAO{addr: &model.UserFavoriteAddress{
		UserID: 2, Longitude: 121, Latitude: 31,
	}})
	_, err := svc.Prepare(context.Background(), 1, ChatRequest{
		Messages: []ChatMessage{{Role: "user", Content: "附近吃什么"}}, AddressID: &addressID,
	})
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("prepare error = %v, want permission denied", err)
	}
}
