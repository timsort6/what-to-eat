package agentchat

import (
	"testing"

	"food-agent/internal/agent"

	"github.com/stretchr/testify/require"
)

// 复核探针并入：真实高德分类形状的替代品类（冒菜店 type=四川菜(川菜)+tag 冒菜；贵州锅 type=火锅店+tag 贵州火锅）。
// 模型 ready 直接选这两家 → 两家都被判不符而剔除；2km 内无合格店 → needs_choice 零卡片，正文模型接触不到替代店。
func TestProbe_E2E_RealisticSubstitutes(t *testing.T) {
	mcp := &t16MCP{around: t16POIs(t16Maocai, t16Guizhou), text: t16POIs(t16Far)}
	model := &t16Model{decide: func(d agent.RecommendationDecision) (agent.RecommendationDecision, map[string]any) {
		d.Status, d.SelectedIDs, d.ReasonCodes, d.Options = agent.DecisionReady, []string{"poi:BMAOCAI1", "poi:BGUIZHOU1"}, []agent.ReasonCode{}, []agent.RecommendationOption{}
		return d, t16Interpretation("四川火锅", "四川火锅", false)
	}}
	result := t16Run(t, t16Service(t, model, mcp), t16Body(t, []ChatMessage{{Role: "user", Content: t16Text}}, t16Prefs(2000, nil), nil))
	require.Equal(t, "decision,delta,delta,done", result.Events, result.Raw)
	require.Equal(t, "needs_choice", result.Done.Status)
	require.Empty(t, result.Done.Places)
	require.Equal(t, []string{"preferences_conflict"}, result.Decision.ReasonCodes)
	for _, substitute := range []string{"巷子冒菜", "黔味酸汤"} {
		require.NotContains(t, model.finalInput, substitute)
		require.NotContains(t, result.Raw, substitute)
	}
}
