package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"food-agent/pkg/config"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
)

type stagesModel struct {
	model.ToolCallingChatModel
	tools         []*schema.ToolInfo
	calls         *atomic.Int32
	searchRounds  int
	decision      string
	decisionText  string
	batch         []schema.ToolCall
	final         []*schema.Message
	finalInput    chan []*schema.Message
	release       <-chan struct{}
	checkDecision func(*schema.ToolInfo) error
	bindings      *atomic.Int32
}

func (m *stagesModel) WithTools(infos []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	if m.bindings != nil {
		m.bindings.Add(1)
	}
	bound := *m
	bound.tools = append([]*schema.ToolInfo(nil), infos...)
	return &bound, nil
}

func (m *stagesModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	number := int(m.calls.Add(1))
	choice := model.GetCommonOptions(nil, opts...).ToolChoice
	if choice != nil && *choice == schema.ToolChoiceForbidden {
		if m.finalInput != nil {
			m.finalInput <- input
		}
		if m.release == nil {
			return schema.StreamReaderFromArray(m.final), nil
		}
		reader, writer := schema.Pipe[*schema.Message](0)
		go func() {
			defer writer.Close()
			for _, message := range m.final {
				if writer.Send(message, nil) {
					return
				}
			}
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
		if m.checkDecision != nil {
			if err := m.checkDecision(m.tools[0]); err != nil {
				return nil, err
			}
		}
		calls := m.batch
		if calls == nil {
			calls = []schema.ToolCall{{ID: "decision", Type: "function", Function: schema.FunctionCall{Name: finalizeName, Arguments: m.decision}}}
		}
		return schema.StreamReaderFromArray([]*schema.Message{{Role: schema.Assistant, Content: m.decisionText, ToolCalls: calls}}), nil
	}
	if number > m.searchRounds {
		return schema.StreamReaderFromArray([]*schema.Message{schema.AssistantMessage("隐藏搜索文字", nil)}), nil
	}
	calls := make([]schema.ToolCall, 3)
	for index := range calls {
		calls[index] = schema.ToolCall{ID: fmt.Sprintf("s%d-%d", number, index), Type: "function", Function: schema.FunctionCall{Name: "find", Arguments: `{}`}}
	}
	return schema.StreamReaderFromArray([]*schema.Message{{Role: schema.Assistant, Content: "隐藏搜索文字", ToolCalls: calls}}), nil
}

func stagesContext(ctx context.Context) context.Context {
	return WithRecommendationPreferences(WithRequestScope(ctx, RequestScope{UserID: 1, Location: "0,0"}), DefaultPreferences())
}

func testRunnerContext(ctx context.Context) context.Context {
	return stagesContext(ctx)
}

func stagesDecision(t *testing.T) string {
	t.Helper()
	policy, err := NewRecommendationPolicy("0,0", DefaultPreferences())
	require.NoError(t, err)
	require.NoError(t, policy.AddCandidates([]Place{{ID: "p1", Name: "面馆", Location: "0,0"}}))
	data, err := json.Marshal(policy.Evaluate())
	require.NoError(t, err)
	return string(data)
}

type stagesTool struct{ count atomic.Int32 }

func (t *stagesTool) Info(context.Context) (*schema.ToolInfo, error) {
	return (&runnerTool{}).Info(context.Background())
}
func (t *stagesTool) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	t.count.Add(1)
	return `{"places":[{"id":"p1","name":"面馆","location":"0,0"},{"id":"rejected","name":"拒绝候选秘密","location":"1,1","raw":{"secret":"秘密"}}],"places_count":2}`, nil
}

