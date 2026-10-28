package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"food-agent/pkg/config"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const maxDiagnosticRecords = 32

type diagnosticCall struct {
	Kind   string
	Number int
	Round  int
	Tool   string
	Status string
}

type diagnosticTrace struct {
	mu                sync.Mutex
	modelAttempted    int
	modelCompleted    int
	toolAttempted     int
	toolCompleted     int
	decisionAttempted int
	decisionCompleted int
	logicalSteps      int
	directReturned    int
	maxModelCalls     int
	maxToolCalls      int
	maxSteps          int
	timeoutSeconds    int
	calls             []diagnosticCall
}

func newDiagnosticTrace(budgets config.AgentConfig) *diagnosticTrace {
	return &diagnosticTrace{
		maxModelCalls:  budgets.MaxModelCalls,
		maxToolCalls:   budgets.MaxToolCalls,
		maxSteps:       budgets.MaxSteps,
		timeoutSeconds: budgets.TimeoutSeconds,
		calls:          make([]diagnosticCall, 0, maxDiagnosticRecords),
	}
}

func (t *diagnosticTrace) record(kind, toolName string, number int, status string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	limit := t.maxToolCalls * 3 // 最多 3 轮搜索，每轮 max_tool_calls 个
	switch kind {
	case "model":
		t.modelAttempted++
		limit = t.maxModelCalls
	case "decision":
		t.decisionAttempted++
		limit = 1
	case "tool":
		t.toolAttempted++
	}
	if !isAllowedTool(toolName) && !(kind == "decision" && toolName == finalizeName) {
		toolName = ""
	}
	if number-1 <= limit && len(t.calls) < maxDiagnosticRecords {
		t.calls = append(t.calls, diagnosticCall{Kind: kind, Number: number, Round: t.modelAttempted, Tool: toolName, Status: status})
	}
}

func (t *diagnosticTrace) complete(kind string, callNumber int, status string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch kind {
	case "model":
		t.modelCompleted++
	case "decision":
		t.decisionCompleted++
	case "tool":
		t.toolCompleted++
	}
	for index := range t.calls {
		call := &t.calls[index]
		if call.Kind == kind && call.Number == callNumber && call.Status == "started" {
			call.Status = status
			return
		}
	}
}

func (t *diagnosticTrace) step() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.logicalSteps++
}

func (t *diagnosticTrace) returned() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.directReturned++
}

func (t *diagnosticTrace) finish(err error) {
	t.mu.Lock()
	fields := []zap.Field{
		zap.String("status", diagnosticStatus(err)),
		zap.Int("model_attempted", t.modelAttempted),
		zap.Int("model_completed", t.modelCompleted),
		zap.Int("tool_attempted", t.toolAttempted),
		zap.Int("tool_completed", t.toolCompleted),
		zap.Int("decision_attempted", t.decisionAttempted),
		zap.Int("decision_completed", t.decisionCompleted),
		zap.Int("logical_steps", t.logicalSteps),
		zap.Int("direct_return_completed", t.directReturned),
		zap.Int("max_model_calls", t.maxModelCalls),
		zap.Int("max_tool_calls", t.maxToolCalls),
		zap.Int("max_steps", t.maxSteps),
		zap.Int("timeout_seconds", t.timeoutSeconds),
		zap.Objects("calls", append([]diagnosticCall(nil), t.calls...)),
	}
	t.mu.Unlock()
	zap.L().Info("agent.run", fields...)
}

func diagnosticStatus(err error) string {
	if err == nil {
		return "ok"
	}
	if errors.Is(err, ErrBudgetExceeded) {
		return "budget_exceeded"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	if errors.Is(err, ErrInvalidToolInput) {
		return "invalid_input"
	}
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &syntaxErr) || errors.As(err, &typeErr) {
		return "invalid_json"
	}
	return "failed"
}

func (c diagnosticCall) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	enc.AddString("kind", c.Kind)
	enc.AddInt("number", c.Number)
	enc.AddInt("round", c.Round)
	if c.Tool != "" {
		enc.AddString("tool", c.Tool)
	}
	enc.AddString("status", c.Status)
	return nil
}
