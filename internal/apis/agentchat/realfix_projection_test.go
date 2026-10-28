package agentchat

import (
	"testing"

	"food-agent/internal/agent"

	"github.com/stretchr/testify/require"
)

// 高德参考人均（缺币种/口径）已参与比较时，卡片保留 cost 且不伪造币种/口径，由前端显示“参考人均 ¥X”。
func TestPlace_Projection_keepsAmapReferenceCost(t *testing.T) {
	evaluation := &agent.CandidateAssessment{Price: agent.MatchEvidence{State: agent.MatchSatisfied, Fields: []agent.EvidenceField{"business.cost"}}}
	place := projectPlace(agent.Place{ID: "P1", Name: "店", Cost: "94.00", Evaluation: evaluation})
	require.Equal(t, "94.00", place.Cost)
	require.Empty(t, place.CostCurrency)
	require.Empty(t, place.CostBasis)
}
