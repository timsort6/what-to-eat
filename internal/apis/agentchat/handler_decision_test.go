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

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

const decisionPOIs = `{"status":"1","pois":[{"id":"P1","name":"餐厅","location":"0,0","business":{"tag":"四川火锅","cost":"约30"}},{"id":"P2","name":"远处","location":"0,0.03"}]}`

type sseEvent struct {
	name string
	data json.RawMessage
}

func parseSSE(body string) []sseEvent {
	var events []sseEvent
	for _, block := range strings.Split(strings.TrimSpace(body), "\n\n") {
		var event sseEvent
		for _, line := range strings.Split(block, "\n") {
			if value, ok := strings.CutPrefix(line, "event:"); ok {
				event.name = value
			}
			if value, ok := strings.CutPrefix(line, "data:"); ok {
				event.data = json.RawMessage(value)
			}
		}
		if event.name != "" {
			events = append(events, event)
		}
	}
	return events
}

func serveChat(t *testing.T, svc *Service, ctx context.Context, body string) *closeNotifyRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	mgr := token.NewJWTTokenManager([]byte("fake-secret"), jwt.SigningMethodHS256)
	auth, err := mgr.IssueTo(token.Info{UID: "1", Username: "u1"}, time.Hour)
	require.NoError(t, err)
	router := gin.New()
	require.NoError(t, RegisterRoutes(router.Group("/api/v1"), RouterDeps{Service: svc, Tokens: mgr}))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/chat", strings.NewReader(body)).WithContext(ctx)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+auth)
	recorder := newCloseNotifyRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func eventNames(events []sseEvent) string {
	names := make([]string, 0, len(events))
	for _, event := range events {
		names = append(names, event.name)
	}
	return strings.Join(names, ",")
}

func preferencesJSON(radius int, allowUnknown bool) string {
	allow := "false"
	if allowUnknown {
		allow = "true"
	}
	return `{"target_count":1,"radius_meters":` + itoa(radius) + `,"meal_style":"any","max_price_cny":50,"allow_unknown_price":` + allow + `,"food_requirement":"四川火锅"}`
}

func itoa(value int) string { b, _ := json.Marshal(value); return string(b) }

func TestChat_DecisionEventPrecedesDeltaAndMatchesDone(t *testing.T) {
	for _, fixture := range []struct {
		name         string
		allowUnknown bool
		status       agent.DecisionStatus
		places       int
	}{
		{"ready", true, agent.DecisionReady, 1},
		{"needs_choice", false, agent.DecisionNeedsChoice, 0},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			llm := &fakeLLM{}
			mcp := &fakeMCP{resp: decisionPOIs}
			body := `{"messages":[{"role":"user","content":"附近吃什么"}],"location":"0,0","preferences":` + preferencesJSON(2000, fixture.allowUnknown) + `}`
			recorder := serveChat(t, newTestService(t, llm, mcp, &fakeAddrDAO{}), context.Background(), body)
			require.Equal(t, http.StatusOK, recorder.Code)
			raw := recorder.Body.String()
			events := parseSSE(raw)
			require.Equal(t, "decision,delta,done", eventNames(events), raw)
			for _, leaked := range []string{`"keywords"`, `"pois"`, `"raw"`, `"tel"`, "system"} {
				require.NotContains(t, raw, leaked)
			}

			var decision agent.RecommendationDecision
			require.NoError(t, json.Unmarshal(events[0].data, &decision))
			require.Equal(t, fixture.status, decision.Status)
			require.LessOrEqual(t, len(decision.Options), 3)

			var done struct {
				AnswerText  string            `json:"answer_text"`
				Places      []Place           `json:"places"`
				PlacesCount *int              `json:"places_count"`
				Status      string            `json:"status"`
				Preferences agent.Preferences `json:"preferences"`
				Decision    json.RawMessage   `json:"decision"`
			}
			require.NoError(t, json.Unmarshal(events[2].data, &done))
			require.Equal(t, "推荐面馆", done.AnswerText)
			require.NotNil(t, done.PlacesCount)
			require.Equal(t, fixture.places, *done.PlacesCount)
			require.Len(t, done.Places, fixture.places)
			require.Equal(t, string(fixture.status), done.Status)
			require.JSONEq(t, string(events[0].data), string(done.Decision))
			require.Equal(t, 2000, done.Preferences.RadiusMeters)
			require.Equal(t, fixture.allowUnknown, done.Preferences.AllowUnknownPrice)
			if fixture.status == agent.DecisionReady {
				require.Equal(t, []string{done.Places[0].CandidateKey}, decision.SelectedIDs)
				require.Equal(t, "P1", done.Places[0].ID)
				require.Len(t, decision.Assessments, 1)
			} else {
				require.Empty(t, decision.SelectedIDs)
				require.NotNil(t, decision.SelectedIDs)
			}
		})
	}
}

