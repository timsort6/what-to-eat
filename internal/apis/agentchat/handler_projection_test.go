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
	"food-agent/pkg/config"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
)

// sequenceMCP 按调用顺序返回固定 POI2 响应，模拟多次真实检索结果。
type sequenceMCP struct {
	responses []string
	calls     int
}

func (m *sequenceMCP) CallToolText(context.Context, string, map[string]any) (string, error) {
	if m.calls >= len(m.responses) {
		return "", errors.New("unexpected extra map call")
	}
	m.calls++
	return m.responses[m.calls-1], nil
}

// projectionLLM：首轮并发两次检索、次轮结束检索；决策阶段按 pick 改写选中集；记录决策与最终正文阶段的输入。
type projectionLLM struct {
	tools         []*schema.ToolInfo
	searches      int
	pick          func(candidates []agent.Place, decision agent.RecommendationDecision) agent.RecommendationDecision
	decisionInput []agent.Place
	finalInput    []*schema.Message
}

func (m *projectionLLM) Generate(context.Context, []*schema.Message, ...einomodel.Option) (*schema.Message, error) {
	return nil, errors.New("unexpected generate")
}

func (m *projectionLLM) WithTools(tools []*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	m.tools = tools
	return m, nil
}

func (m *projectionLLM) Stream(_ context.Context, input []*schema.Message, options ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	if choice := einomodel.GetCommonOptions(nil, options...).ToolChoice; choice != nil && *choice == schema.ToolChoiceForbidden {
		m.finalInput = input
		return schema.StreamReaderFromArray([]*schema.Message{schema.AssistantMessage("## 推荐\n\n已按条件整理。", nil)}), nil
	}
	if len(m.tools) == 1 && m.tools[0].Name == "finalize_recommendation" {
		var payload struct {
			Candidates []agent.Place                `json:"candidates"`
			Decision   agent.RecommendationDecision `json:"decision"`
		}
		if len(input) == 0 || json.Unmarshal([]byte(input[len(input)-1].Content), &payload) != nil {
			return nil, errors.New("decision input missing")
		}
		m.decisionInput = payload.Candidates
		arguments, err := json.Marshal(m.pick(payload.Candidates, payload.Decision))
		if err != nil {
			return nil, err
		}
		return schema.StreamReaderFromArray([]*schema.Message{{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{
			ID: "decision-1", Type: "function", Function: schema.FunctionCall{Name: "finalize_recommendation", Arguments: string(arguments)},
		}}}}), nil
	}
	m.searches++
	if m.searches > 1 {
		return schema.StreamReaderFromArray([]*schema.Message{schema.AssistantMessage("", nil)}), nil
	}
	call := func(id string) schema.ToolCall {
		return schema.ToolCall{ID: id, Type: "function", Function: schema.FunctionCall{Name: "amap_poi2_around", Arguments: `{"keywords":"美食","page_size":25}`}}
	}
	return schema.StreamReaderFromArray([]*schema.Message{{Role: schema.Assistant, ToolCalls: []schema.ToolCall{call("search-1"), call("search-2")}}}), nil
}

func poiJSON(id, name, address string, latitude float64, extra string) string {
	return fmt.Sprintf(`{"id":%q,"name":%q,"address":%q,"location":"0,%.4f","tel":"010-LEAK","photos":[{"url":"https://img.example/leak.png"}]%s}`, id, name, address, latitude, extra)
}

