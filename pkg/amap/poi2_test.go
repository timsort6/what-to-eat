package amap

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPOI2Business_Parse_whenNestedOrLegacyFieldsExist(t *testing.T) {
	for _, fixture := range []struct{ name, data, cost, tag, today, week string }{
		{"嵌套优先", `"cost":"999","tag":"旧值","business":{"cost":"30","tag":"川菜","opentime_today":"10:00-22:00","opentime_week":"周一至周日"}`, "30", "川菜", "10:00-22:00", "周一至周日"},
		{"旧版", `"cost":30,"tag":"火锅","opentime_today":"11:00-21:00","opentime_week":"每天"`, "30", "火锅", "11:00-21:00", "每天"},
		{"嵌套空对象", `"cost":"999","tag":"旧值","business":{}`, "", "", "", ""},
		{"嵌套null", `"cost":"999","tag":"旧值","business":null`, "", "", "", ""},
		{"嵌套空数组", `"cost":"999","business":[]`, "", "", "", ""},
		{"嵌套空字符串", `"cost":"999","business":""`, "", "", "", ""},
		{"嵌套非法字段", `"cost":"999","tag":"旧值","business":{"cost":{"amount":30},"tag":["火锅"],"opentime_today":true,"opentime_week":null}`, "", "", "", ""},
		{"嵌套字段空值", `"cost":"999","business":{"cost":null,"tag":[],"opentime_today":"","opentime_week":[]}`, "", "", "", ""},
		{"旧版空字段", `"cost":null,"tag":[],"opentime_today":"","opentime_week":[]`, "", "", "", ""},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			var poi POI2POI
			err := json.Unmarshal([]byte(`{"id":"P1",`+fixture.data+`}`), &poi)
			require.NoError(t, err)
			require.Equal(t, []string{fixture.cost, fixture.tag, fixture.today, fixture.week}, []string{poi.Cost, poi.Tag, poi.OpentimeToday, poi.OpentimeWeek})
			require.Empty(t, poi.CostCurrency)
			require.Empty(t, poi.CostBasis)
			require.NotContains(t, poi.Extra, "business")
		})
	}
}