func TestRunner_ValidatedStages_whenBudgetsBoundSearch(t *testing.T) {
	for _, fixture := range []struct{ models, steps, rounds int }{{5, 10, 3}, {4, 8, 2}} {
		t.Run(fmt.Sprint(fixture.models), func(t *testing.T) {
			chat := &stagesModel{calls: &atomic.Int32{}, searchRounds: fixture.rounds, decision: stagesDecision(t), final: []*schema.Message{schema.AssistantMessage("最终正文", nil)}, finalInput: make(chan []*schema.Message, 1)}
			search := &stagesTool{}
			runner, err := NewRunner("配置提示词", config.AgentConfig{AllowedTools: []string{"find"}, MaxModelCalls: fixture.models, MaxSteps: fixture.steps, MaxToolCalls: 3, TimeoutSeconds: 30}, chat, map[string]tool.InvokableTool{"find": search})
			require.NoError(t, err)
			stream, err := runner.Stream(stagesContext(t.Context()), []*schema.Message{schema.UserMessage("私有地址信息")})
			require.NoError(t, err)
			defer stream.Close()
			var text string
			for {
				chunk, recvErr := stream.Recv()
				if recvErr == io.EOF {
					break
				}
				require.NoError(t, recvErr)
				text += chunk.Content
			}
			require.Equal(t, "最终正文", text)
			require.EqualValues(t, fixture.models, chat.calls.Load())
			require.EqualValues(t, 3*fixture.rounds, search.count.Load())
			require.Len(t, stream.Places(), 1)
			require.Equal(t, "p1", stream.Places()[0].ID)
			// 正文模型只收到运营配置的提示词（system）和一条已接受事实（user），不含对话历史、被拒候选或原始字段。
			messages := <-chat.finalInput
			require.Len(t, messages, 2)
			require.Equal(t, schema.System, messages[0].Role)
			require.Equal(t, "配置提示词", messages[0].Content)
			require.Equal(t, schema.User, messages[1].Role)
			for _, forbidden := range []string{"rejected", "拒绝候选秘密", "私有地址信息", "raw", "user_id", "address_id", "隐藏搜索文字"} {
				require.NotContains(t, messages[1].Content, forbidden)
			}
		})
	}
}

func TestRunner_ContractSafety(t *testing.T) {
	t.Run("missing_or_invalid_trust_has_zero_calls", func(t *testing.T) {
		for name, ctx := range map[string]context.Context{
			"missing_both":        t.Context(),
			"missing_scope":       WithRecommendationPreferences(t.Context(), DefaultPreferences()),
			"missing_preferences": WithRequestScope(t.Context(), RequestScope{UserID: 1, Location: "0,0"}),
			"invalid_origin":      WithRecommendationPreferences(WithRequestScope(t.Context(), RequestScope{UserID: 1, Location: "invalid"}), DefaultPreferences()),
			"invalid_user":        WithRecommendationPreferences(WithRequestScope(t.Context(), RequestScope{Location: "0,0"}), DefaultPreferences()),
			"invalid_preferences": WithRecommendationPreferences(stagesContext(t.Context()), Preferences{}),
		} {
			t.Run(name, func(t *testing.T) {
				chat := &stagesModel{calls: &atomic.Int32{}, bindings: &atomic.Int32{}, searchRounds: 1, decision: stagesDecision(t), final: []*schema.Message{schema.AssistantMessage("正文", nil)}}
				search := &stagesTool{}
				runner, err := NewRunner("", config.AgentConfig{AllowedTools: []string{"find"}, MaxModelCalls: 3, MaxSteps: 6, MaxToolCalls: 3, TimeoutSeconds: 5}, chat, map[string]tool.InvokableTool{"find": search})
				require.NoError(t, err)

				stream, err := runner.Stream(ctx, nil)
				if stream != nil {
					stream.Close()
				}

				require.Error(t, err)
				require.Zero(t, chat.calls.Load())
				require.Zero(t, chat.bindings.Load())
				require.Zero(t, search.count.Load())
			})
		}
	})
	for name, content := range map[string]string{"empty": "", "spaces": " \t", "newline": "\r\n", "unicode_spaces": "\u00a0\u2003\u3000"} {
		t.Run("blank_final_"+name, func(t *testing.T) {
			chat := &stagesModel{calls: &atomic.Int32{}, searchRounds: 1, decision: stagesDecision(t), final: []*schema.Message{schema.AssistantMessage(content, nil)}, finalInput: make(chan []*schema.Message, 1)}
			search := &stagesTool{}
			runner, err := NewRunner("", config.AgentConfig{AllowedTools: []string{"find"}, MaxModelCalls: 3, MaxSteps: 6, MaxToolCalls: 3, TimeoutSeconds: 5}, chat, map[string]tool.InvokableTool{"find": search})
			require.NoError(t, err)

			stream, finalErr := runner.Stream(stagesContext(t.Context()), nil)
			if stream != nil {
				defer stream.Close()
				for finalErr == nil {
					_, finalErr = stream.Recv()
				}
			}

			require.EqualValues(t, 3, chat.calls.Load())
			require.EqualValues(t, 3, search.count.Load())
			require.Len(t, chat.finalInput, 1)
			require.Error(t, finalErr)
			require.NotErrorIs(t, finalErr, io.EOF)
			require.NotErrorIs(t, finalErr, ErrBudgetExceeded)
		})
	}
	t.Run("leading_whitespace_streams_before_eof_without_trimming", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(stagesContext(t.Context()), 3*time.Second)
		defer cancel()
		release := make(chan struct{})
		chat := &stagesModel{calls: &atomic.Int32{}, searchRounds: 1, decision: stagesDecision(t), final: []*schema.Message{schema.AssistantMessage(" \n", nil), schema.AssistantMessage("    正文", nil)}, release: release}
		runner, err := NewRunner("", config.AgentConfig{AllowedTools: []string{"find"}, MaxModelCalls: 3, MaxSteps: 6, MaxToolCalls: 3, TimeoutSeconds: 5}, chat, map[string]tool.InvokableTool{"find": &stagesTool{}})
		require.NoError(t, err)

		stream, err := runner.Stream(ctx, nil)
		require.NoError(t, err)
		defer stream.Close()
		first, err := stream.Recv()
		require.NoError(t, err)
		require.Equal(t, " \n", first.Content)
		second, err := stream.Recv()
		require.NoError(t, err)
		require.Equal(t, "    正文", second.Content)
		close(release)
		last, err := stream.Recv()
		require.NoError(t, err)
		require.Equal(t, "尾段", last.Content)
		_, err = stream.Recv()
		require.ErrorIs(t, err, io.EOF)
	})
}

