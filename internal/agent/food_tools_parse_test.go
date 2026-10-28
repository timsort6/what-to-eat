package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSearch_Parse_whenBusinessUsesNestedLegacyOrEmptyData(t *testing.T) {
	for _, fixture := range []struct{ name, data, cost, tag string }{
		{"嵌套", `"cost":"999","business":{"cost":"30","tag":"四川火锅","opentime_today":"10:00-22:00"}`, "30", "四川火锅"},
		{"旧版", `"cost":30,"tag":"火锅"`, "30", "火锅"},
		{"空对象", `"cost":"999","business":{}`, "", ""},
		{"null", `"cost":"999","business":null`, "", ""},
		{"空数组", `"cost":"999","business":[]`, "", ""},
		{"空字符串", `"cost":"999","business":""`, "", ""},
		{"非法嵌套", `"cost":"999","business":{"cost":{},"tag":true}`, "", ""},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			input := `{"status":"1","pois":[{"id":"P1","name":"餐厅","location":"0,0","distance":"9999","secret":"sensitive","tel":"private",` + fixture.data + `}]}`
			result, err := parseSearchResult(input, 10, RequestScope{Location: "0,0"})
			require.NoError(t, err)
			require.Len(t, result.Places, 1)
			place := result.Places[0]
			require.Equal(t, fixture.cost, place.Cost)
			require.Equal(t, fixture.tag, place.Tag)
			require.Equal(t, "0.000", place.Distance)
			require.Empty(t, place.CostCurrency)
			require.Empty(t, place.CostBasis)
			data, err := json.Marshal(result)
			require.NoError(t, err)
			require.NotContains(t, string(data), "sensitive")
			require.NotContains(t, string(data), "private")
			require.NotContains(t, string(data), `"raw"`)
		})
	}
}

func TestSearch_Deduplicates_whenRealIDsAndNormalizedSyntheticKeysRepeat(t *testing.T) {
	input := `{"status":"1","pois":[{"id":"P1","name":"餐厅","location":"0,0"},{"id":"P1","name":"改名"},{"name":" A  B ","location":"0.0, 0.00"},{"name":"a b","location":"0,0"},{"id":"synthetic:fake","name":"另一个","location":"0,0"}]}`
	result, err := parseSearchResult(input, 10, RequestScope{Location: "0,0"})
	require.NoError(t, err)
	require.Len(t, result.Places, 3)
	require.Equal(t, "poi:P1", result.Places[0].CandidateKey)
	require.True(t, strings.HasPrefix(result.Places[1].CandidateKey, "synthetic:"))
	require.Empty(t, result.Places[1].ID)
	require.Empty(t, result.Places[2].ID)
	require.NotEqual(t, "synthetic:fake", result.Places[2].CandidateKey)
}

func TestSearch_DoesNotTrustDistance_whenCoordinatesAreMissingOrMalformed(t *testing.T) {
	result, err := parseSearchResult(`{"status":"1","pois":[{"name":"缺坐标","distance":"1"},{"name":"非法坐标","location":"NaN,0","distance":"1"},null,[],{"name":"有效","location":"0,0"}]}`, 2, RequestScope{Location: "0,0"})
	require.NoError(t, err)
	require.Len(t, result.Places, 2)
	for _, place := range result.Places {
		require.Empty(t, place.Distance)
		require.Empty(t, place.Location)
	}
}
