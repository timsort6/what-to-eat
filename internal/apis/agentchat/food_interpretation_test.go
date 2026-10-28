package agentchat

// 任务16：首轮品类为空时由决策阶段的有界“品类理解/冲突”字段参与服务端评估。
// 由 F4 理想态复现（final-F4-20261024-105508/f4_fidelity_test.go）等价并入：真实 Service/Runner/handler + 受控假模型/MCP。
// 假模型以 map 追加新字段，旧代码可编译并以断言失败暴露缺口。

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"food-agent/internal/agent"
	"food-agent/pkg/config"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
)

const t16Origin = "104.06,30.67"

// 2km 内：已知冒菜（约1.0km）、已知贵州火锅（约1.5km）；约3.95km：已知四川火锅。
const (
	t16Maocai  = `{"id":"BMAOCAI1","name":"巷子冒菜","address":"一号路","location":"104.06,30.679","type":"餐饮服务;中餐厅;四川菜(川菜)","tel":"010-SECRET","business":{"tag":"冒菜","cost":"25"}}`
	t16Guizhou = `{"id":"BGUIZHOU1","name":"黔味酸汤","address":"二号路","location":"104.06,30.6835","type":"餐饮服务;中餐厅;火锅店","business":{"tag":"贵州火锅","cost":"60"}}`
	t16Far     = `{"id":"BFARHOT1","name":"蜀地老火锅","address":"三号路","location":"104.06,30.7055","type":"餐饮服务;中餐厅;火锅店","business":{"tag":"四川火锅","cost":"90"}}`
	t16Unknown = `{"id":"BUNK1","name":"未知价小馆","location":"104.06,30.675","type":"餐饮服务;中餐厅","business":{"cost":"约30"}}` // 非数字 cost 才是未知价（纯数字缺币种按高德参考人均）
	t16Known   = `{"id":"BKNOWN1","name":"明码小馆","location":"104.06,30.676","type":"餐饮服务;中餐厅","business":{"cost":"40","currency":"CNY","cost_basis":"per_person"}}`
	t16Text    = "2公里内的四川火锅"
)

func t16POIs(items ...string) string {
	return `{"status":"1","pois":[` + strings.Join(items, ",") + `]}`
}

type t16Call struct {
	Name string
	Args map[string]any
}

type t16MCP struct {
	mu     sync.Mutex
	calls  []t16Call
	around string
	text   string
}

func (m *t16MCP) CallToolText(_ context.Context, name string, args map[string]any) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	copied := make(map[string]any, len(args))
	for key, value := range args {
		copied[key] = value
	}
	m.calls = append(m.calls, t16Call{Name: name, Args: copied})
	if name == agent.ToolNamePOI2Text {
		return m.text, nil
	}
	return m.around, nil
}

// t16Decide 收到服务端预评估决策，返回模型决策与追加的顶层字段（如 food_interpretation/conflict）。
type t16Decide func(agent.RecommendationDecision) (agent.RecommendationDecision, map[string]any)

type t16Model struct {
	tools         []*schema.ToolInfo
	decide        t16Decide
	stages        []string
	decisionInput string
	finalInput    string
}

func (m *t16Model) WithTools(tools []*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	m.tools = tools
	return m, nil
}

func (m *t16Model) Generate(context.Context, []*schema.Message, ...einomodel.Option) (*schema.Message, error) {
	panic("unexpected generate")
}

