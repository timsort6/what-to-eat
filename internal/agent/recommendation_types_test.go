package agent

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestPreferenceChanges_Apply_whenExplicitValuesAndNulls(t *testing.T) {
	previous := DefaultPreferences()
	price, food := 50, "四川火锅"
	previous.MaxPriceCNY, previous.FoodRequirement = &price, &food
	var changes PreferenceChanges
	if err := json.Unmarshal([]byte(`{"target_count":2,"radius_meters":4000,"meal_style":"leisure","max_price_cny":null,"allow_unknown_price":false,"food_requirement":null}`), &changes); err != nil {
		t.Fatal(err)
	}
	next, err := changes.Apply(previous)
	if err != nil {
		t.Fatal(err)
	}
	want := Preferences{TargetCount: 2, RadiusMeters: 4000, MealStyle: MealLeisure, AllowUnknownPrice: false}
	if !reflect.DeepEqual(next, want) {
		t.Fatalf("得到 %+v，预期 %+v", next, want)
	}
	if previous.MaxPriceCNY == nil || *previous.MaxPriceCNY != 50 {
		t.Fatal("修改了原条件")
	}
}

func TestPreferenceChanges_PreserveOmission_whenRoundTripped(t *testing.T) {
	var changes PreferenceChanges
	if err := json.Unmarshal([]byte(`{"radius_meters":4000,"max_price_cny":null}`), &changes); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(changes)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 2 || string(fields["max_price_cny"]) != "null" {
		t.Fatalf("空值和省略混淆: %s", encoded)
	}
}

func TestPreferenceChanges_Reject_whenInputMalformed(t *testing.T) {
	for _, input := range []string{
		`null`, `[]`, `{`, `{} {}`, `{"address_id":2}`, `{"meal_style":"fast"}`,
		`{"target_count":-1}`, `{"target_count":0}`, `{"target_count":11}`, `{"target_count":1.5}`,
		`{"radius_meters":-100}`, `{"radius_meters":0}`, `{"radius_meters":50}`, `{"radius_meters":1234}`, `{"radius_meters":50100}`,
		`{"max_price_cny":0}`, `{"max_price_cny":10001}`, `{"max_price_cny":1.5}`, `{"max_price_cny":NaN}`,
		`{"allow_unknown_price":null}`, `{"allow_unknown_price":"true"}`, `{"meal_style":null}`, `{"target_count":null}`,
		`{"food_requirement":"` + strings.Repeat("锅", 101) + `"}`,
	} {
		t.Run(input, func(t *testing.T) {
			var changes PreferenceChanges
			err := json.Unmarshal([]byte(input), &changes)
			if err == nil {
				_, err = changes.Apply(DefaultPreferences())
			}
			if err == nil {
				t.Fatal("非法条件被接受")
			}
		})
	}
}

