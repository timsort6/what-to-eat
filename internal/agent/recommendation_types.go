package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/go-playground/validator/v10"
)

var ErrInvalidRecommendation = errors.New("推荐契约无效")
var recommendationValidator = validator.New()

const MaxDecisionBytes = 64 * 1024

type MealStyle string

const (
	MealAny     MealStyle = "any"
	MealQuick   MealStyle = "quick"
	MealRegular MealStyle = "regular"
	MealLeisure MealStyle = "leisure"
)

type Preferences struct {
	TargetCount       int       `json:"target_count" validate:"min=1,max=10"`
	RadiusMeters      int       `json:"radius_meters" validate:"min=1,max=50000"`
	MealStyle         MealStyle `json:"meal_style" validate:"oneof=any quick regular leisure"`
	MaxPriceCNY       *int      `json:"max_price_cny" validate:"omitempty,min=1,max=10000"`
	AllowUnknownPrice bool      `json:"allow_unknown_price"`
	FoodRequirement   *string   `json:"food_requirement" validate:"omitempty,max=100"`
}

func DefaultPreferences() Preferences {
	return Preferences{TargetCount: 3, RadiusMeters: 3000, MealStyle: MealAny, AllowUnknownPrice: true}
}

func (p Preferences) Validate() error {
	if err := recommendationValidator.Struct(p); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidRecommendation, err)
	}
	return nil
}

func (p *Preferences) UnmarshalJSON(data []byte) error {
	var changes PreferenceChanges
	if err := json.Unmarshal(data, &changes); err != nil {
		return err
	}
	if !changes.Complete() {
		return fmt.Errorf("旧条件不完整: %w", ErrInvalidRecommendation)
	}
	*p = Preferences{TargetCount: *changes.TargetCount, RadiusMeters: *changes.RadiusMeters, MealStyle: *changes.MealStyle, MaxPriceCNY: changes.MaxPriceCNY.Value, AllowUnknownPrice: *changes.AllowUnknownPrice, FoodRequirement: changes.FoodRequirement.Value}
	return p.Validate()
}

type NullableChange[T any] struct {
	Present bool
	Value   *T
}

func (c *NullableChange[T]) UnmarshalJSON(data []byte) error {
	c.Present = true
	return json.Unmarshal(data, &c.Value)
}

func (c NullableChange[T]) IsZero() bool                 { return !c.Present }
func (c NullableChange[T]) MarshalJSON() ([]byte, error) { return json.Marshal(c.Value) }

type PreferenceChanges struct {
	TargetCount       *int                   `json:"target_count,omitempty"`
	RadiusMeters      *int                   `json:"radius_meters,omitempty"`
	MealStyle         *MealStyle             `json:"meal_style,omitempty"`
	MaxPriceCNY       NullableChange[int]    `json:"max_price_cny,omitzero"`
	AllowUnknownPrice *bool                  `json:"allow_unknown_price,omitempty"`
	FoodRequirement   NullableChange[string] `json:"food_requirement,omitzero"`
}

func (c *PreferenceChanges) UnmarshalJSON(data []byte) error {
	if len(data) > MaxDecisionBytes {
		return ErrInvalidRecommendation
	}
	fields, err := DecodeRecommendationFields(data, []string{"target_count", "radius_meters", "meal_style", "max_price_cny", "allow_unknown_price", "food_requirement"})
	if err != nil {
		return err
	}
	for _, key := range []string{"target_count", "radius_meters", "meal_style", "allow_unknown_price"} {
		if bytes.Equal(bytes.TrimSpace(fields[key]), []byte("null")) {
			return fmt.Errorf("%s 不允许空值: %w", key, ErrInvalidRecommendation)
		}
	}
	type wire PreferenceChanges
	return decodeRecommendationJSON(data, (*wire)(c))
}

func DecodeRecommendationFields(data []byte, allowed []string) (map[string]json.RawMessage, error) {
	if !json.Valid(data) || bytes.TrimSpace(data)[0] != '{' {
		return nil, ErrInvalidRecommendation
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if _, err := decoder.Token(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRecommendation, err)
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidRecommendation, err)
		}
		key, ok := token.(string)
		if !ok || !slices.Contains(allowed, key) {
			return nil, fmt.Errorf("存在非精确或未知字段: %w", ErrInvalidRecommendation)
		}
		if _, exists := fields[key]; exists {
			return nil, fmt.Errorf("字段重复: %w", ErrInvalidRecommendation)
		}
		var field json.RawMessage
		if err := decoder.Decode(&field); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidRecommendation, err)
		}
		fields[key] = field
	}
	return fields, nil
}

