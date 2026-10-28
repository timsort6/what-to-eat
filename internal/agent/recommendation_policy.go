package agent

import (
	"cmp"
	"math"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

const MaxCandidates = 225

type RecommendationPolicy struct {
	origin      string
	preferences Preferences
	places      []Place
	latestUser  string // 最新用户消息原文：品类理解与冲突引用的唯一来源
}

func NewRecommendationPolicy(origin string, preferences Preferences) (*RecommendationPolicy, error) {
	if _, _, err := parseCoordinate(origin); err != nil {
		return nil, err
	}
	if err := preferences.Validate(); err != nil {
		return nil, err
	}
	if preferences.MaxPriceCNY != nil {
		value := *preferences.MaxPriceCNY
		preferences.MaxPriceCNY = &value
	}
	if preferences.FoodRequirement != nil {
		value := *preferences.FoodRequirement
		preferences.FoodRequirement = &value
	}
	return &RecommendationPolicy{origin: origin, preferences: preferences, places: []Place{}}, nil
}

func (policy *RecommendationPolicy) AddCandidates(places []Place) error {
	if len(places) > MaxCandidates {
		return ErrInvalidRecommendation
	}
	next := append([]Place{}, policy.places...)
	seen := make(map[string]bool, len(next))
	for _, place := range next {
		seen[place.CandidateKey] = true
	}
	for _, source := range places {
		place := normalizeCandidate(source)
		if place.Name == "" || seen[place.CandidateKey] {
			continue
		}
		if len(next) == MaxCandidates {
			return ErrInvalidRecommendation
		}
		seen[place.CandidateKey] = true
		if distance, ok := candidateDistance(policy.origin, place.Location); ok {
			place.Distance = strconv.FormatFloat(distance, 'f', 3, 64)
		}
		next = append(next, place)
	}
	policy.places = next
	return nil
}

func EvaluateCandidates(origin string, preferences Preferences, places []Place) (RecommendationDecision, error) {
	policy, err := NewRecommendationPolicy(origin, preferences)
	if err != nil {
		return RecommendationDecision{}, err
	}
	if err := policy.AddCandidates(places); err != nil {
		return RecommendationDecision{}, err
	}
	return policy.Evaluate(), nil
}

func (policy *RecommendationPolicy) Evaluate() RecommendationDecision {
	decision := RecommendationDecision{Status: DecisionNeedsChoice, SelectedIDs: []string{}, Assessments: []CandidateAssessment{}, ReasonCodes: []ReasonCode{}, Options: []RecommendationOption{}}
	for _, place := range policy.places {
		assessment := policy.assess(place, policy.preferences)
		decision.Assessments = append(decision.Assessments, assessment)
		if selectableCandidate(assessment, policy.preferences) && len(decision.SelectedIDs) < policy.preferences.TargetCount {
			decision.SelectedIDs = append(decision.SelectedIDs, place.CandidateKey)
		}
	}
	if len(decision.SelectedIDs) > 0 {
		decision.Status = DecisionReady
		if len(decision.SelectedIDs) < policy.preferences.TargetCount {
			decision.ReasonCodes = []ReasonCode{ReasonInsufficientCount}
		}
	} else if len(policy.places) == 0 {
		decision.ReasonCodes = []ReasonCode{ReasonNoCandidates}
	} else {
		decision.ReasonCodes = []ReasonCode{ReasonPreferencesConflict}
	}
	return decision
}

func (policy *RecommendationPolicy) assess(place Place, preferences Preferences) CandidateAssessment {
	assessment := CandidateAssessment{CandidateID: place.CandidateKey, Range: MatchEvidence{State: MatchUnknown, Fields: []EvidenceField{}}, Food: assessFood(preferences.FoodRequirement, place), Price: assessPrice(preferences.MaxPriceCNY, place)}
	if distance, ok := candidateDistance(policy.origin, place.Location); ok {
		assessment.Range = MatchEvidence{State: MatchSatisfied, Fields: []EvidenceField{"location"}}
		if distance > float64(preferences.RadiusMeters)+1e-7 {
			assessment.Range.State = MatchUnsatisfied
		}
	}
	return assessment
}

func selectableCandidate(assessment CandidateAssessment, preferences Preferences) bool {
	return assessment.Range.State == MatchSatisfied && assessment.Food.State != MatchUnsatisfied && assessment.Price.State != MatchUnsatisfied && (assessment.Price.State != MatchUnknown || preferences.AllowUnknownPrice)
}

func (policy *RecommendationPolicy) find(id string) (Place, bool) {
	for _, place := range policy.places {
		if id == place.CandidateKey || (place.ID != "" && id == place.ID) {
			return place, true
		}
	}
	return Place{}, false
}

// normalizeQuote 去除全部空白并把全角 ASCII 折算为半角，用于原话引用比对。
func normalizeQuote(value string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case unicode.IsSpace(r):
			return -1
		case r >= 0xFF01 && r <= 0xFF5E:
			return r - 0xFEE0
		}
		return r
	}, value)
}