func TestChat_DonePlacesProjectExactlyValidatedSelectionFrom39Candidates(t *testing.T) {
	// 39 个不同候选（第二页还混入一个重复 ID）：同名异址、无效 ID 合成键、未知人均（cost 非数字；纯数字缺币种口径按高德参考人均）以及电话/照片等原始字段。
	first := make([]string, 0, 25)
	for index := 1; index <= 23; index++ {
		first = append(first, poiJSON(fmt.Sprintf("P%02d", index), fmt.Sprintf("候选%02d", index), fmt.Sprintf("%d号街", index), float64(index)*0.0004, ""))
	}
	first = append(first, poiJSON("SAME1", "同名面馆", "一号路", 0.0101, ""), poiJSON("SAME2", "同名面馆", "二号路", 0.0102, `,"business":{"cost":"约777","tel":"010-LEAK"}`))
	second := []string{poiJSON("P01", "候选01", "1号街", 0.0004, "")}
	for index := 26; index <= 38; index++ {
		second = append(second, poiJSON(fmt.Sprintf("P%02d", index), fmt.Sprintf("候选%02d", index), fmt.Sprintf("%d号街", index), float64(index)*0.0004, ""))
	}
	second = append(second, poiJSON("bad:id", "无号小馆", "三号路", 0.0151, ""))
	mcp := &sequenceMCP{responses: []string{
		`{"status":"1","pois":[` + strings.Join(first, ",") + `]}`,
		`{"status":"1","pois":[` + strings.Join(second, ",") + `]}`,
	}}

	var syntheticKey string
	llm := &projectionLLM{pick: func(candidates []agent.Place, decision agent.RecommendationDecision) agent.RecommendationDecision {
		for _, candidate := range candidates {
			if candidate.Name == "无号小馆" {
				syntheticKey = candidate.CandidateKey
			}
		}
		// 选中集故意不取默认的前三家：同名的第二家、合成键候选、靠后的候选。
		decision.SelectedIDs = []string{"poi:SAME2", syntheticKey, "poi:P30"}
		decision.ReasonCodes = []agent.ReasonCode{}
		return decision
	}}
	svc, err := NewService(Deps{LLM: llm, MCP: mcp, Addresses: &fakeAddrDAO{}, Prompt: "提示词：简体中文 Markdown", Budgets: projectionBudgets})
	require.NoError(t, err)
	preferences := `{"target_count":3,"radius_meters":2000,"meal_style":"any","max_price_cny":null,"allow_unknown_price":true,"food_requirement":null}`
	recorder := serveChat(t, svc, context.Background(), `{"messages":[{"role":"user","content":"附近吃什么"}],"location":"0,0","preferences":`+preferences+`}`)
	require.Equal(t, http.StatusOK, recorder.Code)
	raw := recorder.Body.String()
	events := parseSSE(raw)
	require.Equal(t, "decision,delta,done", eventNames(events), raw)
	require.Equal(t, 2, mcp.calls)
	require.Len(t, llm.decisionInput, 39, "决策阶段应看到 39 个去重候选")
	require.True(t, strings.HasPrefix(syntheticKey, "synthetic:"))

	for _, leaked := range []string{"010-LEAK", "leak.png", `"tel"`, `"photos"`, "777", "SAME1", "一号路", "候选01", `"raw"`} {
		require.NotContains(t, raw, leaked)
	}
	var done struct {
		Places      []map[string]json.RawMessage `json:"places"`
		PlacesCount int                          `json:"places_count"`
		Decision    agent.RecommendationDecision `json:"decision"`
	}
	require.NoError(t, json.Unmarshal(events[2].data, &done))
	want := []string{"poi:SAME2", syntheticKey, "poi:P30"}
	require.Equal(t, want, done.Decision.SelectedIDs)
	require.Equal(t, 3, done.PlacesCount)
	require.Len(t, done.Places, 3)
	for index, place := range done.Places {
		var key string
		require.NoError(t, json.Unmarshal(place["candidate_key"], &key))
		require.Equal(t, want[index], key, "卡片与选中集一一对应且保持顺序")
		for _, field := range []string{"cost", "cost_currency", "cost_basis", "price_sources", "tel", "raw", "photos", "url"} {
			require.NotContains(t, place, field, "未知或原始字段不得进入卡片")
		}
		var evaluation agent.CandidateAssessment
		require.NoError(t, json.Unmarshal(place["evaluation"], &evaluation))
		require.Equal(t, agent.MatchUnknown, evaluation.Price.State, "未知人均保持未知")
		require.Equal(t, agent.MatchSatisfied, evaluation.Range.State)
	}
	require.JSONEq(t, `"SAME2"`, string(done.Places[0]["id"]))
	require.JSONEq(t, `"二号路"`, string(done.Places[0]["address"]), "同名异址不得混用另一家的地址")
	require.JSONEq(t, `"0,0.0102"`, string(done.Places[0]["location"]))
	require.NotContains(t, done.Places[1], "id", "无效 ID 不得以合成键冒充真实 POI ID")
	require.JSONEq(t, `"无号小馆"`, string(done.Places[1]["name"]))
	require.JSONEq(t, `"P30"`, string(done.Places[2]["id"]))

	// 最终正文模型：先收到配置提示词，再只收到已接受的三家事实。
	require.Len(t, llm.finalInput, 2)
	require.Equal(t, schema.System, llm.finalInput[0].Role)
	require.Equal(t, "提示词：简体中文 Markdown", llm.finalInput[0].Content)
	final := llm.finalInput[1].Content
	for _, leaked := range []string{"010-LEAK", "leak.png", "777", "SAME1", "一号路", "候选01"} {
		require.NotContains(t, final, leaked)
	}
	var finalPayload struct {
		Places   []agent.Place                `json:"places"`
		Decision agent.RecommendationDecision `json:"decision"`
	}
	require.NoError(t, json.Unmarshal([]byte(final), &finalPayload))
	require.Len(t, finalPayload.Places, 3)
	require.Len(t, finalPayload.Decision.Assessments, 3)
	require.Equal(t, want, finalPayload.Decision.SelectedIDs)
}