func (c PreferenceChanges) Complete() bool {
	return c.TargetCount != nil && c.RadiusMeters != nil && c.MealStyle != nil && c.MaxPriceCNY.Present && c.AllowUnknownPrice != nil && c.FoodRequirement.Present
}

func (c PreferenceChanges) Apply(previous Preferences) (Preferences, error) {
	next := previous
	if c.TargetCount != nil {
		next.TargetCount = *c.TargetCount
	}
	if c.RadiusMeters != nil {
		if *c.RadiusMeters < 100 || *c.RadiusMeters%100 != 0 {
			return Preferences{}, fmt.Errorf("新半径须以100米为步长: %w", ErrInvalidRecommendation)
		}
		next.RadiusMeters = *c.RadiusMeters
	}
	if c.MealStyle != nil {
		next.MealStyle = *c.MealStyle
	}
	if c.MaxPriceCNY.Present {
		next.MaxPriceCNY = c.MaxPriceCNY.Value
	}
	if c.AllowUnknownPrice != nil {
		next.AllowUnknownPrice = *c.AllowUnknownPrice
	}
	if c.FoodRequirement.Present {
		next.FoodRequirement = c.FoodRequirement.Value
	}
	return next, next.Validate()
}

type RecommendationSelection struct {
	OptionID            string            `json:"option_id"`
	PreviousPreferences Preferences       `json:"previous_preferences"`
	Changes             PreferenceChanges `json:"changes"`
}

func (s RecommendationSelection) Validate(current Preferences) error {
	if !boundedText(s.OptionID, 64) {
		return ErrInvalidRecommendation
	}
	if err := s.PreviousPreferences.Validate(); err != nil {
		return err
	}
	next, err := s.Changes.Apply(s.PreviousPreferences)
	if err != nil {
		return err
	}
	if reflect.DeepEqual(next, s.PreviousPreferences) || !reflect.DeepEqual(next, current) {
		return fmt.Errorf("选择差异与本次完整条件不一致: %w", ErrInvalidRecommendation)
	}
	return nil
}

func (s *RecommendationSelection) UnmarshalJSON(data []byte) error {
	type wire RecommendationSelection
	return decodeRecommendationJSON(data, (*wire)(s))
}

type MatchState string

const (
	MatchSatisfied   MatchState = "satisfied"
	MatchUnsatisfied MatchState = "unsatisfied"
	MatchUnknown     MatchState = "unknown"
)

type EvidenceField string

type MatchEvidence struct {
	State  MatchState      `json:"state" validate:"oneof=satisfied unsatisfied unknown"`
	Fields []EvidenceField `json:"fields" validate:"required,max=8,unique,dive,oneof=name type typecode tag location business.cost business.currency business.cost_basis"`
}

type CandidateAssessment struct {
	CandidateID string        `json:"candidate_id"`
	Range       MatchEvidence `json:"range"`
	Food        MatchEvidence `json:"food"`
	Price       MatchEvidence `json:"price"`
}

type DecisionStatus string
type ReasonCode string

const (
	DecisionReady             DecisionStatus = "ready"
	DecisionNeedsChoice       DecisionStatus = "needs_choice"
	ReasonNoCandidates        ReasonCode     = "no_candidates"
	ReasonInsufficientCount   ReasonCode     = "insufficient_count"
	ReasonOutsideRadius       ReasonCode     = "outside_radius"
	ReasonFoodMismatch        ReasonCode     = "food_mismatch"
	ReasonOverBudget          ReasonCode     = "over_budget"
	ReasonUnknownPrice        ReasonCode     = "unknown_price"
	ReasonPreferencesConflict ReasonCode     = "preferences_conflict"
	ReasonUnknownEvidence     ReasonCode     = "unknown_evidence"
)

type RecommendationOption struct {
	OptionID    string            `json:"option_id"`
	Label       string            `json:"label"`
	ObservedIDs []string          `json:"observed_ids" validate:"required,max=10,unique"`
	ReasonCodes []ReasonCode      `json:"reason_codes" validate:"required,max=8,unique,dive,oneof=no_candidates insufficient_count outside_radius food_mismatch over_budget unknown_price preferences_conflict unknown_evidence"`
	Changes     PreferenceChanges `json:"changes"`
}

// FoodInterpretation 是决策阶段对最新用户原话中品类要求的有界理解；仅在请求品类为空时可被采纳，quote 须为最新用户消息的子串。
type FoodInterpretation struct {
	Requirement string `json:"requirement"`
	Quote       string `json:"quote"`
	Ambiguous   bool   `json:"ambiguous"`
}

