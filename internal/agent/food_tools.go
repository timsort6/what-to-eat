package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"
)

const (
	ToolNamePOI2Around = "amap.poi2_around"
	ToolNamePOI2Text   = "amap.poi2_text"
)

var (
	ErrUnknownTool      = errors.New("unknown food tool")
	ErrInvalidScope     = errors.New("invalid food tool request scope")
	ErrInvalidToolInput = errors.New("invalid food tool input")
)

type AmapMCP interface {
	CallToolText(ctx context.Context, name string, arguments map[string]any) (string, error)
}

type RequestScope struct {
	UserID    uint
	Location  string
	AddressID *uint
}

type FoodToolDeps struct {
	MCP          AmapMCP
	Scope        RequestScope
	AllowedTools []string
	Preferences  *Preferences
}

type SearchResult struct {
	Places      []Place                 `json:"places"`
	PlacesCount int                     `json:"places_count"`
	Location    string                  `json:"location,omitempty"`
	Decision    *RecommendationDecision `json:"decision,omitempty"`
}

type PriceSources struct {
	Cost      string `json:"business.cost"`
	Currency  string `json:"business.currency"`
	CostBasis string `json:"business.cost_basis"`
}

func priceSources(nested bool) PriceSources {
	if nested {
		return PriceSources{Cost: "business.cost", Currency: "business.currency", CostBasis: "business.cost_basis"}
	}
	return PriceSources{Cost: "cost", Currency: "cost_currency", CostBasis: "cost_basis"}
}

type aroundSearchInput struct {
	Keywords   string `json:"keywords,omitempty"`
	Types      string `json:"types,omitempty"`
	Radius     int    `json:"radius,omitempty"`
	SortRule   string `json:"sortrule,omitempty"`
	Region     string `json:"region,omitempty"`
	CityLimit  *bool  `json:"city_limit,omitempty"`
	ShowFields string `json:"show_fields,omitempty"`
	PageSize   int    `json:"page_size,omitempty"`
	PageNum    int    `json:"page_num,omitempty"`
}

type textSearchInput struct {
	Keywords   string `json:"keywords,omitempty"`
	Types      string `json:"types,omitempty"`
	Region     string `json:"region,omitempty"`
	CityLimit  *bool  `json:"city_limit,omitempty"`
	ShowFields string `json:"show_fields,omitempty"`
	PageSize   int    `json:"page_size,omitempty"`
	PageNum    int    `json:"page_num,omitempty"`
}

func NewFoodTools(d FoodToolDeps) (map[string]tool.InvokableTool, error) {
	if d.MCP == nil {
		return nil, errors.New("nil amap mcp")
	}
	if err := validateScope(d.Scope); err != nil {
		return nil, err
	}
	preferences := Preferences{TargetCount: 10, RadiusMeters: 5000, MealStyle: MealAny, AllowUnknownPrice: true}
	if d.Preferences != nil {
		preferences = *d.Preferences
	}
	policy, err := NewRecommendationPolicy(d.Scope.Location, preferences)
	if err != nil {
		return nil, err
	}
	d.Preferences = &policy.preferences

	enabled := make(map[string]struct{}, len(d.AllowedTools))
	for _, name := range d.AllowedTools {
		name = strings.TrimSpace(name)
		if !isAllowedTool(name) {
			return nil, fmt.Errorf("%w: %q", ErrUnknownTool, name)
		}
		enabled[name] = struct{}{}
	}

	tools := make(map[string]tool.InvokableTool, len(enabled))
	if _, ok := enabled[ToolNamePOI2Around]; ok {
		params := aroundToolParams()
		delete(params, "radius")
		delete(params, "show_fields")
		around := newFoodTool(
			ToolNamePOI2Around,
			"在用户授权位置附近搜索地点。位置由服务端固定，模型只能提供搜索条件。",
			params,
			func(ctx context.Context, input aroundSearchInput) (SearchResult, error) {
				return d.invokeAround(ctx, input)
			},
		)
		tools[ToolNamePOI2Around] = around
	}
	if _, ok := enabled[ToolNamePOI2Text]; ok {
		params := textToolParams()
		delete(params, "show_fields")
		text := newFoodTool(
			ToolNamePOI2Text,
			"按关键词搜索地点。用户身份和授权位置由服务端固定，模型只能提供搜索条件。",
			params,
			func(ctx context.Context, input textSearchInput) (SearchResult, error) {
				return d.invokeText(ctx, input)
			},
		)
		tools[ToolNamePOI2Text] = text
	}
	return tools, nil
}

func WithRequestScope(ctx context.Context, scope RequestScope) context.Context {
	return context.WithValue(ctx, requestScopeKey{}, scope)
}