func TestChat_NeedsChoiceFinalModelReceivesOnlyOptionFacts(t *testing.T) {
	pois := make([]string, 0, 6)
	for index := range 6 {
		pois = append(pois, poiJSON(fmt.Sprintf("F%d", index), fmt.Sprintf("远处%d", index), fmt.Sprintf("远%d路", index), 0.03+float64(index)*0.0001, ""))
	}
	llm := &projectionLLM{pick: func(_ []agent.Place, decision agent.RecommendationDecision) agent.RecommendationDecision {
		decision.Options = []agent.RecommendationOption{{OptionID: "wider", Label: "扩大到4公里", ObservedIDs: []string{"poi:F2"},
			ReasonCodes: []agent.ReasonCode{agent.ReasonOutsideRadius}, Changes: radiusChange(4000, nil)}}
		return decision
	}}
	mcp := &sequenceMCP{responses: []string{`{"status":"1","pois":[` + strings.Join(pois, ",") + `]}`, `{"status":"1","pois":[]}`}}
	svc, err := NewService(Deps{LLM: llm, MCP: mcp, Addresses: &fakeAddrDAO{}, Prompt: "提示词", Budgets: projectionBudgets})
	require.NoError(t, err)
	preferences := `{"target_count":3,"radius_meters":2000,"meal_style":"any","max_price_cny":null,"allow_unknown_price":true,"food_requirement":null}`
	recorder := serveChat(t, svc, context.Background(), `{"messages":[{"role":"user","content":"附近吃什么"}],"location":"0,0","preferences":`+preferences+`}`)
	raw := recorder.Body.String()
	events := parseSSE(raw)
	require.Equal(t, "decision,delta,done", eventNames(events), raw)
	var done struct {
		Places []json.RawMessage `json:"places"`
		Status string            `json:"status"`
	}
	require.NoError(t, json.Unmarshal(events[2].data, &done))
	require.Equal(t, "needs_choice", done.Status)
	require.Empty(t, done.Places)

	require.Len(t, llm.finalInput, 2)
	final := llm.finalInput[1].Content
	for _, rejected := range []string{"poi:F0", "poi:F1", "poi:F3", "远处0", "远0路", "010-LEAK"} {
		require.NotContains(t, final, rejected, "协商正文只能看到方案引用的候选")
	}
	var payload struct {
		Places   []agent.Place `json:"places"`
		Decision struct {
			Assessments []agent.CandidateAssessment  `json:"assessments"`
			Candidates  []agent.ObservedCandidate    `json:"candidates"`
			Options     []agent.RecommendationOption `json:"options"`
		} `json:"decision"`
	}
	require.NoError(t, json.Unmarshal([]byte(final), &payload))
	require.Empty(t, payload.Places)
	require.Len(t, payload.Decision.Assessments, 1)
	require.Equal(t, "poi:F2", payload.Decision.Assessments[0].CandidateID)
	require.Len(t, payload.Decision.Candidates, 1)
	require.Equal(t, "远处2", payload.Decision.Candidates[0].Name)
	require.Len(t, payload.Decision.Options, 1)
}

// 与 newTestService 相同的预算：最多 2 轮检索、每轮 3 次地图调用。
var projectionBudgets = config.AgentConfig{
	AllowedTools: []string{agent.ToolNamePOI2Around, agent.ToolNamePOI2Text},
	MaxSteps:     8, MaxModelCalls: 4, MaxToolCalls: 3, TimeoutSeconds: 5,
}
