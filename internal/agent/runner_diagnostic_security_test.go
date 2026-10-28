package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"food-agent/pkg/config"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestRunnerDiagnostic_ClassifiesFailuresWithoutPayloads_whenProviderOrToolFails(t *testing.T) {
	const privateValue = "private-payload-credential-address"
	for _, scenario := range []struct {
		name       string
		arguments  string
		modelError error
		toolError  error
		cancelTool bool
		wantStatus string
		wantTools  int
	}{
		{name: "模型失败", modelError: errors.New(privateValue), wantStatus: "failed"},
		{name: "无效JSON", arguments: privateValue, wantStatus: "invalid_json", wantTools: 1},
		{name: "参数类型错误", arguments: `{"radius":"` + privateValue + `"}`, wantStatus: "invalid_json", wantTools: 1},
		{name: "工具失败", arguments: `{}`, toolError: errors.New(privateValue), wantStatus: "failed", wantTools: 1},
		{name: "输入拒绝", arguments: `{}`, toolError: ErrInvalidToolInput, wantStatus: "invalid_input", wantTools: 1},
		{name: "请求取消", arguments: `{}`, cancelTool: true, wantStatus: "canceled", wantTools: 1},
		{name: "工具超时", arguments: `{}`, toolError: context.DeadlineExceeded, wantStatus: "deadline_exceeded", wantTools: 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			core, logs := observer.New(zap.InfoLevel)
			restore := zap.ReplaceGlobals(zap.New(core))
			defer restore()
			ctx, cancel := context.WithCancel(stagesContext(t.Context()))
			defer cancel()
			configured := newFoodTool(ToolNamePOI2Around, "", aroundToolParams(), func(context.Context, aroundSearchInput) (SearchResult, error) {
				if scenario.cancelTool {
					cancel()
					return SearchResult{}, ctx.Err()
				}
				return SearchResult{}, scenario.toolError
			})
			runner, err := NewRunner(privateValue, diagnosticSettings(3), diagnosticProtocolModel(&diagnosticModel{fanout: 1, arguments: scenario.arguments, failure: scenario.modelError}), map[string]tool.InvokableTool{ToolNamePOI2Around: configured})
			require.NoError(t, err)

			err = drainDiagnosticRunner(ctx, runner, []*schema.Message{schema.UserMessage(privateValue)})

			require.Error(t, err)
			runs := diagnosticRuns(t, logs)
			require.Len(t, runs, 1)
			require.Equal(t, scenario.wantStatus, runs[0].Status)
			require.Equal(t, scenario.wantTools, runs[0].ToolAttempted)
			require.Equal(t, scenario.wantTools, runs[0].ToolCompleted)
			require.Equal(t, scenario.wantStatus, runs[0].Calls[len(runs[0].Calls)-1].Status)
			encoded, marshalErr := json.Marshal(logs.All())
			require.NoError(t, marshalErr)
			require.NotContains(t, string(encoded), privateValue)
			require.NotContains(t, string(encoded), "private-call")
		})
	}
}

func TestRunnerDiagnostic_OmitsUnvalidatedToolName_whenConfiguredToolHasAnotherName(t *testing.T) {
	const untrustedName = "private-unvalidated-tool-name"
	core, logs := observer.New(zap.InfoLevel)
	restore := zap.ReplaceGlobals(zap.New(core))
	defer restore()
	settings := diagnosticSettings(1)
	settings.AllowedTools = []string{untrustedName}
	// 3 模型/8 步 = 一轮搜索（真正执行该工具）+ 决策 + 最终正文。
	settings.MaxModelCalls = 3
	chatModel := diagnosticProtocolModel(&nullableRunnerModel{chunks: []*schema.Message{{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: "private-call", Function: schema.FunctionCall{Name: "find", Arguments: `{}`}}}}}})
	search := &runnerTool{}
	runner, err := NewRunner("", settings, chatModel, map[string]tool.InvokableTool{untrustedName: search})
	require.NoError(t, err)

	err = drainDiagnosticRunner(stagesContext(t.Context()), runner, []*schema.Message{schema.UserMessage("测试")})

	require.NoError(t, err)
	runs := diagnosticRuns(t, logs)
	require.Len(t, runs, 1)
	require.Equal(t, 3, runs[0].ModelAttempted)
	require.Equal(t, 3, runs[0].ModelCompleted)
	require.Equal(t, 1, runs[0].ToolAttempted)
	require.Equal(t, 1, runs[0].ToolCompleted)
	require.Equal(t, 1, search.runs)
	var toolRecords int
	for _, call := range runs[0].Calls {
		if call.Kind == "decision" {
			// 内部决策工具名由服务端常量写入，不来自模型或配置。
			require.Equal(t, finalizeName, call.Tool)
			continue
		}
		require.Empty(t, call.Tool)
		if call.Kind == "tool" {
			toolRecords++
			require.Equal(t, "ok", call.Status)
		}
	}
	require.Positive(t, toolRecords)
	encoded, marshalErr := json.Marshal(logs.All())
	require.NoError(t, marshalErr)
	require.NotContains(t, string(encoded), untrustedName)
}

func TestRunnerDiagnostic_FeedsBackWholeOverLimitBatch_whenBudgetAllowsFour(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	restore := zap.ReplaceGlobals(zap.New(core))
	defer restore()
	runner := diagnosticRunner(t, 5, 4)

	err := drainDiagnosticRunner(stagesContext(t.Context()), runner, []*schema.Message{schema.UserMessage("测试")})

	// 每轮上限即 max_tool_calls=4；5 个调用的批次整体不执行（零外部调用），错误反馈给模型后照常收尾。
	require.NoError(t, err)
	runs := diagnosticRuns(t, logs)
	require.Len(t, runs, 1)
	require.Equal(t, "ok", runs[0].Status)
	require.Zero(t, runs[0].ToolAttempted)
	require.Zero(t, runs[0].ToolCompleted)
	require.Len(t, logs.FilterMessage("agent.tool_batch_rejected").All(), 1)
}

func TestRunnerDiagnostic_CapsRecordsAt32_whenConfiguredBudgetsAreLarger(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	restore := zap.ReplaceGlobals(zap.New(core))
	defer restore()
	runner := diagnosticRunner(t, 40, 40)

	err := drainDiagnosticRunner(stagesContext(t.Context()), runner, []*schema.Message{schema.UserMessage("测试")})

	// 每轮上限取配置值 40：40 个调用全部执行，诊断记录仍封顶 32 条。
	require.NoError(t, err)
	runs := diagnosticRuns(t, logs)
	require.Len(t, runs, 1)
	require.Equal(t, 40, runs[0].ToolAttempted)
	require.Len(t, runs[0].Calls, 32)

	// 模型记录同样受 32 条硬上限约束（直接在诊断摘要层验证）。
	trace := newDiagnosticTrace(config.AgentConfig{MaxModelCalls: 40, MaxToolCalls: 40, MaxSteps: 40, TimeoutSeconds: 3})
	for number := 1; number <= 40; number++ {
		trace.record("model", "", number, "started")
		trace.complete("model", number, "ok")
	}
	trace.finish(nil)
	runs = diagnosticRuns(t, logs)
	require.Len(t, runs, 2)
	require.Len(t, runs[1].Calls, 32)
	require.Equal(t, 40, runs[1].ModelAttempted)
	require.Equal(t, 40, runs[1].ModelCompleted)
}
