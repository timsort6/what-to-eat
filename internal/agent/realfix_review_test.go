package agent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// 复核期望表（由复核探针 TestProbe_Food 并入）：真实高德分类形状 × 品类。notUnsatisfied 表示 unknown 或 satisfied 均可。
func TestProbe_Food(t *testing.T) {
	const notUnsatisfied MatchState = "!unsatisfied"
	const hotpot, sichuan, beijing, japanese = "餐饮服务;中餐厅;火锅店", "餐饮服务;中餐厅;四川菜(川菜)", "餐饮服务;中餐厅;北京菜", "餐饮服务;外国餐厅;日本料理"
	pool := realfixPool()
	matouhui, tonglu := pool[4], pool[1]
	for _, fixture := range []struct {
		requirement, kind, tag string
		want                   MatchState
	}{
		{"四川火锅", sichuan, "冒菜", MatchUnsatisfied},
		{"四川火锅", hotpot, "贵州火锅", MatchUnsatisfied},
		{"四川火锅", hotpot, "贵州酸汤锅", MatchUnsatisfied},
		{"四川火锅", hotpot, "贵州酸汤火锅", MatchUnsatisfied},
		{"四川火锅", hotpot, "重庆火锅,烤肉", MatchSatisfied}, // 川渝同组（原为 unsatisfied）
		{"四川火锅", hotpot, "烤肉,毛肚", MatchUnknown},
		{"四川火锅", hotpot, "川味火锅", MatchSatisfied},
		{"四川火锅", hotpot, "四川火锅", MatchSatisfied},
		{"四川火锅", hotpot, "", MatchUnknown},
		{"四川火锅", hotpot, "毛肚", MatchUnknown},
		{"四川火锅", hotpot, "牛肉,单人餐", MatchUnknown},
		{"四川火锅", "餐饮服务;中餐厅;川味火锅", "", MatchSatisfied},
		{"重庆火锅", hotpot, "渝味老火锅", MatchSatisfied},
		{"火锅", hotpot, "", MatchSatisfied},
		{"　火 锅 ", hotpot, "", MatchSatisfied},
		{"火锅", hotpot, "串串", notUnsatisfied},
		{"火锅", "餐饮服务;快餐厅;呷哺呷哺", "", MatchUnknown},
		{"火锅", sichuan, "", notUnsatisfied},
		{"串串", hotpot, "串串", MatchSatisfied},
		{"串串", hotpot, "牛肉,单人餐", notUnsatisfied},
		{"寿司", japanese, "", notUnsatisfied},
		{"寿司", "烤肉", "", MatchUnsatisfied},
		{"披萨", "餐饮服务;外国餐厅;意式菜品餐厅", "", notUnsatisfied},
		{"pizza", "餐饮服务;外国餐厅;意式菜品餐厅", "", notUnsatisfied},
		{"披萨", "餐饮服务;外国餐厅;意式", "", notUnsatisfied},
		{"酸菜鱼", sichuan, "", notUnsatisfied},
		{"麻辣烫", sichuan, "", notUnsatisfied},
		{"烤鸭", beijing, "", notUnsatisfied},
		{"奶茶", "餐饮服务;冷饮店;冷饮店", "", notUnsatisfied},
		{"牛排", "餐饮服务;外国餐厅;西餐厅(综合风味)", "", notUnsatisfied},
		{"汉堡", "餐饮服务;外国餐厅;美式风味", "", notUnsatisfied},
		{"川菜", sichuan, "", MatchSatisfied},
		{"四川火锅", matouhui.Type, matouhui.Tag, MatchSatisfied}, // tag“重庆火锅”，川渝同组（原为 unsatisfied）
		{"火锅", matouhui.Type, matouhui.Tag, MatchSatisfied},
		{"四川火锅", tonglu.Type, tonglu.Tag, MatchSatisfied},
	} {
		requirement := fixture.requirement
		got := assessFood(&requirement, Place{Type: fixture.kind, Tag: fixture.tag})
		if fixture.want == notUnsatisfied {
			require.NotEqual(t, MatchUnsatisfied, got.State, "%s | %s | %s", fixture.requirement, fixture.kind, fixture.tag)
		} else {
			require.Equal(t, fixture.want, got.State, "%s | %s | %s", fixture.requirement, fixture.kind, fixture.tag)
		}
		require.Equal(t, got.State == MatchUnknown, len(got.Fields) == 0)
		require.NotContains(t, got.Fields, EvidenceField("name"))
	}
}

// N1：形态检查与改前一致（缺 selected_ids/reason_codes 键、未知原因码、needs_choice 带选中项一律拒绝）；
// ready 配空 selected_ids 是有意保留的新行为：由服务端按距离回退选择。
func TestRealFix_ShapeChecksKept(t *testing.T) {
	policy := realfixPolicy(t, DefaultPreferences())
	base := string(realfixArgs(`"poi:B0JK7HFEBH"`, ""))
	for name, data := range map[string]string{
		"缺selected_ids键":     strings.Replace(base, `"selected_ids":["poi:B0JK7HFEBH"],`, ``, 1),
		"selected_ids为null":  strings.Replace(base, `["poi:B0JK7HFEBH"]`, `null`, 1),
		"缺reason_codes键":     strings.Replace(base, `"reason_codes":[],`, ``, 1),
		"缺options键":          strings.Replace(base, `,"options":[]`, ``, 1),
		"未知原因码":              strings.Replace(base, `"reason_codes":[]`, `"reason_codes":["hack"]`, 1),
		"needs_choice带不可选选中": strings.Replace(strings.Replace(base, `"ready"`, `"needs_choice"`, 1), `"poi:B0JK7HFEBH"`, `"poi:B000A1A549"`, 1),
	} {
		_, err := policy.ParseDecision([]byte(data))
		require.ErrorIs(t, err, ErrInvalidRecommendation, name)
	}
	// 方案内原因码：合法枚举但不可证实的被过滤；枚举外的拒绝。限价 30 时服务端无可选候选，needs_choice 本身合法。
	price := 30
	strict := DefaultPreferences()
	strict.MaxPriceCNY = &price
	choice := func(reason string) []byte {
		return []byte(strings.Replace(strings.Replace(base, `"status":"ready","selected_ids":["poi:B0JK7HFEBH"]`, `"status":"needs_choice","selected_ids":[]`, 1),
			`"options":[]`, `"options":[{"option_id":"a","label":"b","observed_ids":[],"reason_codes":[`+reason+`],"changes":{"max_price_cny":100}}]`, 1))
	}
	decision, err := realfixPolicy(t, strict).ParseDecision(choice(`"no_candidates"`))
	require.NoError(t, err)
	require.Empty(t, decision.Options[0].ReasonCodes)
	_, err = realfixPolicy(t, strict).ParseDecision(choice(`"hack"`))
	require.ErrorIs(t, err, ErrInvalidRecommendation, "方案内未知原因码")
	decision, err = policy.ParseDecision(realfixArgs(``, ""))
	require.NoError(t, err, "ready + 空 selected_ids：保留新行为，服务端按距离回退")
	require.Equal(t, DecisionReady, decision.Status)
	require.Equal(t, []string{"poi:B0KDMCC8R7", "poi:B0FFKQFIXP", "poi:B0GUJAP8NM"}, decision.SelectedIDs)
}
