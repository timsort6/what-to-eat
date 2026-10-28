package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// 品类理解/冲突字段的严格 schema：以一个本应通过的决策为基线，仅篡改新字段即被拒绝；显式 null 等同未填写。
func TestPolicy_FoodInterpretationSchemaIsStrictAndBounded(t *testing.T) {
	preferences := DefaultPreferences()
	preferences.RadiusMeters = 2000
	policy, err := NewRecommendationPolicy("104.06,30.67", preferences)
	require.NoError(t, err)
	require.NoError(t, policy.AddCandidates([]Place{
		{ID: "BMAOCAI1", Name: "巷子冒菜", Location: "104.06,30.679", Type: "餐饮服务;中餐厅", Tag: "冒菜"},
		{ID: "BFARHOT1", Name: "蜀地老火锅", Location: "104.06,30.7055", Type: "餐饮服务;中餐厅;火锅店", Tag: "四川火锅"},
	}))
	policy.latestUser = "2公里内的四川火锅"
	require.Equal(t, DecisionReady, policy.Evaluate().Status, "品类为空时预评估误判 ready")

	decision := policy.Evaluate()
	radius := 4000
	decision.Status, decision.SelectedIDs, decision.ReasonCodes = DecisionNeedsChoice, []string{}, []ReasonCode{ReasonPreferencesConflict}
	decision.Options = []RecommendationOption{{OptionID: "wider", Label: "扩大到4公里", ObservedIDs: []string{"poi:BFARHOT1"}, ReasonCodes: []ReasonCode{ReasonOutsideRadius}, Changes: PreferenceChanges{RadiusMeters: &radius}}}
	base, err := json.Marshal(decision)
	require.NoError(t, err)
	with := func(extra string) []byte { return []byte(strings.TrimSuffix(string(base), "}") + "," + extra + "}") }
	valid := `"food_interpretation":{"requirement":"四川火锅","quote":"四川火锅","ambiguous":false}`

	_, err = policy.ParseDecision(base)
	require.ErrorIs(t, err, ErrInvalidRecommendation, "无品类理解时预评估 ready 下的 needs_choice 仍被拒")
	parsed, err := policy.ParseDecision(with(valid + `,"conflict":null`))
	require.NoError(t, err)
	require.Equal(t, MatchUnsatisfied, parsed.Assessments[0].Food.State, "服务端按采纳品类重算三态")
	effective, err := policy.EffectivePreferences(parsed)
	require.NoError(t, err)
	require.Equal(t, "四川火锅", *effective.FoodRequirement)
	require.Nil(t, policy.preferences.FoodRequirement, "原策略条件不被改写")
	_, err = policy.SelectedPlaces(parsed)
	require.NoError(t, err, "规范化后的决策可再次通过校验")

	long := strings.Repeat("火", 101)
	for name, extra := range map[string]string{
		"unknown_nested_key":          `"food_interpretation":{"requirement":"四川火锅","quote":"四川火锅","ambiguous":false,"source":"x"}`,
		"case_variant_key":            `"food_interpretation":{"requirement":"四川火锅","Quote":"四川火锅","ambiguous":false}`,
		"requirement_too_long":        `"food_interpretation":{"requirement":"` + long + `","quote":"四川火锅","ambiguous":false}`,
		"quote_too_long":              `"food_interpretation":{"requirement":"四川火锅","quote":"` + long + `","ambiguous":false}`,
		"quote_wrong_type":            `"food_interpretation":{"requirement":"四川火锅","quote":1,"ambiguous":false}`,
		"quote_not_substring":         `"food_interpretation":{"requirement":"四川火锅","quote":"重庆火锅","ambiguous":false}`,
		"conflict_unknown_key":        valid + `,"conflict":{"field":"radius_meters","quote":"2公里","proposed":{"radius_meters":4000},"x":1}`,
		"proposed_unknown_key":        valid + `,"conflict":{"field":"radius_meters","quote":"2公里","proposed":{"radius":4000}}`,
		"top_level_unknown":           `"interpretation":{"requirement":"四川火锅","quote":"四川火锅","ambiguous":false}`,
		"missing_ambiguous":           `"food_interpretation":{"requirement":"四川火锅","quote":"四川火锅"}`,
		"missing_quote":               `"food_interpretation":{"requirement":"四川火锅","ambiguous":false}`,
		"missing_requirement":         `"food_interpretation":{"quote":"四川火锅","ambiguous":false}`,
		"requirement_null":            `"food_interpretation":{"requirement":null,"quote":"四川火锅","ambiguous":false}`,
		"conflict_value_not_in_quote": valid + `,"conflict":{"field":"radius_meters","quote":"2公里","proposed":{"radius_meters":4000}}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := policy.ParseDecision(with(extra))
			require.ErrorIs(t, err, ErrInvalidRecommendation)
		})
	}
	policy.latestUser = "4公里内的四川火锅"
	_, err = policy.ParseDecision(with(valid + `,"conflict":{"field":"radius_meters","quote":"4公里","proposed":{"radius_meters":4000}}`))
	require.NoError(t, err, "合法冲突与品类理解可并存")
}

// 复核16：requirement 须在 quote 内（去空白、全半角一致）；数值冲突的 quote 须含与 proposed 一致的数值；最新原话为空时任何 quote 都拒绝。
func TestPolicy_Review16QuoteBinding(t *testing.T) {
	interpretation := func(requirement, quote string) RecommendationDecision {
		return RecommendationDecision{Status: DecisionNeedsChoice, FoodInterpretation: &FoodInterpretation{Requirement: requirement, Quote: quote}}
	}
	conflict := func(field, quote string, proposed PreferenceChanges) RecommendationDecision {
		return RecommendationDecision{Status: DecisionNeedsChoice, Options: []RecommendationOption{{Changes: proposed}},
			Conflict: &PreferenceConflict{Field: field, Quote: quote, Proposed: proposed}}
	}
	radius := func(value int) PreferenceChanges { return PreferenceChanges{RadiusMeters: &value} }
	count := func(value int) PreferenceChanges { return PreferenceChanges{TargetCount: &value} }
	price := func(value int) PreferenceChanges {
		return PreferenceChanges{MaxPriceCNY: NullableChange[int]{Present: true, Value: &value}}
	}
	food := func(value string) PreferenceChanges {
		return PreferenceChanges{FoodRequirement: NullableChange[string]{Present: true, Value: &value}}
	}
	cases := []struct {
		name, latest string
		decision     RecommendationDecision
		ok           bool
	}{
		{"M1_requirement_not_in_quote", "2公里内的四川火锅", interpretation("冒菜", "四川火锅"), false},
		{"M1_quote_single_char", "2公里内的四川火锅", interpretation("冒菜", "的"), false},
		{"M1_requirement_in_full_quote", "2公里内的四川火锅", interpretation("四川火锅", "2公里内的四川火锅"), true},
		{"M1_width_and_space_normalized", "附近的ｋｆｃ 汉堡", interpretation(" kfc汉堡", "ｋｆｃ 汉堡"), true},
		{"L1_radius_without_number", "附近的面馆", conflict("radius_meters", "面馆", radius(50000)), false},
		{"L1_radius_value_mismatch", "5公里内的面馆", conflict("radius_meters", "5公里", radius(50000)), false},
		{"L1_radius_number_without_unit", "5内的面馆", conflict("radius_meters", "5内", radius(5000)), false},
		{"L1_radius_gongli", "5公里内的面馆", conflict("radius_meters", "5公里", radius(5000)), true},
		{"L1_radius_km", "5KM内的面馆", conflict("radius_meters", "5KM", radius(5000)), true},
		{"L1_radius_mi", "5000米内的面馆", conflict("radius_meters", "5000米", radius(5000)), true},
		{"L1_radius_fullwidth", "５公里内的面馆", conflict("radius_meters", "５公里", radius(5000)), true},
		{"L1_radius_decimal", "1.5公里内的面馆", conflict("radius_meters", "1.5公里", radius(1500)), true},
		{"L1_count_match", "找5家面馆", conflict("target_count", "5家", count(5)), true},
		{"L1_count_without_number", "找几家面馆", conflict("target_count", "几家", count(5)), false},
		{"L1_price_match", "人均80元以内", conflict("max_price_cny", "80元", price(80)), true},
		{"L1_price_mismatch", "人均80元以内", conflict("max_price_cny", "80元", price(30)), false},
		{"L1_food_in_quote", "想吃冒菜", conflict("food_requirement", "冒菜", food("冒菜")), true},
		{"L1_food_not_in_quote", "想吃冒菜", conflict("food_requirement", "冒菜", food("火锅")), false},
		{"L2_blank_latest", "   ", interpretation("四川火锅", "四川火锅"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			preferences := DefaultPreferences()
			preferences.RadiusMeters = 2000
			policy, err := NewRecommendationPolicy("104.06,30.67", preferences)
			require.NoError(t, err)
			policy.latestUser = tc.latest
			_, err = policy.EffectivePreferences(tc.decision)
			if tc.ok {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrInvalidRecommendation)
			}
		})
	}
}
