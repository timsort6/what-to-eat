package agentchat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"food-agent/pkg/model"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
)

type ChatModel interface {
	Stream(ctx context.Context, input []*schema.Message, opts ...einomodel.Option) (*schema.StreamReader[*schema.Message], error)
}

type FavoriteAddressDAO interface {
	GetByID(ctx context.Context, id uint) (*model.UserFavoriteAddress, error)
}

type AmapMCP interface {
	CallToolText(ctx context.Context, name string, arguments map[string]any) (string, error)
}

var (
	errInvalidInput = errors.New("invalid input")
)

type Service struct {
	llm ChatModel
	mcp AmapMCP

	addresses FavoriteAddressDAO

	mcpCallTimeoutSeconds int
}

type Deps struct {
	LLM ChatModel
	MCP AmapMCP

	Addresses FavoriteAddressDAO

	MCPCallTimeoutSeconds int
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
	if d.MCPCallTimeoutSeconds <= 0 {
		return nil, errors.New("invalid mcp call timeout seconds")
	}
	return &Service{
		llm:                   d.LLM,
		mcp:                   d.MCP,
		addresses:             d.Addresses,
		mcpCallTimeoutSeconds: d.MCPCallTimeoutSeconds,
	}, nil
}

type Prepared struct {
	Location      string
	UsedAddressID *uint
	Places        []Place

	ModelInput []*schema.Message
}