func (m *t16Model) Stream(_ context.Context, input []*schema.Message, opts ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	if choice := einomodel.GetCommonOptions(nil, opts...).ToolChoice; choice != nil && *choice == schema.ToolChoiceForbidden {
		m.stages = append(m.stages, "final")
		m.finalInput = input[len(input)-1].Content
		return schema.StreamReaderFromArray([]*schema.Message{schema.AssistantMessage("## 推荐\n", nil), schema.AssistantMessage("- 依据已接受条件作答", nil)}), nil
	}
	if len(m.tools) == 1 && m.tools[0].Name == "finalize_recommendation" {
		m.stages = append(m.stages, "decision")
		m.decisionInput = input[len(input)-1].Content
		var in struct {
			Decision agent.RecommendationDecision `json:"decision"`
		}
		if err := json.Unmarshal([]byte(m.decisionInput), &in); err != nil {
			return nil, err
		}
		decision, extra := in.Decision, map[string]any(nil)
		if m.decide != nil {
			decision, extra = m.decide(decision)
		}
		args, err := json.Marshal(decision)
		if err != nil {
			return nil, err
		}
		if len(extra) > 0 {
			var merged map[string]any
			if err := json.Unmarshal(args, &merged); err != nil {
				return nil, err
			}
			for key, value := range extra {
				merged[key] = value
			}
			if args, err = json.Marshal(merged); err != nil {
				return nil, err
			}
		}
		return schema.StreamReaderFromArray([]*schema.Message{{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{
			ID: "decision-1", Type: "function", Function: schema.FunctionCall{Name: "finalize_recommendation", Arguments: string(args)},
		}}}}), nil
	}
	if input[len(input)-1].Role == schema.Tool {
		m.stages = append(m.stages, "search-stop")
		return schema.StreamReaderFromArray([]*schema.Message{schema.AssistantMessage("", nil)}), nil
	}
	m.stages = append(m.stages, "search")
	var around, text string
	for _, info := range m.tools {
		if strings.Contains(info.Name, "around") {
			around = info.Name
		}
		if strings.Contains(info.Name, "text") {
			text = info.Name
		}
	}
	return schema.StreamReaderFromArray([]*schema.Message{{Role: schema.Assistant, ToolCalls: []schema.ToolCall{
		{ID: "s1", Type: "function", Function: schema.FunctionCall{Name: around, Arguments: `{"keywords":"川味火锅"}`}},
		{ID: "s2", Type: "function", Function: schema.FunctionCall{Name: text, Arguments: `{"keywords":"四川火锅"}`}},
	}}}), nil
}

func t16Service(t *testing.T, model *t16Model, mcp *t16MCP) *Service {
	t.Helper()
	svc, err := NewService(Deps{LLM: model, MCP: mcp, Addresses: &fakeAddrDAO{}, Prompt: "system",
		Budgets: config.AgentConfig{AllowedTools: []string{agent.ToolNamePOI2Around, agent.ToolNamePOI2Text},
			MaxSteps: 10, MaxModelCalls: 5, MaxToolCalls: 3, TimeoutSeconds: 5}})
	require.NoError(t, err)
	return svc
}

func t16Body(t *testing.T, messages []ChatMessage, preferences map[string]any, selection any) string {
	t.Helper()
	body := map[string]any{"messages": messages, "location": t16Origin, "preferences": preferences}
	if selection != nil {
		body["selection"] = selection
	}
	data, err := json.Marshal(body)
	require.NoError(t, err)
	return string(data)
}

func t16Prefs(radius int, food any) map[string]any {
	return map[string]any{"target_count": 3, "radius_meters": radius, "meal_style": "any", "max_price_cny": nil, "allow_unknown_price": true, "food_requirement": food}
}

type t16State struct {
	State string `json:"state"`
}

type t16Option struct {
	OptionID    string                     `json:"option_id"`
	Label       string                     `json:"label"`
	ObservedIDs []string                   `json:"observed_ids"`
	Changes     map[string]json.RawMessage `json:"changes"`
}

type t16Decision struct {
	Status      string   `json:"status"`
	SelectedIDs []string `json:"selected_ids"`
	ReasonCodes []string `json:"reason_codes"`
	Assessments []struct {
		CandidateID string   `json:"candidate_id"`
		Range       t16State `json:"range"`
		Food        t16State `json:"food"`
		Price       t16State `json:"price"`
	} `json:"assessments"`
	Options            []t16Option     `json:"options"`
	FoodInterpretation json.RawMessage `json:"food_interpretation"`
	Conflict           json.RawMessage `json:"conflict"`
	Candidates         []struct {
		CandidateID    string   `json:"candidate_id"`
		DistanceMeters *float64 `json:"distance_meters"`
	} `json:"candidates"`
}

type t16Done struct {
	AnswerText  string            `json:"answer_text"`
	Places      []Place           `json:"places"`
	PlacesCount int               `json:"places_count"`
	Status      string            `json:"status"`
	Preferences agent.Preferences `json:"preferences"`
	Decision    json.RawMessage   `json:"decision"`
}

type t16Result struct {
	Code     int
	Raw      string
	Events   string
	Decision t16Decision
	Done     t16Done
}

func t16Run(t *testing.T, svc *Service, body string) t16Result {
	t.Helper()
	recorder := serveChat(t, svc, context.Background(), body)
	result := t16Result{Code: recorder.Code, Raw: recorder.Body.String()}
	events := parseSSE(result.Raw)
	result.Events = eventNames(events)
	for _, event := range events {
		switch event.name {
		case "decision":
			require.NoError(t, json.Unmarshal(event.data, &result.Decision))
		case "done":
			require.NoError(t, json.Unmarshal(event.data, &result.Done))
			require.JSONEq(t, string(events[0].data), string(result.Done.Decision), "done.decision 必须与 decision 事件同一快照")
		}
	}
	return result
}

