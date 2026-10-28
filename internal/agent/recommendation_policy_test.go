package agent

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func policyPlace(id string, meters float64) Place {
	return Place{ID: id, Name: "餐厅", Location: "0," + strconv.FormatFloat(meters/6371008.8*180/math.Pi, 'f', 15, 64)}
}

func TestPolicy_Evaluate_whenDistanceIsBoundaryOrUnknown(t *testing.T) {
	for _, fixture := range []struct {
		name     string
		meters   float64
		location string
		state    MatchState
	}{
		{"范围内", 1999.9, "", MatchSatisfied}, {"精确边界", 2000, "", MatchSatisfied},
		{"边界外", 2000.1, "", MatchUnsatisfied}, {"缺坐标", 0, "missing", MatchUnknown},
		{"非法坐标", 0, "NaN,0", MatchUnknown}, {"坐标越界", 0, "181,0", MatchUnknown},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			preferences := DefaultPreferences()
			preferences.RadiusMeters = 2000
			place := policyPlace("P1", fixture.meters)
			place.Distance = "1"
			if fixture.location != "" {
				place.Location = fixture.location
			}
			decision, err := EvaluateCandidates("0,0", preferences, []Place{place})
			require.NoError(t, err)
			require.Equal(t, fixture.state, decision.Assessments[0].Range.State)
			require.Equal(t, fixture.state == MatchSatisfied, decision.Status == DecisionReady)
		})
	}
}

func TestPolicy_RequiresChoice_whenNearbyFoodConflictsAndMatchIsFourKilometers(t *testing.T) {
	requirement := "四川火锅"
	preferences := DefaultPreferences()
	preferences.RadiusMeters = 2000
	preferences.FoodRequirement = &requirement
	maocai, guizhou, sichuan := policyPlace("P1", 1000), policyPlace("P2", 1500), policyPlace("P3", 4000)
	maocai.Name, guizhou.Name, sichuan.Tag = "冒菜", "贵州酸汤锅", requirement
	maocai.Type, guizhou.Tag = "冒菜", "贵州酸汤锅"
	decision, err := EvaluateCandidates("0,0", preferences, []Place{maocai, guizhou, sichuan})
	require.NoError(t, err)
	require.Equal(t, DecisionNeedsChoice, decision.Status)
	require.Empty(t, decision.SelectedIDs)
	require.Equal(t, MatchUnsatisfied, decision.Assessments[0].Food.State)
	require.Equal(t, MatchUnsatisfied, decision.Assessments[1].Food.State)
	require.Equal(t, MatchSatisfied, decision.Assessments[2].Food.State)
	require.Equal(t, MatchUnsatisfied, decision.Assessments[2].Range.State)
}

func TestPolicy_PreservesUnknownCuisine_whenOnlyNameOrGenericHotpotMatches(t *testing.T) {
	for _, fixture := range []struct {
		name, category string
		state          MatchState
	}{
		{"四川火锅直营店", "餐饮服务;火锅店", MatchUnknown}, {"火锅", "火锅", MatchUnknown},
		{"餐厅", "餐饮服务;四川火锅", MatchSatisfied}, {"餐厅", "", MatchUnknown},
	} {
		t.Run(fixture.name+fixture.category, func(t *testing.T) {
			requirement := "四川火锅"
			preferences := DefaultPreferences()
			preferences.FoodRequirement = &requirement
			place := policyPlace("P1", 100)
			place.Name, place.Type = fixture.name, fixture.category
			decision, err := EvaluateCandidates("0,0", preferences, []Place{place})
			require.NoError(t, err)
			require.Equal(t, fixture.state, decision.Assessments[0].Food.State)
			require.Equal(t, DecisionReady, decision.Status)
			require.Equal(t, []ReasonCode{ReasonInsufficientCount}, decision.ReasonCodes)
		})
	}
}

func TestPolicy_RequiresChoice_whenExplicitCuisineCategoryConflicts(t *testing.T) {
	requirement := "寿司"
	preferences := DefaultPreferences()
	preferences.FoodRequirement = &requirement
	place := policyPlace("P1", 100)
	place.Type = "烤肉"
	decision, err := EvaluateCandidates("0,0", preferences, []Place{place})
	require.NoError(t, err)
	require.Equal(t, MatchUnsatisfied, decision.Assessments[0].Food.State)
	require.Equal(t, DecisionNeedsChoice, decision.Status)
}

