package agent

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPolicy_RejectDecision_whenCurrentCandidateReferencesOrFactsAreInvalid(t *testing.T) {
	for name, mutate := range map[string]func(*RecommendationDecision){
		"伪造编号": func(decision *RecommendationDecision) {
			decision.SelectedIDs = []string{"missing"}
			decision.Assessments[0].CandidateID = "missing"
		},
		"重复选择": func(decision *RecommendationDecision) { decision.SelectedIDs = []string{"poi:P1", "poi:P1"} },
		"编号别名重复": func(decision *RecommendationDecision) {
			decision.SelectedIDs = []string{"P1", "poi:P1"}
			assessment := decision.Assessments[0]
			assessment.CandidateID = "P1"
			decision.Assessments = append(decision.Assessments, assessment)
		},
		"超出目标数量": func(decision *RecommendationDecision) { decision.SelectedIDs = []string{"poi:P1", "poi:P2"} },
		"超出绝对数量": func(decision *RecommendationDecision) { decision.SelectedIDs = make([]string, 11) },
		"越界选择":   func(decision *RecommendationDecision) { decision.SelectedIDs = []string{"poi:P3"} },
		// 三态证据不再与模型比对（ParseDecision 整体替换为服务端评估，见 TestRealFix_*）；仅保留结构非法的证据。
		"Raw引用": func(decision *RecommendationDecision) {
			decision.Assessments[0].Range.Fields = []EvidenceField{"raw.location"}
		},
		"虚假原因": func(decision *RecommendationDecision) { decision.ReasonCodes = []ReasonCode{ReasonOverBudget} },
	} {
		t.Run(name, func(t *testing.T) {
			preferences := DefaultPreferences()
			preferences.TargetCount = 1
			policy, err := NewRecommendationPolicy("0,0", preferences)
			require.NoError(t, err)
			require.NoError(t, policy.AddCandidates([]Place{policyPlace("P1", 100), policyPlace("P2", 200), policyPlace("P3", 4000)}))
			decision := policy.Evaluate()
			mutate(&decision)
			require.ErrorIs(t, policy.ValidateDecision(decision), ErrInvalidRecommendation)
		})
	}
}

func TestPolicy_AcceptsRealID_whenItRefersToOnlyCurrentCandidate(t *testing.T) {
	policy, err := NewRecommendationPolicy("0,0", DefaultPreferences())
	require.NoError(t, err)
	require.NoError(t, policy.AddCandidates([]Place{policyPlace("P1", 100)}))
	decision := policy.Evaluate()
	decision.SelectedIDs = []string{"P1"}
	decision.Assessments[0].CandidateID = "P1"
	places, err := policy.SelectedPlaces(decision)
	require.NoError(t, err)
	require.Len(t, places, 1)
	require.Equal(t, "P1", places[0].ID)
}

func TestPolicy_RejectsFabricatedSyntheticID_whenNameAndCoordinatesLackPOIID(t *testing.T) {
	policy, err := NewRecommendationPolicy("0,0", DefaultPreferences())
	require.NoError(t, err)
	place := policyPlace("", 100)
	require.NoError(t, policy.AddCandidates([]Place{place}))
	decision := policy.Evaluate()
	require.Contains(t, decision.SelectedIDs[0], "synthetic:")
	places, err := policy.SelectedPlaces(decision)
	require.NoError(t, err)
	require.Empty(t, places[0].ID)
	decision.SelectedIDs = []string{"synthetic:invented"}
	decision.Assessments[0].CandidateID = "synthetic:invented"
	require.ErrorIs(t, policy.ValidateDecision(decision), ErrInvalidRecommendation)
}