func t16Keys(places []Place) []string {
	keys := make([]string, 0, len(places))
	for _, place := range places {
		keys = append(keys, place.CandidateKey)
	}
	return keys
}

func t16States(d t16Decision, id string) [3]string {
	for _, a := range d.Assessments {
		if a.CandidateID == id {
			return [3]string{a.Range.State, a.Food.State, a.Price.State}
		}
	}
	return [3]string{}
}

func t16Interpretation(requirement, quote string, ambiguous bool) map[string]any {
	return map[string]any{"food_interpretation": map[string]any{"requirement": requirement, "quote": quote, "ambiguous": ambiguous}}
}

// 模型从原话理解到“四川火锅”，按理解后的事实给出两项协商方案（服务端重算三态并校验）。
func t16Negotiate(quote string) t16Decide {
	return func(d agent.RecommendationDecision) (agent.RecommendationDecision, map[string]any) {
		food, radius := "冒菜", 4000
		d.Status, d.SelectedIDs, d.ReasonCodes = agent.DecisionNeedsChoice, []string{}, []agent.ReasonCode{agent.ReasonPreferencesConflict}
		d.Options = []agent.RecommendationOption{
			{OptionID: "keep-2km-swap", Label: "保持2公里，改吃冒菜", ObservedIDs: []string{"poi:BMAOCAI1"},
				ReasonCodes: []agent.ReasonCode{agent.ReasonFoodMismatch}, Changes: agent.PreferenceChanges{FoodRequirement: agent.NullableChange[string]{Present: true, Value: &food}}},
			{OptionID: "keep-sichuan-4km", Label: "保留四川火锅，扩大到4公里", ObservedIDs: []string{"poi:BFARHOT1"},
				ReasonCodes: []agent.ReasonCode{agent.ReasonOutsideRadius}, Changes: agent.PreferenceChanges{RadiusMeters: &radius}},
		}
		return d, t16Interpretation("四川火锅", quote, false)
	}
}

// 核心两轮：真实首轮请求 food_requirement=null + 原话“2公里内的四川火锅” → needs_choice 零卡片两方案 → 选择 4km → ready 仅川锅。
// quote 为品类本身或包含品类的完整原话均可（requirement 须是 quote 的子串）。
func TestT16_NullFoodTwoRoundNegotiation(t *testing.T) {
	for _, quote := range []string{" 四川火锅 ", t16Text} {
		t.Run(quote, func(t *testing.T) { t16TwoRound(t, quote) })
	}
}

