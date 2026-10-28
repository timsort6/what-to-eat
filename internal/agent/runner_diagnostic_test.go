package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"testing"

	"food-agent/pkg/config"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type diagnosticModel struct {
	model.ToolCallingChatModel
	fanout    int
	arguments string
	failure   error
}

func (m *diagnosticModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}

func (m *diagnosticModel) Stream(_ context.Context, messages []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	if m.failure != nil {
		return nil, m.failure
	}
	if messages[len(messages)-1].Role == schema.Tool {
		return schema.StreamReaderFromArray([]*schema.Message{schema.AssistantMessage("诊断完成", nil)}), nil
	}
	count := m.fanout
	if messages[len(messages)-1].Content == "single" {
		count = 1
	}
	calls := make([]schema.ToolCall, count)
	for index := range calls {
		calls[index] = schema.ToolCall{ID: fmt.Sprintf("private-call-%d", index), Type: "function", Function: schema.FunctionCall{Name: "amap_poi2_around", Arguments: m.arguments}}
	}
	return schema.StreamReaderFromArray([]*schema.Message{{Role: schema.Assistant, ToolCalls: calls}}), nil
}

type recordedCall struct {
	Kind   string `json:"kind"`
	Number int    `json:"number"`
	Round  int    `json:"round"`
	Tool   string `json:"tool"`
	Status string `json:"status"`
}

type recordedRun struct {
	Status         string         `json:"status"`
	ModelAttempted int            `json:"model_attempted"`
	ModelCompleted int            `json:"model_completed"`
	ToolAttempted  int            `json:"tool_attempted"`
	ToolCompleted  int            `json:"tool_completed"`
	MaxModelCalls  int            `json:"max_model_calls"`
	MaxToolCalls   int            `json:"max_tool_calls"`
	MaxSteps       int            `json:"max_steps"`
	TimeoutSeconds int            `json:"timeout_seconds"`
	Calls          []recordedCall `json:"calls"`
}

func diagnosticSettings(maxTools int) config.AgentConfig {
	return config.AgentConfig{AllowedTools: []string{ToolNamePOI2Around}, MaxSteps: max(8, maxTools), MaxModelCalls: 4, MaxToolCalls: maxTools, TimeoutSeconds: 3}
}

func diagnosticRunner(t *testing.T, fanout, maxTools int) *Runner {
	t.Helper()
	settings := diagnosticSettings(maxTools)
	tools, err := NewFoodTools(FoodToolDeps{MCP: &fakeFoodMCP{response: `{"status":"1","pois":[{"name":"private-place","address":"private-address"}]}`}, Scope: testScope(), AllowedTools: settings.AllowedTools})
	require.NoError(t, err)
	runner, err := NewRunner("private-prompt", settings, diagnosticProtocolModel(&diagnosticModel{fanout: fanout, arguments: `{"keywords":"private-keyword"}`}), tools)
	require.NoError(t, err)
	return runner
}

// diagnosticProtocolModel 让诊断搜索夹具经过内部决策后进入固定文本的最终正文阶段。
func diagnosticProtocolModel(search model.ToolCallingChatModel) *protocolModel {
	return &protocolModel{ToolCallingChatModel: search, final: &nullableRunnerModel{chunks: []*schema.Message{schema.AssistantMessage("诊断完成", nil)}}}
}

