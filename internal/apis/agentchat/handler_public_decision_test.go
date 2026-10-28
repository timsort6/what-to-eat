package agentchat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"food-agent/internal/agent"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
)

// choiceLLM 在决策阶段沿用服务端预评估结果，只追加测试给定的协商方案。
type choiceLLM struct {
	fakeLLM
	options      func(agent.RecommendationDecision) []agent.RecommendationOption
	reachedFinal bool
}

func (model *choiceLLM) Stream(ctx context.Context, input []*schema.Message, options ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	forbidden := false
	if choice := einomodel.GetCommonOptions(nil, options...).ToolChoice; choice != nil && *choice == schema.ToolChoiceForbidden {
		forbidden, model.reachedFinal = true, true
	}
	if forbidden || len(model.fakeLLM.tools) != 1 || model.fakeLLM.tools[0].Name != "finalize_recommendation" {
		return model.fakeLLM.Stream(ctx, input, options...)
	}
	model.fakeLLM.calls++
	var decisionInput struct {
		Decision agent.RecommendationDecision `json:"decision"`
	}
	if len(input) == 0 || json.Unmarshal([]byte(input[len(input)-1].Content), &decisionInput) != nil {
		return nil, errors.New("decision input missing")
	}
	decision := decisionInput.Decision
	decision.Options = model.options(decision)
	arguments, err := json.Marshal(decision)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{
		ID: "decision-1", Type: "function", Function: schema.FunctionCall{Name: "finalize_recommendation", Arguments: string(arguments)},
	}}}}), nil
}

func (model *choiceLLM) WithTools(tools []*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	model.fakeLLM.tools = tools
	return model, nil
}

func radiusChange(meters int, allowUnknown *bool) agent.PreferenceChanges {
	return agent.PreferenceChanges{RadiusMeters: &meters, AllowUnknownPrice: allowUnknown}
}

func TestChat_NeedsChoiceDecisionCarriesWhitelistedAlternativeCandidates(t *testing.T) {
	pois := `{"status":"1","pois":[{"id":"P1","name":"餐厅","location":"0,0","tel":"010-SECRET","business":{"tag":"四川火锅","cost":"约30","tel":"010-SECRET"}},` +
		`{"id":"P2","name":"远处","address":"二号路","location":"0,0.03","tel":"010-SECRET","photos":[{"url":"https://img.example/secret.png"}]}]}`
	allow := true
	llm := &choiceLLM{options: func(agent.RecommendationDecision) []agent.RecommendationOption {
		return []agent.RecommendationOption{{OptionID: "wider", Label: "扩大到4公里并接受价格待核实", ObservedIDs: []string{"poi:P2"},
			ReasonCodes: []agent.ReasonCode{agent.ReasonOutsideRadius}, Changes: radiusChange(4000, &allow)}}
	}}
	body := `{"messages":[{"role":"user","content":"附近吃什么"}],"location":"0,0","preferences":` + preferencesJSON(2000, false) + `}`
	recorder := serveChat(t, newTestService(t, llm, &fakeMCP{resp: pois}, &fakeAddrDAO{}), context.Background(), body)
	raw := recorder.Body.String()
	events := parseSSE(raw)
	require.Equal(t, "decision,delta,done", eventNames(events), raw)
	for _, leaked := range []string{"010-SECRET", "secret.png", `"tel"`, `"photos"`, `"pois"`} {
		require.NotContains(t, raw, leaked)
	}
	var decision struct {
		Status      string `json:"status"`
		Assessments []struct {
			CandidateID string `json:"candidate_id"`
		} `json:"assessments"`
		Candidates []map[string]json.RawMessage `json:"candidates"`
	}
	require.NoError(t, json.Unmarshal(events[0].data, &decision))
	require.Equal(t, "needs_choice", decision.Status)
	require.Len(t, decision.Assessments, 1, "needs_choice 只保留方案引用的候选评估")
	require.Equal(t, "poi:P2", decision.Assessments[0].CandidateID)
	require.Len(t, decision.Candidates, 1)
	candidate := decision.Candidates[0]
	keys := make([]string, 0, len(candidate))
	for name := range candidate {
		keys = append(keys, name)
	}
	require.ElementsMatch(t, []string{"candidate_id", "name", "address", "distance_meters"}, keys)
	require.JSONEq(t, `"poi:P2"`, string(candidate["candidate_id"]))
	require.JSONEq(t, `"远处"`, string(candidate["name"]))
	require.JSONEq(t, `"二号路"`, string(candidate["address"]))
	require.JSONEq(t, `3336`, string(candidate["distance_meters"]))

	var done struct {
		Decision json.RawMessage `json:"decision"`
	}
	require.NoError(t, json.Unmarshal(events[2].data, &done))
	require.JSONEq(t, string(events[0].data), string(done.Decision))
}

func TestChat_OversizedPublicDecisionFailsClosedBeforeAnswer(t *testing.T) {
	pois := make([]string, 0, 10)
	observed := make([]string, 0, 10)
	for index := range 10 {
		long := strings.Repeat("<", 1000)
		pois = append(pois, fmt.Sprintf(`{"id":"P%d","name":"%s","address":"%s","location":"0,0.02%d"}`, index, long, long, index))
		observed = append(observed, fmt.Sprintf("poi:P%d", index))
	}
	llm := &choiceLLM{options: func(agent.RecommendationDecision) []agent.RecommendationOption {
		options := make([]agent.RecommendationOption, 0, 3)
		for index, meters := range []int{4000, 4100, 4200} {
			options = append(options, agent.RecommendationOption{OptionID: fmt.Sprintf("wider-%d", index), Label: "扩大范围",
				ObservedIDs: observed, ReasonCodes: []agent.ReasonCode{agent.ReasonOutsideRadius}, Changes: radiusChange(meters, nil)})
		}
		return options
	}}
	preferences := `{"target_count":3,"radius_meters":2000,"meal_style":"any","max_price_cny":null,"allow_unknown_price":true,"food_requirement":null}`
	body := `{"messages":[{"role":"user","content":"附近吃什么"}],"location":"0,0","preferences":` + preferences + `}`
	recorder := serveChat(t, newTestService(t, llm, &fakeMCP{resp: `{"status":"1","pois":[` + strings.Join(pois, ",") + `]}`}, &fakeAddrDAO{}), context.Background(), body)
	require.Equal(t, http.StatusOK, recorder.Code)
	raw := recorder.Body.String()
	require.Equal(t, "error", eventNames(parseSSE(raw)), raw)
	require.False(t, llm.reachedFinal, "超限决策不能进入正文阶段")
	require.NotContains(t, raw, "<<<")
	require.NotContains(t, raw, `\u003c`)
}
