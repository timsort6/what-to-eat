package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"food-agent/pkg/config"

	openai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/schema"
)

func TestFoodTools_CompletesToolRoundTrip_whenProviderRequiresValidFunctionNames(t *testing.T) {
	for _, toolName := range []string{ToolNamePOI2Around, ToolNamePOI2Text} {
		t.Run(toolName, func(t *testing.T) {
			validName := regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request providerRequest
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Errorf("请求解码失败：%v", err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if request.final() {
					// 最终正文禁止工具，不受工具命名约束。
					w.Header().Set("Content-Type", "text/event-stream")
					if _, err := io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"完成\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"); err != nil {
						t.Errorf("写入模型响应失败：%v", err)
					}
					return
				}
				// 搜索与内部决策阶段都只能绑定一个合法命名的工具。
				if len(request.Tools) != 1 || !validName.MatchString(request.Tools[0].Function.Name) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusBadRequest)
					if _, err := io.WriteString(w, `{"error":{"message":"Invalid tools function name","type":"invalid_request_error"}}`); err != nil {
						t.Errorf("写入错误响应失败：%v", err)
					}
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if request.decision() {
					writeProviderDecision(t, w, request)
					return
				}
				response := fmt.Sprintf(`{"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"search-1","type":"function","function":{"name":%q,"arguments":"{\"keywords\":\"面馆\"}"}}]},"finish_reason":"tool_calls"}]}`, request.Tools[0].Function.Name)
				if _, err := fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", response); err != nil {
					t.Errorf("写入模型响应失败：%v", err)
				}
			}))
			defer provider.Close()
			chatModel, err := openai.NewChatModel(context.Background(), &openai.ChatModelConfig{BaseURL: provider.URL + "/v1", Model: "test-model"})
			if err != nil {
				t.Fatal(err)
			}
			// 候选位于授权位置，才能通过服务端半径校验成为交付地点。
			mcp := &fakeFoodMCP{response: `{"status":"1","pois":[{"id":"p1","name":"面馆","address":"测试地址","location":"` + testScope().Location + `"}]}`}
			// 3 模型/8 步 = 一轮搜索 + 内部决策 + 最终正文。
			settings := config.AgentConfig{AllowedTools: []string{toolName}, MaxSteps: 8, MaxModelCalls: 3, MaxToolCalls: 1, TimeoutSeconds: 3}
			tools, err := NewFoodTools(FoodToolDeps{MCP: mcp, Scope: testScope(), AllowedTools: settings.AllowedTools})
			if err != nil {
				t.Fatal(err)
			}
			runner, err := NewRunner("", settings, chatModel, tools)
			if err != nil {
				t.Fatal(err)
			}

			ctx := WithRecommendationPreferences(WithRequestScope(context.Background(), testScope()), DefaultPreferences())
			stream, err := runner.Stream(ctx, []*schema.Message{schema.UserMessage("搜索面馆")})
			if err != nil {
				t.Fatalf("严格工具命名协议拒绝请求：%v", err)
			}
			defer stream.Close()
			var text string
			for {
				chunk, recvErr := stream.Recv()
				if errors.Is(recvErr, io.EOF) {
					break
				}
				if recvErr != nil {
					t.Fatal(recvErr)
				}
				text += chunk.Content
			}

			name, arguments, scope := mcp.snapshot()
			if text != "完成" || len(stream.Places()) != 1 || name != toolName || arguments["keywords"] != "面馆" || scope.Location != testScope().Location || scope.UserID != testScope().UserID {
				t.Fatalf("工具往返结果不符：文本=%q 地点数=%d MCP名称=%q 参数=%v 身份=%d", text, len(stream.Places()), name, arguments, scope.UserID)
			}
		})
	}
}
