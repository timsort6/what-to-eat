package amap

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// PlaceService groups "POI/place search" related APIs (v3).
type PlaceService struct {
	client *Client
}

// PlaceAroundRequest is the v3 "around" search request.
type PlaceAroundRequest struct {
	// Location in "lng,lat", e.g. "116.481499,39.990475".
	Location string
	Keywords string
	Radius   int
	Page     int
	Offset   int
}

type PlaceAroundResponse struct {
	openAPIStatus
	Count string `json:"count"`
	Pois  []POI  `json:"pois"`
}

type POI struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	TypeCode string `json:"typecode"`
	Address  string `json:"address"`
	Location string `json:"location"`
	Tel      string `json:"tel"`
	Distance string `json:"distance"`
}

func (p *POI) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	p.ID = flexString(raw["id"])
	p.Name = flexString(raw["name"])
	p.Type = flexString(raw["type"])
	p.TypeCode = flexString(raw["typecode"])
	p.Address = flexString(raw["address"])
	p.Location = flexString(raw["location"])
	p.Tel = flexString(raw["tel"])
	p.Distance = flexString(raw["distance"])
	return nil
}

// Around searches nearby POIs: v3 /place/around.
func (s *PlaceService) Around(ctx context.Context, req *PlaceAroundRequest) (*PlaceAroundResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("amap place around: nil request")
	}
	if stringsTrim(req.Location) == "" {
		return nil, fmt.Errorf("amap place around: empty location")
	}

	query := map[string]string{
		"location": req.Location,
	}
	if stringsTrim(req.Keywords) != "" {
		query["keywords"] = req.Keywords
	}
	if req.Radius > 0 {
		query["radius"] = strconv.Itoa(req.Radius)
	}
	if req.Page > 0 {
		query["page"] = strconv.Itoa(req.Page)
	}
	if req.Offset > 0 {
		query["offset"] = strconv.Itoa(req.Offset)
	}

	var out PlaceAroundResponse
	if err := s.client.doGetV3(ctx, "/place/around", query, &out); err != nil {
		return nil, fmt.Errorf("amap place around: %w", err)
	}
	if !out.ok() {
		return nil, fmt.Errorf("amap place around failed: info=%s infocode=%s", out.Info, out.Infocode)
	}
	return &out, nil
}

func stringsTrim(s string) string {
	return strings.TrimSpace(s)
}
