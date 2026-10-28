package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	"food-agent/pkg/config"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
)

type runnerModel struct{ calls int }

func (m *runnerModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}
func (m *runnerModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return nil, errors.New("unexpected generate")
}
func (m *runnerModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	m.calls++
	if m.calls == 1 {
		return schema.StreamReaderFromArray([]*schema.Message{{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: "call-1", Type: "function", Function: schema.FunctionCall{Name: "find", Arguments: `{}`}}}}}), nil
	}
	return schema.StreamReaderFromArray([]*schema.Message{{Role: schema.Assistant, Content: "找到地点"}}), nil
}

type requestRunnerModel struct{ runnerModel }

func (m *requestRunnerModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}
func (m *requestRunnerModel) Stream(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	m.calls %= 2
	return m.runnerModel.Stream(ctx, messages, opts...)
}

type runnerTool struct{ runs int }

func (t *runnerTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "find", ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{})}, nil
}
func (t *runnerTool) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	t.runs++
	return `{"places":[{"id":"p1","name":"面馆"}],"places_count":1}`, nil
}

// protocolModel 把旧搜索夹具接入三阶段协议：搜索交给内嵌夹具，决策回显服务端白名单评估，最终正文交给 final（为空时复用内嵌夹具）。
type protocolModel struct {
	model.ToolCallingChatModel
	final    model.ToolCallingChatModel
	decision bool
}

func (m *protocolModel) WithTools(infos []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	bound := *m
	bound.decision = len(infos) == 1 && infos[0].Name == finalizeName
	return &bound, nil
}

func (m *protocolModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	if choice := model.GetCommonOptions(nil, opts...).ToolChoice; choice != nil && *choice == schema.ToolChoiceForbidden {
		if m.final != nil {
			return m.final.Stream(ctx, input, opts...)
		}
		return m.ToolCallingChatModel.Stream(ctx, input, opts...)
	}
	if !m.decision {
		return m.ToolCallingChatModel.Stream(ctx, input, opts...)
	}
	if len(input) == 0 {
		return nil, errors.New("missing decision input")
	}
	arguments, err := echoedDecision(input[len(input)-1].Content)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: "decision", Type: "function", Function: schema.FunctionCall{Name: finalizeName, Arguments: arguments}}}}}), nil
}

// echoedDecision 从决策阶段白名单输入中取出服务端评估结果，作为模型提交的 finalize 参数。
func echoedDecision(content string) (string, error) {
	var payload struct {
		Decision json.RawMessage `json:"decision"`
	}
	if json.Unmarshal([]byte(content), &payload) != nil || len(payload.Decision) == 0 {
		return "", errors.New("missing decision input")
	}
	return string(payload.Decision), nil
}

type baseOnlyModel struct{}

func (baseOnlyModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return nil, nil
}
func (baseOnlyModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, nil
}

type liveStreamRunnerModel struct {
	calls    int
	started  chan struct{}
	canceled chan struct{}
	finished chan struct{}
}

func (m *liveStreamRunnerModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}
func (m *liveStreamRunnerModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return nil, errors.New("unexpected generate")
}
func (m *liveStreamRunnerModel) Stream(ctx context.Context, _ []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	m.calls++
	if m.calls == 1 {
		return schema.StreamReaderFromArray([]*schema.Message{{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: "call-1", Type: "function", Function: schema.FunctionCall{Name: "find", Arguments: `{}`}}}}}), nil
	}
	reader, writer := schema.Pipe[*schema.Message](1)
	writer.Send(schema.AssistantMessage("开始处理", nil), nil)
	close(m.started)
	go func() {
		defer close(m.finished)
		<-ctx.Done()
		close(m.canceled)
		writer.Send(nil, ctx.Err())
		writer.Close()
	}()
	return reader, nil
}

func TestNewRunner_RejectsModelWithoutToolCalling(t *testing.T) {
	_, err := NewRunner("prompt", config.AgentConfig{MaxSteps: 1, MaxModelCalls: 1, MaxToolCalls: 1, TimeoutSeconds: 1}, baseOnlyModel{}, nil)
	require.Error(t, err)
}

