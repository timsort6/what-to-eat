package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// realfixOrigin 与 realfixPool 取自 2026-10-24 真实诊断 run2 的候选池形状（仅保留店名、坐标、高德分类/标签/人均，无电话与原始数据）。
const realfixOrigin = "116.397428,39.90923"

func realfixPool() []Place {
	nested := priceSources(true)
	return []Place{
		{ID: "B0KDMCC8R7", Name: "熊喵来了火锅(北京王府井喜悦店)", Location: "116.41031,39.910006", Type: "餐饮服务;中餐厅;火锅店", TypeCode: "050117", Tag: "菌香乌鸡卷,屠场水牛毛肚,杨枝甘露,羊肉,肥牛,豆腐,虾滑", Cost: "87.00", PriceSources: nested},
		{ID: "B0FFKQFIXP", Name: "铜炉蛙蛙叫.烤鱼牛蛙(合景摩方店)", Location: "116.417738,39.899723", Type: "餐饮服务;中餐厅;中餐厅", TypeCode: "050100", Tag: "馋嘴蛙,川渝麻辣火锅双人餐,牛蛙双层锅,香辣烤鱼", Cost: "70.00", PriceSources: nested},
		{ID: "B0JK7HFEBH", Name: "萍姐火锅(崇文门新世界百货2期店)", Location: "116.418244,39.896355", Type: "餐饮服务;中餐厅;火锅店", TypeCode: "050117", Tag: "双人餐,牛肉,8秒水爆肚", Cost: "72.00", PriceSources: nested},
		{ID: "B0GUJAP8NM", Name: "钢管厂五区小郡肝串串香(西单店)", Location: "116.372543,39.91409", Type: "餐饮服务;中餐厅;火锅店", TypeCode: "050117", Tag: "牛肉,单人餐,双人餐", Cost: "78.00", PriceSources: nested},
		{ID: "B0FFGCY0CW", Name: "码头汇重庆火锅(太平桥大街店)", Location: "116.366458,39.909383", Type: "餐饮服务;中餐厅;火锅店|餐饮服务;中餐厅;特色/地方风味餐厅", TypeCode: "050117|050118", Tag: "烤肉,毛肚,烤串,重庆小面,鸭肠,重庆火锅,面食,鸳鸯锅", Cost: "94.00", PriceSources: nested},
		{ID: "B000A1A549", Name: "海底捞火锅(牡丹园店)", Location: "116.368364,39.979011", Type: "餐饮服务;中餐厅;火锅店", TypeCode: "050117", Tag: "滑牛肉,一根面,毛肚,双人餐,牛肉", Cost: "120.00", PriceSources: nested},
	}
}

func realfixPolicy(t *testing.T, preferences Preferences) *RecommendationPolicy {
	t.Helper()
	policy, err := NewRecommendationPolicy(realfixOrigin, preferences)
	require.NoError(t, err)
	require.NoError(t, policy.AddCandidates(realfixPool()))
	policy.latestUser = "想吃四川火锅，人均100以内"
	return policy
}

func realfixArgs(selected string, assessments string) []byte {
	return []byte(`{"food_interpretation":{"ambiguous":false,"quote":"四川火锅","requirement":"四川火锅"},"conflict":null,"status":"ready","selected_ids":[` + selected + `]` + assessments + `,"reason_codes":[],"options":[]}`)
}

