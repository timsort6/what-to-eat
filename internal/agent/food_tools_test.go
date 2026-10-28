package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeFoodMCP struct {
	mu       sync.Mutex
	response string
	name     string
	args     map[string]any
	scope    RequestScope
	called   chan struct{}
	wait     bool
	calls    int
}

func (f *fakeFoodMCP) CallToolText(ctx context.Context, name string, args map[string]any) (string, error) {
	f.mu.Lock()
	f.calls++
	f.name = name
	f.args = args
	f.scope, _ = RequestScopeFromContext(ctx)
	if f.called != nil {
		close(f.called)
	}
	f.mu.Unlock()
	if f.wait {
		<-ctx.Done()
		return "", ctx.Err()
	}
	return f.response, nil
}

func TestFoodTools_BindsPreferences_whenModelRequestsWiderRadiusOrPhotos(t *testing.T) {
	preferences := DefaultPreferences()
	preferences.RadiusMeters = 2000
	preferences.AllowUnknownPrice = false
	fake := &fakeFoodMCP{response: `{"status":"1","pois":[{"id":"P1","name":"餐厅","location":"0,0","business":{"cost":"30","opentime_today":"00:00-24:00"},"secret":"private"}]}`}
	tools, err := NewFoodTools(FoodToolDeps{MCP: fake, Scope: RequestScope{UserID: 1, Location: "0,0"}, AllowedTools: []string{ToolNamePOI2Around, ToolNamePOI2Text}, Preferences: &preferences})
	require.NoError(t, err)
	preferences.RadiusMeters = 10000
	for _, name := range []string{ToolNamePOI2Around, ToolNamePOI2Text} {
		output, err := tools[name].InvokableRun(context.Background(), `{"keywords":"餐厅","radius":50000,"show_fields":"photos,tel"}`)
		require.NoError(t, err)
		_, args, _ := fake.snapshot()
		require.Equal(t, "business", args["show_fields"])
		if name == ToolNamePOI2Around {
			require.Equal(t, 2000, args["radius"])
		} else {
			require.NotContains(t, args, "radius")
		}
		var result SearchResult
		require.NoError(t, json.Unmarshal([]byte(output), &result))
		// 高德 business.cost 缺币种/口径时按参考人均参与比较（证据仅 business.cost），不再视为未知价格。
		require.Equal(t, DecisionReady, result.Decision.Status)
		require.Equal(t, MatchEvidence{State: MatchSatisfied, Fields: []EvidenceField{"business.cost"}}, result.Places[0].Evaluation.Price)
		require.NotContains(t, output, "private")
		require.NotContains(t, output, `"meal_duration"`)
	}
	fake.mu.Lock()
	calls := fake.calls
	fake.mu.Unlock()
	require.Equal(t, 2, calls)
}

func TestFoodTools_BindsContextPreferences_whenRequestCarriesNormalizedConditions(t *testing.T) {
	fake := &fakeFoodMCP{response: `{"status":"1","pois":[]}`}
	tools, err := NewFoodTools(FoodToolDeps{MCP: fake, Scope: testScope(), AllowedTools: []string{ToolNamePOI2Around}})
	require.NoError(t, err)
	preferences := DefaultPreferences()
	preferences.RadiusMeters = 4000
	_, err = tools[ToolNamePOI2Around].InvokableRun(WithRecommendationPreferences(context.Background(), preferences), `{"radius":50000}`)
	require.NoError(t, err)
	_, args, _ := fake.snapshot()
	require.Equal(t, 4000, args["radius"])
}

func TestFoodTools_RejectScope_whenCoordinatesAreNonFinite(t *testing.T) {
	for _, location := range []string{"NaN,0", "0,Inf", "181,0"} {
		_, err := NewFoodTools(FoodToolDeps{MCP: &fakeFoodMCP{}, Scope: RequestScope{UserID: 1, Location: location}})
		require.ErrorIs(t, err, ErrInvalidScope)
	}
}

func (f *fakeFoodMCP) snapshot() (string, map[string]any, RequestScope) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.name, f.args, f.scope
}

func testScope() RequestScope {
	addressID := uint(7)
	return RequestScope{UserID: 42, Location: "121.4737,31.2304", AddressID: &addressID}
}