func TestCallBudget_ExhaustionAndCancellation(t *testing.T) {
	// 步数充足，第二次失败只能来自模型预算。
	budget := &callBudget{models: 1, steps: 2}
	_, err := budget.takeModel(t.Context())
	require.NoError(t, err)
	_, err = budget.takeModel(t.Context())
	require.ErrorIs(t, err, ErrBudgetExceeded)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	// 生产路径在 InvokableRun 前调用 takeTool：取消与超额都必须在执行工具前拒绝。
	tools := &callBudget{tools: 1}
	_, err = tools.takeTool("find", canceled)
	require.ErrorIs(t, err, context.Canceled)
	_, err = tools.takeTool("find", t.Context())
	require.NoError(t, err)
	_, err = tools.takeTool("find", t.Context())
	require.ErrorIs(t, err, ErrBudgetExceeded)
}

func TestCallBudget_AccruesToolAllowancePerModelRound(t *testing.T) {
	budget := newCallBudget(config.AgentConfig{MaxModelCalls: 2, MaxToolCalls: 3, MaxSteps: 4})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := budget.takeModel(ctx)
	require.ErrorIs(t, err, context.Canceled)

	_, err = budget.takeSearchModel(t.Context())
	require.NoError(t, err)
	accepted, rejected := runConcurrentToolCalls(t, budget, 4)
	require.Equal(t, 3, accepted)
	require.Equal(t, 1, rejected)

	_, err = budget.takeSearchModel(t.Context())
	require.NoError(t, err)
	accepted, rejected = runConcurrentToolCalls(t, budget, 3)
	require.Equal(t, 3, accepted)
	require.Zero(t, rejected)

	_, err = budget.takeModel(t.Context())
	require.ErrorIs(t, err, ErrBudgetExceeded)
	_, err = budget.takeTool("find", t.Context())
	require.ErrorIs(t, err, ErrBudgetExceeded)

	toolRounds := make(map[int]bool)
	for _, call := range budget.trace.calls {
		if call.Kind == "tool" {
			toolRounds[call.Round] = true
		}
	}
	require.True(t, toolRounds[1])
	require.True(t, toolRounds[2])
}

func TestEinoRunner_IsolatesBudget_whenStreamCalledTwice(t *testing.T) {
	search := &finalizationTool{}
	// 3 模型/6 步 = 1 轮搜索 + 决策 + 最终正文；每次 Stream 都必须拿到完整的独立预算。
	settings := config.AgentConfig{AllowedTools: []string{"find"}, MaxSteps: 6, MaxModelCalls: 3, MaxToolCalls: 1, TimeoutSeconds: 2}
	chat := &requestRunnerModel{}
	runner, err := NewRunner("system", settings, &protocolModel{ToolCallingChatModel: chat}, map[string]tool.InvokableTool{"find": search})
	require.NoError(t, err)
	for round := range 2 {
		stream, err := runner.Stream(stagesContext(t.Context()), []*schema.Message{schema.UserMessage("附近找面馆")})
		require.NoError(t, err)
		defer stream.Close()
		var text string
		for err == nil {
			var message *schema.Message
			message, err = stream.Recv()
			if err == nil {
				text += message.Content
			}
		}
		require.ErrorIs(t, err, io.EOF)
		require.Equal(t, "找到地点", text)
		require.Len(t, stream.Places(), 1)
		require.Equal(t, "p1", stream.Places()[0].ID)
		require.EqualValues(t, round+1, search.count.Load())
	}
	require.EqualValues(t, 2, search.count.Load())
}

func runConcurrentToolCalls(t *testing.T, budget *callBudget, count int) (int, int) {
	results := make(chan error, count)
	ready, start := make(chan struct{}), make(chan struct{})
	for range count {
		go func() {
			ready <- struct{}{}
			<-start
			_, err := budget.takeTool("find", t.Context())
			results <- err
		}()
	}
	for range count {
		<-ready
	}
	close(start)
	var accepted, rejected int
	for range count {
		err := <-results
		if errors.Is(err, ErrBudgetExceeded) {
			rejected++
		} else {
			require.NoError(t, err)
			accepted++
		}
	}
	return accepted, rejected
}

