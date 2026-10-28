package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"food-agent/pkg/config"

	openai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
)

// providerRequest 是本地 OpenAI 兼容提供方区分搜索、决策、最终正文三阶段的最小请求视图。
type providerRequest struct {
	ToolChoice string `json:"tool_choice"`
	Tools      []struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	} `json:"tools"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

func (r providerRequest) final() bool { return len(r.Tools) == 0 && r.ToolChoice == "none" }

func (r providerRequest) decision() bool {
	return len(r.Tools) == 1 && r.Tools[0].Function.Name == finalizeName
}

// writeProviderDecision 以流式 tool_call 回显决策输入里的服务端评估结果。
func writeProviderDecision(t *testing.T, w http.ResponseWriter, request providerRequest) {
	t.Helper()
	if len(request.Messages) == 0 {
		t.Error("决策请求缺少输入")
		return
	}
	arguments, err := echoedDecision(request.Messages[len(request.Messages)-1].Content)
	if err != nil {
		t.Error(err)
		return
	}
	encoded, err := json.Marshal(arguments)
	if err != nil {
		t.Error(err)
		return
	}
	writeRunnerDelta(t, w, fmt.Sprintf(`{"role":"assistant","tool_calls":[{"index":0,"id":"decision-1","type":"function","function":{"name":%q,"arguments":%s}}]}`, finalizeName, encoded))
	if _, err := io.WriteString(w, "data: [DONE]\n\n"); err != nil {
		t.Error(err)
	}
}

func TestRunner_FirstDeltaArrives_whenProviderWithholdsEOF(t *testing.T) {
	proceed := make(chan struct{})
	var requests atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request providerRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		requests.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		if request.decision() {
			writeProviderDecision(t, w, request)
			return
		}
		if !request.final() {
			t.Error("2 模型/4 步不应产生搜索请求")
			return
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", `{"choices":[{"index":0,"delta":{"role":"assistant","content":" 前段 "}}]}`); err != nil {
			t.Error(err)
			return
		}
		w.(http.Flusher).Flush()
		select {
		case <-proceed:
		case <-r.Context().Done():
			return
		}
		if _, err := io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"尾段\\n\"}}]}\n\ndata: [DONE]\n\n"); err != nil {
			t.Error(err)
		}
	}))
	defer provider.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	chatModel, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{BaseURL: provider.URL + "/v1", Model: "local-test", HTTPClient: provider.Client()})
	require.NoError(t, err)
	// 2 模型/4 步 = 零轮搜索 + 内部决策 + 最终正文，首段来自真实最终 HTTP 流；生产配置要求至少一个工具。
	search := &runnerTool{}
	runner, err := NewRunner("", config.AgentConfig{AllowedTools: []string{"find"}, MaxSteps: 4, MaxModelCalls: 2, MaxToolCalls: 1, TimeoutSeconds: 3}, chatModel, map[string]tool.InvokableTool{"find": search})
	require.NoError(t, err)

	stream, err := runner.Stream(stagesContext(ctx), []*schema.Message{schema.UserMessage("测试")})
	require.NoError(t, err, "服务端仍等待放行，Stream 必须提前返回")
	defer stream.Close()
	message, err := stream.Recv()
	require.NoError(t, err, "服务端仍等待放行，必须先收到首段")
	require.Equal(t, " 前段 ", message.Content)
	require.EqualValues(t, 2, requests.Load())
	close(proceed)
	message, err = stream.Recv()
	require.NoError(t, err)
	require.Equal(t, "尾段\n", message.Content)
	_, err = stream.Recv()
	require.ErrorIs(t, err, io.EOF)
	require.Zero(t, search.runs)
}

func TestEinoRunner_HandlesToolCalls_whenStreamStartsWithContent(t *testing.T) {
	preamble := `{"role":"assistant","content":"准备搜索"}`
	toolStart := `{"tool_calls":[{"index":0,"id":"search-1","type":"function","function":{"name":"find","arguments":"{"}}]}`
	toolEnd := `{"tool_calls":[{"index":0,"function":{"arguments":"}"}}]}`
	for _, scenario := range []struct {
		name      string
		deltas    []string
		wantText  string
		wantRuns  int
		wantError bool
	}{
		// 搜索阶段正文一律不外泄；可见文本只来自最终正文阶段。
		{name: "工具先行", deltas: []string{toolStart, toolEnd}, wantText: "完成", wantRuns: 1},
		{name: "正文先行", deltas: []string{preamble, toolStart, toolEnd}, wantText: "完成", wantRuns: 1},
		{name: "纯文本", deltas: []string{preamble, `{"content":"完成"}`}, wantText: "完成"},
		{name: "空流", wantText: "完成"},
		{name: "正文后错误", deltas: []string{preamble, `invalid`}, wantError: true},
		{name: "工具参数中断", deltas: []string{preamble, toolStart, `invalid`}, wantError: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var searches atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request providerRequest
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if request.decision() {
					writeProviderDecision(t, w, request)
					return
				}
				deltas := scenario.deltas
				if request.final() {
					deltas = []string{`{"role":"assistant","content":"完成"}`}
				} else {
					searches.Add(1)
				}
				for _, delta := range deltas {
					if _, err := fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":%s}]}\n\n", delta); err != nil {
						t.Error(err)
						return
					}
					w.(http.Flusher).Flush()
				}
				if _, err := io.WriteString(w, "data: [DONE]\n\n"); err != nil {
					t.Error(err)
				}
			}))
			defer provider.Close()
			chatModel, err := openai.NewChatModel(t.Context(), &openai.ChatModelConfig{BaseURL: provider.URL + "/v1", Model: "local-test", HTTPClient: provider.Client()})
			require.NoError(t, err)
			search := &finalizationTool{}
			// 3 模型/6 步 = 一轮真实搜索流 + 决策 + 最终正文。
			runner, err := NewRunner("system", config.AgentConfig{AllowedTools: []string{"find"}, MaxSteps: 6, MaxModelCalls: 3, MaxToolCalls: 1, TimeoutSeconds: 3}, chatModel, map[string]tool.InvokableTool{"find": search})
			require.NoError(t, err)
			stream, err := runner.Stream(stagesContext(t.Context()), []*schema.Message{schema.UserMessage("附近找面馆")})
			var text string
			if stream != nil {
				defer stream.Close()
				for err == nil {
					var message *schema.Message
					message, err = stream.Recv()
					if err == nil {
						text += message.Content
						require.Empty(t, message.ToolCalls)
					}
				}
				require.Len(t, stream.Places(), scenario.wantRuns)
			}
			if scenario.wantError {
				require.Error(t, err)
				require.NotErrorIs(t, err, io.EOF)
			} else {
				require.ErrorIs(t, err, io.EOF)
			}
			require.Equal(t, scenario.wantText, text)
			require.NotContains(t, text, "准备搜索")
			require.EqualValues(t, scenario.wantRuns, search.count.Load())
			require.EqualValues(t, 1, searches.Load())
		})
	}
}

func TestRunner_EmitsEachRoundOnce_whenLateToolsRepeat(t *testing.T) {
	for _, cycles := range []int{1, 2} {
		t.Run(fmt.Sprint(cycles), func(t *testing.T) {
			proceed := make(chan struct{})
			var searches atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				var stage providerRequest
				var request struct {
					Messages []*schema.Message `json:"messages"`
				}
				if err := errors.Join(err, json.Unmarshal(body, &stage), json.Unmarshal(body, &request)); err != nil {
					t.Error(err)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if stage.decision() {
					writeProviderDecision(t, w, stage)
					return
				}
				if stage.final() {
					writeRunnerDelta(t, w, `{"role":"assistant","content":"最终正文"}`)
					if _, err := io.WriteString(w, "data: [DONE]\n\n"); err != nil {
						t.Error(err)
					}
					return
				}
				searches.Add(1)
				round := 0
				for _, message := range request.Messages {
					if message.Role == schema.Tool {
						round++
					}
					if len(message.ToolCalls) > 0 && message.ToolCalls[0].Function.Arguments != `{}` {
						t.Errorf("工具参数未正确拼合：%q", message.ToolCalls[0].Function.Arguments)
					}
				}
				writeRunnerDelta(t, w, fmt.Sprintf(`{"role":"assistant","content":"第%d轮"}`, round))
				if round == 0 {
					select {
					case <-proceed:
					case <-r.Context().Done():
						return
					}
				}
				if round < cycles {
					writeRunnerDelta(t, w, fmt.Sprintf(`{"tool_calls":[{"index":0,"id":"call-%d","type":"function","function":{"name":"find","arguments":"{"}}]}`, round))
					writeRunnerDelta(t, w, `{"tool_calls":[{"index":0,"function":{"arguments":"}"}}]}`)
				}
				if _, err := io.WriteString(w, "data: [DONE]\n\n"); err != nil {
					t.Error(err)
				}
			}))
			defer provider.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			chatModel, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{BaseURL: provider.URL + "/v1", Model: "local-test", HTTPClient: provider.Client()})
			require.NoError(t, err)
			search := &runnerTool{}
			// 5 模型/12 步 = 最多三轮搜索（cycles 轮工具 + 一轮纯文本收敛）+ 决策 + 最终正文。
			runner, err := NewRunner("", config.AgentConfig{AllowedTools: []string{"find"}, MaxSteps: 12, MaxModelCalls: 5, MaxToolCalls: 2, TimeoutSeconds: 4}, chatModel, map[string]tool.InvokableTool{"find": search})
			require.NoError(t, err)

			stream, err := runner.Stream(stagesContext(ctx), []*schema.Message{schema.UserMessage("测试")})
			require.NoError(t, err, "首轮搜索仍被提供方挂起时 Stream 必须已返回")
			defer stream.Close()
			close(proceed)
			var text string
			for {
				message, err := stream.Recv()
				if errors.Is(err, io.EOF) {
					break
				}
				require.NoError(t, err)
				require.Empty(t, message.ToolCalls)
				text += message.Content
			}
			// 搜索轮正文（第N轮）既不重复也不外泄，只交付一次最终正文。
			require.Equal(t, "最终正文", text)
			require.Equal(t, cycles, search.runs)
			require.EqualValues(t, cycles+1, searches.Load())
		})
	}
}

func writeRunnerDelta(t *testing.T, w http.ResponseWriter, delta string) {
	t.Helper()
	if _, err := fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":%s}]}\n\n", delta); err != nil {
		t.Error(err)
		return
	}
	w.(http.Flusher).Flush()
}

func TestRunner_CancelsRemoteHTTP_whenClosedOrCanceled(t *testing.T) {
	for _, action := range []string{"首段后关闭", "背压时关闭", "首段后取消", "首段前取消"} {
		t.Run(action, func(t *testing.T) {
			search := &finalizationTool{}
			decision := stagesDecision(t)
			var requests atomic.Int32
			started, canceled := make(chan struct{}), make(chan error, 1)
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Stream     bool   `json:"stream"`
					ToolChoice string `json:"tool_choice"`
					Tools      []struct {
						Type     string                `json:"type"`
						Function struct{ Name string } `json:"function"`
					} `json:"tools"`
					Messages []struct{ Content string } `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				round := requests.Add(1)
				if !request.Stream {
					t.Error("必须使用真实流式 HTTP 请求")
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				switch round {
				case 1, 2:
					name, arguments := "find", `{}`
					if round == 2 {
						name, arguments = finalizeName, decision
					}
					if len(request.Tools) != 1 || request.Tools[0].Type != "function" || request.Tools[0].Function.Name != name {
						t.Errorf("第 %d 次请求未绑定预期工具 %s", round, name)
						return
					}
					encoded, err := json.Marshal(arguments)
					if err != nil {
						t.Error(err)
						return
					}
					writeRunnerDelta(t, w, fmt.Sprintf(`{"role":"assistant","tool_calls":[{"index":0,"id":"call-%d","type":"function","function":{"name":%q,"arguments":%s}}]}`, round, name, encoded))
					if _, err := io.WriteString(w, "data: [DONE]\n\n"); err != nil {
						t.Error(err)
					}
					return
				case 3:
					if len(request.Tools) != 0 || request.ToolChoice != "none" || len(request.Messages) != 1 {
						t.Error("第三次请求必须是禁用工具的最终正文请求")
						return
					}
					var input struct {
						Decision RecommendationDecision `json:"decision"`
						Places   []Place                `json:"places"`
					}
					if err := json.Unmarshal([]byte(request.Messages[0].Content), &input); err != nil {
						t.Error(err)
						return
					}
					if input.Decision.Status != DecisionReady || len(input.Places) != 1 || input.Places[0].ID != "p1" || search.count.Load() != 1 {
						t.Error("最终 HTTP 请求未携带经过搜索和校验的真实工具候选")
						return
					}
				default:
					t.Errorf("意外的额外 HTTP 请求：%d", round)
					return
				}
				if action == "首段前取消" {
					w.(http.Flusher).Flush()
				} else {
					writeRunnerDelta(t, w, `{"role":"assistant","content":"首段"}`)
					writeRunnerDelta(t, w, `{"content":"后续"}`)
				}
				close(started)
				<-r.Context().Done()
				canceled <- r.Context().Err()
			}))
			defer func() { provider.CloseClientConnections(); provider.Close() }()
			ctx, cancel := context.WithTimeout(stagesContext(t.Context()), 3*time.Second)
			defer cancel()
			chatModel, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{BaseURL: provider.URL + "/v1", Model: "local-test", HTTPClient: provider.Client()})
			require.NoError(t, err)
			var runnerModel model.ToolCallingChatModel = chatModel
			if action == "首段后关闭" {
				inherited, err := chatModel.WithTools([]*schema.ToolInfo{{Name: "inherited", ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{})}})
				require.NoError(t, err)
				runnerModel = inherited
			}
			runner, err := NewRunner("", config.AgentConfig{AllowedTools: []string{"find"}, MaxSteps: 6, MaxModelCalls: 3, MaxToolCalls: 1, TimeoutSeconds: 4}, runnerModel, map[string]tool.InvokableTool{"find": search})
			require.NoError(t, err)
			stream, err := runner.Stream(ctx, []*schema.Message{schema.UserMessage("测试")})
			require.NoError(t, err)
			defer stream.Close()
			select {
			case <-started:
			case <-stream.done:
				t.Fatalf("最终 HTTP 请求前执行失败：请求数=%d，错误=%v", requests.Load(), stream.err)
			case <-ctx.Done():
				t.Fatalf("搜索、决策后最终正文 HTTP 请求未启动：请求数=%d", requests.Load())
			}
			require.EqualValues(t, 3, requests.Load())
			trace := stream.budget.trace
			trace.mu.Lock()
			decisionCompleted, directReturned := trace.decisionCompleted, trace.directReturned
			trace.mu.Unlock()
			require.Equal(t, 1, decisionCompleted)
			require.Equal(t, 1, directReturned)
			if action == "首段后关闭" || action == "首段后取消" {
				message, recvErr := stream.Recv()
				require.NoError(t, recvErr)
				require.Equal(t, "首段", message.Content)
			}
			select {
			case <-stream.done:
				t.Fatal("操作前最终正文流不应结束")
			default:
			}
			deadline := time.NewTimer(time.Second)
			defer deadline.Stop()
			closed := make(chan struct{})
			go func() {
				switch action {
				case "首段前取消", "首段后取消":
					cancel()
				case "首段后关闭", "背压时关闭":
				}
				stream.Close()
				stream.Close()
				close(closed)
			}()
			select {
			case serverErr := <-canceled:
				require.ErrorIs(t, serverErr, context.Canceled)
			case <-deadline.C:
				t.Fatal("关闭或取消未在一秒内传到最终正文 HTTP 上下文")
			}
			select {
			case <-closed:
			case <-deadline.C:
				t.Fatal("背压或等待首段时 Close 未在一秒内幂等退出")
			}
			message, recvErr := stream.Recv()
			require.Nil(t, message, "取消后不得交付正文")
			require.ErrorIs(t, recvErr, context.Canceled)
			require.NotErrorIs(t, recvErr, io.EOF)
			require.EqualValues(t, 3, requests.Load())
			t.Logf("真实最终 HTTP 已到达；请求数=%d，搜索=1，已验证决策=1，终止=%v，服务端上下文已取消，Close 幂等退出", requests.Load(), recvErr)
		})
	}
}