func t16TwoRound(t *testing.T, quote string) {
	mcp := &t16MCP{around: t16POIs(t16Maocai, t16Guizhou), text: t16POIs(t16Far, t16Maocai)}
	model := &t16Model{decide: func(d agent.RecommendationDecision) (agent.RecommendationDecision, map[string]any) {
		if d.Status == agent.DecisionReady && len(d.SelectedIDs) == 2 { // 第一轮：预评估在品类为空时误判 ready
			return t16Negotiate(quote)(d)
		}
		return d, nil
	}}
	svc := t16Service(t, model, mcp)
	prefs1 := t16Prefs(2000, nil)

	first := t16Run(t, svc, t16Body(t, []ChatMessage{{Role: "user", Content: t16Text}}, prefs1, nil))
	require.Equal(t, http.StatusOK, first.Code, first.Raw)
	require.Equal(t, "decision,delta,delta,done", first.Events, first.Raw)
	require.Equal(t, "needs_choice", first.Decision.Status)
	require.Empty(t, first.Decision.SelectedIDs)
	require.Empty(t, first.Done.Places)
	require.Zero(t, first.Done.PlacesCount)
	require.NotContains(t, first.Raw, "010-SECRET")
	require.Contains(t, model.decisionInput, t16Text, "决策阶段须看到最新用户原话以便引用")
	require.Equal(t, 2000, mcp.calls[0].Args["radius"])
	ids := []string{}
	for _, option := range first.Decision.Options {
		ids = append(ids, option.OptionID)
	}
	require.ElementsMatch(t, []string{"keep-2km-swap", "keep-sichuan-4km"}, ids)
	// 价格为高德参考人均（缺币种/口径），按人民币人均参与比较；本轮不限价故为 satisfied。
	require.Equal(t, [3]string{"unsatisfied", "satisfied", "satisfied"}, t16States(first.Decision, "poi:BFARHOT1"))
	require.Equal(t, [3]string{"satisfied", "unsatisfied", "satisfied"}, t16States(first.Decision, "poi:BMAOCAI1"), "服务端按原话品类重算三态")
	require.JSONEq(t, `{"requirement":"四川火锅","quote":"`+quote+`","ambiguous":false}`, string(first.Decision.FoodInterpretation))
	require.NotNil(t, first.Done.Preferences.FoodRequirement)
	require.Equal(t, "四川火锅", *first.Done.Preferences.FoodRequirement, "done.preferences 写入被采纳的品类")
	require.Equal(t, 2000, first.Done.Preferences.RadiusMeters)
	t.Logf("T16EVIDENCE round1 events=%s status=%s options=%v food=%s", first.Events, first.Decision.Status, ids, *first.Done.Preferences.FoodRequirement)

	var chosen t16Option
	for _, option := range first.Decision.Options {
		if option.OptionID == "keep-sichuan-4km" {
			chosen = option
		}
	}
	require.Equal(t, map[string]json.RawMessage{"radius_meters": json.RawMessage("4000")}, chosen.Changes)
	previous := t16Prefs(2000, "四川火锅") // 前端下一轮 previous_preferences 取 done.preferences
	selection := map[string]any{"option_id": chosen.OptionID, "previous_preferences": previous, "changes": chosen.Changes}
	mcp.calls, model.stages = nil, nil
	second := t16Run(t, svc, t16Body(t, []ChatMessage{{Role: "user", Content: t16Text}, {Role: "assistant", Content: first.Done.AnswerText},
		{Role: "user", Content: "我选择方案：" + chosen.Label}}, t16Prefs(4000, "四川火锅"), selection))
	require.Equal(t, "decision,delta,delta,done", second.Events, second.Raw)
	require.Equal(t, 4000, mcp.calls[0].Args["radius"], "必须以4000米重新检索")
	require.Equal(t, "ready", second.Done.Status)
	require.Equal(t, []string{"poi:BFARHOT1"}, second.Decision.SelectedIDs)
	require.Equal(t, second.Decision.SelectedIDs, t16Keys(second.Done.Places))
	require.Contains(t, second.Decision.ReasonCodes, "insufficient_count")
	require.Empty(t, second.Decision.FoodInterpretation)
	require.Equal(t, "四川火锅", *second.Done.Preferences.FoodRequirement)
	for _, rejected := range []string{"巷子冒菜", "黔味酸汤"} {
		require.NotContains(t, model.finalInput, rejected, "正文模型不得接触被拒绝候选")
	}
	t.Logf("T16EVIDENCE round2 events=%s status=%s selected=%v radius=%v", second.Events, second.Done.Status, second.Decision.SelectedIDs, mcp.calls[0].Args["radius"])
}

// 品类理解的引用必须来自最新用户消息：不在原话、只在助手/更早历史、或请求已有品类时一律受控 error 且无正文。
func TestT16_InterpretationRejectedUnlessQuotedFromLatestUserMessage(t *testing.T) {
	cases := []struct {
		name     string
		messages []ChatMessage
		food     any
		quote    string
	}{
		{"quote_not_in_text", []ChatMessage{{Role: "user", Content: t16Text}}, nil, "重庆火锅"},
		{"quote_only_in_assistant_history", []ChatMessage{{Role: "user", Content: "附近吃点"}, {Role: "assistant", Content: "可以试试四川火锅"}, {Role: "user", Content: "2公里内的吧"}}, nil, "四川火锅"},
		{"request_already_has_food", []ChatMessage{{Role: "user", Content: t16Text}}, "四川火锅", "四川火锅"},
		{"empty_quote", []ChatMessage{{Role: "user", Content: t16Text}}, nil, "   "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mcp := &t16MCP{around: t16POIs(t16Maocai, t16Guizhou), text: t16POIs(t16Far)}
			model := &t16Model{decide: t16Negotiate(tc.quote)}
			result := t16Run(t, t16Service(t, model, mcp), t16Body(t, tc.messages, t16Prefs(2000, tc.food), nil))
			require.Equal(t, "error", result.Events, result.Raw)
			require.NotContains(t, model.stages, "final")
			require.NotContains(t, result.Raw, "event:delta")
		})
	}
}