func RequestScopeFromContext(ctx context.Context) (RequestScope, bool) {
	scope, ok := ctx.Value(requestScopeKey{}).(RequestScope)
	return scope, ok
}

type requestScopeKey struct{}
type recommendationPreferencesKey struct{}

func WithRecommendationPreferences(ctx context.Context, preferences Preferences) context.Context {
	return context.WithValue(ctx, recommendationPreferencesKey{}, preferences)
}

func RecommendationPreferencesFromContext(ctx context.Context) (Preferences, bool) {
	preferences, ok := ctx.Value(recommendationPreferencesKey{}).(Preferences)
	return preferences, ok
}

func (d FoodToolDeps) invokeAround(ctx context.Context, input aroundSearchInput) (SearchResult, error) {
	if preferences, ok := ctx.Value(recommendationPreferencesKey{}).(Preferences); ok {
		d.Preferences = &preferences
	}
	if err := d.Preferences.Validate(); err != nil {
		return SearchResult{}, err
	}
	args := map[string]any{
		"location":    d.Scope.Location,
		"keywords":    strings.TrimSpace(input.Keywords),
		"types":       strings.TrimSpace(input.Types),
		"radius":      d.Preferences.RadiusMeters,
		"sortrule":    strings.TrimSpace(input.SortRule),
		"region":      strings.TrimSpace(input.Region),
		"show_fields": "business",
		"page_size":   boundedPageSize(input.PageSize),
		"page_num":    boundedPageNum(input.PageNum),
	}
	if args["types"] == "" {
		args["types"] = "050000"
	}
	if args["sortrule"] == "" {
		args["sortrule"] = "distance"
	}
	if input.CityLimit != nil {
		args["city_limit"] = *input.CityLimit
	}
	return d.callSearch(ctx, ToolNamePOI2Around, args)
}

func (d FoodToolDeps) invokeText(ctx context.Context, input textSearchInput) (SearchResult, error) {
	if preferences, ok := ctx.Value(recommendationPreferencesKey{}).(Preferences); ok {
		d.Preferences = &preferences
	}
	if err := d.Preferences.Validate(); err != nil {
		return SearchResult{}, err
	}
	keywords := strings.TrimSpace(input.Keywords)
	types := strings.TrimSpace(input.Types)
	if keywords == "" && types == "" {
		return SearchResult{}, fmt.Errorf("%w: keywords or types is required", ErrInvalidToolInput)
	}
	args := map[string]any{
		"keywords":    keywords,
		"types":       types,
		"region":      strings.TrimSpace(input.Region),
		"show_fields": "business",
		"page_size":   boundedPageSize(input.PageSize),
		"page_num":    boundedPageNum(input.PageNum),
	}
	if input.CityLimit != nil {
		args["city_limit"] = *input.CityLimit
	}
	return d.callSearch(ctx, ToolNamePOI2Text, args)
}

func (d FoodToolDeps) callSearch(ctx context.Context, name string, args map[string]any) (SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return SearchResult{}, err
	}
	callCtx := WithRequestScope(ctx, d.Scope)
	txt, err := d.MCP.CallToolText(callCtx, name, args)
	if err != nil {
		return SearchResult{}, fmt.Errorf("call %s: %w", name, err)
	}
	limit, ok := args["page_size"].(int)
	if !ok {
		return SearchResult{}, ErrInvalidToolInput
	}
	result, err := parseSearchResult(txt, limit, d.Scope)
	if err != nil {
		return SearchResult{}, fmt.Errorf("parse %s response: %w", name, err)
	}
	decision, err := EvaluateCandidates(d.Scope.Location, *d.Preferences, result.Places)
	if err != nil {
		return SearchResult{}, fmt.Errorf("evaluate %s response: %w", name, err)
	}
	result.Decision = &decision
	for index := range result.Places {
		for _, assessment := range decision.Assessments {
			if assessment.CandidateID == result.Places[index].CandidateKey {
				result.Places[index].Evaluation = &assessment
				break
			}
		}
	}
	return result, nil
}

func validateScope(scope RequestScope) error {
	if scope.UserID == 0 || strings.TrimSpace(scope.Location) == "" {
		return fmt.Errorf("%w: user and location are required", ErrInvalidScope)
	}
	if _, _, err := parseCoordinate(scope.Location); err != nil {
		return fmt.Errorf("%w: location must be a valid lng,lat", ErrInvalidScope)
	}
	return nil
}

func isAllowedTool(name string) bool {
	return name == ToolNamePOI2Around || name == ToolNamePOI2Text
}

func boundedPageSize(pageSize int) int {
	if pageSize <= 0 {
		return 10
	}
	if pageSize > 25 {
		return 25
	}
	return pageSize
}

func boundedPageNum(pageNum int) int {
	if pageNum <= 0 {
		return 1
	}
	return pageNum
}
