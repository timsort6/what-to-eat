package agent

import (
	"encoding/json"
	"io"
	"sync/atomic"
	"testing"

	"food-agent/pkg/config"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func orchestrationRunner(t *testing.T, chat model.ToolCallingChatModel, cfg config.AgentConfig) (*Runner, *stagesTool) {
	t.Helper()
	search := &stagesTool{}
	runner, err := NewRunner("private-prompt", cfg, chat, map[string]tool.InvokableTool{"find": search})
	require.NoError(t, err)
	return runner, search
}

func orchestrationConfig() config.AgentConfig {
	return config.AgentConfig{AllowedTools: []string{"find"}, MaxModelCalls: 5, MaxSteps: 10, MaxToolCalls: 3, TimeoutSeconds: 5}
}

func drainOrchestration(t *testing.T, runner *Runner) (*RunStream, error) {
	t.Helper()
	stream, err := runner.Stream(stagesContext(t.Context()), nil)
	if err != nil {
		return stream, err
	}
	defer stream.Close()
	for err == nil {
		_, err = stream.Recv()
	}
	return stream, err
}

func TestDiagnostic_InternalDecision_whenSearchQuotaIsIndependent(t *testing.T) {
	for _, fixture := range []struct{ models, steps, rounds int }{{5, 10, 3}, {4, 8, 2}} {
		chat := &stagesModel{calls: &atomic.Int32{}, searchRounds: fixture.rounds, decision: stagesDecision(t), final: []*schema.Message{schema.AssistantMessage("answer", nil)}}
		cfg := orchestrationConfig()
		cfg.MaxModelCalls = fixture.models
		cfg.MaxSteps = fixture.steps
		core, logs := observer.New(zap.InfoLevel)
		restore := zap.ReplaceGlobals(zap.New(core))
		runner, search := orchestrationRunner(t, chat, cfg)
		_, err := drainOrchestration(t, runner)
		restore()
		require.ErrorIs(t, err, io.EOF)
		require.EqualValues(t, fixture.rounds*3, search.count.Load())
		entries := logs.FilterMessage("agent.run").All()
		require.Len(t, entries, 1)
		fields := entries[0].ContextMap()
		require.EqualValues(t, 1, fields["decision_attempted"])
		require.EqualValues(t, 1, fields["decision_completed"])
		require.EqualValues(t, fixture.rounds*3, fields["tool_completed"])
		require.EqualValues(t, fixture.steps, fields["logical_steps"])
		require.EqualValues(t, 1, fields["direct_return_completed"])
		require.LessOrEqual(t, len(entries[0].Context), 16)
		encoded, marshalErr := json.Marshal(fields)
		require.NoError(t, marshalErr)
		for _, secret := range []string{"private-prompt", "rejected", "location", "arguments", "面馆"} {
			require.NotContains(t, string(encoded), secret)
		}
	}
}

func TestRunner_Orchestration_whenDecisionBatchIsInvalid(t *testing.T) {
	valid := schema.ToolCall{ID: "decision", Type: "function", Function: schema.FunctionCall{Name: finalizeName, Arguments: stagesDecision(t)}}
	missingID := valid
	missingID.ID = ""
	for name, fixture := range map[string]struct {
		batch []schema.ToolCall
		text  string
	}{
		"duplicate": {batch: []schema.ToolCall{valid, valid}}, "missing_id": {batch: []schema.ToolCall{missingID}},
		"mixed":                {batch: []schema.ToolCall{valid, {ID: "search", Function: schema.FunctionCall{Name: "find", Arguments: `{}`}}}},
		"prose":                {batch: []schema.ToolCall{valid}, text: "untrusted prose"},
		"bogus_type":           {batch: []schema.ToolCall{{ID: "decision", Type: "bogus", Function: schema.FunctionCall{Name: finalizeName, Arguments: stagesDecision(t)}}}},
		"function_type_case":   {batch: []schema.ToolCall{{ID: "decision", Type: "Function", Function: schema.FunctionCall{Name: finalizeName, Arguments: stagesDecision(t)}}}},
		"padded_function_type": {batch: []schema.ToolCall{{ID: "decision", Type: " function ", Function: schema.FunctionCall{Name: finalizeName, Arguments: stagesDecision(t)}}}},
	} {
		t.Run(name, func(t *testing.T) {
			chat := &stagesModel{calls: &atomic.Int32{}, searchRounds: 1, batch: fixture.batch, decisionText: fixture.text}
			core, logs := observer.New(zap.InfoLevel)
			restore := zap.ReplaceGlobals(zap.New(core))
			defer restore()
			cfg := orchestrationConfig()
			cfg.MaxModelCalls = 3
			cfg.MaxSteps = 6
			runner, search := orchestrationRunner(t, chat, cfg)
			stream, err := drainOrchestration(t, runner)
			require.Error(t, err)
			require.NotErrorIs(t, err, io.EOF)
			require.EqualValues(t, 2, chat.calls.Load())
			require.EqualValues(t, 3, search.count.Load())
			if stream != nil {
				require.Empty(t, stream.Places())
			}
			entries := logs.FilterMessage("agent.run").All()
			require.Len(t, entries, 1)
			require.EqualValues(t, 0, entries[0].ContextMap()["decision_attempted"])
		})
	}
}

func TestRunner_Orchestration_whenDecisionTypeIsCompatible(t *testing.T) {
	for _, callType := range []string{"", "function"} {
		t.Run(callType, func(t *testing.T) {
			chat := &stagesModel{calls: &atomic.Int32{}, searchRounds: 1, decision: stagesDecision(t), batch: []schema.ToolCall{{ID: "decision", Type: callType, Function: schema.FunctionCall{Name: finalizeName, Arguments: stagesDecision(t)}}}, final: []*schema.Message{schema.AssistantMessage("answer", nil)}}
			core, logs := observer.New(zap.InfoLevel)
			restore := zap.ReplaceGlobals(zap.New(core))
			defer restore()
			runner, search := orchestrationRunner(t, chat, orchestrationConfig())

			stream, err := drainOrchestration(t, runner)
			require.ErrorIs(t, err, io.EOF)
			require.EqualValues(t, 4, chat.calls.Load())
			require.EqualValues(t, 3, search.count.Load())
			require.Len(t, stream.Places(), 1)
			entries := logs.FilterMessage("agent.run").All()
			require.Len(t, entries, 1)
			require.EqualValues(t, 1, entries[0].ContextMap()["decision_attempted"])
		})
	}
}

func TestRunner_Orchestration_whenBudgetIsLowOrSearchEndsEarly(t *testing.T) {
	for _, steps := range []int{1, 2, 3} {
		chat := &stagesModel{calls: &atomic.Int32{}}
		cfg := orchestrationConfig()
		cfg.MaxSteps = steps
		runner, search := orchestrationRunner(t, chat, cfg)
		_, err := drainOrchestration(t, runner)
		require.ErrorIs(t, err, ErrBudgetExceeded)
		require.Zero(t, chat.calls.Load())
		require.Zero(t, search.count.Load())
	}
	policy, err := NewRecommendationPolicy("0,0", DefaultPreferences())
	require.NoError(t, err)
	decision, err := json.Marshal(policy.Evaluate())
	require.NoError(t, err)
	chat := &stagesModel{calls: &atomic.Int32{}, decision: string(decision), final: []*schema.Message{schema.AssistantMessage("choice", nil)}}
	runner, search := orchestrationRunner(t, chat, orchestrationConfig())
	_, err = drainOrchestration(t, runner)
	require.ErrorIs(t, err, io.EOF)
	require.EqualValues(t, 3, chat.calls.Load())
	require.Zero(t, search.count.Load())
}

func TestDiagnostic_InternalDecision_whenSecondInvocationIsRejected(t *testing.T) {
	budget := newCallBudget(orchestrationConfig())
	_, err := budget.takeSearchModel(t.Context())
	require.NoError(t, err)
	_, err = budget.takeTool("find", t.Context())
	require.NoError(t, err)
	remaining := budget.tools
	_, err = budget.takeDecision(t.Context())
	require.NoError(t, err)
	require.Equal(t, remaining, budget.tools)
	_, err = budget.takeDecision(t.Context())
	require.ErrorIs(t, err, ErrBudgetExceeded)
	require.Equal(t, 1, budget.decisions)
	require.Equal(t, 1, budget.trace.decisionAttempted)
	require.Equal(t, remaining, budget.tools)
}