// 含糊：模型标记 ambiguous → 澄清型 needs_choice，品类不写入 done.preferences。
func TestT16_AmbiguousInterpretationClarifies(t *testing.T) {
	mcp := &t16MCP{around: t16POIs(t16Maocai, t16Guizhou), text: t16POIs(t16Far)}
	model := &t16Model{decide: func(d agent.RecommendationDecision) (agent.RecommendationDecision, map[string]any) {
		food := "四川火锅"
		d.Status, d.SelectedIDs, d.ReasonCodes = agent.DecisionNeedsChoice, []string{}, []agent.ReasonCode{agent.ReasonPreferencesConflict}
		d.Options = []agent.RecommendationOption{{OptionID: "sichuan-hotpot", Label: "是指四川火锅", ObservedIDs: []string{}, ReasonCodes: []agent.ReasonCode{},
			Changes: agent.PreferenceChanges{FoodRequirement: agent.NullableChange[string]{Present: true, Value: &food}}}}
		return d, t16Interpretation("川味", "川味", true)
	}}
	result := t16Run(t, t16Service(t, model, mcp), t16Body(t, []ChatMessage{{Role: "user", Content: "2公里内来点川味"}}, t16Prefs(2000, nil), nil))
	require.Equal(t, "decision,delta,delta,done", result.Events, result.Raw)
	require.Equal(t, "needs_choice", result.Done.Status)
	require.Empty(t, result.Done.Places)
	require.Nil(t, result.Done.Preferences.FoodRequirement, "含糊理解不得写入品类")
}

func t16ConflictDecide(conflict map[string]any, optionRadius int) t16Decide {
	return func(d agent.RecommendationDecision) (agent.RecommendationDecision, map[string]any) {
		d.Status, d.SelectedIDs, d.ReasonCodes = agent.DecisionNeedsChoice, []string{}, []agent.ReasonCode{agent.ReasonPreferencesConflict}
		d.Options = []agent.RecommendationOption{{OptionID: "use-5km", Label: "按文字改为5公里", ObservedIDs: []string{},
			ReasonCodes: []agent.ReasonCode{}, Changes: agent.PreferenceChanges{RadiusMeters: &optionRadius}}}
		if conflict == nil {
			return d, nil
		}
		return d, map[string]any{"conflict": conflict}
	}
}

// 文字“5公里”与选单 2km 冲突：带可校验 conflict 时即使预评估 ready 也允许澄清；任何校验不过仍拒绝。
func TestT16_TextControlConflict(t *testing.T) {
	near := `{"id":"BNOODLE1","name":"近处面馆","location":"104.06,30.679","type":"餐饮服务;中餐厅;面馆","business":{"tag":"面馆"}}`
	text := "要5公里内的面馆"
	run := func(t *testing.T, decide t16Decide) (t16Result, *t16Model) {
		model := &t16Model{decide: decide}
		mcp := &t16MCP{around: t16POIs(near), text: t16POIs()}
		return t16Run(t, t16Service(t, model, mcp), t16Body(t, []ChatMessage{{Role: "user", Content: text}}, t16Prefs(2000, nil), nil)), model
	}
	t.Run("clarifies", func(t *testing.T) {
		result, _ := run(t, t16ConflictDecide(map[string]any{"field": "radius_meters", "quote": "5公里", "proposed": map[string]any{"radius_meters": 5000}}, 5000))
		require.Equal(t, "decision,delta,delta,done", result.Events, result.Raw)
		require.Equal(t, "needs_choice", result.Done.Status)
		require.Empty(t, result.Done.Places)
		require.JSONEq(t, `{"field":"radius_meters","quote":"5公里","proposed":{"radius_meters":5000}}`, string(result.Decision.Conflict))
		require.Equal(t, 2000, result.Done.Preferences.RadiusMeters, "冲突只澄清，不擅自采用文字值")
	})
	for name, conflict := range map[string]map[string]any{
		"quote_not_in_text":    {"field": "radius_meters", "quote": "8公里", "proposed": map[string]any{"radius_meters": 5000}},
		"proposed_other_field": {"field": "radius_meters", "quote": "5公里", "proposed": map[string]any{"target_count": 5}},
		"proposed_two_fields":  {"field": "radius_meters", "quote": "5公里", "proposed": map[string]any{"radius_meters": 5000, "target_count": 5}},
		"proposed_invalid":     {"field": "radius_meters", "quote": "5公里", "proposed": map[string]any{"radius_meters": 5050}},
		"proposed_unchanged":   {"field": "radius_meters", "quote": "5公里", "proposed": map[string]any{"radius_meters": 2000}},
		"unknown_field_name":   {"field": "location", "quote": "5公里", "proposed": map[string]any{"radius_meters": 5000}},
		"unknown_key":          {"field": "radius_meters", "quote": "5公里", "proposed": map[string]any{"radius_meters": 5000}, "why": "x"},
	} {
		t.Run(name, func(t *testing.T) {
			result, model := run(t, t16ConflictDecide(conflict, 5000))
			require.Equal(t, "error", result.Events, result.Raw)
			require.NotContains(t, model.stages, "final")
		})
	}
	t.Run("no_option_carries_proposed", func(t *testing.T) {
		result, _ := run(t, t16ConflictDecide(map[string]any{"field": "radius_meters", "quote": "5公里", "proposed": map[string]any{"radius_meters": 5000}}, 6000))
		require.Equal(t, "error", result.Events, result.Raw)
	})
}