func TestPreferenceChanges_Accept_whenAtBounds(t *testing.T) {
	for _, input := range []string{
		`{"target_count":1,"radius_meters":100,"max_price_cny":1,"meal_style":"quick"}`,
		`{"target_count":10,"radius_meters":50000,"max_price_cny":10000,"meal_style":"regular"}`,
		`{"food_requirement":"` + strings.Repeat("锅", 100) + `"}`,
	} {
		var changes PreferenceChanges
		if err := json.Unmarshal([]byte(input), &changes); err != nil {
			t.Fatal(err)
		}
		if _, err := changes.Apply(DefaultPreferences()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPreferenceChanges_Reject_whenJSONKeysAliasedOrRepeated(t *testing.T) {
	for _, input := range []string{
		`{"allow_unknown_price":false,"ALLOW_UNKNOWN_PRICE":null}`,
		`{"radius_meters":2000,"Radius_Meters":null}`,
		`{"ALLOW_UNKNOWN_PRICE":null,"allow_unknown_price":false}`,
		`{"Radius_Meters":null,"radius_meters":2000}`,
		`{"ALLOW_UNKNOWN_PRICE":false}`, `{"Radius_Meters":2000}`,
		`{"allow_unknown_price":false,"allow_unknown_price":true}`,
		`{"allow_unknown_price":false,"allow_unknown_price":false}`,
		`{"radius_meters":2000,"radius_meters":4000}`,
		`{"radius_meters":2000,"radius_meters":2000}`,
		`{"max_price_cny":50,"Max_Price_Cny":null}`,
		`{"food_requirement":"四川火锅","FOOD_REQUIREMENT":null}`,
		`{"target_count":3,"TARGET_COUNT":5}`,
		`{"meal_style":"quick","Meal_Style":"any"}`,
		`{"radius_meters":2000,"radius_meterſ":null}`,
		`{"allow_unknown_price":false,"\u0061llow_unknown_price":true}`,
		`{"max_price_cny":null,"max_price_cny":null}`,
		`{"unknown_preference":null}`,
	} {
		t.Run(input, func(t *testing.T) {
			var changes PreferenceChanges
			err := json.Unmarshal([]byte(input), &changes)
			if err == nil {
				next, applyErr := changes.Apply(DefaultPreferences())
				t.Fatalf("键未在解析边界拒绝，条件=%+v，应用错误=%v", next, applyErr)
			}
		})
	}
}

func TestPreferenceChanges_Accept_whenExactKeysEscapedAndValuesContainKeys(t *testing.T) {
	input := `{"\u0061llow_unknown_price":false,"radius_meters":2000,"food_requirement":"radius_meters radius_meters 川锅","max_price_cny":null}`
	var changes PreferenceChanges
	if err := json.Unmarshal([]byte(input), &changes); err != nil {
		t.Fatal(err)
	}
	next, err := changes.Apply(DefaultPreferences())
	if err != nil {
		t.Fatal(err)
	}
	if next.AllowUnknownPrice || next.RadiusMeters != 2000 || next.FoodRequirement == nil || *next.FoodRequirement != "radius_meters radius_meters 川锅" {
		t.Fatalf("合法精确键或原始文本被改写: %+v", next)
	}
}

func TestDecodeRecommendationFields_Reject_whenRequestKeysAliasedOrRepeated(t *testing.T) {
	for _, input := range []string{
		`{"preferences":{},"Preferences":null}`,
		`{"preferences":{"allow_unknown_price":false},"preferences":{}}`,
		`{"preferences":{},"\u0070references":{}}`,
		`{"selection":{},"SELECTION":null}`,
		`{"unknown":null}`,
	} {
		t.Run(input, func(t *testing.T) {
			_, err := DecodeRecommendationFields([]byte(input), []string{"preferences", "selection"})
			if !errors.Is(err, ErrInvalidRecommendation) {
				t.Fatalf("请求别名或重复键未被拒绝: %v", err)
			}
		})
	}
}

func TestSelection_Validate_whenDeltaMatchesFullRequest(t *testing.T) {
	previous := DefaultPreferences()
	radius := 4000
	selection := RecommendationSelection{OptionID: "expand-4km", PreviousPreferences: previous, Changes: PreferenceChanges{RadiusMeters: &radius}}
	current := previous
	current.RadiusMeters = radius
	if err := selection.Validate(current); err != nil {
		t.Fatal(err)
	}
}

func TestSelection_Reject_whenAmbiguousOrInconsistent(t *testing.T) {
	previous := DefaultPreferences()
	radius := 4000
	for name, selection := range map[string]RecommendationSelection{
		"缺少方案":  {PreviousPreferences: previous, Changes: PreferenceChanges{RadiusMeters: &radius}},
		"空差异":   {OptionID: "expand", PreviousPreferences: previous},
		"请求不一致": {OptionID: "expand", PreviousPreferences: previous, Changes: PreferenceChanges{RadiusMeters: &radius}},
	} {
		t.Run(name, func(t *testing.T) {
			err := selection.Validate(previous)
			if !errors.Is(err, ErrInvalidRecommendation) {
				t.Fatalf("预期拒绝选择，得到 %v", err)
			}
		})
	}
}

func TestSelection_Reject_whenWireInputAmbiguous(t *testing.T) {
	for _, input := range []string{`"好的"`, `{"option_id":"expand","previous_preferences":{},"changes":{"radius_meters":4000}}`, `{"option_id":"expand","user_id":2}`} {
		var selection RecommendationSelection
		err := json.Unmarshal([]byte(input), &selection)
		if err == nil {
			err = selection.Validate(DefaultPreferences())
		}
		if err == nil {
			t.Fatalf("歧义输入被接受: %s", input)
		}
	}
}