// quoted 判断 quote 是否为最新用户消息的子串（规范化后）；助手历史与更早轮次不参与。
func (policy *RecommendationPolicy) quoted(quote string) bool {
	needle := normalizeQuote(quote)
	return needle != "" && strings.Contains(normalizeQuote(policy.latestUser), needle)
}

// 只用双字词：单字“别”会误中“特别/区别”等。
var negationWords = []string{"不要", "不吃", "不想", "没有", "除了", "别吃", "别要"}

// negated 判断 requirement 在最新用户原话中首次出现处，同一子句内紧邻的前 4 个字里是否有否定词；是则不得按非含糊采纳。
// shortcut: 只看首次出现、固定词表与前 4 字窗口，不识别双重否定、后置否定（“冒菜就算了”）或更远的否定；出现误判再改为语义校验。
func (policy *RecommendationPolicy) negated(requirement string) bool {
	// “有没有”是疑问而非否定；替换为等长且不含否定词的串，保持窗口字数不变。
	text := strings.ReplaceAll(normalizeQuote(policy.latestUser), "有没有", "有无有")
	needle := normalizeQuote(requirement)
	index := strings.Index(text, needle)
	if needle == "" || index < 0 {
		return false
	}
	before := []rune(text[:index])
	start := len(before)
	for start > 0 && len(before)-start < 4 && !strings.ContainsRune(",.;!?。、", before[start-1]) {
		start--
	}
	window := string(before[start:])
	return slices.ContainsFunc(negationWords, func(word string) bool { return strings.Contains(window, word) })
}

var quoteNumber = regexp.MustCompile(`(\d+(?:\.\d+)?)(公里|千米|km|米|m)?`)
var radiusUnits = map[string]float64{"公里": 1000, "千米": 1000, "km": 1000, "米": 1, "m": 1}

// quoteSupports 判断冲突 quote 是否含 proposed 的取值：半径须为带距离单位且换算成米后相等的数字，数量/限价须为不带距离单位的相等数字，品类须原文出现。
// shortcut: 只认阿拉伯数字（不认“三家”“五公里”等中文数字），出现误拒再扩充。
func quoteSupports(field, quote string, proposed PreferenceChanges) bool {
	text := strings.ToLower(normalizeQuote(quote))
	var want int
	switch {
	case field == "radius_meters" && proposed.RadiusMeters != nil:
		want = *proposed.RadiusMeters
	case field == "target_count" && proposed.TargetCount != nil:
		want = *proposed.TargetCount
	case field == "max_price_cny" && proposed.MaxPriceCNY.Value != nil:
		want = *proposed.MaxPriceCNY.Value
	case field == "food_requirement" && proposed.FoodRequirement.Value != nil:
		return strings.Contains(text, strings.ToLower(normalizeQuote(*proposed.FoodRequirement.Value)))
	default: // meal_style、allow_unknown_price、置空：仍只要求 quote 来自原话
		return true
	}
	for _, match := range quoteNumber.FindAllStringSubmatch(text, -1) {
		scale, hasUnit := radiusUnits[match[2]]
		if hasUnit != (field == "radius_meters") {
			continue
		}
		if !hasUnit {
			scale = 1
		}
		if value, err := strconv.ParseFloat(match[1], 64); err == nil && math.Abs(value*scale-float64(want)) < 1e-6 {
			return true
		}
	}
	return false
}

