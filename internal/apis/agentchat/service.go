package agentchat

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"food-agent/internal/agent"
	"food-agent/pkg/config"
	"food-agent/pkg/model"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
)

type FavoriteAddressDAO interface {
	GetByID(ctx context.Context, id uint) (*model.UserFavoriteAddress, error)
}

type AmapMCP interface {
	CallToolText(ctx context.Context, name string, arguments map[string]any) (string, error)
}

var (
	errInvalidInput    = errors.New("invalid input")
	errMissingDecision = errors.New("stream produced output without validated decision")
)

type Service struct {
	llm         einomodel.BaseChatModel
	decisionLLM einomodel.BaseChatModel
	mcp         AmapMCP
	prompt      string
	budgets     config.AgentConfig

	addresses FavoriteAddressDAO
}

type Deps struct {
	LLM einomodel.BaseChatModel
	// DecisionLLM 可选：仅用于决策阶段；为 nil 时所有阶段使用 LLM。
	DecisionLLM einomodel.BaseChatModel
	MCP         AmapMCP
	Prompt      string
	Budgets     config.AgentConfig

	Addresses FavoriteAddressDAO
}

func NewService(d Deps) (*Service, error) {
	if d.LLM == nil {
		return nil, errors.New("nil llm")
	}
	if d.MCP == nil {
		return nil, errors.New("nil amap mcp")
	}
	if d.Addresses == nil {
		return nil, errors.New("nil addresses dao")
	}
	return &Service{
		llm:         d.LLM,
		decisionLLM: d.DecisionLLM,
		mcp:         d.MCP,
		prompt:      d.Prompt,
		budgets:     d.Budgets,
		addresses:   d.Addresses,
	}, nil
}

type Prepared struct {
	Location      string
	UsedAddressID *uint
	userID        uint

	ModelInput  []*schema.Message
	Preferences agent.Preferences
	// Selection 仅用于请求校验（见 ChatRequest.NormalizePreferences）；按计划每次以 Preferences 重新检索，不信任旧候选。
	Selection *agent.RecommendationSelection
}

func (s *Service) Prepare(ctx context.Context, userID uint, req ChatRequest) (*Prepared, error) {
	if err := validateHistory(req.Messages); err != nil {
		return nil, err
	}
	preferences, err := req.NormalizePreferences()
	if err != nil {
		return nil, err
	}

	loc, usedAddrID, err := s.resolveLocation(ctx, userID, req)
	if err != nil {
		return nil, err
	}

	msgs := buildModelInput(req.Messages)

	return &Prepared{
		Location:      loc,
		UsedAddressID: usedAddrID,
		userID:        userID,
		ModelInput:    msgs,
		Preferences:   preferences,
		Selection:     req.Selection,
	}, nil
}

func validateHistory(history []ChatMessage) error {
	if len(history) == 0 {
		return fmt.Errorf("messages is required: %w", errInvalidInput)
	}
	if len(history) > 50 {
		return fmt.Errorf("invalid messages: maximum 50 messages: %w", errInvalidInput)
	}
	messageChars := 0
	for _, message := range history {
		messageChars += utf8.RuneCountInString(message.Content)
		if messageChars > 20000 {
			return fmt.Errorf("invalid messages: maximum 20000 characters: %w", errInvalidInput)
		}
		switch strings.ToLower(strings.TrimSpace(message.Role)) {
		case "", "user", "assistant":
		case "system":
			return fmt.Errorf("invalid message role: system is not allowed: %w", errInvalidInput)
		default:
			return fmt.Errorf("invalid message role: %s: %w", message.Role, errInvalidInput)
		}
	}
	// buildModelInput 会丢弃空白消息；末条用户消息为空白时须拒绝，否则品类引用会回退到更早的用户原话。
	last := history[len(history)-1]
	if role := strings.ToLower(strings.TrimSpace(last.Role)); (role == "" || role == "user") && strings.TrimSpace(last.Content) == "" {
		return fmt.Errorf("invalid messages: latest user message is blank: %w", errInvalidInput)
	}
	return nil
}