// 回归：真实推理模型按语义给出的三态与服务端不同（run1 形状），只要选中的是候选池内的合法候选，就以服务端三态为准并 ready 成功。
func TestRealFix_ReadyWhenModelAssessmentsDifferFromServer(t *testing.T) {
	policy := realfixPolicy(t, DefaultPreferences())
	mismatched := `,"assessments":[` +
		`{"candidate_id":"poi:B0FFGCY0CW","range":{"state":"satisfied","fields":["location"]},"food":{"state":"satisfied","fields":["tag","typecode"]},"price":{"state":"satisfied","fields":["business.cost"]}},` +
		`{"candidate_id":"poi:B0JK7HFEBH","range":{"state":"unknown","fields":[]},"food":{"state":"satisfied","fields":["name"]},"price":{"state":"unsatisfied","fields":["business.cost","business.currency"]}},` +
		`{"candidate_id":"poi:B0GUJAP8NM","range":{"state":"satisfied","fields":["location"]},"food":{"state":"satisfied","fields":["type"]},"price":{"state":"satisfied","fields":["business.cost"]}}]`
	run2 := `,"assessments":[` +
		`{"candidate_id":"poi:B0FFGCY0CW","range":{"state":"satisfied","fields":["location"]},"food":{"state":"unknown","fields":[]},"price":{"state":"satisfied","fields":["business.cost"]}},` +
		`{"candidate_id":"poi:B0JK7HFEBH","range":{"state":"satisfied","fields":["location"]},"food":{"state":"unknown","fields":[]},"price":{"state":"satisfied","fields":["business.cost"]}},` +
		`{"candidate_id":"poi:B0GUJAP8NM","range":{"state":"satisfied","fields":["location"]},"food":{"state":"unknown","fields":[]},"price":{"state":"satisfied","fields":["business.cost"]}}]`
	selected := `"poi:B0FFGCY0CW","poi:B0JK7HFEBH","poi:B0GUJAP8NM"`
	for name, assessments := range map[string]string{"三态与服务端不同": mismatched, "run2原样参数": run2, "省略assessments": ""} {
		t.Run(name, func(t *testing.T) {
			decision, err := policy.ParseDecision(realfixArgs(selected, assessments))
			require.NoError(t, err)
			require.Equal(t, DecisionReady, decision.Status)
			// 川渝同属一个地域组（2026-10-25 起）：码头汇 tag“重庆火锅”满足“四川火锅”，三家均保留，不再补 insufficient_count。
			require.Equal(t, []string{"poi:B0FFGCY0CW", "poi:B0JK7HFEBH", "poi:B0GUJAP8NM"}, decision.SelectedIDs)
			require.Empty(t, decision.ReasonCodes)
			effective, err := policy.EffectivePreferences(decision)
			require.NoError(t, err)
			require.Equal(t, "四川火锅", *effective.FoodRequirement)
			byID := map[string]CandidateAssessment{}
			for _, assessment := range decision.Assessments {
				byID[assessment.CandidateID] = assessment
			}
			for _, id := range decision.SelectedIDs {
				assessment, ok := byID[id]
				require.True(t, ok, "选中项必须带服务端三态: %s", id)
				require.Equal(t, MatchSatisfied, assessment.Range.State, id)
				require.Equal(t, MatchEvidence{State: MatchSatisfied, Fields: []EvidenceField{"business.cost"}}, assessment.Price, "高德参考人均: %s", id)
				if id != "poi:B0FFGCY0CW" {
					require.Equal(t, MatchUnknown, assessment.Food.State, "火锅店且 tag 只有菜名: %s", id)
				}
			}
			require.Equal(t, MatchEvidence{State: MatchSatisfied, Fields: []EvidenceField{"tag"}}, byID["poi:B0FFGCY0CW"].Food, "码头汇因 tag“重庆火锅”（川渝同组）判满足")
			encoded, err := json.Marshal(decision)
			require.NoError(t, err)
			var roundTrip RecommendationDecision
			require.NoError(t, json.Unmarshal(encoded, &roundTrip))
			places, err := policy.SelectedPlaces(roundTrip)
			require.NoError(t, err)
			require.Len(t, places, 3)
			require.Equal(t, "94.00", places[0].Cost)
		})
	}
}

func TestRealFix_FiltersUnselectableAndFallsBackByDistance(t *testing.T) {
	policy := realfixPolicy(t, DefaultPreferences())

	decision, err := policy.ParseDecision(realfixArgs(`"poi:B000A1A549","B0JK7HFEBH"`, ""))
	require.NoError(t, err, "范围外候选被过滤而不是整体拒绝")
	require.Equal(t, DecisionReady, decision.Status)
	require.Equal(t, []string{"poi:B0JK7HFEBH"}, decision.SelectedIDs, "POI ID 规范为候选键")
	require.Equal(t, []ReasonCode{ReasonInsufficientCount}, decision.ReasonCodes)

	decision, err = policy.ParseDecision(realfixArgs(`"poi:B000A1A549"`, ""))
	require.NoError(t, err)
	require.Equal(t, DecisionReady, decision.Status)
	require.Equal(t, []string{"poi:B0KDMCC8R7", "poi:B0FFKQFIXP", "poi:B0GUJAP8NM"}, decision.SelectedIDs, "过滤后为空时回退为服务端按距离选择")
	require.Empty(t, decision.ReasonCodes)

	price := 30
	strict := DefaultPreferences()
	strict.MaxPriceCNY = &price
	decision, err = realfixPolicy(t, strict).ParseDecision(realfixArgs(`"poi:B0JK7HFEBH"`, ""))
	require.NoError(t, err)
	require.Equal(t, DecisionNeedsChoice, decision.Status, "服务端无可选候选时转为 needs_choice")
	require.Empty(t, decision.SelectedIDs)
	require.Equal(t, []ReasonCode{ReasonPreferencesConflict}, decision.ReasonCodes)

	for name, selected := range map[string]string{"池外": `"poi:NOTEXIST"`, "重复": `"poi:B0JK7HFEBH","B0JK7HFEBH"`, "超过目标数": `"poi:B0KDMCC8R7","poi:B0FFKQFIXP","poi:B0GUJAP8NM","poi:B0JK7HFEBH"`} {
		_, err := policy.ParseDecision(realfixArgs(selected, ""))
		require.ErrorIs(t, err, ErrInvalidRecommendation, name)
	}
}

