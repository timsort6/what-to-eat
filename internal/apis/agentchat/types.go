package agentchat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"food-agent/internal/agent"
)

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Messages []ChatMessage `json:"messages"`

	// Location context (required: at least one).
	AddressID *uint  `json:"address_id,omitempty"`
	Location  string `json:"location,omitempty"` // "lng,lat"

	// Optional search preferences passed to AMap POI2 around.
	Keywords       string                         `json:"keywords,omitempty"`
	Types          string                         `json:"types,omitempty"`
	Radius         int                            `json:"radius,omitempty"`
	SortRule       string                         `json:"sortrule,omitempty"`
	Region         string                         `json:"region,omitempty"`
	CityLimit      *bool                          `json:"city_limit,omitempty"`
	Limit          int                            `json:"limit,omitempty"` // how many places to return (default 10)
	Preferences    *agent.PreferenceChanges       `json:"preferences,omitempty"`
	Selection      *agent.RecommendationSelection `json:"selection,omitempty"`
	radiusProvided bool
	limitProvided  bool
}

func (r *ChatRequest) UnmarshalJSON(data []byte) error {
	if !json.Valid(data) || bytes.TrimSpace(data)[0] != '{' {
		return errInvalidInput
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if _, err := decoder.Token(); err != nil {
		return fmt.Errorf("%w: %v", errInvalidInput, err)
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("%w: %v", errInvalidInput, err)
		}
		key, ok := token.(string)
		if !ok {
			return errInvalidInput
		}
		if strings.EqualFold(key, "preferences") {
			_, exists := fields["preferences"]
			if key != "preferences" || exists {
				return fmt.Errorf("条件字段非精确或重复: %w", errInvalidInput)
			}
		}
		var field json.RawMessage
		if err := decoder.Decode(&field); err != nil {
			return fmt.Errorf("%w: %v", errInvalidInput, err)
		}
		fields[key] = field
	}
	// 有意只对 preferences 做精确键/去重：任务1要求普通字段保持标准 JSON 行为（大小写不敏感、重复键后者覆盖）以兼容旧客户端。
	// 下方 null 检查只认精确键；大小写变体绕过它也无安全影响："Radius"/"Limit":null 不改零值但仍记为已提供，
	// 会被 NormalizePreferences 的 1..上限 越界检查拒绝；"Selection":null 等同未传；非 null 值照常走越界与新旧冲突校验。
	for _, name := range []string{"radius", "limit", "preferences", "selection"} {
		if bytes.Equal(bytes.TrimSpace(fields[name]), []byte("null")) {
			return fmt.Errorf("%s 不允许空值: %w", name, errInvalidInput)
		}
	}
	type wire ChatRequest
	var value wire
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	for key := range fields {
		value.radiusProvided = value.radiusProvided || strings.EqualFold(key, "radius")
		value.limitProvided = value.limitProvided || strings.EqualFold(key, "limit")
	}
	*r = ChatRequest(value)
	return nil
}

func (r ChatRequest) NormalizePreferences() (agent.Preferences, error) {
	preferences := agent.DefaultPreferences()
	hasRadius, hasLimit := r.radiusProvided || r.Radius != 0, r.limitProvided || r.Limit != 0
	if (hasRadius && (r.Radius < 1 || r.Radius > 50000)) || (hasLimit && (r.Limit < 1 || r.Limit > 10)) {
		return preferences, fmt.Errorf("旧数量或半径越界: %w", errInvalidInput)
	}
	if r.Preferences == nil {
		preferences.RadiusMeters, preferences.TargetCount = 5000, 10
		if hasRadius {
			preferences.RadiusMeters = r.Radius
		}
		if hasLimit {
			preferences.TargetCount = r.Limit
		}
	} else {
		var err error
		preferences, err = r.Preferences.Apply(preferences)
		if err != nil {
			return preferences, fmt.Errorf("%w: %v", errInvalidInput, err)
		}
		if (hasRadius && r.Radius != preferences.RadiusMeters) || (hasLimit && r.Limit != preferences.TargetCount) {
			return preferences, fmt.Errorf("新旧条件冲突: %w", errInvalidInput)
		}
	}
	if r.Selection != nil {
		if r.Preferences == nil || !r.Preferences.Complete() {
			return preferences, fmt.Errorf("选择需要完整新条件: %w", errInvalidInput)
		}
		if err := r.Selection.Validate(preferences); err != nil {
			return preferences, fmt.Errorf("%w: %v", errInvalidInput, err)
		}
	}
	return preferences, nil
}

type Place struct {
	// Common fields (best-effort extraction from AMap POI2).
	ID           string `json:"id,omitempty"`
	CandidateKey string `json:"candidate_key,omitempty"`
	Name         string `json:"name,omitempty"`
	Location     string `json:"location,omitempty"` // "lng,lat"
	Address      string `json:"address,omitempty"`
	Distance     string `json:"distance,omitempty"`

	Type     string `json:"type,omitempty"`
	TypeCode string `json:"typecode,omitempty"`

	Pname    string `json:"pname,omitempty"`
	Cityname string `json:"cityname,omitempty"`
	Adname   string `json:"adname,omitempty"`
	Citycode string `json:"citycode,omitempty"`
	Adcode   string `json:"adcode,omitempty"`

	Tel           string                     `json:"-"`
	Tag           string                     `json:"tag,omitempty"`
	Cost          string                     `json:"cost,omitempty"`
	CostCurrency  string                     `json:"cost_currency,omitempty"`
	CostBasis     string                     `json:"cost_basis,omitempty"`
	OpentimeToday string                     `json:"opentime_today,omitempty"`
	OpentimeWeek  string                     `json:"opentime_week,omitempty"`
	Evaluation    *agent.CandidateAssessment `json:"evaluation,omitempty"`

	Raw json.RawMessage `json:"-"`
}

type DoneData struct {
	AnswerText    string  `json:"answer_text"`
	Places        []Place `json:"places"`
	PlacesCount   int     `json:"places_count"`
	Location      string  `json:"location,omitempty"`
	UsedAddressID *uint   `json:"address_id,omitempty"`

	// 新版字段：与先前唯一的 decision 事件是同一快照；旧客户端忽略即可。
	Status      agent.DecisionStatus `json:"status"`
	Preferences agent.Preferences    `json:"preferences"`
	Decision    agent.PublicDecision `json:"decision"`
}