// 无冲突、无品类理解时，模型在预评估 ready 下返回 needs_choice 仍被拒绝；模型擅自 ready 替代品类亦被拒绝。
func TestT16_ModelCannotBypassPolicy(t *testing.T) {
	t.Run("needs_choice_without_conflict", func(t *testing.T) {
		mcp := &t16MCP{around: t16POIs(t16Maocai, t16Guizhou), text: t16POIs(t16Far, t16Maocai)}
		model := &t16Model{decide: func(d agent.RecommendationDecision) (agent.RecommendationDecision, map[string]any) {
			d, _ = t16Negotiate("四川火锅")(d)
			d.ReasonCodes = []agent.ReasonCode{agent.ReasonOutsideRadius}
			return d, nil
		}}
		result := t16Run(t, t16Service(t, model, mcp), t16Body(t, []ChatMessage{{Role: "user", Content: t16Text}}, t16Prefs(2000, nil), nil))
		require.Equal(t, "error", result.Events, result.Raw)
		require.NotContains(t, model.stages, "final")
	})
	t.Run("ready_substitute_after_interpretation", func(t *testing.T) {
		mcp := &t16MCP{around: t16POIs(t16Maocai, t16Guizhou), text: t16POIs(t16Far)}
		model := &t16Model{decide: func(d agent.RecommendationDecision) (agent.RecommendationDecision, map[string]any) {
			d.Status, d.SelectedIDs, d.ReasonCodes, d.Options = agent.DecisionReady, []string{"poi:BMAOCAI1"}, []agent.ReasonCode{agent.ReasonInsufficientCount}, []agent.RecommendationOption{}
			return d, t16Interpretation("四川火锅", "四川火锅", false)
		}}
		result := t16Run(t, t16Service(t, model, mcp), t16Body(t, []ChatMessage{{Role: "user", Content: t16Text}}, t16Prefs(2000, nil), nil))
		// 2026-10-24 起不可选的选中项被服务端剔除而非整体报错；剔除后无合格候选 → needs_choice 零卡片，替代品类仍到不了用户。
		require.Equal(t, "decision,delta,delta,done", result.Events, result.Raw)
		require.Equal(t, "needs_choice", result.Done.Status)
		require.Empty(t, result.Done.Places)
		require.Equal(t, []string{"preferences_conflict"}, result.Decision.ReasonCodes)
		require.NotContains(t, model.finalInput, "巷子冒菜")
	})
}

// 复核16（M1/L1/L3）：经真实 handler 的绕过尝试一律受控 error、不进入正文；L2 见 TestT16_BlankLatestUserRejectedBeforeStream（开流前 400）。
func TestT16_Review16BypassesRejected(t *testing.T) {
	near := `{"id":"BNOODLE1","name":"近处面馆","location":"104.06,30.679","type":"餐饮服务;中餐厅;面馆","business":{"tag":"面馆"}}`
	readyMaocai := func(extra map[string]any) t16Decide {
		return func(d agent.RecommendationDecision) (agent.RecommendationDecision, map[string]any) {
			d.Status, d.SelectedIDs, d.Options = agent.DecisionReady, []string{"poi:BMAOCAI1"}, []agent.RecommendationOption{}
			d.ReasonCodes = []agent.ReasonCode{agent.ReasonInsufficientCount}
			return d, extra
		}
	}
	missingAmbiguous := func(d agent.RecommendationDecision) (agent.RecommendationDecision, map[string]any) {
		d, _ = t16Negotiate("四川火锅")(d)
		return d, map[string]any{"food_interpretation": map[string]any{"requirement": "四川火锅", "quote": "四川火锅"}}
	}
	user := []ChatMessage{{Role: "user", Content: t16Text}}
	cases := []struct {
		name     string
		around   string
		messages []ChatMessage
		decide   t16Decide
	}{
		{"M1_requirement_not_in_quote", t16POIs(t16Maocai, t16Guizhou), user, readyMaocai(t16Interpretation("冒菜", "四川火锅", false))},
		{"M1_quote_single_char", t16POIs(t16Maocai, t16Guizhou), user, readyMaocai(t16Interpretation("冒菜", "的", false))},
		{"L1_conflict_value_not_in_quote", t16POIs(near), []ChatMessage{{Role: "user", Content: "附近的面馆"}},
			t16ConflictDecide(map[string]any{"field": "radius_meters", "quote": "面馆", "proposed": map[string]any{"radius_meters": 50000}}, 50000)},
		{"L3_missing_ambiguous", t16POIs(t16Maocai, t16Guizhou), user, missingAmbiguous},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			model := &t16Model{decide: tc.decide}
			mcp := &t16MCP{around: tc.around, text: t16POIs(t16Far)}
			result := t16Run(t, t16Service(t, model, mcp), t16Body(t, tc.messages, t16Prefs(2000, nil), nil))
			require.Equal(t, "error", result.Events, result.Raw)
			require.NotContains(t, model.stages, "final")
			require.NotContains(t, result.Raw, "event:delta")
		})
	}
}

