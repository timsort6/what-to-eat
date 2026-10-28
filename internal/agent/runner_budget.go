package agent

import (
	"context"
	"fmt"
	"sync"

	"food-agent/pkg/config"
)

type callBudget struct {
	mu          sync.Mutex
	models      int
	tools       int
	maxTools    int
	modelNumber int
	toolNumber  int
	trace       *diagnosticTrace
	steps       int
	decisions   int
}

func newCallBudget(budgets config.AgentConfig) *callBudget {
	return &callBudget{
		models:   budgets.MaxModelCalls,
		maxTools: budgets.MaxToolCalls,
		trace:    newDiagnosticTrace(budgets),
		steps:    budgets.MaxSteps,
	}
}

func (b *callBudget) takeTool(toolName string, ctx context.Context) (int, error) {
	return b.takeCall(false, toolName, ctx)
}

func (b *callBudget) takeModel(ctx context.Context) (int, error) {
	return b.takeCall(true, "", ctx)
}

func (b *callBudget) takeSearchModel(ctx context.Context) (int, error) {
	number, err := b.takeModel(ctx)
	if err == nil {
		b.mu.Lock()
		b.tools = b.maxTools
		b.mu.Unlock()
	}
	return number, err
}

func (b *callBudget) takeStep(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.stepLocked()
}

func (b *callBudget) stepLocked() error {
	if b.steps <= 0 {
		return ErrBudgetExceeded
	}
	b.steps--
	if b.trace != nil {
		b.trace.step()
	}
	return nil
}

func (b *callBudget) takeDecision(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.decisions != 0 {
		return 0, ErrBudgetExceeded
	}
	if err := b.stepLocked(); err != nil {
		return 0, err
	}
	b.decisions++
	if b.trace != nil {
		b.trace.record("decision", finalizeName, 1, "started")
	}
	return 1, nil
}

func (b *callBudget) takeCall(modelCall bool, toolName string, ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if modelCall {
		b.modelNumber++
		number := b.modelNumber
		if b.models == 0 {
			if b.trace != nil {
				b.trace.record("model", "", number, "budget_exceeded")
			}
			return 0, fmt.Errorf("%w: model", ErrBudgetExceeded)
		}
		if err := b.stepLocked(); err != nil {
			return 0, err
		}
		b.models--
		if b.trace != nil {
			b.trace.record("model", "", number, "started")
		}
		return number, nil
	}
	b.toolNumber++
	number := b.toolNumber
	if b.tools == 0 {
		if b.trace != nil {
			b.trace.record("tool", toolName, number, "budget_exceeded")
		}
		return 0, fmt.Errorf("%w: tool", ErrBudgetExceeded)
	}
	b.tools--
	if b.trace != nil {
		b.trace.record("tool", toolName, number, "started")
	}
	return number, nil
}

func (b *callBudget) complete(kind string, number int, err error) {
	if b.trace == nil {
		return
	}
	b.trace.complete(kind, number, diagnosticStatus(err))
}