func TestEinoRunner_StopsBeforeModelBudgetIsExceeded(t *testing.T) {
	settings := config.AgentConfig{AllowedTools: []string{"find"}, MaxSteps: 8, MaxModelCalls: 1, MaxToolCalls: 1, TimeoutSeconds: 2}
	runner, err := NewRunner("system", settings, &runnerModel{}, map[string]tool.InvokableTool{"find": &runnerTool{}})
	require.NoError(t, err)
	stream, err := runner.Stream(context.Background(), []*schema.Message{schema.UserMessage("附近找面馆")})
	if errors.Is(err, ErrBudgetExceeded) {
		return
	}
	require.NoError(t, err)
	defer stream.Close()
	for {
		_, recvErr := stream.Recv()
		if errors.Is(recvErr, ErrBudgetExceeded) {
			break
		}
		require.NoError(t, recvErr)
	}
}

func TestEinoRunner_PropagatesCanceledContext(t *testing.T) {
	settings := config.AgentConfig{MaxSteps: 2, MaxModelCalls: 1, MaxToolCalls: 1, TimeoutSeconds: 2}
	runner, err := NewRunner("system", settings, &runnerModel{}, nil)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stream, err := runner.Stream(ctx, []*schema.Message{schema.UserMessage("hello")})
	if stream != nil {
		stream.Close()
	}
	require.ErrorIs(t, err, context.Canceled)
}

func TestEinoRunner_StreamTimeoutReturnsDeadlineExceeded(t *testing.T) {
	model := &liveStreamRunnerModel{
		started:  make(chan struct{}),
		canceled: make(chan struct{}),
		finished: make(chan struct{}),
	}
	search := &runnerTool{}
	// 3 模型/6 步：搜索一轮、决策后进入最终正文流，再由超时打断。
	runner, err := NewRunner("system", config.AgentConfig{AllowedTools: []string{"find"}, MaxSteps: 6, MaxModelCalls: 3, MaxToolCalls: 1, TimeoutSeconds: 1}, &protocolModel{ToolCallingChatModel: model}, map[string]tool.InvokableTool{"find": search})
	require.NoError(t, err)
	stream, err := runner.Stream(stagesContext(context.Background()), []*schema.Message{schema.UserMessage("hello")})
	require.NoError(t, err)
	defer stream.Close()
	message, err := stream.Recv()
	require.NoError(t, err)
	require.Equal(t, "开始处理", message.Content)
	require.Equal(t, 1, search.runs)
	_, err = stream.Recv()
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, 2, model.calls)
	select {
	case <-model.finished:
	case <-time.After(time.Second):
		t.Fatal("model stream goroutine did not finish")
	}
}

func TestRunStream_CloseCancelsAndClosesUnderlyingStream(t *testing.T) {
	model := &liveStreamRunnerModel{
		started:  make(chan struct{}),
		canceled: make(chan struct{}),
		finished: make(chan struct{}),
	}
	search := &runnerTool{}
	// 3 模型/8 步：一轮搜索 + 决策后才进入可被 Close 打断的最终正文流。
	runner, err := NewRunner("", config.AgentConfig{AllowedTools: []string{"find"}, MaxSteps: 8, MaxModelCalls: 3, MaxToolCalls: 1, TimeoutSeconds: 3}, &protocolModel{ToolCallingChatModel: model}, map[string]tool.InvokableTool{"find": search})
	require.NoError(t, err)
	stream, err := runner.Stream(stagesContext(t.Context()), []*schema.Message{schema.UserMessage("hello")})
	require.NoError(t, err)
	defer stream.Close()
	message, err := stream.Recv()
	require.NoError(t, err)
	require.Equal(t, "开始处理", message.Content)
	require.Equal(t, 1, search.runs)
	stream.Close()
	stream.Close()

	select {
	case <-model.canceled:
	case <-time.After(time.Second):
		t.Fatal("model context was not canceled")
	}
	_, err = stream.Recv()
	require.Error(t, err)
	select {
	case <-model.finished:
	case <-time.After(time.Second):
		t.Fatal("model stream goroutine did not finish")
	}
}
