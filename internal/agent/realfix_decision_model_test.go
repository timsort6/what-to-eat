package agent

import (
	"io"
	"sync/atomic"
	"testing"

	"food-agent/pkg/config"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
)

// 配置 decision_model 时：检索与最终回答用主模型，只有决策阶段用独立模型。
func TestRunner_UsesDecisionModelOnlyForDecisionStage(t *testing.T) {
	// 主模型的决策参数非法：若决策阶段误用主模型，本次运行必然失败。
	chat := &stagesModel{calls: &atomic.Int32{}, searchRounds: 1, decision: `{`, final: []*schema.Message{schema.AssistantMessage("最终正文", nil)}}
	decider := &stagesModel{calls: &atomic.Int32{}, decision: stagesDecision(t)}
	runner, err := NewRunner("", config.AgentConfig{AllowedTools: []string{"find"}, MaxModelCalls: 3, MaxSteps: 6, MaxToolCalls: 3, TimeoutSeconds: 5}, chat, map[string]tool.InvokableTool{"find": &stagesTool{}})
	require.NoError(t, err)
	require.NoError(t, runner.SetDecisionModel(decider))
	require.Error(t, runner.SetDecisionModel(baseOnlyModel{}), "决策模型必须支持工具调用")

	stream, err := runner.Stream(stagesContext(t.Context()), []*schema.Message{schema.UserMessage("面馆")})
	require.NoError(t, err)
	defer stream.Close()
	chunk, err := stream.Recv()
	require.NoError(t, err)
	require.Equal(t, "最终正文", chunk.Content)
	_, err = stream.Recv()
	require.ErrorIs(t, err, io.EOF)
	require.EqualValues(t, 1, decider.calls.Load(), "决策模型只调用一次")
	require.EqualValues(t, 2, chat.calls.Load(), "主模型：一轮检索 + 最终回答")
}
