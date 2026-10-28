package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"testing"

	"food-agent/pkg/config"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
)

type finalizationModel struct {
	calls            int
	finalToolResults int
	finalChunks      []*schema.Message
	failure          error
	hostile          bool
	release          <-chan struct{}
	tools            []*schema.ToolInfo
}

type finalizationTool struct {
	runnerTool
	count atomic.Int32
}

func (t *finalizationTool) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	t.count.Add(1)
	return `{"places":[{"id":"p1","name":"面馆","location":"0,0"}],"location":"0,0","places_count":1}`, nil
}

func (m *finalizationModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	m.tools = tools
	return m, nil
}

func (m *finalizationModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return nil, nil
}

func (m *finalizationModel) Stream(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	m.calls++
	options := model.GetCommonOptions(nil, opts...)
	if options.ToolChoice != nil && *options.ToolChoice == schema.ToolChoiceForbidden && !m.hostile {
		if m.failure != nil {
			return nil, m.failure
		}
		for _, message := range messages {
			if message.Role == schema.Tool {
				m.finalToolResults++
			}
		}
		if m.release == nil {
			return schema.StreamReaderFromArray(m.finalChunks), nil
		}
		reader, writer := schema.Pipe[*schema.Message](0)
		go func() {
			defer writer.Close()
			writer.Send(schema.AssistantMessage("首段", nil), nil)
			select {
			case <-m.release:
				writer.Send(schema.AssistantMessage("尾段", nil), nil)
			case <-ctx.Done():
				writer.Send(nil, ctx.Err())
			}
		}()
		return reader, nil
	}
	if len(m.tools) == 1 && m.tools[0].Name == finalizeName {
		var input struct {
			Decision RecommendationDecision `json:"decision"`
		}
		if len(messages) == 0 || json.Unmarshal([]byte(messages[len(messages)-1].Content), &input) != nil {
			return nil, errors.New("missing decision input")
		}
		arguments, err := json.Marshal(input.Decision)
		if err != nil {
			return nil, err
		}
		return schema.StreamReaderFromArray([]*schema.Message{{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: "decision", Type: "function", Function: schema.FunctionCall{Name: finalizeName, Arguments: string(arguments)}}}}}), nil
	}
	return schema.StreamReaderFromArray([]*schema.Message{{Role: schema.Assistant, ToolCalls: []schema.ToolCall{
		{ID: "a", Type: "function", Function: schema.FunctionCall{Name: "find", Arguments: `{}`}},
		{ID: "b", Type: "function", Function: schema.FunctionCall{Name: "find", Arguments: `{}`}},
	}}}), nil
}

func TestRunner_FinalizesWithinModelBudgetAfterThreeToolRounds(t *testing.T) {
	search := &finalizationTool{}
	chat := &finalizationModel{finalChunks: []*schema.Message{schema.AssistantMessage("整理结果", nil)}}
	// 5 模型/10 步 = 三轮搜索 + 内部决策 + 最终正文。
	runner, err := NewRunner("", config.AgentConfig{AllowedTools: []string{"find"}, MaxSteps: 10, MaxModelCalls: 5, MaxToolCalls: 3, TimeoutSeconds: 5}, chat, map[string]tool.InvokableTool{"find": search})
	require.NoError(t, err)

	stream, err := runner.Stream(stagesContext(t.Context()), []*schema.Message{schema.UserMessage("find places")})
	require.NoError(t, err)
	defer stream.Close()
	var content string
	for {
		message, recvErr := stream.Recv()
		if recvErr == io.EOF {
			break
		}
		require.NoError(t, recvErr)
		content += message.Content
	}

	require.Equal(t, "整理结果", content)
	require.Equal(t, 5, chat.calls)
	// 最终正文只接收白名单决策输入，不得再看到原始工具消息。
	require.Zero(t, chat.finalToolResults)
	require.EqualValues(t, 6, search.count.Load())
	// 六次工具结果是同一候选，经策略去重后只交付一个已验证地点。
	require.Len(t, stream.Places(), 1)
	require.Equal(t, "p1", stream.Places()[0].ID)
}