// interpret 校验品类理解与文字/控件冲突，返回用于评估的策略（采纳品类时为副本）及是否允许澄清型 needs_choice。
func (policy *RecommendationPolicy) interpret(decision RecommendationDecision) (*RecommendationPolicy, bool, error) {
	effective, clarify := policy, false
	if fi := decision.FoodInterpretation; fi != nil {
		if policy.preferences.FoodRequirement != nil || !policy.quoted(fi.Quote) {
			return nil, false, ErrInvalidRecommendation
		}
		if fi.Ambiguous {
			clarify = true
		} else {
			adopted := *policy
			requirement := strings.TrimSpace(fi.Requirement)
			// 采纳的品类必须逐字出现在引用里，防止借“理解”把替代品类说成满足。
			if !strings.Contains(normalizeQuote(fi.Quote), normalizeQuote(requirement)) || policy.negated(requirement) {
				return nil, false, ErrInvalidRecommendation
			}
			adopted.preferences.FoodRequirement = &requirement
			if adopted.preferences.Validate() != nil {
				return nil, false, ErrInvalidRecommendation
			}
			effective = &adopted
		}
	}
	if c := decision.Conflict; c != nil {
		next, err := c.Proposed.Apply(effective.preferences)
		if !policy.quoted(c.Quote) || !quoteSupports(c.Field, c.Quote, c.Proposed) || err != nil || reflect.DeepEqual(next, effective.preferences) ||
			!slices.ContainsFunc(decision.Options, func(option RecommendationOption) bool { return reflect.DeepEqual(option.Changes, c.Proposed) }) {
			return nil, false, ErrInvalidRecommendation
		}
		clarify = true
	}
	if clarify && (decision.Status != DecisionNeedsChoice || len(decision.Options) == 0) {
		return nil, false, ErrInvalidRecommendation
	}
	return effective, clarify, nil
}

// EffectivePreferences 返回决策实际采用的条件（含被采纳的原话品类）。
func (policy *RecommendationPolicy) EffectivePreferences(decision RecommendationDecision) (Preferences, error) {
	effective, _, err := policy.interpret(decision)
	if err != nil {
		return Preferences{}, err
	}
	return effective.preferences, nil
}

var knownReasons = []ReasonCode{ReasonNoCandidates, ReasonInsufficientCount, ReasonOutsideRadius, ReasonFoodMismatch, ReasonOverBudget, ReasonUnknownPrice, ReasonPreferencesConflict, ReasonUnknownEvidence}

// decisionShapeKept 在规范化前保留改前的形态要求：selected_ids/reason_codes 键必须存在，原因码须在枚举内，needs_choice 不得带选中项。
// ready 配空 selected_ids 有意放行，由 normalizeDecision 走服务端按距离回退。
func decisionShapeKept(decision RecommendationDecision) bool {
	known := func(reasons []ReasonCode) bool {
		return !slices.ContainsFunc(reasons, func(reason ReasonCode) bool { return !slices.Contains(knownReasons, reason) })
	}
	if decision.SelectedIDs == nil || decision.ReasonCodes == nil || !known(decision.ReasonCodes) ||
		(decision.Status == DecisionNeedsChoice && len(decision.SelectedIDs) != 0) {
		return false
	}
	return !slices.ContainsFunc(decision.Options, func(option RecommendationOption) bool { return !known(option.ReasonCodes) })
}

// nearestSelectable 按直线距离由近到远返回服务端可选候选，至多 target_count 个。
func (policy *RecommendationPolicy) nearestSelectable() []string {
	places := slices.Clone(policy.places)
	distance := func(place Place) float64 {
		meters, _ := candidateDistance(policy.origin, place.Location)
		return meters
	}
	slices.SortStableFunc(places, func(a, b Place) int { return cmp.Compare(distance(a), distance(b)) })
	selected := []string{}
	for _, place := range places {
		if len(selected) < policy.preferences.TargetCount && selectableCandidate(policy.assess(place, policy.preferences), policy.preferences) {
			selected = append(selected, place.CandidateKey)
		}
	}
	return selected
}

// validReasons 去重并只保留服务端可证实的原因码；allow 为额外放行条件。
func (policy *RecommendationPolicy) validReasons(reasons []ReasonCode, decision RecommendationDecision, clarify bool, allow func(ReasonCode) bool) []ReasonCode {
	kept := []ReasonCode{}
	for _, reason := range reasons {
		if !slices.Contains(kept, reason) && (allow(reason) || policy.validateReasons([]ReasonCode{reason}, decision, clarify) == nil) {
			kept = append(kept, reason)
		}
	}
	return kept
}