func TestRealFix_ReasonCodesFilteredAndStrictnessKept(t *testing.T) {
	policy := realfixPolicy(t, DefaultPreferences())
	data := strings.Replace(string(realfixArgs(`"poi:B0JK7HFEBH","poi:B0GUJAP8NM","poi:B0KDMCC8R7"`, "")), `"reason_codes":[]`, `"reason_codes":["over_budget","unknown_price","outside_radius","insufficient_count"]`, 1)
	decision, err := policy.ParseDecision([]byte(data))
	require.NoError(t, err)
	require.Equal(t, []ReasonCode{ReasonOutsideRadius}, decision.ReasonCodes, "只保留服务端可证实的原因码")

	for name, mutate := range map[string]func(string) string{
		"未知顶层字段": func(s string) string { return strings.Replace(s, `"options":[]`, `"options":[],"raw":{}`, 1) },
		"assessments未知字段": func(s string) string {
			return strings.Replace(s, `"selected_ids"`, `"assessments":[{"candidate_id":"poi:B0JK7HFEBH","x":1}],"selected_ids"`, 1)
		},
		"quote不在原话": func(s string) string {
			return strings.Replace(s, `"quote":"四川火锅"`, `"quote":"重庆火锅"`, 1)
		},
		"ready带options": func(s string) string {
			return strings.Replace(s, `"options":[]`, `"options":[{"option_id":"a","label":"b","observed_ids":[],"reason_codes":[],"changes":{"radius_meters":4000}}]`, 1)
		},
		"超过64KB": func(s string) string {
			return strings.Replace(s, `"reason_codes":[]`, `"reason_codes":[]`+strings.Repeat(" ", MaxDecisionBytes), 1)
		},
	} {
		_, err := policy.ParseDecision([]byte(mutate(string(realfixArgs(`"poi:B0JK7HFEBH"`, "")))))
		require.ErrorIs(t, err, ErrInvalidRecommendation, name)
	}
}

func TestRealFix_AmapReferencePrice(t *testing.T) {
	limit := func(value int) *int { return &value }
	place := func(cost, currency, basis string) Place {
		return normalizeCandidate(Place{ID: "P1", Name: "店", Location: "0,0", Cost: cost, CostCurrency: currency, CostBasis: basis, PriceSources: priceSources(true)})
	}
	reference := []EvidenceField{"business.cost"}
	for _, fixture := range []struct {
		name  string
		place Place
		max   *int
		want  MatchEvidence
	}{
		{"缺币种口径_不超限", place("94.00", "", ""), limit(100), MatchEvidence{State: MatchSatisfied, Fields: reference}},
		{"缺币种口径_超限", place("94.00", "", ""), limit(90), MatchEvidence{State: MatchUnsatisfied, Fields: reference}},
		{"缺币种口径_不限价", place("94.00", "", ""), nil, MatchEvidence{State: MatchSatisfied, Fields: reference}},
		{"非数字", place("约90", "", ""), limit(100), MatchEvidence{State: MatchUnknown, Fields: []EvidenceField{}}},
		{"空", place("", "", ""), limit(100), MatchEvidence{State: MatchUnknown, Fields: []EvidenceField{}}},
		{"明确外币", place("94", "USD", ""), limit(100), MatchEvidence{State: MatchUnknown, Fields: []EvidenceField{}}},
		{"已核实人民币人均", place("94", "CNY", "per_person"), limit(100), MatchEvidence{State: MatchSatisfied, Fields: []EvidenceField{"business.cost", "business.currency", "business.cost_basis"}}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			require.Equal(t, fixture.want, assessPrice(fixture.max, fixture.place))
		})
	}
	result, err := parseSearchResult(`{"status":"1","pois":[{"id":"B1","name":"店","location":"0,0","business":{"cost":"94.00"}},{"id":"B2","name":"店2","location":"0,0","cost":"88"}]}`, 10, RequestScope{Location: "0,0"})
	require.NoError(t, err)
	require.Equal(t, MatchSatisfied, assessPrice(limit(100), result.Places[0]).State, "business.cost")
	require.Equal(t, MatchSatisfied, assessPrice(limit(100), result.Places[1]).State, "顶层 cost")
}