func TestRunner_FinalizationRespectsSmallBounds(t *testing.T) {
	// 低于“决策 + 最终正文”最低额度（2 模型/4 步）时必须零调用拒绝；其余按 S 轮搜索收敛。
	for _, limits := range []struct {
		steps, models, wantModels int
		wantErr                   bool
	}{{1, 4, 0, true}, {2, 4, 0, true}, {3, 4, 0, true}, {8, 1, 0, true}, {4, 2, 2, false}, {8, 2, 2, false}, {6, 3, 3, false}, {8, 4, 4, false}} {
		t.Run(fmt.Sprintf("steps%d_models%d", limits.steps, limits.models), func(t *testing.T) {
			search := &finalizationTool{}
			chat := &finalizationModel{finalChunks: []*schema.Message{schema.AssistantMessage("结果", nil)}}
			runner, err := NewRunner("", config.AgentConfig{AllowedTools: []string{"find"}, MaxSteps: limits.steps, MaxModelCalls: limits.models, MaxToolCalls: 3, TimeoutSeconds: 3}, chat, map[string]tool.InvokableTool{"find": search})
			require.NoError(t, err)

			err = drainDiagnosticRunner(stagesContext(t.Context()), runner, []*schema.Message{schema.UserMessage("测试")})

			if limits.wantErr {
				require.ErrorIs(t, err, ErrBudgetExceeded)
				require.Zero(t, chat.calls)
				require.Zero(t, search.count.Load())
				return
			}
			require.NoError(t, err)
			require.Equal(t, limits.wantModels, chat.calls)
			require.EqualValues(t, 2*(limits.wantModels-2), search.count.Load())
		})
	}
}

func TestRunner_FinalizationRejectsToolCallsAndEmptyAnswers(t *testing.T) {
	providerFailure := errors.New("本地模拟提供方失败")
	for _, scenario := range []struct {
		name    string
		hostile bool
		chunks  []*schema.Message
		failure error
	}{
		{name: "继续调用工具", hostile: true},
		{name: "空回答"},
		{name: "提供方失败", failure: providerFailure},
		{name: "空白回答", chunks: []*schema.Message{schema.AssistantMessage(" \n", nil)}},
		{name: "正文后调用工具", chunks: []*schema.Message{schema.AssistantMessage("未完成", nil), {Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: "late", Function: schema.FunctionCall{Name: "find", Arguments: `{}`}}}}}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			search := &finalizationTool{}
			chat := &finalizationModel{hostile: scenario.hostile, finalChunks: scenario.chunks, failure: scenario.failure}
			// 3 模型/6 步：一轮搜索 + 已完成决策，失败只能发生在最终正文阶段。
			runner, err := NewRunner("", config.AgentConfig{AllowedTools: []string{"find"}, MaxSteps: 6, MaxModelCalls: 3, MaxToolCalls: 3, TimeoutSeconds: 3}, chat, map[string]tool.InvokableTool{"find": search})
			require.NoError(t, err)

			err = drainDiagnosticRunner(stagesContext(t.Context()), runner, []*schema.Message{schema.UserMessage("测试")})

			require.Error(t, err)
			require.False(t, errors.Is(err, io.EOF))
			if scenario.failure != nil {
				require.ErrorIs(t, err, providerFailure)
			}
			require.Equal(t, 3, chat.calls)
			require.EqualValues(t, 2, search.count.Load())
		})
	}
}

func TestRunner_FinalizationStreamsBeforeCompletion(t *testing.T) {
	release := make(chan struct{})
	chat := &finalizationModel{release: release}
	// 2 模型/4 步是进入最终正文的最低额度：决策后直接流式正文。
	runner, err := NewRunner("", config.AgentConfig{AllowedTools: []string{"find"}, MaxSteps: 4, MaxModelCalls: 2, MaxToolCalls: 1, TimeoutSeconds: 3}, chat, map[string]tool.InvokableTool{"find": &finalizationTool{}})
	require.NoError(t, err)

	stream, err := runner.Stream(stagesContext(t.Context()), []*schema.Message{schema.UserMessage("测试")})
	require.NoError(t, err)
	defer stream.Close()
	first, err := stream.Recv()
	require.NoError(t, err)
	require.Equal(t, "首段", first.Content)
	close(release)
	last, err := stream.Recv()
	require.NoError(t, err)
	require.Equal(t, "尾段", last.Content)
	_, err = stream.Recv()
	require.ErrorIs(t, err, io.EOF)
	require.Equal(t, 2, chat.calls)
}