// normalizeDecision 以服务端为准规范化模型决策：三态整体替换为服务端（采纳品类后）评估；
// 选中项池外/重复/超量仍拒绝，服务端判为不可选的过滤掉，过滤后为空则回退为服务端按距离选择；原因码过滤为服务端可证实的值。
func (policy *RecommendationPolicy) normalizeDecision(decision RecommendationDecision) (RecommendationDecision, error) {
	effective, clarify, err := policy.interpret(decision)
	if err != nil {
		return RecommendationDecision{}, err
	}
	decision.Assessments = make([]CandidateAssessment, 0, len(policy.places))
	for _, place := range policy.places {
		decision.Assessments = append(decision.Assessments, effective.assess(place, effective.preferences))
	}
	if len(decision.SelectedIDs) > effective.preferences.TargetCount {
		return RecommendationDecision{}, ErrInvalidRecommendation
	}
	selected, seen := []string{}, make(map[string]bool, len(decision.SelectedIDs))
	for _, id := range decision.SelectedIDs {
		place, ok := policy.find(id)
		if !ok || seen[place.CandidateKey] {
			return RecommendationDecision{}, ErrInvalidRecommendation
		}
		seen[place.CandidateKey] = true
		if selectableCandidate(effective.assess(place, effective.preferences), effective.preferences) {
			selected = append(selected, place.CandidateKey)
		}
	}
	reasons := decision.ReasonCodes
	if decision.Status == DecisionReady && len(selected) == 0 {
		if selected = effective.nearestSelectable(); len(selected) == 0 {
			decision.Status, reasons = DecisionNeedsChoice, effective.Evaluate().ReasonCodes
		}
	}
	decision.SelectedIDs = selected
	decision.ReasonCodes = effective.validReasons(reasons, decision, clarify, func(ReasonCode) bool { return false })
	if decision.Status == DecisionReady && len(selected) < effective.preferences.TargetCount && !slices.Contains(decision.ReasonCodes, ReasonInsufficientCount) {
		decision.ReasonCodes = append(decision.ReasonCodes, ReasonInsufficientCount)
	}
	for index := range decision.Options {
		option := &decision.Options[index]
		for i, id := range option.ObservedIDs {
			if place, ok := policy.find(id); ok {
				option.ObservedIDs[i] = place.CandidateKey
			}
		}
		preferences, err := option.Changes.Apply(effective.preferences)
		widened := err == nil && preferences.RadiusMeters > effective.preferences.RadiusMeters
		option.ReasonCodes = effective.validReasons(option.ReasonCodes, decision, clarify, func(reason ReasonCode) bool { return widened && reason == ReasonOutsideRadius })
	}
	return decision, nil
}

func (policy *RecommendationPolicy) ValidateDecision(decision RecommendationDecision) error {
	if err := decision.Validate(); err != nil {
		return err
	}
	effective, clarify, err := policy.interpret(decision)
	if err != nil {
		return err
	}
	if len(decision.SelectedIDs) > policy.preferences.TargetCount {
		return ErrInvalidRecommendation
	}
	if decision.Status == DecisionNeedsChoice && !clarify && effective.Evaluate().Status == DecisionReady {
		return ErrInvalidRecommendation
	}
	assessed := make(map[string]bool, len(decision.Assessments))
	for _, assessment := range decision.Assessments {
		place, ok := policy.find(assessment.CandidateID)
		if !ok || assessed[place.CandidateKey] {
			return ErrInvalidRecommendation
		}
		assessed[place.CandidateKey] = true
	}
	seen := make(map[string]bool, len(decision.SelectedIDs))
	for _, id := range decision.SelectedIDs {
		place, ok := effective.find(id)
		if !ok || seen[place.CandidateKey] || !selectableCandidate(effective.assess(place, effective.preferences), effective.preferences) {
			return ErrInvalidRecommendation
		}
		seen[place.CandidateKey] = true
	}
	if err := effective.validateReasons(decision.ReasonCodes, decision, clarify); err != nil {
		return err
	}
	for _, option := range decision.Options {
		preferences, err := option.Changes.Apply(effective.preferences)
		if err != nil || reflect.DeepEqual(preferences, effective.preferences) {
			return ErrInvalidRecommendation
		}
		seen := make(map[string]bool, len(option.ObservedIDs))
		for _, id := range option.ObservedIDs {
			place, ok := effective.find(id)
			if !ok || seen[place.CandidateKey] || !selectableCandidate(effective.assess(place, preferences), preferences) {
				return ErrInvalidRecommendation
			}
			seen[place.CandidateKey] = true
		}
		for _, reason := range option.ReasonCodes {
			if reason == ReasonOutsideRadius && preferences.RadiusMeters > effective.preferences.RadiusMeters {
				continue
			}
			if err := effective.validateReasons([]ReasonCode{reason}, decision, clarify); err != nil {
				return err
			}
		}
	}
	return nil
}

