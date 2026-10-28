package agent

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"food-agent/pkg/amap"
)

type Place struct {
	ID            string               `json:"id,omitempty"`
	CandidateKey  string               `json:"candidate_key,omitempty"`
	Name          string               `json:"name,omitempty"`
	Location      string               `json:"location,omitempty"`
	Address       string               `json:"address,omitempty"`
	Distance      string               `json:"distance,omitempty"`
	Type          string               `json:"type,omitempty"`
	TypeCode      string               `json:"typecode,omitempty"`
	Pname         string               `json:"pname,omitempty"`
	Cityname      string               `json:"cityname,omitempty"`
	Adname        string               `json:"adname,omitempty"`
	Citycode      string               `json:"citycode,omitempty"`
	Adcode        string               `json:"adcode,omitempty"`
	Tel           string               `json:"-"`
	Raw           json.RawMessage      `json:"-"`
	Tag           string               `json:"tag,omitempty"`
	Cost          string               `json:"cost,omitempty"`
	CostCurrency  string               `json:"cost_currency,omitempty"`
	CostBasis     string               `json:"cost_basis,omitempty"`
	PriceSources  PriceSources         `json:"price_sources,omitzero"`
	OpentimeToday string               `json:"opentime_today,omitempty"`
	OpentimeWeek  string               `json:"opentime_week,omitempty"`
	Evaluation    *CandidateAssessment `json:"evaluation,omitempty"`
}

type poiResponse struct {
	Status   string            `json:"status"`
	Info     string            `json:"info"`
	Infocode string            `json:"infocode"`
	Pois     []json.RawMessage `json:"pois"`
}

func parseSearchResult(txt string, limit int, scope RequestScope) (SearchResult, error) {
	var resp poiResponse
	if err := json.Unmarshal([]byte(txt), &resp); err != nil {
		return SearchResult{}, fmt.Errorf("invalid poi2 response json: %w", err)
	}
	if strings.TrimSpace(resp.Status) != "1" {
		return SearchResult{}, fmt.Errorf("amap poi2 failed: %s(%s)", strings.TrimSpace(resp.Info), strings.TrimSpace(resp.Infocode))
	}
	limit = boundedPageSize(limit)
	places := make([]Place, 0, min(limit, len(resp.Pois)))
	seen := make(map[string]bool, limit)
	for _, raw := range resp.Pois {
		var fields amap.POI2POI
		if err := json.Unmarshal(raw, &fields); err != nil || strings.TrimSpace(fields.Name) == "" {
			continue
		}
		var source map[string]json.RawMessage
		if err := json.Unmarshal(raw, &source); err != nil {
			continue
		}
		place := normalizeCandidate(Place{ID: fields.ID, Name: fields.Name, Location: fields.Location, Address: fields.Address,
			Type: fields.Type, TypeCode: fields.Typecode, Pname: fields.Pname, Cityname: fields.Cityname, Adname: fields.Adname,
			Citycode: fields.Citycode, Adcode: fields.Adcode, Tag: fields.Tag, Cost: fields.Cost, CostCurrency: fields.CostCurrency,
			CostBasis: fields.CostBasis, PriceSources: priceSources(source["business"] != nil), OpentimeToday: fields.OpentimeToday, OpentimeWeek: fields.OpentimeWeek})
		if seen[place.CandidateKey] {
			continue
		}
		seen[place.CandidateKey] = true
		if distance, ok := candidateDistance(scope.Location, place.Location); ok {
			place.Distance = strconv.FormatFloat(distance, 'f', 3, 64)
		}
		places = append(places, place)
		if len(places) == limit {
			break
		}
	}
	return SearchResult{Places: places, PlacesCount: len(places), Location: scope.Location}, nil
}

func normalizeCandidate(place Place) Place {
	place.Raw, place.Tel, place.Evaluation, place.Distance = nil, "", nil, ""
	for _, field := range []*string{&place.Name, &place.Address, &place.Type, &place.TypeCode, &place.Pname, &place.Cityname,
		&place.Adname, &place.Citycode, &place.Adcode, &place.Tag, &place.Cost, &place.CostCurrency, &place.CostBasis, &place.OpentimeToday, &place.OpentimeWeek} {
		*field = strings.TrimSpace(*field)
		if len(*field) > 1024 {
			*field = ""
		}
	}
	if place.Cost == "" {
		place.PriceSources = PriceSources{}
	} else if place.PriceSources == (PriceSources{}) {
		place.PriceSources = priceSources(false)
	}
	lng, lat, err := parseCoordinate(place.Location)
	place.Location = ""
	if err == nil {
		place.Location = strconv.FormatFloat(lng+0, 'f', -1, 64) + "," + strconv.FormatFloat(lat+0, 'f', -1, 64)
	}
	place.ID = strings.TrimSpace(place.ID)
	if validPOIID(place.ID) {
		place.CandidateKey = "poi:" + place.ID
	} else {
		place.ID = ""
		name := strings.ToLower(strings.Join(strings.Fields(place.Name), " "))
		digest := sha256.Sum256([]byte(name + "\x00" + place.Location))
		place.CandidateKey = "synthetic:" + hex.EncodeToString(digest[:])
	}
	return place
}

