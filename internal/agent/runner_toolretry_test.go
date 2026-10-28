package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
	"testing"

	"food-agent/pkg/config"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// fanoutScriptModel 按脚本逐轮返回指定数量的工具调用，并记录每轮搜索模型收到的输入；脚本用完后返回纯文本结束检索。
type fanoutScriptModel struct {
	model.ToolCallingChatModel
	mu      sync.Mutex
	fanouts []int
	inputs  [][]*schema.Message
}

func (m *fanoutScriptModel) Stream(_ context.Context, input []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	round := len(m.inputs)
	m.inputs = append(m.inputs, slices.Clone(input))
	if round >= len(m.fanouts) {
		return schema.StreamReaderFromArray([]*schema.Message{schema.AssistantMessage("检索结束", nil)}), nil
	}
	calls := make([]schema.ToolCall, m.fanouts[round])
	for index := range calls {
		calls[index] = schema.ToolCall{ID: fmt.Sprintf("r%d-%d", round, index), Type: "function", Function: schema.FunctionCall{Name: "find", Arguments: `{"keywords":"private-keyword"}`}}
	}
	return schema.StreamReaderFromArray([]*schema.Message{{Role: schema.Assistant, ToolCalls: calls}}), nil
}

// runFanoutScript 以脚本搜索模型跑完整三阶段（决策回显服务端评估），返回搜索输入记录、工具计数、公开决策与流错误（EOF 视为成功）。
func runFanoutScript(t *testing.T, settings config.AgentConfig, fanouts ...int) (*fanoutScriptModel, *stagesTool, PublicDecision, error) {
	t.Helper()
	search := &fanoutScriptModel{fanouts: fanouts}
	finder := &stagesTool{}
	settings.AllowedTools = []string{"find"}
	settings.TimeoutSeconds = 5
	chat := &protocolModel{ToolCallingChatModel: search, final: &nullableRunnerModel{chunks: []*schema.Message{schema.AssistantMessage("正文", nil)}}}
	runner, err := NewRunner("", settings, chat, map[string]tool.InvokableTool{"find": finder})
	require.NoError(t, err)
	stream, err := runner.Stream(stagesContext(t.Context()), []*schema.Message{schema.UserMessage("附近吃点")})
	if err != nil {
		return search, finder, PublicDecision{}, err
	}
	defer stream.Close()
	for err == nil {
		_, err = stream.Recv()
	}
	if errors.Is(err, io.EOF) {
		err = nil
	}
	decision, _ := stream.Decision()
	return search, finder, decision, err
}

func TestRunnerToolRetry_FeedsBackOverflow_thenExecutesNextBatch(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	defer zap.ReplaceGlobals(zap.New(core))()

	search, finder, decision, err := runFanoutScript(t, config.AgentConfig{MaxModelCalls: 5, MaxSteps: 10, MaxToolCalls: 6}, 7, 2)

	require.NoError(t, err)
	require.Equal(t, DecisionReady, decision.Status)
	require.EqualValues(t, 2, finder.count.Load(), "超量批次不执行任何工具，只执行第二轮的 2 个")
	require.Len(t, search.inputs, 3)
	second := search.inputs[1]
	require.Len(t, second, 1+1+7, "第二轮输入 = 用户消息 + 被拒 assistant + 7 条错误 ToolMessage")
	require.Len(t, second[1].ToolCalls, 7)
	for index, message := range second[2:] {
		require.Equal(t, schema.Tool, message.Role)
		require.Equal(t, fmt.Sprintf("r0-%d", index), message.ToolCallID, "每个 tool_call_id 都必须有对应结果")
		require.Equal(t, "本轮请求了7个工具调用，超过每轮上限6个，未执行。请合并关键词，最多6个后重试。", message.Content)
	}

	rejected := logs.FilterMessage("agent.tool_batch_rejected").All()
	require.Len(t, rejected, 1)
	fields := rejected[0].ContextMap()
	require.EqualValues(t, 1, fields["round"])
	require.EqualValues(t, 7, fields["requested"])
	require.EqualValues(t, 6, fields["limit"])
	require.Equal(t, []any{"find", "find", "find", "find", "find", "find", "find"}, fields["tools"])
	runs := diagnosticRuns(t, logs)
	require.Len(t, runs, 1)
	require.Equal(t, 2, runs[0].ToolAttempted, "被拒批次不计入 tool_attempted")
	encoded, marshalErr := json.Marshal(logs.All())
	require.NoError(t, marshalErr)
	require.NotContains(t, string(encoded), "private-keyword", "日志不得记录工具参数")
}

