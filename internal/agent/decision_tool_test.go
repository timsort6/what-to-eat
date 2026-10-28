package agent

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"food-agent/pkg/config"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"
	"github.com/stretchr/testify/require"
)

func checkDecisionSchema(info *schema.ToolInfo) error {
	contract, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil {
		return err
	}
	var checkObject func(*jsonschema.Schema, reflect.Type) error
	checkObject = func(object *jsonschema.Schema, kind reflect.Type) error {
		// assessments 由服务端计算，不出现在模型契约中。
		serverOnly := map[bool]int{true: 1}[kind == reflect.TypeFor[RecommendationDecision]()]
		if object == nil || object.Type != "object" || object.Properties == nil || object.Properties.Len() != kind.NumField()-serverOnly || object.AdditionalProperties != jsonschema.FalseSchema {
			return fmt.Errorf("对象契约不完整: %s", kind.Name())
		}
		for index := range kind.NumField() {
			field := kind.Field(index)
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			if serverOnly == 1 && name == "assessments" {
				continue
			}
			property, ok := object.Properties.Get(name)
			if !ok || property == nil {
				return fmt.Errorf("缺少字段: %s.%s", kind.Name(), name)
			}
			if kind != reflect.TypeFor[PreferenceChanges]() && !slices.Contains(object.Required, name) {
				return fmt.Errorf("缺少必填约束: %s", name)
			}
			if kind == reflect.TypeFor[PreferenceChanges]() {
				continue
			}
			nested := field.Type
			if nested.Kind() == reflect.Slice {
				if property.Type != "array" || property.Items == nil || property.MaxItems == nil {
					return fmt.Errorf("数组契约不完整: %s", name)
				}
				property, nested = property.Items, nested.Elem()
			}
			if nested.Kind() == reflect.Struct {
				if err := checkObject(property, nested); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return checkObject(contract, reflect.TypeFor[RecommendationDecision]())
}

func TestDecisionTool_ModelReceivesNestedContract(t *testing.T) {
	checked := &atomic.Int32{}
	chat := &stagesModel{calls: &atomic.Int32{}, searchRounds: 1, decision: stagesDecision(t), final: []*schema.Message{schema.AssistantMessage("正文", nil)}, checkDecision: func(info *schema.ToolInfo) error {
		checked.Add(1)
		return checkDecisionSchema(info)
	}}
	runner, err := NewRunner("", config.AgentConfig{AllowedTools: []string{"find"}, MaxModelCalls: 3, MaxSteps: 6, MaxToolCalls: 3, TimeoutSeconds: 5}, chat, map[string]tool.InvokableTool{"find": &stagesTool{}})
	require.NoError(t, err)

	stream, err := runner.Stream(stagesContext(t.Context()), nil)
	require.NoError(t, err)
	defer stream.Close()
	chunk, err := stream.Recv()
	require.NoError(t, err)
	require.Equal(t, "正文", chunk.Content)
	_, err = stream.Recv()
	require.ErrorIs(t, err, io.EOF)
	require.EqualValues(t, 1, checked.Load())
}

func TestDecisionTool_RejectsStatusOnlyArguments(t *testing.T) {
	policy, err := NewRecommendationPolicy("0,0", DefaultPreferences())
	require.NoError(t, err)
	_, err = newDecisionTool(policy).InvokableRun(context.Background(), `{"status":"needs_choice"}`)
	require.ErrorIs(t, err, ErrInvalidRecommendation)
}

func TestDecisionTool_EnumsAndChangesMatchDTO(t *testing.T) {
	info, err := newDecisionTool(nil).Info(t.Context())
	require.NoError(t, err)
	contract, err := info.ParamsOneOf.ToJSONSchema()
	require.NoError(t, err)
	field := func(object *jsonschema.Schema, name string) *jsonschema.Schema {
		require.NotNil(t, object)
		require.NotNil(t, object.Properties)
		property, ok := object.Properties.Get(name)
		require.True(t, ok, name)
		return property
	}
	require.ElementsMatch(t, []any{"ready", "needs_choice"}, field(contract, "status").Enum)
	reasons := []any{"no_candidates", "insufficient_count", "outside_radius", "food_mismatch", "over_budget", "unknown_price", "preferences_conflict", "unknown_evidence"}
	require.ElementsMatch(t, reasons, field(contract, "reason_codes").Items.Enum)
	_, modelAssesses := contract.Properties.Get("assessments")
	require.False(t, modelAssesses, "三态由服务端计算，契约不再要求模型输出 assessments")
	require.NotContains(t, contract.Required, "assessments")
	option := field(contract, "options").Items
	require.ElementsMatch(t, reasons, field(option, "reason_codes").Items.Enum)
	changes := field(option, "changes")
	require.Empty(t, changes.Required)
	require.ElementsMatch(t, []any{"any", "quick", "regular", "leisure"}, field(changes, "meal_style").Enum)
	require.Equal(t, "integer", field(changes, "target_count").Type)
	require.Equal(t, "integer", field(changes, "radius_meters").Type)
	require.Equal(t, "boolean", field(changes, "allow_unknown_price").Type)
	require.ElementsMatch(t, []string{"integer", "null"}, field(changes, "max_price_cny").TypeEnhanced)
	require.ElementsMatch(t, []string{"string", "null"}, field(changes, "food_requirement").TypeEnhanced)
	for name, keys := range map[string][]string{"food_interpretation": {"ambiguous", "quote", "requirement"}, "conflict": {"field", "proposed", "quote"}} {
		object := field(contract, name)
		require.ElementsMatch(t, []string{"object", "null"}, object.TypeEnhanced)
		require.Equal(t, jsonschema.FalseSchema, object.AdditionalProperties)
		require.Equal(t, keys, object.Required)
		require.EqualValues(t, MaxQuoteRunes, *field(object, "quote").MaxLength)
	}
	require.EqualValues(t, 100, *field(field(contract, "food_interpretation"), "requirement").MaxLength)
}