func validPOIID(id string) bool {
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	for _, char := range id {
		if !(char >= 'a' && char <= 'z') && !(char >= 'A' && char <= 'Z') && !(char >= '0' && char <= '9') {
			return false
		}
	}
	return true
}

func parseCoordinate(value string) (float64, float64, error) {
	parts := strings.Split(strings.TrimSpace(value), ",")
	if len(parts) != 2 {
		return 0, 0, ErrInvalidScope
	}
	lng, lngErr := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	lat, latErr := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if lngErr != nil || latErr != nil || math.IsNaN(lng) || math.IsNaN(lat) || math.IsInf(lng, 0) || math.IsInf(lat, 0) || lng < -180 || lng > 180 || lat < -90 || lat > 90 {
		return 0, 0, ErrInvalidScope
	}
	return lng, lat, nil
}

func candidateDistance(origin, location string) (float64, bool) {
	lng1, lat1, err1 := parseCoordinate(origin)
	lng2, lat2, err2 := parseCoordinate(location)
	if err1 != nil || err2 != nil {
		return 0, false
	}
	lat1Rad, lat2Rad := lat1*math.Pi/180, lat2*math.Pi/180
	dLat, dLng := (lat2-lat1)*math.Pi/180, (lng2-lng1)*math.Pi/180
	value := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1Rad)*math.Cos(lat2Rad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 6371008.8 * 2 * math.Asin(math.Sqrt(min(1, max(0, value)))), true
}

// 品类规则表（shortcut: 只覆盖下列地域、替代族与菜品族；未列出的组合一律 unknown，出现误判再扩表或改用分类码映射）。
// foodRegions：地域组及其写法（同组内长写法在前）；品类可写成“地域前缀+核心菜品”，如“四川火锅”=川+火锅。
// 川渝视为同一地域组：“四川火锅”与“重庆火锅/渝味火锅”互相满足。
var foodRegions = []struct {
	group   string
	aliases []string
}{
	{"川", []string{"四川", "重庆", "川", "蜀", "渝"}}, {"黔", []string{"贵州", "贵阳", "黔"}},
	{"湘", []string{"湖南", "湘"}}, {"粤", []string{"广东", "粤"}}, {"潮", []string{"潮汕", "潮州"}}, {"滇", []string{"云南", "滇"}},
}

// foodSubstitutes：核心菜品 → 同类不同品（如要火锅时的冒菜/麻辣烫/串串）；核心本身是替代品之一时不互斥。
var foodSubstitutes = map[string][]string{"火锅": {"冒菜", "麻辣烫", "串串", "钵钵鸡", "酸汤锅"}}

// foodFamilies：菜品族；type 某一级恰为另一族时视为明确不符（如要寿司而分类为烤肉）。高德菜系末级（川菜/北京菜/日本料理等）不在表内，不判不符。
var foodFamilies = []string{"火锅", "冒菜", "烤肉", "寿司", "披萨", "面馆"}

// splitFoodRequirement 把品类拆成地域组与核心菜品；核心不足两个字（如“川菜”）时不拆。
func splitFoodRequirement(required string) (string, string) {
	for _, region := range foodRegions {
		for _, alias := range region.aliases {
			if rest, ok := strings.CutPrefix(required, alias); ok {
				if core := strings.TrimPrefix(rest, "味"); utf8.RuneCountInString(core) >= 2 {
					return region.group, core
				}
			}
		}
	}
	return "", required
}

func foodRegionsOf(text string) []string {
	groups := []string{}
	for _, region := range foodRegions {
		if slices.ContainsFunc(region.aliases, func(alias string) bool { return strings.Contains(text, alias) }) {
			groups = append(groups, region.group)
		}
	}
	return groups
}

