package agentchat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"food-agent/internal/agent"
	"food-agent/pkg/token"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

func TestRequest_Normalize_whenLegacyOrNewPreferences(t *testing.T) {
	for _, fixture := range []struct {
		input         string
		radius, count int
		style         agent.MealStyle
		allow         bool
	}{
		{`{}`, 5000, 10, agent.MealAny, true},
		{`{"radius":1234,"limit":2}`, 1234, 2, agent.MealAny, true},
		{`{"preferences":{}}`, 3000, 3, agent.MealAny, true},
		{`{"radius":4000,"limit":5,"preferences":{"target_count":5,"radius_meters":4000,"meal_style":"quick","max_price_cny":100,"allow_unknown_price":false,"food_requirement":"四川火锅"}}`, 4000, 5, agent.MealQuick, false},
	} {
		t.Run(fixture.input, func(t *testing.T) {
			var request ChatRequest
			if err := json.Unmarshal([]byte(fixture.input), &request); err != nil {
				t.Fatal(err)
			}
			preferences, err := request.NormalizePreferences()
			if err != nil {
				t.Fatal(err)
			}
			if preferences.RadiusMeters != fixture.radius || preferences.TargetCount != fixture.count || preferences.MealStyle != fixture.style || preferences.AllowUnknownPrice != fixture.allow {
				t.Fatalf("归一化条件不符: %+v", preferences)
			}
		})
	}
}

func TestChat_ProjectsSafeBusinessFields_whenSourceHasUnknownPrice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mcp := &fakeMCP{resp: `{"status":"1","pois":[{"id":"P1","name":"餐厅","location":"0,0","secret":"private-raw","tel":"private-phone","business":{"cost":"约30","opentime_today":"10:00-22:00"}}]}`}
	svc := newTestService(t, &fakeLLM{}, mcp, &fakeAddrDAO{})
	mgr := token.NewJWTTokenManager([]byte("fake-secret"), jwt.SigningMethodHS256)
	auth, err := mgr.IssueTo(token.Info{UID: "1", Username: "u1"}, time.Hour)
	require.NoError(t, err)
	router := gin.New()
	require.NoError(t, RegisterRoutes(router.Group("/api/v1"), RouterDeps{Service: svc, Tokens: mgr}))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/chat", strings.NewReader(`{"messages":[{"role":"user","content":"附近吃什么"}],"location":"0,0"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+auth)
	response := newCloseNotifyRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, 5000, mcp.args["radius"])
	require.Equal(t, "business", mcp.args["show_fields"])
	body := response.Body.String()
	require.Contains(t, body, "event:done")
	require.Contains(t, body, `"candidate_key":"poi:P1"`)
	require.Contains(t, body, `"opentime_today":"10:00-22:00"`)
	require.Contains(t, body, `"state":"unknown"`)
	require.NotContains(t, body, `"cost"`)
	require.NotContains(t, body, "private-raw")
	require.NotContains(t, body, "private-phone")
}

func TestPlace_Projection_whenInternalRawAndPhoneArePopulated(t *testing.T) {
	value := projectPlace(agent.Place{ID: "P1", Name: "餐厅", Raw: json.RawMessage(`{"secret":true}`), Tel: "private"})
	data, err := json.Marshal(value)
	require.NoError(t, err)
	require.NotContains(t, string(data), "secret")
	require.NotContains(t, string(data), "private")
	require.Empty(t, value.Raw)
	require.Empty(t, value.Tel)
}

func TestRequest_Reject_whenConflictingOrInvalid(t *testing.T) {
	for _, input := range []string{
		`{"radius":2000,"preferences":{"radius_meters":4000}}`, `{"limit":5,"preferences":{"target_count":3}}`,
		`{"radius":-1}`, `{"radius":0}`, `{"radius":50001}`, `{"limit":-1}`, `{"limit":11}`, `{"limit":NaN}`,
		`{"radius":null}`, `{"preferences":null}`, `{"selection":null}`,
		`{"preferences":{"meal_style":"unknown"}}`, `{"preferences":{"radius_meters":NaN}}`, `{"preferences":{"target_count":11}}`,
		`{"selection":"好的"}`, `{"selection":{"option_id":"expand"}}`,
		`{"preferences":{"allow_unknown_price":false,"ALLOW_UNKNOWN_PRICE":null}}`,
		`{"preferences":{"radius_meters":2000,"Radius_Meters":null}}`,
		`{"preferences":{"radius_meters":2000,"radius_meters":4000}}`,
		`{"preferences":{},"Preferences":null}`,
		`{"preferences":{"allow_unknown_price":false},"preferences":{}}`,
	} {
		t.Run(input, func(t *testing.T) {
			var request ChatRequest
			err := json.Unmarshal([]byte(input), &request)
			if err == nil {
				_, err = request.NormalizePreferences()
			}
			if err == nil {
				t.Fatal("非法请求被接受")
			}
		})
	}
}

