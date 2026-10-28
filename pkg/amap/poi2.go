package amap

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// POI2Service groups Place Search 2.0 APIs (v5).
type POI2Service struct {
	client *Client
}

type POI2TextRequest struct {
	Keywords string
	Types    string

	Region    string
	CityLimit *bool

	ShowFields string
	PageSize   int
	PageNum    int

	Sig      string
	Callback string
}

type POI2AroundRequest struct {
	Location string // "lng,lat"

	Keywords string
	Types    string
	Radius   int
	SortRule string // distance/weight

	Region    string
	CityLimit *bool

	ShowFields string
	PageSize   int
	PageNum    int

	Sig      string
	Callback string
}

type POI2TextResponse struct {
	openAPIStatus
	Count string    `json:"count"`
	Pois  []POI2POI `json:"pois"`
}

type POI2AroundResponse struct {
	openAPIStatus
	Count string    `json:"count"`
	Pois  []POI2POI `json:"pois"`
}

// POI2POI keeps commonly used fields typed, and preserves all other fields
// returned by AMap in Extra.
type POI2POI struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Location string `json:"location"`
	Distance string `json:"distance"`

	Type     string `json:"type"`
	Typecode string `json:"typecode"`

	Pname    string `json:"pname"`
	Cityname string `json:"cityname"`
	Adname   string `json:"adname"`

	Address  string `json:"address"`
	Pcode    string `json:"pcode"`
	Adcode   string `json:"adcode"`
	Citycode string `json:"citycode"`

	Tel          string `json:"tel"`
	Tag          string `json:"tag"`
	Rating       string `json:"rating"`
	Cost         string `json:"cost"`
	CostCurrency string `json:"cost_currency"`
	CostBasis    string `json:"cost_basis"`
	ParkingType  string `json:"parking_type"`
	Alias        string `json:"alias"`
	Keytag       string `json:"keytag"`
	Rectag       string `json:"rectag"`

	EntrLocation string `json:"entr_location"`
	ExitLocation string `json:"exit_location"`
	Gridcode     string `json:"gridcode"`

	OpentimeToday string `json:"opentime_today"`
	OpentimeWeek  string `json:"opentime_week"`

	Extra map[string]json.RawMessage `json:"-"`
}

func (p *POI2POI) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	// Decode known fields defensively (AMap may return []/null for "missing" fields).
	p.ID = flexString(raw["id"])
	p.Name = flexString(raw["name"])
	p.Location = flexString(raw["location"])
	p.Distance = flexString(raw["distance"])
	p.Type = flexString(raw["type"])
	p.Typecode = flexString(raw["typecode"])
	p.Pname = flexString(raw["pname"])
	p.Cityname = flexString(raw["cityname"])
	p.Adname = flexString(raw["adname"])
	p.Address = flexString(raw["address"])
	p.Pcode = flexString(raw["pcode"])
	p.Adcode = flexString(raw["adcode"])
	p.Citycode = flexString(raw["citycode"])
	p.Tel = flexString(raw["tel"])
	p.Rating = flexString(raw["rating"])
	p.ParkingType = flexString(raw["parking_type"])
	p.Alias = flexString(raw["alias"])
	p.Keytag = flexString(raw["keytag"])
	p.Rectag = flexString(raw["rectag"])
	p.EntrLocation, p.ExitLocation = flexString(raw["entr_location"]), flexString(raw["exit_location"])
	p.Gridcode = flexString(raw["gridcode"])
	business, currencyField := raw, "cost_currency"
	if nested, present := raw["business"]; present {
		business, currencyField = nil, "currency"
		if err := json.Unmarshal(nested, &business); err != nil {
			business = nil
		}
	}
	p.Tag, p.Cost = poi2BusinessString(business["tag"]), poi2BusinessText(business["cost"])
	p.CostCurrency, p.CostBasis = poi2BusinessString(business[currencyField]), poi2BusinessString(business["cost_basis"])
	p.OpentimeToday, p.OpentimeWeek = poi2BusinessString(business["opentime_today"]), poi2BusinessString(business["opentime_week"])

	known := map[string]struct{}{
		"id": {}, "name": {}, "location": {}, "distance": {},
		"type": {}, "typecode": {},
		"pname": {}, "cityname": {}, "adname": {},
		"address": {}, "pcode": {}, "adcode": {}, "citycode": {},
		"tel": {}, "tag": {}, "rating": {}, "cost": {}, "cost_currency": {}, "cost_basis": {}, "business": {}, "parking_type": {}, "alias": {}, "keytag": {}, "rectag": {},
		"entr_location": {}, "exit_location": {}, "gridcode": {},
		"opentime_today": {}, "opentime_week": {},
	}
	extra := make(map[string]json.RawMessage)
	for k, v := range raw {
		if _, ok := known[k]; ok {
			continue
		}
		extra[k] = v
	}
	p.Extra = extra
	return nil
}