// UnmarshalJSON 要求 requirement、quote、ambiguous 三个键齐全且精确，缺键不按零值接受。
func (f *FoodInterpretation) UnmarshalJSON(data []byte) error {
	fields, err := DecodeRecommendationFields(data, []string{"requirement", "quote", "ambiguous"})
	if err != nil {
		return err
	}
	if len(fields) != 3 {
		return fmt.Errorf("food_interpretation 缺少字段: %w", ErrInvalidRecommendation)
	}
	type wire FoodInterpretation
	return decodeRecommendationJSON(data, (*wire)(f))
}

// PreferenceConflict 表示最新用户原话与控件条件冲突；只用于澄清型 needs_choice，从不自动采用 proposed。
type PreferenceConflict struct {
	Field    string            `json:"field"`
	Quote    string            `json:"quote"`
	Proposed PreferenceChanges `json:"proposed"`
}

const MaxQuoteRunes = 100

var preferenceFieldNames = []string{"target_count", "radius_meters", "meal_style", "max_price_cny", "allow_unknown_price", "food_requirement"}

type RecommendationDecision struct {
	Status             DecisionStatus         `json:"status"`
	SelectedIDs        []string               `json:"selected_ids" validate:"required,max=10,unique"`
	Assessments        []CandidateAssessment  `json:"assessments" validate:"required,max=225,unique=CandidateID,dive"`
	ReasonCodes        []ReasonCode           `json:"reason_codes" validate:"required,max=8,unique,dive,oneof=no_candidates insufficient_count outside_radius food_mismatch over_budget unknown_price preferences_conflict unknown_evidence"`
	Options            []RecommendationOption `json:"options" validate:"required,max=3,unique=OptionID,dive"`
	FoodInterpretation *FoodInterpretation    `json:"food_interpretation,omitempty"`
	Conflict           *PreferenceConflict    `json:"conflict,omitempty"`
}

func ParseRecommendationDecision(data []byte) (RecommendationDecision, error) {
	var decision RecommendationDecision
	if err := decodeRecommendationJSON(data, &decision); err != nil {
		return decision, err
	}
	return decision, decision.Validate()
}

func (d RecommendationDecision) Validate() error {
	if err := recommendationValidator.Struct(d); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidRecommendation, err)
	}
	switch d.Status {
	case DecisionReady:
		if len(d.SelectedIDs) == 0 || len(d.Options) != 0 {
			return ErrInvalidRecommendation
		}
	case DecisionNeedsChoice:
		if len(d.SelectedIDs) != 0 {
			return ErrInvalidRecommendation
		}
	default:
		return ErrInvalidRecommendation
	}
	// assessments 由服务端生成（见 RecommendationPolicy.ParseDecision），不再要求覆盖 selected_ids/observed_ids；候选归属由策略校验。
	for _, assessment := range d.Assessments {
		if !boundedText(assessment.CandidateID, 256) {
			return ErrInvalidRecommendation
		}
		for _, evidence := range []MatchEvidence{assessment.Range, assessment.Food, assessment.Price} {
			if evidence.State != MatchUnknown && len(evidence.Fields) == 0 {
				return ErrInvalidRecommendation
			}
		}
	}
	for _, option := range d.Options {
		if !boundedText(option.OptionID, 64) || !boundedText(option.Label, 200) {
			return ErrInvalidRecommendation
		}
		if _, err := option.Changes.Apply(DefaultPreferences()); err != nil || reflect.DeepEqual(option.Changes, PreferenceChanges{}) {
			return ErrInvalidRecommendation
		}
	}
	if fi := d.FoodInterpretation; fi != nil && (!boundedText(fi.Requirement, 100) || !boundedText(fi.Quote, MaxQuoteRunes)) {
		return ErrInvalidRecommendation
	}
	if c := d.Conflict; c != nil {
		if !slices.Contains(preferenceFieldNames, c.Field) || !boundedText(c.Quote, MaxQuoteRunes) {
			return ErrInvalidRecommendation
		}
		// proposed 只能包含 field 这一个键（含显式 null）。
		encoded, err := json.Marshal(c.Proposed)
		var keys map[string]json.RawMessage
		if err != nil || json.Unmarshal(encoded, &keys) != nil || len(keys) != 1 || keys[c.Field] == nil {
			return ErrInvalidRecommendation
		}
		if _, err := c.Proposed.Apply(DefaultPreferences()); err != nil {
			return ErrInvalidRecommendation
		}
	}
	return nil
}

func boundedText(value string, limit int) bool {
	return strings.TrimSpace(value) != "" && utf8.RuneCountInString(value) <= limit
}

func decodeRecommendationJSON(data []byte, value any) error {
	if len(data) > MaxDecisionBytes || len(bytes.TrimSpace(data)) == 0 || bytes.TrimSpace(data)[0] != '{' || !json.Valid(data) {
		return ErrInvalidRecommendation
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidRecommendation, err)
	}
	return nil
}
