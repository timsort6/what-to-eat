package agent

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPolicy_UsesStructuredCuisine_whenNameIsAmbiguousOrSourcesConflict(t *testing.T) {
	for _, fixture := range []struct {
		name, category, tag string
		state               MatchState
	}{
		{"四川火锅（贵州路店）", "", "四川火锅", MatchSatisfied},
		{"四川火锅（贵阳路店）", "餐饮服务;四川火锅", "", MatchSatisfied},
		{"冒菜烤肉混合店", "", "四川火锅", MatchSatisfied},
		{"四川火锅（贵州路店）", "", "", MatchUnknown},
		{"冒菜烤肉混合店", "火锅", "", MatchUnknown},
		{"冒菜", "", "", MatchUnknown},
		{"贵州酸汤锅", "", "", MatchUnknown},
		{"餐厅", "冒菜", "四川火锅", MatchUnknown},
		{"餐厅", "四川火锅", "贵州酸汤锅", MatchUnknown},
		{"四川火锅门店", "餐饮服务;冒菜", "", MatchUnsatisfied},
		{"餐厅", "", "贵州酸汤锅", MatchUnsatisfied},
	} {
		t.Run(fixture.name+fixture.category+fixture.tag, func(t *testing.T) {
			requirement := "四川火锅"
			preferences := DefaultPreferences()
			preferences.FoodRequirement = &requirement
			place := policyPlace("P1", 100)
			place.Name, place.Type, place.Tag = fixture.name, fixture.category, fixture.tag

			decision, err := EvaluateCandidates("0,0", preferences, []Place{place})

			require.NoError(t, err)
			require.Equal(t, fixture.state, decision.Assessments[0].Food.State)
			require.NotContains(t, decision.Assessments[0].Food.Fields, EvidenceField("name"))
			require.Equal(t, fixture.state != MatchUnsatisfied, decision.Status == DecisionReady)
		})
	}
}

func TestPolicy_MapsCanonicalPriceEvidenceToActualSource_whenLegacyOrNestedPricesAreParsed(t *testing.T) {
	for _, fixture := range []struct {
		name, fields string
		sources      map[string]string
	}{
		{"旧形状", `"cost":"30","cost_currency":"CNY","cost_basis":"per_person"`, map[string]string{"business.cost": "cost", "business.currency": "cost_currency", "business.cost_basis": "cost_basis"}},
		{"大小写扩展不冒充来源", `"Business":null,"cost":"30","cost_currency":"CNY","cost_basis":"per_person"`, map[string]string{"business.cost": "cost", "business.currency": "cost_currency", "business.cost_basis": "cost_basis"}},
		{"嵌套形状", `"cost":"999","business":{"cost":"30","currency":"CNY","cost_basis":"per_person"}`, map[string]string{"business.cost": "business.cost", "business.currency": "business.currency", "business.cost_basis": "business.cost_basis"}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			result, err := parseSearchResult(`{"status":"1","pois":[{"id":"P1","name":"餐厅","location":"0,0",`+fixture.fields+`}]}`, 10, RequestScope{Location: "0,0"})
			require.NoError(t, err)
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			var projection struct {
				Places []struct {
					Sources map[string]string `json:"price_sources"`
				} `json:"places"`
			}
			require.NoError(t, json.Unmarshal(encoded, &projection))
			require.Equal(t, fixture.sources, projection.Places[0].Sources)
			var candidates SearchResult
			require.NoError(t, json.Unmarshal(encoded, &candidates))
			price := 50
			preferences := DefaultPreferences()
			preferences.MaxPriceCNY = &price
			policy, err := NewRecommendationPolicy("0,0", preferences)
			require.NoError(t, err)
			require.NoError(t, policy.AddCandidates(candidates.Places))

			decision := policy.Evaluate()

			require.Equal(t, MatchSatisfied, decision.Assessments[0].Price.State)
			for _, field := range decision.Assessments[0].Price.Fields {
				require.Contains(t, fixture.sources, string(field))
			}
			require.NoError(t, policy.ValidateDecision(decision))
			decisionJSON, err := json.Marshal(decision)
			require.NoError(t, err)
			_, err = policy.ParseDecision(decisionJSON)
			require.NoError(t, err)
		})
	}
}

func TestPolicy_PreservesUnknownPrice_whenSourceMappingIsInvalid(t *testing.T) {
	place := policyPlace("P1", 100)
	place.Cost, place.CostCurrency, place.CostBasis = "30", "CNY", "per_person"
	place.PriceSources = PriceSources{Cost: "raw.secret", Currency: "cost_currency", CostBasis: "cost_basis"}

	decision, err := EvaluateCandidates("0,0", DefaultPreferences(), []Place{place})

	require.NoError(t, err)
	require.Equal(t, MatchUnknown, decision.Assessments[0].Price.State)
	require.Empty(t, decision.Assessments[0].Price.Fields)
}