func (policy *RecommendationPolicy) validateReasons(reasons []ReasonCode, decision RecommendationDecision, clarify bool) error {
	for _, reason := range reasons {
		valid := false
		switch reason {
		case ReasonNoCandidates:
			valid = len(policy.places) == 0
		case ReasonInsufficientCount:
			valid = len(decision.SelectedIDs) > 0 && len(decision.SelectedIDs) < policy.preferences.TargetCount
		case ReasonPreferencesConflict:
			valid = clarify || (policy.Evaluate().Status == DecisionNeedsChoice && len(policy.places) > 0)
		case ReasonOutsideRadius, ReasonFoodMismatch, ReasonOverBudget, ReasonUnknownPrice, ReasonUnknownEvidence:
			for _, place := range policy.places {
				assessment := policy.assess(place, policy.preferences)
				switch reason {
				case ReasonOutsideRadius:
					valid = valid || assessment.Range.State == MatchUnsatisfied
				case ReasonFoodMismatch:
					valid = valid || assessment.Food.State == MatchUnsatisfied
				case ReasonOverBudget:
					valid = valid || assessment.Price.State == MatchUnsatisfied
				case ReasonUnknownPrice:
					valid = valid || assessment.Price.State == MatchUnknown
				case ReasonUnknownEvidence:
					valid = valid || assessment.Range.State == MatchUnknown || assessment.Food.State == MatchUnknown || assessment.Price.State == MatchUnknown
				default:
					return ErrInvalidRecommendation
				}
			}
		default:
			return ErrInvalidRecommendation
		}
		if !valid {
			return ErrInvalidRecommendation
		}
	}
	return nil
}

func (policy *RecommendationPolicy) SelectedPlaces(decision RecommendationDecision) ([]Place, error) {
	if err := policy.ValidateDecision(decision); err != nil {
		return nil, err
	}
	effective, _, err := policy.interpret(decision)
	if err != nil {
		return nil, err
	}
	places := make([]Place, 0, len(decision.SelectedIDs))
	for _, id := range decision.SelectedIDs {
		place, found := policy.find(id)
		if !found {
			return nil, ErrInvalidRecommendation
		}
		assessment := effective.assess(place, effective.preferences)
		place.Evaluation = &assessment
		if assessment.Price.State == MatchUnknown {
			place.Cost, place.CostCurrency, place.CostBasis = "", "", ""
		}
		places = append(places, place)
	}
	return places, nil
}

func (policy *RecommendationPolicy) ParseDecision(data []byte) (RecommendationDecision, error) {
	if len(data) > MaxDecisionBytes {
		return RecommendationDecision{}, ErrInvalidRecommendation
	}
	if err := strictDecisionObject(data, reflect.TypeFor[RecommendationDecision]()); err != nil {
		return RecommendationDecision{}, err
	}
	// 模型提交的 assessments（可省略）只做严格解码，内容不被信任，随即整体替换为服务端评估。
	var decision RecommendationDecision
	if err := decodeRecommendationJSON(data, &decision); err != nil {
		return RecommendationDecision{}, err
	}
	if !decisionShapeKept(decision) {
		return RecommendationDecision{}, ErrInvalidRecommendation
	}
	decision, err := policy.normalizeDecision(decision)
	if err != nil {
		return RecommendationDecision{}, err
	}
	if err := policy.ValidateDecision(decision); err != nil {
		return RecommendationDecision{}, err
	}
	return decision, nil
}