func drainDiagnosticRunner(ctx context.Context, runner *Runner, history []*schema.Message) error {
	stream, err := runner.Stream(ctx, history)
	if err != nil {
		return err
	}
	defer stream.Close()
	for {
		_, err = stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func diagnosticRuns(t *testing.T, logs *observer.ObservedLogs) []recordedRun {
	t.Helper()
	var runs []recordedRun
	for _, entry := range logs.FilterMessage("agent.run").All() {
		encoded, err := json.Marshal(entry.ContextMap())
		require.NoError(t, err)
		var run recordedRun
		require.NoError(t, json.Unmarshal(encoded, &run))
		runs = append(runs, run)
	}
	return runs
}

func TestRunnerDiagnostic_RecordsFanoutAndBudget_whenFourToolsAreRequested(t *testing.T) {
	for _, maxTools := range []int{3, 4} {
		t.Run(fmt.Sprint(maxTools), func(t *testing.T) {
			core, logs := observer.New(zap.InfoLevel)
			restore := zap.ReplaceGlobals(zap.New(core))
			defer restore()
			runner := diagnosticRunner(t, 4, maxTools)

			err := drainDiagnosticRunner(stagesContext(t.Context()), runner, []*schema.Message{schema.UserMessage("private-request")})

			runs := diagnosticRuns(t, logs)
			require.Len(t, runs, 1, "必须产出一条请求级安全诊断摘要")
			encoded, marshalErr := json.Marshal(logs.All())
			require.NoError(t, marshalErr)
			require.NotContains(t, string(encoded), "private-request")
			require.NotContains(t, string(encoded), "private-prompt")
			require.NotContains(t, string(encoded), "private-call")
			require.NotContains(t, string(encoded), "private-keyword")
			require.NotContains(t, string(encoded), "private-place")
			require.NotContains(t, string(encoded), "private-address")
			require.NotContains(t, string(encoded), testScope().Location)
			run := runs[0]
			require.Equal(t, maxTools, run.MaxToolCalls)
			require.Equal(t, 4, run.MaxModelCalls)
			require.Equal(t, 8, run.MaxSteps)
			require.Equal(t, 3, run.TimeoutSeconds)
			// 每轮上限即 MaxToolCalls：上限 3 时四个调用整体不执行、反馈后第二轮收敛；上限 4 时全部执行。两种情况都照常收尾。
			require.NoError(t, err)
			require.Equal(t, "ok", run.Status)
			require.Equal(t, 4, run.ModelAttempted, "两轮搜索 + 决策 + 正文")
			require.Equal(t, run.ModelAttempted, run.ModelCompleted)
			require.Equal(t, recordedCall{Kind: "model", Number: 1, Round: 1, Status: "ok"}, run.Calls[0])
			rejected := logs.FilterMessage("agent.tool_batch_rejected").All()
			if maxTools == 3 {
				require.Zero(t, run.ToolAttempted, "被拒批次不计入 tool_attempted")
				require.Len(t, rejected, 1)
				require.EqualValues(t, 4, rejected[0].ContextMap()["requested"])
				require.EqualValues(t, 3, rejected[0].ContextMap()["limit"])
			} else {
				require.Equal(t, 4, run.ToolAttempted)
				require.Equal(t, 4, run.ToolCompleted)
				require.Empty(t, rejected)
			}
		})
	}
}

func TestRunnerDiagnostic_IsolatesRequests_whenRunnerIsSharedConcurrently(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	restore := zap.ReplaceGlobals(zap.New(core))
	defer restore()
	runner := diagnosticRunner(t, 4, 3)
	returned := make(chan error, 2)

	for _, request := range []string{"single", "overflow"} {
		go func() {
			returned <- drainDiagnosticRunner(stagesContext(t.Context()), runner, []*schema.Message{schema.UserMessage(request)})
		}()
	}
	first, second := <-returned, <-returned

	require.NoError(t, first)
	require.NoError(t, second)
	runs := diagnosticRuns(t, logs)
	require.Len(t, runs, 2)
	// 两请求都是两轮搜索（第二轮无工具即收敛）+ 决策 + 最终正文；single 执行 1 个工具，overflow 的 4 个被整体反馈、不执行。
	attempted := []int{}
	for _, run := range runs {
		require.Equal(t, "ok", run.Status)
		require.Equal(t, 4, run.ModelAttempted)
		require.Equal(t, 4, run.ModelCompleted)
		require.Equal(t, run.ToolAttempted, run.ToolCompleted)
		attempted = append(attempted, run.ToolAttempted)
	}
	require.ElementsMatch(t, []int{0, 1}, attempted)
}

func TestRunnerDiagnostic_BoundsRecords_whenFanoutIsExcessive(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	restore := zap.ReplaceGlobals(zap.New(core))
	defer restore()
	runner := diagnosticRunner(t, 20, 3)

	err := drainDiagnosticRunner(stagesContext(t.Context()), runner, []*schema.Message{schema.UserMessage("private-request")})

	require.NoError(t, err, "超额批次反馈给模型后照常收尾")
	runs := diagnosticRuns(t, logs)
	require.Len(t, runs, 1)
	require.Zero(t, runs[0].ToolAttempted, "超额批次必须在任何外部工具调用前拒绝")
	require.Zero(t, runs[0].ToolCompleted)
	for _, call := range runs[0].Calls {
		require.NotEqual(t, "tool", call.Kind, "超额扇出不得产生逐个工具记录")
	}

	// 预算层兜底：20 次工具尝试只记录三轮额度（9 条）加首个越界编号，共 10 条工具记录。
	budget := newCallBudget(diagnosticSettings(3))
	_, err = budget.takeSearchModel(t.Context())
	require.NoError(t, err)
	for range 20 {
		_, _ = budget.takeTool(ToolNamePOI2Around, t.Context())
	}
	require.Equal(t, 20, budget.trace.toolAttempted)
	require.Len(t, budget.trace.calls, 1+10)
}