type nullableRunnerModel struct {
	model.ToolCallingChatModel
	chunks []*schema.Message
}

func (m *nullableRunnerModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}

func (m *nullableRunnerModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return schema.StreamReaderFromArray(m.chunks), nil
}

func TestRunner_SkipsNilChunks_whenModelStreamAllowsNil(t *testing.T) {
	for _, chunks := range [][]*schema.Message{nil, {nil, nil}, {nil, schema.AssistantMessage(" 文本\n", nil), nil}} {
		// 3 模型/6 步：同一组含 nil 的分片先经过搜索阶段，再经过最终正文阶段。
		runner, err := NewRunner("", config.AgentConfig{MaxSteps: 6, MaxModelCalls: 3, MaxToolCalls: 1, TimeoutSeconds: 2}, &protocolModel{ToolCallingChatModel: &nullableRunnerModel{chunks: chunks}}, nil)
		require.NoError(t, err)
		stream, err := runner.Stream(stagesContext(t.Context()), []*schema.Message{schema.UserMessage("测试")})
		require.NoError(t, err)
		var text string
		var recvErr error
		for {
			var message *schema.Message
			message, recvErr = stream.Recv()
			if recvErr != nil {
				break
			}
			require.NotNil(t, message)
			text += message.Content
		}
		if len(chunks) == 3 {
			require.ErrorIs(t, recvErr, io.EOF)
			require.Equal(t, " 文本\n", text)
		} else {
			// 仅含 nil 的最终流等同空正文：必须报错而不是静默 EOF。
			require.Error(t, recvErr)
			require.NotErrorIs(t, recvErr, io.EOF)
			require.NotErrorIs(t, recvErr, ErrBudgetExceeded)
			require.Empty(t, text)
		}
		stream.Close()
	}
}