// assessFood（type 各级与 tag 各项逐项判断，规范化为去空白、全角转半角、小写）：
//   - 匹配：项含完整品类，或含核心菜品且地域与要求一致（要求无地域时只看核心）；
//   - 冲突：项含核心菜品或替代品但地域明确不同；或只有替代品而任何项都不含核心菜品；或 type 某一级恰为另一菜品族；
//   - 只匹配 → satisfied；只冲突 → unsatisfied；都有或都无 → unknown。tag 中的普通菜名（烤肉、毛肚等）既不匹配也不冲突。
func assessFood(requirement *string, place Place) MatchEvidence {
	result := MatchEvidence{State: MatchUnknown, Fields: []EvidenceField{}}
	if requirement == nil {
		return result
	}
	required := strings.ToLower(normalizeQuote(*requirement))
	if required == "" {
		return result
	}
	region, core := splitFoodRequirement(required)
	type item struct {
		field EvidenceField
		text  string
	}
	items := []item{}
	for _, source := range []struct {
		field      EvidenceField
		value, sep string
	}{{"type", place.Type, ";|"}, {"tag", place.Tag, ",;|、"}} {
		for _, text := range strings.FieldsFunc(strings.ToLower(normalizeQuote(source.value)), func(char rune) bool { return strings.ContainsRune(source.sep, char) }) {
			items = append(items, item{source.field, text})
		}
	}
	anyCore := slices.ContainsFunc(items, func(it item) bool { return strings.Contains(it.text, core) })
	substitute := func(text string) bool {
		for key, list := range foodSubstitutes {
			if strings.Contains(core, key) && !strings.Contains(text, core) && slices.ContainsFunc(list, func(other string) bool { return strings.Contains(text, other) }) {
				return true
			}
		}
		return false
	}
	familyConflict := func(text string) bool {
		return slices.Contains(foodFamilies, text) && slices.ContainsFunc(foodFamilies, func(wanted string) bool { return wanted != text && strings.Contains(required, wanted) })
	}
	var matched, conflicting EvidenceField
	for _, it := range items {
		regions := foodRegionsOf(it.text)
		hasCore := strings.Contains(it.text, core)
		switch {
		case strings.Contains(it.text, required) || (hasCore && (region == "" || slices.Contains(regions, region))):
			matched = cmp.Or(matched, it.field)
		case region != "" && len(regions) > 0 && !slices.Contains(regions, region) && (hasCore || substitute(it.text)),
			!anyCore && substitute(it.text),
			it.field == "type" && familyConflict(it.text):
			conflicting = cmp.Or(conflicting, it.field)
		}
	}
	switch {
	case matched != "" && conflicting == "":
		return MatchEvidence{State: MatchSatisfied, Fields: []EvidenceField{matched}}
	case matched == "" && conflicting != "":
		return MatchEvidence{State: MatchUnsatisfied, Fields: []EvidenceField{conflicting}}
	}
	return result
}

// assessPrice：币种与口径明确为人民币人均时按已核实比较（三个字段为证据）；
// 高德 business.cost / 顶层 cost 缺币种或口径时按“高德参考人均（人民币）”比较，证据只有 business.cost；明确的其他币种/口径与非数字仍为 unknown。
func assessPrice(maxPrice *int, place Place) MatchEvidence {
	result := MatchEvidence{State: MatchUnknown, Fields: []EvidenceField{}}
	if place.PriceSources != priceSources(false) && place.PriceSources != priceSources(true) {
		return result
	}
	currency, basis := strings.ToUpper(place.CostCurrency), strings.ToLower(place.CostBasis)
	if (currency != "" && currency != "CNY" && currency != "RMB" && currency != "人民币") || (basis != "" && basis != "per_person" && basis != "人均") {
		return result
	}
	for _, char := range place.Cost {
		if (char < '0' || char > '9') && char != '.' {
			return result
		}
	}
	value, err := strconv.ParseFloat(place.Cost, 64)
	if err != nil || value <= 0 {
		return result
	}
	result = MatchEvidence{State: MatchSatisfied, Fields: []EvidenceField{"business.cost", "business.currency", "business.cost_basis"}}
	if currency == "" || basis == "" {
		result.Fields = []EvidenceField{"business.cost"}
	}
	if maxPrice != nil && value > float64(*maxPrice) {
		result.State = MatchUnsatisfied
	}
	return result
}

func strictDecisionObject(data []byte, kind reflect.Type) error {
	if kind == reflect.TypeFor[PreferenceChanges]() {
		var changes PreferenceChanges
		return json.Unmarshal(data, &changes)
	}
	if kind.Kind() == reflect.Pointer {
		if string(bytes.TrimSpace(data)) == "null" {
			return nil
		}
		return strictDecisionObject(data, kind.Elem())
	}
	if kind.Kind() == reflect.Slice {
		var entries []json.RawMessage
		if err := json.Unmarshal(data, &entries); err != nil {
			return ErrInvalidRecommendation
		}
		for _, entry := range entries {
			if err := strictDecisionObject(entry, kind.Elem()); err != nil {
				return err
			}
		}
		return nil
	}
	if kind.Kind() != reflect.Struct {
		return nil
	}
	names := make([]string, kind.NumField())
	for index := range names {
		names[index] = strings.Split(kind.Field(index).Tag.Get("json"), ",")[0]
	}
	fields, err := DecodeRecommendationFields(data, names)
	if err != nil {
		return err
	}
	for index, name := range names {
		if field, exists := fields[name]; exists {
			if err := strictDecisionObject(field, kind.Field(index).Type); err != nil {
				return err
			}
		}
	}
	return nil
}