func TestPolicy_ValidatesChoice_whenObservedCandidateRequiresExplicitRadiusChange(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		t.Run(map[bool]string{false: "四公里明确放宽", true: "伪造观察"}[invalid], func(t *testing.T) {
			preferences := DefaultPreferences()
			preferences.RadiusMeters = 2000
			policy, err := NewRecommendationPolicy("0,0", preferences)
			require.NoError(t, err)
			require.NoError(t, policy.AddCandidates([]Place{policyPlace("P1", 4000)}))
			decision := policy.Evaluate()
			radius := 4000
			decision.Options = []RecommendationOption{{OptionID: "expand", Label: "扩大到四公里", ObservedIDs: []string{"poi:P1"}, ReasonCodes: []ReasonCode{ReasonOutsideRadius}, Changes: PreferenceChanges{RadiusMeters: &radius}}}
			if invalid {
				decision.Options[0].ObservedIDs = []string{"missing"}
				decision.Assessments[0].CandidateID = "missing"
			}
			err = policy.ValidateDecision(decision)
			if invalid {
				require.ErrorIs(t, err, ErrInvalidRecommendation)
			} else {
				require.NoError(t, err)
				places, err := policy.SelectedPlaces(decision)
				require.NoError(t, err)
				require.Empty(t, places)
			}
		})
	}
}

func TestPolicy_RejectsDecisionJSON_whenFieldsAreUnknownOrDuplicated(t *testing.T) {
	policy, err := NewRecommendationPolicy("0,0", DefaultPreferences())
	require.NoError(t, err)
	require.NoError(t, policy.AddCandidates([]Place{policyPlace("P1", 100)}))
	data, err := json.Marshal(policy.Evaluate())
	require.NoError(t, err)
	_, err = policy.ParseDecision(data)
	require.NoError(t, err)
	for _, suffix := range []string{`,"raw":{}}`, `,"status":"ready"}`, `,"Status":"ready"}`} {
		_, err := policy.ParseDecision(append(append([]byte{}, data[:len(data)-1]...), suffix...))
		require.ErrorIs(t, err, ErrInvalidRecommendation)
	}
	data = []byte(`{"status":"ready","selected_ids":["poi:P1"],"reason_codes":[],"options":[],"assessments":[{"candidate_id":"poi:P1","range":{"state":"satisfied","state":"satisfied","fields":["location"]},"food":{"state":"unknown","fields":[]},"price":{"state":"unknown","fields":[]}}]}`)
	_, err = policy.ParseDecision(data)
	require.ErrorIs(t, err, ErrInvalidRecommendation)
}

func TestPolicy_BlocksModelSelection_whenUnknownPriceIsExplicitlyDisallowed(t *testing.T) {
	preferences := DefaultPreferences()
	preferences.AllowUnknownPrice = false
	policy, err := NewRecommendationPolicy("0,0", preferences)
	require.NoError(t, err)
	require.NoError(t, policy.AddCandidates([]Place{policyPlace("P1", 100)}))
	decision := policy.Evaluate()
	decision.Status, decision.SelectedIDs, decision.ReasonCodes = DecisionReady, []string{"poi:P1"}, []ReasonCode{}
	require.ErrorIs(t, policy.ValidateDecision(decision), ErrInvalidRecommendation)
}

func TestPolicy_RejectsForcedChoice_whenOnlyUnknownCuisineAndPriceRemain(t *testing.T) {
	policy, err := NewRecommendationPolicy("0,0", DefaultPreferences())
	require.NoError(t, err)
	require.NoError(t, policy.AddCandidates([]Place{policyPlace("P1", 100)}))
	decision := policy.Evaluate()
	decision.Status, decision.SelectedIDs, decision.ReasonCodes = DecisionNeedsChoice, []string{}, []ReasonCode{ReasonUnknownPrice}
	require.ErrorIs(t, policy.ValidateDecision(decision), ErrInvalidRecommendation)
}

func TestPolicy_RejectsUnchangedOption_whenDeltaDoesNotChangeCurrentPreferences(t *testing.T) {
	preferences := DefaultPreferences()
	preferences.RadiusMeters = 2000
	policy, err := NewRecommendationPolicy("0,0", preferences)
	require.NoError(t, err)
	require.NoError(t, policy.AddCandidates([]Place{policyPlace("P1", 4000)}))
	decision := policy.Evaluate()
	radius := 2000
	decision.Options = []RecommendationOption{{OptionID: "same", Label: "保持半径", ObservedIDs: []string{}, ReasonCodes: []ReasonCode{ReasonOutsideRadius}, Changes: PreferenceChanges{RadiusMeters: &radius}}}
	require.ErrorIs(t, policy.ValidateDecision(decision), ErrInvalidRecommendation)
}
