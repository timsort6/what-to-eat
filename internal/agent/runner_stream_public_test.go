package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func publicPolicy(t *testing.T, places []Place) (*RecommendationPolicy, func(string) string) {
	t.Helper()
	policy, err := NewRecommendationPolicy("0,0", DefaultPreferences())
	require.NoError(t, err)
	require.NoError(t, policy.AddCandidates(places))
	return policy, func(id string) string {
		place, ok := policy.find(id)
		require.True(t, ok, id)
		return place.CandidateKey
	}
}

func TestPublicDecision_NeedsChoiceProjectsOnlyReferencedCandidatesWithWhitelistedFields(t *testing.T) {
	policy, key := publicPolicy(t, []Place{
		{ID: "P1", Name: "近处冒菜", Address: "一号", Location: "0,0.001", Tel: "010-SECRET", Tag: "冒菜", Raw: json.RawMessage(`{"secret":"RAW-SECRET"}`)},
		{ID: "P2", Name: "远处川锅", Location: "0,0.036", Tel: "010-SECRET", Tag: "四川火锅", OpentimeToday: "OPEN-SECRET"},
		{ID: "P3", Name: "无坐标店", Address: "三号"},
	})
	evaluated := policy.Evaluate()
	decision := RecommendationDecision{Status: DecisionNeedsChoice, SelectedIDs: []string{}, Assessments: evaluated.Assessments,
		ReasonCodes: []ReasonCode{ReasonOutsideRadius}, Options: []RecommendationOption{
			// 模型可用 POI ID 引用；公开投影统一为候选键。
			{OptionID: "wider", Label: "扩大到4公里", ObservedIDs: []string{"P2", key("P3")}, ReasonCodes: []ReasonCode{ReasonOutsideRadius}, Changes: PreferenceChanges{RadiusMeters: new(int)}},
			{OptionID: "again", Label: "再查一次", ObservedIDs: []string{key("P2")}, ReasonCodes: []ReasonCode{}, Changes: PreferenceChanges{RadiusMeters: new(int)}},
		}}
	require.Len(t, decision.Assessments, 3)

	public, err := publicDecision(policy, decision, nil)
	require.NoError(t, err)
	require.Equal(t, []string{key("P2"), key("P3")}, public.Options[0].ObservedIDs)
	ids := make([]string, 0, len(public.Assessments))
	for _, assessment := range public.Assessments {
		ids = append(ids, assessment.CandidateID)
	}
	require.Equal(t, []string{key("P2"), key("P3")}, ids, "needs_choice 只保留方案引用的候选评估")

	distance, ok := candidateDistance("0,0", "0,0.036")
	require.True(t, ok)
	meters := int(math.Round(distance))
	require.Equal(t, []ObservedCandidate{
		{CandidateID: key("P2"), Name: "远处川锅", Address: "", DistanceMeters: &meters},
		{CandidateID: key("P3"), Name: "无坐标店", Address: "三号", DistanceMeters: nil},
	}, public.Candidates)

	raw, err := json.Marshal(public)
	require.NoError(t, err)
	for _, leaked := range []string{"010-SECRET", "RAW-SECRET", "OPEN-SECRET", "近处冒菜", `"tel"`, `"raw"`, `"tag"`} {
		require.NotContains(t, string(raw), leaked)
	}
	var wire struct {
		Candidates []map[string]json.RawMessage `json:"candidates"`
	}
	require.NoError(t, json.Unmarshal(raw, &wire))
	for _, candidate := range wire.Candidates {
		keys := make([]string, 0, len(candidate))
		for name := range candidate {
			keys = append(keys, name)
		}
		require.ElementsMatch(t, []string{"candidate_id", "name", "address", "distance_meters"}, keys)
	}
	require.Equal(t, "null", string(wire.Candidates[1]["distance_meters"]))
}

func TestPublicDecision_ReadyKeepsSelectedAssessmentsAndEmptyCandidates(t *testing.T) {
	policy, key := publicPolicy(t, []Place{{ID: "P1", Name: "近处", Location: "0,0.001"}, {ID: "P2", Name: "范围外", Location: "0,0.05"}})
	decision := policy.Evaluate()
	require.Equal(t, DecisionReady, decision.Status)
	require.Len(t, decision.Assessments, 2)
	selected, err := policy.SelectedPlaces(decision)
	require.NoError(t, err)
	public, err := publicDecision(policy, decision, selected)
	require.NoError(t, err)
	require.Equal(t, []string{key("P1")}, public.SelectedIDs)
	require.Len(t, public.Assessments, 1)
	require.Equal(t, key("P1"), public.Assessments[0].CandidateID)
	require.NotNil(t, public.Candidates)
	require.Empty(t, public.Candidates)
	raw, err := json.Marshal(public)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"candidates":[]`)
}

func TestPublicDecision_RejectsProjectionOverFrontendLimit(t *testing.T) {
	places := make([]Place, 0, 10)
	ids := make([]string, 0, 10)
	for index := range 10 {
		// '<' 经 JSON 转义为 \u003c，长名称/地址使 30 个引用候选的公开投影超过 64KiB。
		places = append(places, Place{ID: fmt.Sprintf("P%d", index), Name: strings.Repeat("<", 1000), Address: strings.Repeat("<", 1000), Location: fmt.Sprintf("0,0.0%d", 30+index)})
		ids = append(ids, fmt.Sprintf("P%d", index))
	}
	policy, _ := publicPolicy(t, places)
	evaluated := policy.Evaluate()
	decision := RecommendationDecision{Status: DecisionNeedsChoice, SelectedIDs: []string{}, Assessments: evaluated.Assessments, ReasonCodes: []ReasonCode{},
		Options: []RecommendationOption{{OptionID: "wider", Label: "扩大", ObservedIDs: ids, ReasonCodes: []ReasonCode{}, Changes: PreferenceChanges{}}}}
	_, err := publicDecision(policy, decision, nil)
	require.True(t, errors.Is(err, ErrInvalidRecommendation), err)

	decision.Options[0].ObservedIDs = ids[:1]
	public, err := publicDecision(policy, decision, nil)
	require.NoError(t, err, "单个长名称候选仍在上限内")
	raw, err := json.Marshal(public)
	require.NoError(t, err)
	require.LessOrEqual(t, len(raw), MaxDecisionBytes)
}