func poi2BusinessText(raw json.RawMessage) string {
	var number json.Number
	if err := json.Unmarshal(raw, &number); err != nil {
		return ""
	}
	return number.String()
}

func poi2BusinessString(raw json.RawMessage) string {
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

// Text searches POIs by keywords/types: v5 /place/text.
func (s *POI2Service) Text(ctx context.Context, req *POI2TextRequest) (*POI2TextResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("amap poi2 text: nil request")
	}
	if stringsTrim(req.Keywords) == "" && stringsTrim(req.Types) == "" {
		return nil, fmt.Errorf("amap poi2 text: keywords or types is required")
	}

	query := map[string]string{}
	if stringsTrim(req.Keywords) != "" {
		query["keywords"] = req.Keywords
	}
	if stringsTrim(req.Types) != "" {
		query["types"] = req.Types
	}
	if stringsTrim(req.Region) != "" {
		query["region"] = req.Region
	}
	if req.CityLimit != nil {
		if *req.CityLimit {
			query["city_limit"] = "true"
		} else {
			query["city_limit"] = "false"
		}
	}
	if stringsTrim(req.ShowFields) != "" {
		query["show_fields"] = req.ShowFields
	}
	if req.PageSize > 0 {
		query["page_size"] = strconv.Itoa(req.PageSize)
	}
	if req.PageNum > 0 {
		query["page_num"] = strconv.Itoa(req.PageNum)
	}
	if stringsTrim(req.Sig) != "" {
		query["sig"] = req.Sig
	}
	if stringsTrim(req.Callback) != "" {
		query["callback"] = req.Callback
	}

	var out POI2TextResponse
	if err := s.client.doGetAbs(ctx, s.client.v5URL("/place/text"), query, &out); err != nil {
		return nil, fmt.Errorf("amap poi2 text: %w", err)
	}
	if !out.ok() {
		return nil, fmt.Errorf("amap poi2 text failed: info=%s infocode=%s", out.Info, out.Infocode)
	}
	return &out, nil
}

// Around searches nearby POIs: v5 /place/around.
func (s *POI2Service) Around(ctx context.Context, req *POI2AroundRequest) (*POI2AroundResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("amap poi2 around: nil request")
	}
	if stringsTrim(req.Location) == "" {
		return nil, fmt.Errorf("amap poi2 around: empty location")
	}

	query := map[string]string{
		"location": req.Location,
	}
	if stringsTrim(req.Keywords) != "" {
		query["keywords"] = req.Keywords
	}
	if stringsTrim(req.Types) != "" {
		query["types"] = req.Types
	}
	if req.Radius > 0 {
		query["radius"] = strconv.Itoa(req.Radius)
	}
	if stringsTrim(req.SortRule) != "" {
		query["sortrule"] = req.SortRule
	}
	if stringsTrim(req.Region) != "" {
		query["region"] = req.Region
	}
	if req.CityLimit != nil {
		if *req.CityLimit {
			query["city_limit"] = "true"
		} else {
			query["city_limit"] = "false"
		}
	}
	if stringsTrim(req.ShowFields) != "" {
		query["show_fields"] = req.ShowFields
	}
	if req.PageSize > 0 {
		query["page_size"] = strconv.Itoa(req.PageSize)
	}
	if req.PageNum > 0 {
		query["page_num"] = strconv.Itoa(req.PageNum)
	}
	if stringsTrim(req.Sig) != "" {
		query["sig"] = req.Sig
	}
	if stringsTrim(req.Callback) != "" {
		query["callback"] = req.Callback
	}

	var out POI2AroundResponse
	if err := s.client.doGetAbs(ctx, s.client.v5URL("/place/around"), query, &out); err != nil {
		return nil, fmt.Errorf("amap poi2 around: %w", err)
	}
	if !out.ok() {
		return nil, fmt.Errorf("amap poi2 around failed: info=%s infocode=%s", out.Info, out.Infocode)
	}
	return &out, nil
}