func TestFoodTools_ExposesExactlyTheServerAllowlist(t *testing.T) {
	fake := &fakeFoodMCP{}
	tools, err := NewFoodTools(FoodToolDeps{
		MCP: fake, Scope: testScope(),
		AllowedTools: []string{ToolNamePOI2Around, ToolNamePOI2Text},
	})
	if err != nil {
		t.Fatalf("new food tools: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("tool count = %d, want 2", len(tools))
	}
	if _, ok := tools[ToolNamePOI2Around]; !ok {
		t.Fatalf("missing %s", ToolNamePOI2Around)
	}
	if _, ok := tools[ToolNamePOI2Text]; !ok {
		t.Fatalf("missing %s", ToolNamePOI2Text)
	}
}

func TestFoodTools_ConfiguredSubsetAndUnknownRejection(t *testing.T) {
	fake := &fakeFoodMCP{}
	tools, err := NewFoodTools(FoodToolDeps{
		MCP: fake, Scope: testScope(), AllowedTools: []string{ToolNamePOI2Text},
	})
	if err != nil {
		t.Fatalf("new subset: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("subset size = %d, want 1", len(tools))
	}
	if _, ok := tools[ToolNamePOI2Text]; !ok {
		t.Fatalf("configured tool missing")
	}
	if _, ok := tools[ToolNamePOI2Around]; ok {
		t.Fatalf("unconfigured tool exposed")
	}

	_, err = NewFoodTools(FoodToolDeps{
		MCP: fake, Scope: testScope(), AllowedTools: []string{"amap.geocode"},
	})
	if !errors.Is(err, ErrUnknownTool) {
		t.Fatalf("unknown tool error = %v, want ErrUnknownTool", err)
	}
}

func TestFoodTools_TrustedScopeAndPlaceOutput(t *testing.T) {
	response := `{"status":"1","info":"OK","infocode":"10000","pois":[{"id":"p1","name":" 面馆 ","location":"121,31","address":"街道","distance":"20","typecode":"050301"}]}`
	fake := &fakeFoodMCP{response: response}
	tools, err := NewFoodTools(FoodToolDeps{
		MCP: fake, Scope: testScope(), AllowedTools: []string{ToolNamePOI2Around},
	})
	if err != nil {
		t.Fatalf("new tools: %v", err)
	}

	output, err := tools[ToolNamePOI2Around].InvokableRun(context.Background(), `{"location":"0,0","user_id":999,"keywords":" 面 " ,"radius":999999,"page_size":100}`)
	if err != nil {
		t.Fatalf("invoke around: %v", err)
	}
	var result SearchResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("decode result: %v; output=%s", err, output)
	}
	if result.Location != testScope().Location || result.PlacesCount != 1 || result.Places[0].Name != "面馆" {
		t.Fatalf("unexpected result: %+v", result)
	}

	name, args, scope := fake.snapshot()
	if name != ToolNamePOI2Around {
		t.Fatalf("mcp tool = %q", name)
	}
	if args["location"] != testScope().Location || args["radius"] != 5000 || args["page_size"] != 25 {
		t.Fatalf("trusted or bounded args = %#v", args)
	}
	if _, ok := args["user_id"]; ok {
		t.Fatalf("model identity reached mcp")
	}
	if scope.UserID != testScope().UserID || scope.Location != testScope().Location {
		t.Fatalf("trusted scope = %+v", scope)
	}
}

func TestFoodTools_InputSchemaExcludesTrustedFields(t *testing.T) {
	fake := &fakeFoodMCP{}
	tools, err := NewFoodTools(FoodToolDeps{
		MCP: fake, Scope: testScope(), AllowedTools: []string{ToolNamePOI2Around},
	})
	if err != nil {
		t.Fatalf("new tools: %v", err)
	}
	info, err := tools[ToolNamePOI2Around].Info(context.Background())
	if err != nil {
		t.Fatalf("tool info: %v", err)
	}
	params, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil {
		t.Fatalf("tool schema: %v", err)
	}
	schemaJSON, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	if strings.Contains(string(schemaJSON), "location") || strings.Contains(string(schemaJSON), "user_id") {
		t.Fatalf("trusted fields exposed in schema: %s", schemaJSON)
	}
}

func TestFoodTools_PropagatesCancellation(t *testing.T) {
	fake := &fakeFoodMCP{wait: true, called: make(chan struct{})}
	tools, err := NewFoodTools(FoodToolDeps{
		MCP: fake, Scope: testScope(), AllowedTools: []string{ToolNamePOI2Text},
	})
	if err != nil {
		t.Fatalf("new tools: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, invokeErr := tools[ToolNamePOI2Text].InvokableRun(ctx, `{"keywords":"面馆"}`)
		result <- invokeErr
	}()
	<-fake.called
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("invoke error = %v, want context.Canceled", err)
	}
}
