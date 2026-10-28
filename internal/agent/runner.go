package agent

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"food-agent/pkg/config"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

var ErrBudgetExceeded = errors.New("agent call budget exceeded")

// maxToolCallsPlaceholder 是提示词中每轮工具上限的占位符，由 NewRunner 替换为 agent.max_tool_calls。
const maxToolCallsPlaceholder = "{{max_tool_calls}}"

// Runner 封装使用固定工具集合和配置预算的 Eino ReAct 执行器。
type Runner struct {
	prompt  string
	budgets config.AgentConfig
	model   model.ToolCallingChatModel
	// decisionModel 只用于决策阶段；默认与 model 相同。
	decisionModel model.ToolCallingChatModel
	tools         []tool.InvokableTool
	toolNames     []string
}

// NewRunner 构造 runner；chatModel 必须支持 Eino 工具调用。
func NewRunner(prompt string, budgets config.AgentConfig, chatModel model.BaseChatModel, tools map[string]tool.InvokableTool) (*Runner, error) {
	toolModel, ok := chatModel.(model.ToolCallingChatModel)
	if !ok {
		return nil, errors.New("agent chat model does not support tool calling")
	}
	if budgets.MaxSteps <= 0 || budgets.MaxModelCalls <= 0 || budgets.MaxToolCalls <= 0 || budgets.TimeoutSeconds <= 0 {
		return nil, errors.New("agent execution budgets must be positive")
	}
	// 提示词里的每轮工具上限与 newCallBudget 取同一个 MaxToolCalls；旧提示词无占位符时原样使用。
	prompt = strings.ReplaceAll(prompt, maxToolCallsPlaceholder, strconv.Itoa(budgets.MaxToolCalls))
	runner := &Runner{prompt: prompt, budgets: budgets, model: toolModel, decisionModel: toolModel}
	for _, name := range budgets.AllowedTools {
		configured, exists := tools[name]
		if !exists {
			return nil, fmt.Errorf("configured agent tool %q is unavailable", name)
		}
		runner.tools = append(runner.tools, configured)
		runner.toolNames = append(runner.toolNames, name)
	}
	return runner, nil
}

// SetDecisionModel 让决策阶段改用独立模型（检索与最终回答仍用构造时的模型）；须支持工具调用。
func (r *Runner) SetDecisionModel(chatModel model.BaseChatModel) error {
	toolModel, ok := chatModel.(model.ToolCallingChatModel)
	if !ok {
		return errors.New("agent decision model does not support tool calling")
	}
	r.decisionModel = toolModel
	return nil
}

// Stream 执行一次多轮 ReAct，并返回需由调用方持续 Recv 或 Close 的流。
func (r *Runner) Stream(ctx context.Context, history []*schema.Message) (*RunStream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(r.budgets.TimeoutSeconds)*time.Second)
	calls := newCallBudget(r.budgets)
	stream := &RunStream{ctx: ctx, cancel: cancel, budget: calls, deltas: make(chan *schema.Message), ready: make(chan struct{}), done: make(chan struct{})}
	input := append([]*schema.Message(nil), history...)
	if r.prompt != "" {
		input = append([]*schema.Message{schema.SystemMessage(r.prompt)}, input...)
	}
	go stream.execute(r, input)
	select {
	case <-stream.ready:
		return stream, nil
	case <-stream.done:
		if stream.err != nil {
			stream.Close()
			return nil, stream.err
		}
		return stream, nil
	}
}

func searchRounds(budgets config.AgentConfig) int {
	return max(0, min(3, min(budgets.MaxModelCalls-2, (budgets.MaxSteps-4)/2)))
}

func copyMessages(messages []*schema.Message) []*schema.Message {
	return slices.Clone(messages)
}
