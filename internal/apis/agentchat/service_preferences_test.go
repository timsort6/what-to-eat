package agentchat

import (
	"context"
	"encoding/json"
	"io"
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

type preferenceLLM struct {
	fakeLLM
	observed chan agent.SearchResult
}

func (model *preferenceLLM) Stream(ctx context.Context, input []*schema.Message, options ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	for _, message := range input {
		if message.Role != schema.Tool {
			continue
		}
		var result agent.SearchResult
		if err := json.Unmarshal([]byte(message.Content), &result); err != nil {
			return nil, err
		}
		select {
		case model.observed <- result:
		default:
		}
		break
	}
	return model.fakeLLM.Stream(ctx, input, options...)
}

func (model *preferenceLLM) WithTools(tools []*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	model.fakeLLM.tools = tools
	return model, nil
}

func TestChat_BindsNormalizedPreferences_whenHTTPRequestsOverrideDefaults(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, fixture := range []struct {
		name, fields string
		radius       int
		status       agent.DecisionStatus
		food         agent.MatchState
	}{
		{"旧版半径", `"radius":1234,"limit":1`, 1234, agent.DecisionReady, agent.MatchUnknown},
		{"新版允许未知价格", `"preferences":{"radius_meters":2000,"target_count":1,"food_requirement":"四川火锅","max_price_cny":50,"allow_unknown_price":true}`, 2000, agent.DecisionReady, agent.MatchSatisfied},
		{"新版禁止未知价格", `"preferences":{"radius_meters":2000,"target_count":1,"food_requirement":"四川火锅","max_price_cny":50,"allow_unknown_price":false}`, 2000, agent.DecisionNeedsChoice, agent.MatchSatisfied},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			model := &preferenceLLM{observed: make(chan agent.SearchResult, 1)}
			mcp := &fakeMCP{resp: `{"status":"1","pois":[{"id":"P1","name":"餐厅","location":"0,0","business":{"tag":"四川火锅","cost":"约30"}},{"id":"P2","name":"远处","location":"0,0.03"}]}`}
			svc := newTestService(t, model, mcp, &fakeAddrDAO{})
			mgr := token.NewJWTTokenManager([]byte("fake-secret"), jwt.SigningMethodHS256)
			auth, err := mgr.IssueTo(token.Info{UID: "1", Username: "u1"}, time.Hour)
			require.NoError(t, err)
			router := gin.New()
			require.NoError(t, RegisterRoutes(router.Group("/api/v1"), RouterDeps{Service: svc, Tokens: mgr}))
			server := httptest.NewServer(router)
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/api/v1/agent/chat", strings.NewReader(`{"messages":[{"role":"user","content":"附近吃什么"}],"location":"0,0",`+fixture.fields+`}`))
			require.NoError(t, err)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer "+auth)

			response, err := server.Client().Do(request)
			require.NoError(t, err)
			defer response.Body.Close()
			_, err = io.Copy(io.Discard, response.Body)
			require.NoError(t, err)

			require.Equal(t, http.StatusOK, response.StatusCode)
			select {
			case result := <-model.observed:
				require.Equal(t, fixture.radius, mcp.args["radius"])
				require.Equal(t, fixture.status, result.Decision.Status)
				require.Equal(t, fixture.food, result.Places[0].Evaluation.Food.State)
				require.Equal(t, agent.MatchUnknown, result.Places[0].Evaluation.Price.State)
				require.Equal(t, agent.MatchUnsatisfied, result.Places[1].Evaluation.Range.State)
			case <-ctx.Done():
				t.Fatal("未观察到实际工具结果")
			}
		})
	}
}