func (s *Service) Prepare(ctx context.Context, userID uint, req ChatRequest) (*Prepared, error) {
	if err := validateHistory(req.Messages); err != nil {
		return nil, err
	}

	loc, usedAddrID, err := s.resolveLocation(ctx, userID, req)
	if err != nil {
		return nil, err
	}

	places, err := s.searchPlaces(ctx, loc, req)
	if err != nil {
		return nil, err
	}

	msgs, err := buildModelInput(req.Messages, loc, places)
	if err != nil {
		return nil, err
	}

	return &Prepared{
		Location:      loc,
		UsedAddressID: usedAddrID,
		Places:        places,
		ModelInput:    msgs,
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
	return nil
}

func (s *Service) StreamAnswer(ctx context.Context, prepared *Prepared) (*schema.StreamReader[*schema.Message], error) {
	if prepared == nil {
		return nil, fmt.Errorf("nil prepared")
	}
	return s.llm.Stream(ctx, prepared.ModelInput)
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
	if strings.TrimSpace(addr.Location) == "" {
		return "", nil, fmt.Errorf("address location is empty: %w", errInvalidInput)
	}
	if _, _, err := parseLocation(addr.Location); err != nil {
		return "", nil, fmt.Errorf("invalid address location: %w", errInvalidInput)
	}
	id := *req.AddressID
	return strings.TrimSpace(addr.Location), &id, nil
}

func (s *Service) searchPlaces(ctx context.Context, location string, req ChatRequest) ([]Place, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 10
	}
	// POI2 page_size is typically 1-25.
	if limit > 25 {
		limit = 25
	}

	radius := req.Radius
	if radius <= 0 {
		radius = 5000
	}
	if radius > 50000 {
		radius = 50000
	}

	args := map[string]any{
		"location":  location,
		"keywords":  strings.TrimSpace(req.Keywords),
		"types":     strings.TrimSpace(req.Types),
		"radius":    radius,
		"sortrule":  strings.TrimSpace(req.SortRule),
		"region":    strings.TrimSpace(req.Region),
		"page_size": limit,
		"page_num":  1,
	}
	if args["types"] == "" {
		args["types"] = "050000"
	}
	if args["sortrule"] == "" {
		args["sortrule"] = "distance"
	}
	if req.CityLimit != nil {
		args["city_limit"] = *req.CityLimit
	}

	callCtx, cancel := context.WithTimeout(ctx, time.Duration(s.mcpCallTimeoutSeconds)*time.Second)
	defer cancel()
	txt, err := s.mcp.CallToolText(callCtx, "amap.poi2_around", args)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Status   string            `json:"status"`
		Info     string            `json:"info"`
		Infocode string            `json:"infocode"`
		Count    string            `json:"count"`
		Pois     []json.RawMessage `json:"pois"`
	}
	if err := json.Unmarshal([]byte(txt), &resp); err != nil {
		return nil, fmt.Errorf("invalid poi2 response json: %w", err)
	}
	if strings.TrimSpace(resp.Status) != "1" {
		return nil, fmt.Errorf("amap poi2 failed: %s(%s)", strings.TrimSpace(resp.Info), strings.TrimSpace(resp.Infocode))
	}

	type poiCommon struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Location string `json:"location"`
		Address  string `json:"address"`
		Distance string `json:"distance"`

		Type     string `json:"type"`
		TypeCode string `json:"typecode"`

		Pname    string `json:"pname"`
		Cityname string `json:"cityname"`
		Adname   string `json:"adname"`
		Citycode string `json:"citycode"`
		Adcode   string `json:"adcode"`

		Tel string `json:"tel"`
		Tag string `json:"tag"`
	}

	out := make([]Place, 0, minInt(limit, len(resp.Pois)))
	for _, raw := range resp.Pois {
		if len(raw) == 0 || !json.Valid(raw) {
			continue
		}
		var c poiCommon
		_ = json.Unmarshal(raw, &c)
		out = append(out, Place{
			ID:       strings.TrimSpace(c.ID),
			Name:     strings.TrimSpace(c.Name),
			Location: strings.TrimSpace(c.Location),
			Address:  strings.TrimSpace(c.Address),
			Distance: strings.TrimSpace(c.Distance),
			Type:     strings.TrimSpace(c.Type),
			TypeCode: strings.TrimSpace(c.TypeCode),
			Pname:    strings.TrimSpace(c.Pname),
			Cityname: strings.TrimSpace(c.Cityname),
			Adname:   strings.TrimSpace(c.Adname),
			Citycode: strings.TrimSpace(c.Citycode),
			Adcode:   strings.TrimSpace(c.Adcode),
			Tel:      strings.TrimSpace(c.Tel),
			Tag:      strings.TrimSpace(c.Tag),
			Raw:      raw,
		})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func buildModelInput(history []ChatMessage, location string, places []Place) ([]*schema.Message, error) {
	msgs := make([]*schema.Message, 0, len(history)+2)

	// A small system instruction for safety & formatting.
	msgs = append(msgs, schema.SystemMessage(strings.TrimSpace(fmt.Sprintf(
		"你是一个美食助手。用户位置为 %s。请基于候选店铺列表给出简洁可执行的推荐（可给出 3-6 家），并说明理由（距离/口味/人均/营业时间等）。不要编造未提供的信息；如果缺字段就不要乱填。",
		location,
	))))

	for _, m := range history {
		role := strings.ToLower(strings.TrimSpace(m.Role))
		content := strings.TrimSpace(m.Content)
		if content == "" {
			continue
		}
		switch role {
		case "", "user":
			msgs = append(msgs, schema.UserMessage(content))
		case "assistant":
			msgs = append(msgs, schema.AssistantMessage(content, nil))
		case "system":
			return nil, fmt.Errorf("invalid message role: system is not allowed: %w", errInvalidInput)
		default:
			return nil, fmt.Errorf("invalid message role: %s: %w", m.Role, errInvalidInput)
		}
	}

	// Provide candidate places as compact JSON for the model.
	type placeForPrompt struct {
		ID       string `json:"id,omitempty"`
		Name     string `json:"name,omitempty"`
		Address  string `json:"address,omitempty"`
		Location string `json:"location,omitempty"`
		Distance string `json:"distance,omitempty"`
		Tel      string `json:"tel,omitempty"`
		TypeCode string `json:"typecode,omitempty"`
	}
	compact := make([]placeForPrompt, 0, len(places))
	for _, p := range places {
		compact = append(compact, placeForPrompt{
			ID:       p.ID,
			Name:     p.Name,
			Address:  p.Address,
			Location: p.Location,
			Distance: p.Distance,
			Tel:      p.Tel,
			TypeCode: p.TypeCode,
		})
	}
	b, _ := json.Marshal(compact)
	msgs = append(msgs, schema.SystemMessage("候选店铺列表(JSON): "+string(b)))

	return msgs, nil
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

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
