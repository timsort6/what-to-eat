package agent

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
)

type decisionStageModel struct {
	model.ToolCallingChatModel
	budget *callBudget
}

func (m *decisionStageModel) WithTools(infos []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	bound, err := m.ToolCallingChatModel.WithTools(slices.Clone(infos))
	if err != nil {
		return nil, err
	}
	return &decisionStageModel{ToolCallingChatModel: bound, budget: m.budget}, nil
}

func (m *decisionStageModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	number, err := m.budget.takeModel(ctx)
	if err != nil {
		return nil, err
	}
	reader, err := m.ToolCallingChatModel.Stream(ctx, input, opts...)
	message, err := collectModelMessage(ctx, reader, err)
	if err == nil {
		if message == nil || len(message.ToolCalls) != 1 || message.Content != "" || message.ToolCalls[0].ID == "" || message.ToolCalls[0].Function.Name != finalizeName {
			err = ErrInvalidRecommendation
		} else if callType := message.ToolCalls[0].Type; callType != "" && callType != "function" {
			err = ErrInvalidRecommendation
		}
	}
	m.budget.complete("model", number, err)
	return message, err
}

type internalDecisionTool struct {
	tool.InvokableTool
	budget *callBudget
}

func (t *internalDecisionTool) InvokableRun(ctx context.Context, args string, opts ...tool.Option) (string, error) {
	if compose.GetToolCallID(ctx) == "" {
		return "", ErrInvalidRecommendation
	}
	number, err := t.budget.takeDecision(ctx)
	if err != nil {
		return "", err
	}
	output, err := t.InvokableTool.InvokableRun(ctx, args, opts...)
	t.budget.complete("decision", number, err)
	return output, err
}

func (s *RunStream) finalize(runner *Runner, policy *RecommendationPolicy) (RecommendationDecision, []Place, error) {
	finalizer := &internalDecisionTool{InvokableTool: newDecisionTool(policy), budget: s.budget}
	decisionAgent, err := react.NewAgent(s.ctx, &react.AgentConfig{
		ToolCallingModel:   &decisionStageModel{ToolCallingChatModel: runner.decisionModel, budget: s.budget},
		ToolsConfig:        compose.ToolsNodeConfig{Tools: []tool.BaseTool{finalizer}},
		ToolReturnDirectly: map[string]struct{}{finalizeName: {}},
		MaxStep:            3,
	})
	if err != nil {
		return RecommendationDecision{}, nil, err
	}
	input := []*schema.Message{schema.UserMessage(whitelistedDecisionInput(policy))}
	if runner.prompt != "" { // 提示词的“决策阶段”一节说明何时填写品类理解/冲突
		input = append([]*schema.Message{schema.SystemMessage(runner.prompt)}, input...)
	}
	output, err := decisionAgent.Generate(s.ctx, input)
	if err != nil {
		return RecommendationDecision{}, nil, err
	}
	if output == nil || output.Role != schema.Tool || output.ToolCallID == "" {
		return RecommendationDecision{}, nil, ErrInvalidRecommendation
	}
	if err := s.budget.takeStep(s.ctx); err != nil {
		return RecommendationDecision{}, nil, err
	}
	s.budget.trace.returned()
	var decision RecommendationDecision
	if err := json.Unmarshal([]byte(output.Content), &decision); err != nil {
		return RecommendationDecision{}, nil, err
	}
	selected, err := policy.SelectedPlaces(decision)
	return decision, selected, err
}