// 品类理解后仍有范围内合格候选：ready 只含合格项，done.preferences 写入品类。
func TestT16_InterpretationReadyKeepsOnlyMatchingCandidates(t *testing.T) {
	near := `{"id":"BNEARHOT1","name":"近处川锅","location":"104.06,30.68","type":"餐饮服务;中餐厅;火锅店","business":{"tag":"四川火锅"}}`
	mcp := &t16MCP{around: t16POIs(t16Maocai, near), text: t16POIs()}
	model := &t16Model{decide: func(d agent.RecommendationDecision) (agent.RecommendationDecision, map[string]any) {
		d.SelectedIDs, d.ReasonCodes = []string{"poi:BNEARHOT1"}, []agent.ReasonCode{agent.ReasonInsufficientCount}
		return d, t16Interpretation("四川火锅", "四川火锅", false)
	}}
	result := t16Run(t, t16Service(t, model, mcp), t16Body(t, []ChatMessage{{Role: "user", Content: t16Text}}, t16Prefs(2000, nil), nil))
	require.Equal(t, "decision,delta,delta,done", result.Events, result.Raw)
	require.Equal(t, []string{"poi:BNEARHOT1"}, t16Keys(result.Done.Places))
	require.Equal(t, agent.MatchSatisfied, result.Done.Places[0].Evaluation.Food.State)
	require.Equal(t, "四川火锅", *result.Done.Preferences.FoodRequirement)
	require.NotContains(t, model.finalInput, "巷子冒菜")
}

// 复核N1：requirement 前同一子句紧邻处有否定词（“不要冒菜”）时不得按非含糊采纳；未被否定的品类照常采纳。
func TestT16_NegatedRequirementRejected(t *testing.T) {
	near := `{"id":"BNEARHOT1","name":"近处川锅","location":"104.06,30.68","type":"餐饮服务;中餐厅;火锅店","business":{"tag":"四川火锅"}}`
	run := func(t *testing.T, text, requirement, selected string) (t16Result, *t16Model) {
		mcp := &t16MCP{around: t16POIs(t16Maocai, near), text: t16POIs()}
		model := &t16Model{decide: func(d agent.RecommendationDecision) (agent.RecommendationDecision, map[string]any) {
			d.Status, d.SelectedIDs, d.Options = agent.DecisionReady, []string{selected}, []agent.RecommendationOption{}
			d.ReasonCodes = []agent.ReasonCode{agent.ReasonInsufficientCount}
			return d, t16Interpretation(requirement, text, false)
		}}
		return t16Run(t, t16Service(t, model, mcp), t16Body(t, []ChatMessage{{Role: "user", Content: text}}, t16Prefs(2000, nil), nil)), model
	}
	t.Run("negated_maocai_ready", func(t *testing.T) {
		result, model := run(t, "2公里内不要冒菜要四川火锅", "冒菜", "poi:BMAOCAI1")
		require.Equal(t, "error", result.Events, result.Raw)
		require.NotContains(t, model.stages, "final")
	})
	t.Run("sichuan_hotpot_ready", func(t *testing.T) {
		result, _ := run(t, "2公里内不要冒菜要四川火锅", "四川火锅", "poi:BNEARHOT1")
		require.Equal(t, "decision,delta,delta,done", result.Events, result.Raw)
		require.Equal(t, []string{"poi:BNEARHOT1"}, t16Keys(result.Done.Places))
		require.Equal(t, "四川火锅", *result.Done.Preferences.FoodRequirement)
	})
	// 复核F2-N1：“特别”里的“别”、“有没有”里的“没有”不是否定。
	for _, text := range []string{"特别想吃火锅", "附近有没有火锅"} {
		t.Run("affirmative_"+text, func(t *testing.T) {
			result, _ := run(t, text, "火锅", "poi:BNEARHOT1")
			require.Equal(t, "decision,delta,delta,done", result.Events, result.Raw)
			require.Equal(t, []string{"poi:BNEARHOT1"}, t16Keys(result.Done.Places))
			require.Equal(t, "火锅", *result.Done.Preferences.FoodRequirement)
		})
	}
}