// sliceWritingModel 在 WithTools 内把入参切片全部改写为哨兵，模拟不守规矩的模型实现。
type sliceWritingModel struct{ *stagesModel }

func (m *sliceWritingModel) WithTools(infos []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	bound, err := m.stagesModel.WithTools(infos)
	for index := range infos {
		infos[index] = &schema.ToolInfo{Name: "sentinel"}
	}
	return bound, err
}

func TestWithTools_CallerSliceProtected(t *testing.T) {
	t.Run("decision_stage_model", func(t *testing.T) { // runner_decision.go 的 slices.Clone
		held := []*schema.ToolInfo{{Name: finalizeName}}
		hostile := &sliceWritingModel{stagesModel: &stagesModel{calls: &atomic.Int32{}}}
		_, err := (&decisionStageModel{ToolCallingChatModel: hostile}).WithTools(held)
		require.NoError(t, err)
		require.Equal(t, finalizeName, held[0].Name, "调用方持有的工具切片不得被模型改写")
	})
	t.Run("search_and_decision_bindings_end_to_end", func(t *testing.T) { // runner_stream.go 与决策两处绑定都经过改写切片的模型
		chat := &sliceWritingModel{stagesModel: &stagesModel{calls: &atomic.Int32{}, searchRounds: 1, decision: stagesDecision(t), final: []*schema.Message{schema.AssistantMessage("正文", nil)}}}
		search := &stagesTool{}
		runner, err := NewRunner("", config.AgentConfig{AllowedTools: []string{"find"}, MaxModelCalls: 3, MaxSteps: 6, MaxToolCalls: 3, TimeoutSeconds: 5}, chat, map[string]tool.InvokableTool{"find": search})
		require.NoError(t, err)
		stream, err := runner.Stream(stagesContext(t.Context()), []*schema.Message{schema.UserMessage("附近吃点")})
		require.NoError(t, err)
		defer stream.Close()
		var text string
		for {
			chunk, recvErr := stream.Recv()
			if recvErr == io.EOF {
				break
			}
			require.NoError(t, recvErr)
			text += chunk.Content
		}
		require.Equal(t, "正文", text)
		require.EqualValues(t, 3, search.count.Load(), "搜索仍按真实工具名执行")
		require.Len(t, stream.Places(), 1)
	})
}