func TestPolicy_EvaluatesPrice_whenEvidenceIsStructuredAndExplicit(t *testing.T) {
	for _, fixture := range []struct {
		name, cost, currency, basis string
		state                       MatchState
	}{
		// 缺币种或口径按高德参考人均（人民币）比较。
		{"缺失", "", "", "", MatchUnknown}, {"缺币种按参考人均", "30", "", "per_person", MatchSatisfied},
		{"缺口径按参考人均", "30", "CNY", "", MatchSatisfied}, {"缺口径参考价超限", "51", "", "", MatchUnsatisfied}, {"美元", "30", "USD", "per_person", MatchUnknown},
		{"明确非人均口径", "30", "CNY", "total", MatchUnknown},
		{"负数", "-30", "CNY", "per_person", MatchUnknown}, {"区间", "30-100", "CNY", "per_person", MatchUnknown},
		{"估计", "约30元", "CNY", "per_person", MatchUnknown}, {"NaN", "NaN", "CNY", "per_person", MatchUnknown},
		{"无限", "Inf", "CNY", "per_person", MatchUnknown}, {"零", "0", "CNY", "per_person", MatchUnknown},
		{"边界", "50", "CNY", "per_person", MatchSatisfied}, {"超价", "50.01", "CNY", "per_person", MatchUnsatisfied},
		{"已知高价", "1000001", "CNY", "per_person", MatchUnsatisfied},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			price := 50
			preferences := DefaultPreferences()
			preferences.MaxPriceCNY = &price
			place := policyPlace("P1", 100)
			place.Cost, place.CostCurrency, place.CostBasis = fixture.cost, fixture.currency, fixture.basis
			decision, err := EvaluateCandidates("0,0", preferences, []Place{place})
			require.NoError(t, err)
			require.Equal(t, fixture.state, decision.Assessments[0].Price.State)
			require.Equal(t, fixture.state != MatchUnsatisfied, decision.Status == DecisionReady)
		})
	}
}

func TestPolicy_BlocksUnknownPrice_whenUserExplicitlyDisallowsIt(t *testing.T) {
	for _, allow := range []bool{true, false} {
		t.Run(strconv.FormatBool(allow), func(t *testing.T) {
			preferences := DefaultPreferences()
			preferences.AllowUnknownPrice = allow
			decision, err := EvaluateCandidates("0,0", preferences, []Place{policyPlace("P1", 100)})
			require.NoError(t, err)
			require.Equal(t, allow, decision.Status == DecisionReady)
			require.Equal(t, MatchUnknown, decision.Assessments[0].Price.State)
		})
	}
}

func TestPolicy_ReadyWithShortfall_whenOnlyOneVerifiedPriceRemains(t *testing.T) {
	preferences := DefaultPreferences()
	preferences.AllowUnknownPrice = false
	verified := policyPlace("P1", 100)
	verified.Cost, verified.CostCurrency, verified.CostBasis = "30", "CNY", "per_person"
	decision, err := EvaluateCandidates("0,0", preferences, []Place{verified, policyPlace("P2", 200)})
	require.NoError(t, err)
	require.Equal(t, DecisionReady, decision.Status)
	require.Equal(t, []string{"poi:P1"}, decision.SelectedIDs)
	require.Equal(t, []ReasonCode{ReasonInsufficientCount}, decision.ReasonCodes)
}

func TestPolicy_BoundsAndDeduplicates_whenCandidatesRepeatAcrossCalls(t *testing.T) {
	policy, err := NewRecommendationPolicy("0,0", DefaultPreferences())
	require.NoError(t, err)
	places := make([]Place, MaxCandidates)
	for index := range places {
		places[index] = policyPlace(fmt.Sprintf("P%d", index), 100)
	}
	require.NoError(t, policy.AddCandidates(places))
	require.NoError(t, policy.AddCandidates(places))
	require.ErrorIs(t, policy.AddCandidates([]Place{policyPlace("extra", 100)}), ErrInvalidRecommendation)
	require.Len(t, policy.Evaluate().Assessments, MaxCandidates)
	require.ErrorIs(t, policy.AddCandidates(append(places, places[0])), ErrInvalidRecommendation)
}

func TestPolicy_SelectedProjection_whenUnknownPriceAndRawArePresent(t *testing.T) {
	policy, err := NewRecommendationPolicy("0,0", DefaultPreferences())
	require.NoError(t, err)
	place := policyPlace("P1", 100)
	place.Cost, place.Tel, place.Raw = "约999", "private", json.RawMessage(`{"secret":true}`)
	require.NoError(t, policy.AddCandidates([]Place{place}))
	places, err := policy.SelectedPlaces(policy.Evaluate())
	require.NoError(t, err)
	require.Len(t, places, 1)
	require.Empty(t, places[0].Cost)
	require.Empty(t, places[0].Raw)
	require.Empty(t, places[0].Tel)
	encoded, err := json.Marshal(places)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "secret")
	require.NotContains(t, string(encoded), "private")
}