// 未知价格：默认 allow_unknown_price=true 可选但不透出价格；=false 时不可选；与任务16改动无关的行为保持不变。
func TestT16_UnknownPricePolicyUnchanged(t *testing.T) {
	run := func(t *testing.T, around string, prefs map[string]any) t16Result {
		mcp := &t16MCP{around: around, text: t16POIs()}
		return t16Run(t, t16Service(t, &t16Model{}, mcp), t16Body(t, []ChatMessage{{Role: "user", Content: "附近吃点"}}, prefs, nil))
	}
	t.Run("default_allow_true", func(t *testing.T) {
		result := run(t, t16POIs(t16Unknown), map[string]any{"target_count": 1, "radius_meters": 2000, "max_price_cny": 50})
		require.Equal(t, "ready", result.Done.Status, result.Raw)
		require.True(t, result.Done.Preferences.AllowUnknownPrice)
		require.Nil(t, result.Done.Preferences.FoodRequirement)
		require.Equal(t, agent.MatchUnknown, result.Done.Places[0].Evaluation.Price.State)
		require.NotContains(t, result.Raw, `约30`)
	})
	t.Run("explicit_false_only_unknown", func(t *testing.T) {
		prefs := t16Prefs(2000, nil)
		prefs["allow_unknown_price"], prefs["max_price_cny"], prefs["target_count"] = false, 50, 1
		result := run(t, t16POIs(t16Unknown), prefs)
		require.Equal(t, "needs_choice", result.Done.Status, result.Raw)
		require.Empty(t, result.Done.Places)
		require.False(t, result.Done.Preferences.AllowUnknownPrice)
	})
	t.Run("explicit_false_with_verified", func(t *testing.T) {
		prefs := t16Prefs(2000, nil)
		prefs["allow_unknown_price"], prefs["max_price_cny"] = false, 50
		result := run(t, t16POIs(t16Unknown, t16Known), prefs)
		require.Equal(t, "ready", result.Done.Status, result.Raw)
		require.Equal(t, []string{"poi:BKNOWN1"}, t16Keys(result.Done.Places))
		require.Contains(t, result.Decision.ReasonCodes, "insufficient_count")
	})
}

// 复核16 L2 收尾：末条用户消息去空白为空时开流前 400，零模型/地图调用，quote 不会回退到更早用户原话。
func TestT16_BlankLatestUserRejectedBeforeStream(t *testing.T) {
	for name, messages := range map[string][]ChatMessage{
		"consecutive_user":        {{Role: "user", Content: t16Text}, {Role: "user", Content: "   "}},
		"after_assistant":         {{Role: "user", Content: t16Text}, {Role: "assistant", Content: "好的"}, {Role: "user", Content: "   "}},
		"blank_assistant_between": {{Role: "user", Content: t16Text}, {Role: "assistant", Content: " "}, {Content: "\t\n"}},
	} {
		t.Run(name, func(t *testing.T) {
			model := &t16Model{decide: t16Negotiate("四川火锅")}
			mcp := &t16MCP{around: t16POIs(t16Maocai, t16Guizhou), text: t16POIs(t16Far)}
			result := t16Run(t, t16Service(t, model, mcp), t16Body(t, messages, t16Prefs(2000, nil), nil))
			require.Equal(t, http.StatusBadRequest, result.Code, result.Raw)
			require.Empty(t, model.stages)
			require.Empty(t, mcp.calls)
		})
	}
	t.Run("blank_assistant_last_still_allowed", func(t *testing.T) {
		messages := []ChatMessage{{Role: "user", Content: t16Text}, {Role: "assistant", Content: "  "}}
		result := t16Run(t, t16Service(t, &t16Model{decide: t16Negotiate("四川火锅")}, &t16MCP{around: t16POIs(t16Maocai, t16Guizhou), text: t16POIs(t16Far)}),
			t16Body(t, messages, t16Prefs(2000, nil), nil))
		require.Equal(t, http.StatusOK, result.Code, result.Raw)
	})
}