func (s *Service) StreamAnswer(ctx context.Context, prepared *Prepared) (*agent.RunStream, error) {
	if prepared == nil {
		return nil, fmt.Errorf("nil prepared")
	}
	tools, err := agent.NewFoodTools(agent.FoodToolDeps{
		MCP: s.mcp,
		Scope: agent.RequestScope{
			UserID: prepared.userID, Location: prepared.Location, AddressID: prepared.UsedAddressID,
		},
		AllowedTools: s.budgets.AllowedTools,
		Preferences:  &prepared.Preferences,
	})
	if err != nil {
		return nil, fmt.Errorf("create agent tools: %w", err)
	}
	runner, err := agent.NewRunner(s.prompt, s.budgets, s.llm, tools)
	if err != nil {
		return nil, fmt.Errorf("create agent runner: %w", err)
	}
	if s.decisionLLM != nil {
		if err := runner.SetDecisionModel(s.decisionLLM); err != nil {
			return nil, fmt.Errorf("create agent runner: %w", err)
		}
	}
	runCtx := agent.WithRequestScope(ctx, agent.RequestScope{
		UserID: prepared.userID, Location: prepared.Location, AddressID: prepared.UsedAddressID,
	})
	runCtx = agent.WithRecommendationPreferences(runCtx, prepared.Preferences)
	return runner.Stream(runCtx, prepared.ModelInput)
}

func (s *Service) resolveLocation(ctx context.Context, userID uint, req ChatRequest) (location string, usedAddrID *uint, err error) {
	if strings.TrimSpace(req.Location) != "" {
		loc := strings.TrimSpace(req.Location)
		if _, _, err := parseLocation(loc); err != nil {
			return "", nil, fmt.Errorf("%w: %v", errInvalidInput, err)
		}
		return loc, nil, nil
	}

	if req.AddressID == nil || *req.AddressID == 0 {
		return "", nil, fmt.Errorf("address_id or location is required: %w", errInvalidInput)
	}

	addr, err := s.addresses.GetByID(ctx, *req.AddressID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil, fmt.Errorf("address not found")
		}
		return "", nil, err
	}
	if addr.UserID != userID {
		return "", nil, fmt.Errorf("permission denied")
	}
	location = strconv.FormatFloat(addr.Longitude, 'f', -1, 64) + "," + strconv.FormatFloat(addr.Latitude, 'f', -1, 64)
	if _, _, err := parseLocation(location); err != nil {
		return "", nil, fmt.Errorf("invalid address location: %w", errInvalidInput)
	}
	id := *req.AddressID
	return location, &id, nil
}

func buildModelInput(history []ChatMessage) []*schema.Message {
	msgs := make([]*schema.Message, 0, len(history))
	for _, m := range history {
		role := strings.ToLower(strings.TrimSpace(m.Role))
		content := m.Content
		if strings.TrimSpace(content) == "" {
			continue
		}
		switch role {
		case "", "user":
			msgs = append(msgs, schema.UserMessage(content))
		case "assistant":
			msgs = append(msgs, schema.AssistantMessage(content, nil))
		}
	}
	return msgs
}

func parseLocation(s string) (lng float64, lat float64, err error) {
	s = strings.TrimSpace(s)
	parts := strings.Split(s, ",")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid location (require lng,lat): %w", errInvalidInput)
	}
	lng, err = strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid lng: %w", errInvalidInput)
	}
	lat, err = strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid lat: %w", errInvalidInput)
	}
	if math.IsNaN(lng) || math.IsInf(lng, 0) || lng < -180 || lng > 180 {
		return 0, 0, fmt.Errorf("invalid lng: must be between -180 and 180: %w", errInvalidInput)
	}
	if math.IsNaN(lat) || math.IsInf(lat, 0) || lat < -90 || lat > 90 {
		return 0, 0, fmt.Errorf("invalid lat: must be between -90 and 90: %w", errInvalidInput)
	}
	return lng, lat, nil
}