func TestChat_InvalidDecisionEmitsNoDecisionDeltaOrDone(t *testing.T) {
	llm := &fakeLLM{decisionArguments: `{"status":"ready","selected_ids":["poi:NOPE"],"assessments":[],"reason_codes":[],"options":[]}`}
	body := `{"messages":[{"role":"user","content":"附近吃什么"}],"location":"0,0","preferences":` + preferencesJSON(2000, true) + `}`
	recorder := serveChat(t, newTestService(t, llm, &fakeMCP{resp: decisionPOIs}, &fakeAddrDAO{}), context.Background(), body)
	require.Equal(t, "error", eventNames(parseSSE(recorder.Body.String())), recorder.Body.String())
	require.NotContains(t, recorder.Body.String(), "NOPE")
}

type cancelingFinalLLM struct {
	fakeLLM
	cancel       context.CancelFunc
	reachedFinal bool
}

func (model *cancelingFinalLLM) Stream(ctx context.Context, input []*schema.Message, options ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	if choice := einomodel.GetCommonOptions(nil, options...).ToolChoice; choice != nil && *choice == schema.ToolChoiceForbidden {
		model.reachedFinal = true
		model.cancel()
		<-ctx.Done()
		return schema.StreamReaderFromArray([]*schema.Message{schema.AssistantMessage("迟到正文", nil)}), nil
	}
	return model.fakeLLM.Stream(ctx, input, options...)
}

func (model *cancelingFinalLLM) WithTools(tools []*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	model.fakeLLM.tools = tools
	return model, nil
}

func TestChat_CanceledAfterDecisionEmitsNoDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	llm := &cancelingFinalLLM{cancel: cancel}
	body := `{"messages":[{"role":"user","content":"附近吃什么"}],"location":"0,0","preferences":` + preferencesJSON(2000, true) + `}`
	recorder := serveChat(t, newTestService(t, llm, &fakeMCP{resp: decisionPOIs}, &fakeAddrDAO{}), ctx, body)
	raw := recorder.Body.String()
	require.NotContains(t, raw, "event:done")
	require.NotContains(t, raw, "迟到正文")
	// 搜索2次+决策1次已完成，取消发生在正文阶段（已验证决策之后）。
	require.Equal(t, 3, llm.calls)
	require.True(t, llm.reachedFinal)
}

func TestChat_SelectionValidatedBeforeAnyExternalCall(t *testing.T) {
	previous := preferencesJSON(2000, true)
	for name, selection := range map[string]string{
		"差异不一致":     `{"option_id":"wider","previous_preferences":` + previous + `,"changes":{"radius_meters":5000}}`,
		"无差异":       `{"option_id":"same","previous_preferences":` + previous + `,"changes":{"radius_meters":2000}}`,
		"越权字段":      `{"option_id":"wider","previous_preferences":` + previous + `,"changes":{"radius_meters":4000,"address_id":9}}`,
		"旧条件越权字段":   `{"option_id":"wider","previous_preferences":{"target_count":1,"radius_meters":2000,"meal_style":"any","max_price_cny":50,"allow_unknown_price":true,"food_requirement":"四川火锅","user_id":2},"changes":{"radius_meters":4000}}`,
		"选择未知字段":    `{"option_id":"wider","previous_preferences":` + previous + `,"changes":{"radius_meters":4000},"candidate_ids":["poi:P1"]}`,
		"半径非100米步长": `{"option_id":"wider","previous_preferences":` + previous + `,"changes":{"radius_meters":4050}}`,
	} {
		t.Run(name, func(t *testing.T) {
			llm, mcp := &fakeLLM{}, &fakeMCP{resp: decisionPOIs}
			body := `{"messages":[{"role":"user","content":"扩大到4公里"}],"location":"0,0","preferences":` + preferencesJSON(4000, true) + `,"selection":` + selection + `}`
			recorder := serveChat(t, newTestService(t, llm, mcp, &fakeAddrDAO{}), context.Background(), body)
			require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
			require.NotContains(t, recorder.Body.String(), "event:")
			require.Zero(t, llm.calls)
			require.Nil(t, mcp.args)
		})
	}
}

func TestChat_ValidSelectionReSearchesWithAppliedPreferences(t *testing.T) {
	llm, mcp := &fakeLLM{}, &fakeMCP{resp: decisionPOIs}
	selection := `{"option_id":"wider","previous_preferences":` + preferencesJSON(2000, true) + `,"changes":{"radius_meters":4000}}`
	body := `{"messages":[{"role":"user","content":"扩大到4公里"}],"location":"0,0","preferences":` + preferencesJSON(4000, true) + `,"selection":` + selection + `}`
	recorder := serveChat(t, newTestService(t, llm, mcp, &fakeAddrDAO{}), context.Background(), body)
	events := parseSSE(recorder.Body.String())
	require.Equal(t, "decision,delta,done", eventNames(events), recorder.Body.String())
	require.Equal(t, 4000, mcp.args["radius"])
	var done struct {
		Preferences agent.Preferences `json:"preferences"`
	}
	require.NoError(t, json.Unmarshal(events[2].data, &done))
	require.Equal(t, 4000, done.Preferences.RadiusMeters)
}
