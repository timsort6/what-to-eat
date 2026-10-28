package agent

import (
	"os"
	"testing"

	"food-agent/pkg/config"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// 提示词中的每轮工具上限由 agent.max_tool_calls 注入（与 runner_budget 同一个值），发给模型的系统提示不含占位符。
func TestRunner_InjectsMaxToolCallsIntoPrompt(t *testing.T) {
	file, err := os.ReadFile("../../configs/prompts/food-agent.md")
	require.NoError(t, err)
	require.Contains(t, string(file), "{{max_tool_calls}}", "仓库提示词须使用占位符而不是写死上限")

	for _, fixture := range []struct {
		maxTools int
		want     string
	}{{4, "每轮最多 4 个工具调用"}, {6, "每轮最多 6 个工具调用"}} {
		search := &fanoutScriptModel{}
		settings := config.AgentConfig{AllowedTools: []string{"find"}, MaxModelCalls: 5, MaxSteps: 10, MaxToolCalls: fixture.maxTools, TimeoutSeconds: 5}
		chat := &protocolModel{ToolCallingChatModel: search, final: &nullableRunnerModel{chunks: []*schema.Message{schema.AssistantMessage("正文", nil)}}}
		runner, err := NewRunner(string(file), settings, chat, map[string]tool.InvokableTool{"find": &stagesTool{}})
		require.NoError(t, err)
		require.NoError(t, drainDiagnosticRunner(stagesContext(t.Context()), runner, []*schema.Message{schema.UserMessage("附近吃点")}))

		require.NotEmpty(t, search.inputs)
		system := search.inputs[0][0]
		require.Equal(t, schema.System, system.Role)
		require.Contains(t, system.Content, fixture.want)
		require.NotContains(t, system.Content, "{{")
		require.Equal(t, fixture.maxTools, newCallBudget(settings).maxTools, "提示词与预算使用同一个值")
	}

	// 旧提示词没有占位符时原样使用，不报错。
	runner, err := NewRunner("旧提示词：每轮最多 3 个", config.AgentConfig{MaxModelCalls: 3, MaxSteps: 6, MaxToolCalls: 4, TimeoutSeconds: 5}, &stagesModel{}, nil)
	require.NoError(t, err)
	require.Equal(t, "旧提示词：每轮最多 3 个", runner.prompt)
}

// agent.run 的 calls 中工具记录须带规范工具名（配置名 amap.poi2_*），即使模型调用的是适配器名 amap_poi2_*。
func TestRunnerDiagnostic_RecordsCanonicalToolName_whenModelUsesAdapterName(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	defer zap.ReplaceGlobals(zap.New(core))()
	settings := config.AgentConfig{AllowedTools: []string{ToolNamePOI2Around, ToolNamePOI2Text}, MaxSteps: 8, MaxModelCalls: 3, MaxToolCalls: 6, TimeoutSeconds: 3}
	tools, err := NewFoodTools(FoodToolDeps{MCP: &fakeFoodMCP{response: `{"status":"1","pois":[]}`}, Scope: testScope(), AllowedTools: settings.AllowedTools})
	require.NoError(t, err)
	calls := []schema.ToolCall{
		{ID: "c1", Type: "function", Function: schema.FunctionCall{Name: "amap_poi2_around", Arguments: `{"keywords":"火锅"}`}},
		{ID: "c2", Type: "function", Function: schema.FunctionCall{Name: "amap_poi2_text", Arguments: `{"keywords":"火锅"}`}},
		{ID: "c3", Type: "function", Function: schema.FunctionCall{Name: "amap_poi2_around", Arguments: `{"keywords":"串串"}`}},
	}
	chat := diagnosticProtocolModel(&nullableRunnerModel{chunks: []*schema.Message{{Role: schema.Assistant, ToolCalls: calls}}})
	runner, err := NewRunner("", settings, chat, tools)
	require.NoError(t, err)

	require.NoError(t, drainDiagnosticRunner(stagesContext(t.Context()), runner, []*schema.Message{schema.UserMessage("测试")}))

	runs := diagnosticRuns(t, logs)
	require.Len(t, runs, 1)
	require.Equal(t, 3, runs[0].ToolAttempted)
	require.Equal(t, 3, runs[0].ToolCompleted)
	var names []string
	for _, call := range runs[0].Calls {
		if call.Kind == "tool" {
			require.Equal(t, "ok", call.Status)
			names = append(names, call.Tool)
		}
	}
	require.Equal(t, []string{ToolNamePOI2Around, ToolNamePOI2Text, ToolNamePOI2Around}, names)
}