func TestRunnerToolRetry_ExecutesBatchWithinLimit(t *testing.T) {
	_, finder, decision, err := runFanoutScript(t, config.AgentConfig{MaxModelCalls: 5, MaxSteps: 10, MaxToolCalls: 6}, 4)

	require.NoError(t, err)
	require.Equal(t, DecisionReady, decision.Status)
	require.EqualValues(t, 4, finder.count.Load())
}

func TestRunnerToolRetry_ConfiguredLimitIsNotCappedAtThree(t *testing.T) {
	require.Equal(t, 10, newCallBudget(config.AgentConfig{MaxModelCalls: 5, MaxSteps: 10, MaxToolCalls: 10}).maxTools)

	_, finder, _, err := runFanoutScript(t, config.AgentConfig{MaxModelCalls: 5, MaxSteps: 10, MaxToolCalls: 10}, 10)

	require.NoError(t, err)
	require.EqualValues(t, 10, finder.count.Load())
}

func TestRunnerToolRetry_RepeatedOverflowEndsInExistingFinalization(t *testing.T) {
	for _, fixture := range []struct {
		name          string
		models, steps int
		rounds        int
	}{{"三轮", 5, 10, 3}, {"一轮", 3, 6, 1}} {
		t.Run(fixture.name, func(t *testing.T) {
			search, finder, decision, err := runFanoutScript(t, config.AgentConfig{MaxModelCalls: fixture.models, MaxSteps: fixture.steps, MaxToolCalls: 6}, 7, 7, 7)

			require.NoError(t, err, "搜索轮次用尽后照常进入决策与正文")
			require.Zero(t, finder.count.Load())
			require.Len(t, search.inputs, fixture.rounds, "不额外新增搜索轮次")
			require.Equal(t, DecisionNeedsChoice, decision.Status)
			require.Equal(t, []ReasonCode{ReasonNoCandidates}, decision.ReasonCodes)
		})
	}
}

// 只对“超量”反馈重试；未知工具名、空/重复 ID 仍整体拒绝（即使同时超量）。
func TestRunnerToolRetry_InvalidBatchStillRejected(t *testing.T) {
	tools := map[string]tool.InvokableTool{"find": &stagesTool{}}
	find := func(id string) schema.ToolCall {
		return schema.ToolCall{ID: id, Function: schema.FunctionCall{Name: "find"}}
	}
	overflow := []schema.ToolCall{find("a"), find("b"), find("c"), find("d"), find("e"), find("f"), {ID: "g", Function: schema.FunctionCall{Name: "other"}}}
	for name, calls := range map[string][]schema.ToolCall{
		"未知工具":     {{ID: "a", Function: schema.FunctionCall{Name: "other"}}},
		"空ID":      {find("")},
		"重复ID":     {find("a"), find("a")},
		"超量且含未知工具": overflow,
	} {
		require.ErrorIs(t, validateSearchBatch(calls, tools), ErrInvalidToolInput, name)
	}
	require.NoError(t, validateSearchBatch(overflow[:6], tools))
}

func TestAssessFood_SichuanAndChongqingAreOneRegion(t *testing.T) {
	const hotpot = "餐饮服务;中餐厅;火锅店"
	for _, fixture := range []struct {
		requirement, kind, tag string
		want                   MatchState
	}{
		{"四川火锅", hotpot, "重庆火锅", MatchSatisfied},
		{"四川火锅", hotpot, "渝味火锅", MatchSatisfied},
		{"重庆火锅", hotpot, "川味火锅", MatchSatisfied},
		{"重庆火锅", hotpot, "四川火锅", MatchSatisfied},
		{"重庆火锅", "餐饮服务;中餐厅;川味火锅", "", MatchSatisfied},
		{"重庆火锅", hotpot, "贵州火锅", MatchUnsatisfied},
		{"四川火锅", hotpot, "贵州火锅", MatchUnsatisfied},
		{"重庆火锅", "餐饮服务;中餐厅;四川菜(川菜)", "冒菜", MatchUnsatisfied},
		{"四川火锅", hotpot, "湘味火锅", MatchUnsatisfied},
	} {
		requirement := fixture.requirement
		got := assessFood(&requirement, Place{Type: fixture.kind, Tag: fixture.tag})
		require.Equal(t, fixture.want, got.State, "%s | %s | %s", fixture.requirement, fixture.kind, fixture.tag)
	}
}