func TestRequest_Selection_whenFullPreferencesMatchDelta(t *testing.T) {
	input := `{"preferences":{"target_count":3,"radius_meters":4000,"meal_style":"any","max_price_cny":null,"allow_unknown_price":true,"food_requirement":null},"selection":{"option_id":"expand","previous_preferences":{"target_count":3,"radius_meters":3000,"meal_style":"any","max_price_cny":null,"allow_unknown_price":true,"food_requirement":null},"changes":{"radius_meters":4000}}}`
	var request ChatRequest
	if err := json.Unmarshal([]byte(input), &request); err != nil {
		t.Fatal(err)
	}
	preferences, err := request.NormalizePreferences()
	if err != nil {
		t.Fatal(err)
	}
	if preferences.RadiusMeters != 4000 {
		t.Fatalf("半径=%d", preferences.RadiusMeters)
	}
}

func TestPrepare_PreserveExplicitConditions_whenHistorySuggestsAlternative(t *testing.T) {
	mcp := &fakeMCP{}
	svc := newTestService(t, &fakeLLM{}, mcp, &fakeAddrDAO{})
	var request ChatRequest
	if err := json.Unmarshal([]byte(`{"location":"121,31","preferences":{"radius_meters":2000,"food_requirement":"四川火锅"},"messages":[{"role":"user","content":"我要四川火锅"},{"role":"assistant","content":"扩大到4公里，改吃冒菜？"},{"role":"user","content":"好的"}]}`), &request); err != nil {
		t.Fatal(err)
	}
	prepared, err := svc.Prepare(context.Background(), 1, request)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Preferences.RadiusMeters != 2000 || prepared.Preferences.FoodRequirement == nil || *prepared.Preferences.FoodRequirement != "四川火锅" || prepared.Selection != nil || mcp.args != nil {
		t.Fatalf("历史改变了显式条件: %+v", prepared)
	}
}

func TestPrepare_PreserveText_whenUserSuppliesWhitespace(t *testing.T) {
	svc := newTestService(t, &fakeLLM{}, &fakeMCP{}, &fakeAddrDAO{})
	content := "  四川火锅\n 不要改条件  "
	prepared, err := svc.Prepare(context.Background(), 1, ChatRequest{Messages: []ChatMessage{{Role: "user", Content: content}}, Location: "121,31"})
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared.ModelInput) != 1 || prepared.ModelInput[0].Content != content {
		t.Fatal("自由文本被改写")
	}
}

func TestChat_RejectConflictBeforeExternalCalls_whenPreferencesDisagree(t *testing.T) {
	gin.SetMode(gin.TestMode)
	llm, mcp := &fakeLLM{}, &fakeMCP{}
	svc := newTestService(t, llm, mcp, &fakeAddrDAO{})
	mgr := token.NewJWTTokenManager([]byte("fake-secret"), jwt.SigningMethodHS256)
	auth, err := mgr.IssueTo(token.Info{UID: "1", Username: "u1"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	if err := RegisterRoutes(router.Group("/api/v1"), RouterDeps{Service: svc, Tokens: mgr}); err != nil {
		t.Fatal(err)
	}
	for _, fields := range []string{
		`"radius":2000,"preferences":{"radius_meters":4000}`,
		`"preferences":{"meal_style":"unknown"}`, `"selection":"好的"`, `"preferences":{"max_price_cny":NaN}`,
		`"preferences":{"allow_unknown_price":false,"ALLOW_UNKNOWN_PRICE":null}`,
		`"preferences":{"radius_meters":2000,"Radius_Meters":null}`,
		`"preferences":{"allow_unknown_price":false,"allow_unknown_price":true}`,
		`"preferences":{"radius_meters":2000,"radius_meters":4000}`,
		`"preferences":{},"Preferences":null`,
		`"preferences":{"allow_unknown_price":false},"preferences":{}`,
	} {
		body := `{"messages":[{"role":"user","content":"附近吃什么"}],"location":"121,31",` + fields + `}`
		request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/chat", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+auth)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest || llm.calls != 0 || mcp.args != nil {
			t.Fatalf("非法请求=%d，模型调用=%d，地图参数=%v", response.Code, llm.calls, mcp.args)
		}
	}
}
