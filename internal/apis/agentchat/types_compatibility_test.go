package agentchat

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"food-agent/internal/agent"
)

func TestChatRequest_Accept_whenTopLevelExtensions(t *testing.T) {
	for _, extension := range []string{
		`"client_meta":{"version":1}`,
		`"client_meta":null,"extra":[1,true,"text"]`,
		`"client_meta":1,"client_meta":2`,
		`"client_meta":{"Preferences":null,"preferences":{},"preferences":null}`,
		`"preference":false,"preferences_extra":[]`,
	} {
		t.Run(extension, func(t *testing.T) {
			input := `{"messages":[{"role":"user","content":"附近吃什么"}],"location":"121,31",` + extension + `}`
			var request ChatRequest

			err := json.Unmarshal([]byte(input), &request)

			if err != nil {
				t.Fatalf("顶层扩展字段应被忽略: %v", err)
			}
			if request.Location != "121,31" || !reflect.DeepEqual(request.Messages, []ChatMessage{{Role: "user", Content: "附近吃什么"}}) {
				t.Fatalf("旧请求字段发生变化: %+v", request)
			}
		})
	}
}

func TestChatRequest_PreserveLegacyFields_whenStandardJSONSpellingsUsed(t *testing.T) {
	type legacyRequest ChatRequest
	for _, input := range []string{
		`{"messages":[{"role":"user","content":"附近吃什么"}],"address_id":7,"location":"121,31","keywords":"面馆","types":"050000","radius":1234,"sortrule":"distance","region":"上海","city_limit":false,"limit":2}`,
		`{"MESSAGES":[{"ROLE":"user","CONTENT":"面馆"}],"ADDRESS_ID":8,"LOCATION":"120,30","KEYWORDS":"汤","TYPES":"050100","RADIUS":2000,"SORTRULE":"weight","REGION":"杭州","CITY_LIMIT":true,"LIMIT":4}`,
		`{"radius":1000,"Radius":2000,"limit":2,"LIMIT":4,"location":"old","Location":"121,31","city_limit":true,"City_Limit":false}`,
		`{"messages":[],"MESSAGES":null,"address_id":7,"Address_ID":null,"location":"121,31","LOCATION":null,"keywords":"汤","KEYWORDS":null,"city_limit":true,"CITY_LIMIT":null}`,
		`{"radiuſ":2000,"LIMIT":4,"ſortrule":"distance"}`,
		`{"selection":{"option_id":"expand","previous_preferences":{"target_count":3,"radius_meters":3000,"meal_style":"any","max_price_cny":null,"allow_unknown_price":true,"food_requirement":null},"changes":{"radius_meters":4000}},"SELECTION":null}`,
	} {
		t.Run(input, func(t *testing.T) {
			var expected legacyRequest
			if err := json.Unmarshal([]byte(input), &expected); err != nil {
				t.Fatal(err)
			}
			var request ChatRequest

			err := json.Unmarshal([]byte(input), &request)

			if err != nil {
				t.Fatalf("标准旧字段请求应被接受: %v", err)
			}
			request.radiusProvided, request.limitProvided = false, false
			if !reflect.DeepEqual(request, ChatRequest(expected)) {
				t.Fatalf("与标准 JSON 解码不一致: 得到 %+v，预期 %+v", request, expected)
			}
		})
	}
}

func TestChatRequest_Normalize_whenLegacyFieldsUseAliases(t *testing.T) {
	for _, fixture := range []struct {
		input         string
		radius, count int
		invalid       bool
	}{
		{`{"client_meta":{}}`, 5000, 10, false},
		{`{"RADIUS":1234,"LIMIT":2}`, 1234, 2, false},
		{`{"radius":1000,"Radius":2000,"limit":2,"Limit":4}`, 2000, 4, false},
		{`{"RADIUS":0}`, 0, 0, true},
		{`{"LIMIT":0}`, 0, 0, true},
		{`{"radiuſ":0}`, 0, 0, true},
	} {
		t.Run(fixture.input, func(t *testing.T) {
			var request ChatRequest
			if err := json.Unmarshal([]byte(fixture.input), &request); err != nil {
				t.Fatal(err)
			}

			preferences, err := request.NormalizePreferences()

			if fixture.invalid {
				if !errors.Is(err, errInvalidInput) {
					t.Fatalf("显式无效旧值应被拒绝: %v", err)
				}
				return
			}
			if err != nil || preferences.RadiusMeters != fixture.radius || preferences.TargetCount != fixture.count {
				t.Fatalf("旧条件归一化发生变化: %+v，错误=%v", preferences, err)
			}
		})
	}
}

func TestChatRequest_Reject_whenPreferenceKeysAliasedOrRepeated(t *testing.T) {
	for _, input := range []string{
		`{"Preferences":{}}`, `{"PREFERENCES":null}`, `{"preferenceſ":{}}`,
		`{"preferences":{},"Preferences":null}`, `{"Preferences":null,"preferences":{}}`,
		`{"preferences":{},"preferences":{}}`,
		`{"preferences":{"allow_unknown_price":false},"preferences":null}`,
		`{"preferences":{"radius_meters":2000},"preferences":{}}`,
		`{"preferences":{},"\u0070references":{}}`,
		`{"preferences":{"allow_unknown_price":false,"ALLOW_UNKNOWN_PRICE":null}}`,
		`{"preferences":{"radius_meters":2000,"Radius_Meters":null}}`,
		`{"preferences":{"ALLOW_UNKNOWN_PRICE":null,"allow_unknown_price":false}}`,
		`{"preferences":{"Radius_Meters":null,"radius_meters":2000}}`,
		`{"preferences":{"allow_unknown_price":false,"allow_unknown_price":null}}`,
		`{"preferences":{"radius_meters":2000,"radius_meters":null}}`,
		`{"preferences":{"radius_meters":2000,"radius_meters":2000}}`,
		`{"preferences":{"allow_unknown_price":false,"\u0061llow_unknown_price":true}}`,
		`{"preferences":{"unknown":null}}`,
	} {
		t.Run(input, func(t *testing.T) {
			var request ChatRequest

			err := json.Unmarshal([]byte(input), &request)

			if err == nil {
				t.Fatal("偏好别名、重复或未知键未在解码边界拒绝")
			}
		})
	}
}

func TestChatRequest_PreservePreferences_whenExactKeysEscaped(t *testing.T) {
	for _, input := range []string{
		`{"client_meta":{},"preferences":{"allow_unknown_price":false,"radius_meters":2000}}`,
		`{"\u0070references":{"\u0061llow_unknown_price":false,"radius_\u006deters":2000},"client_meta":null}`,
	} {
		t.Run(input, func(t *testing.T) {
			var request ChatRequest
			if err := json.Unmarshal([]byte(input), &request); err != nil {
				t.Fatal(err)
			}

			preferences, err := request.NormalizePreferences()

			if err != nil {
				t.Fatal(err)
			}
			expected := agent.DefaultPreferences()
			expected.AllowUnknownPrice, expected.RadiusMeters = false, 2000
			if !reflect.DeepEqual(preferences, expected) {
				t.Fatalf("显式 false 或 2000 米被改写: %+v", preferences)
			}
		})
	}
}
