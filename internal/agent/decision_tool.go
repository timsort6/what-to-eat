package agent

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"
	orderedmap "github.com/wk8/go-ordered-map/v2"
)

const finalizeName = "finalize_recommendation"

type decisionTool struct {
	policy *RecommendationPolicy
}

func newDecisionTool(policy *RecommendationPolicy) tool.InvokableTool {
	return &decisionTool{policy: policy}
}

func (t *decisionTool) Info(context.Context) (*schema.ToolInfo, error) {
	one, optionIDLimit, foodLimit, labelLimit, candidateIDLimit := uint64(1), uint64(64), uint64(100), uint64(200), uint64(256)
	reasons := &jsonschema.Schema{Type: "string", Enum: []any{
		"no_candidates", "insufficient_count", "outside_radius", "food_mismatch",
		"over_budget", "unknown_price", "preferences_conflict", "unknown_evidence",
	}}
	id := &jsonschema.Schema{Type: "string", MinLength: &one, MaxLength: &candidateIDLimit}
	changes := decisionObject(map[string]*jsonschema.Schema{
		"target_count":        {Type: "integer", Minimum: "1", Maximum: "10"},
		"radius_meters":       {Type: "integer", Minimum: "100", Maximum: "50000", MultipleOf: "100"},
		"meal_style":          {Type: "string", Enum: []any{"any", "quick", "regular", "leisure"}},
		"max_price_cny":       {TypeEnhanced: []string{"integer", "null"}, Minimum: "1", Maximum: "10000"},
		"allow_unknown_price": {Type: "boolean"},
		"food_requirement":    {TypeEnhanced: []string{"string", "null"}, MaxLength: &foodLimit},
	})
	changes.Required = nil
	changes.MinProperties = &one
	option := decisionObject(map[string]*jsonschema.Schema{
		"option_id":    {Type: "string", MinLength: &one, MaxLength: &optionIDLimit},
		"label":        {Type: "string", MinLength: &one, MaxLength: &labelLimit},
		"observed_ids": decisionArray(id, 10), "reason_codes": decisionArray(reasons, 8), "changes": changes,
	})
	quoteLimit := uint64(MaxQuoteRunes)
	quote := &jsonschema.Schema{Type: "string", MinLength: &one, MaxLength: &quoteLimit, Description: "最新用户消息中的原文片段（逐字摘录）"}
	interpretation := decisionObject(map[string]*jsonschema.Schema{
		"requirement": {Type: "string", MinLength: &one, MaxLength: &foodLimit}, "quote": quote, "ambiguous": {Type: "boolean"},
	})
	interpretation.Type, interpretation.TypeEnhanced = "", []string{"object", "null"}
	interpretation.Description = "仅当 preferences.food_requirement 为 null 且最新用户消息提到品类时填写，否则为 null；含糊时 ambiguous=true 并返回 needs_choice 澄清。"
	conflict := decisionObject(map[string]*jsonschema.Schema{
		"field": {Type: "string", Enum: []any{"target_count", "radius_meters", "meal_style", "max_price_cny", "allow_unknown_price", "food_requirement"}},
		"quote": quote, "proposed": changes,
	})
	conflict.Type, conflict.TypeEnhanced = "", []string{"object", "null"}
	conflict.Description = "仅当最新用户消息与 preferences 明确冲突时填写，否则为 null；proposed 只含 field 一个键，且须有一个 option 的 changes 与之相同。"
	// 三态评估（assessments）由服务端计算，不再要求模型输出；为兼容旧输出，服务端仍接受并忽略该字段（见 ParseDecision）。
	contract := decisionObject(map[string]*jsonschema.Schema{
		"status":       {Type: "string", Enum: []any{"ready", "needs_choice"}},
		"selected_ids": decisionArray(id, 10),
		"reason_codes": decisionArray(reasons, 8), "options": decisionArray(option, 3),
		"food_interpretation": interpretation, "conflict": conflict,
	})
	contract.Description = "候选必须来自本次搜索；三态评估由服务端计算，无需提交。选择、原因和修改后的偏好必须通过服务端策略校验（服务端判为不可选的候选会被剔除）。ready 须选择至少一个候选且 options 为空；needs_choice 的 selected_ids 为空。food_interpretation/conflict 的 quote 必须逐字摘自 latest_user_message。"
	return &schema.ToolInfo{Name: finalizeName, Desc: "验证并完成推荐决策", ParamsOneOf: schema.NewParamsOneOfByJSONSchema(contract)}, nil
}

func decisionObject(properties map[string]*jsonschema.Schema) *jsonschema.Schema {
	names := make([]string, 0, len(properties))
	for name := range properties {
		names = append(names, name)
	}
	slices.Sort(names)
	object := &jsonschema.Schema{Type: "object", Properties: orderedmap.New[string, *jsonschema.Schema](), Required: names, AdditionalProperties: jsonschema.FalseSchema}
	for _, name := range names {
		object.Properties.Set(name, properties[name])
	}
	return object
}

func decisionArray(item *jsonschema.Schema, limit uint64) *jsonschema.Schema {
	return &jsonschema.Schema{Type: "array", Items: item, MaxItems: &limit, UniqueItems: true}
}

func (t *decisionTool) InvokableRun(_ context.Context, args string, _ ...tool.Option) (string, error) {
	decision, err := t.policy.ParseDecision([]byte(args))
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(decision)
	return string(encoded), err
}
