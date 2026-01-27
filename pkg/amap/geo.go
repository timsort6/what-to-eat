package amap

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
)

// GeoService groups geocoding and reverse-geocoding APIs (v3).
type GeoService struct {
	client *Client
}

// StringOrEmptyArray decodes AMap fields that may be a string when present, but an empty
// JSON array when absent (per AMap docs).
type StringOrEmptyArray string

func (s *StringOrEmptyArray) UnmarshalJSON(data []byte) error {
	// Common "missing" shape: [].
	var arr []any
	if err := json.Unmarshal(data, &arr); err == nil {
		*s = ""
		return nil
	}
	// Sometimes vendors return null.
	if string(data) == "null" {
		*s = ""
		return nil
	}
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	*s = StringOrEmptyArray(str)
	return nil
}

type GeocodeRequest struct {
	// Address is a structured address string.
	Address string
	// City is optional; it can be cityname/citycode/adcode.
	City string

	// Optional parameters.
	Output   string
	Sig      string
	Callback string
}

type GeocodeResponse struct {
	openAPIStatus
	Count    string        `json:"count"`
	Geocodes []GeocodeItem `json:"geocodes"`
}

type GeocodeItem struct {
	FormattedAddress string             `json:"formatted_address"`
	Country          string             `json:"country"`
	Province         string             `json:"province"`
	Citycode         string             `json:"citycode"`
	City             StringOrEmptyArray `json:"city"` // string or []
	District         string             `json:"district"`
	Township         StringOrEmptyArray `json:"township"` // string or []

	Neighborhood any `json:"neighborhood"`
	Building     any `json:"building"`

	Adcode   string             `json:"adcode"`
	Street   StringOrEmptyArray `json:"street"` // string or []
	Number   StringOrEmptyArray `json:"number"` // string or []
	Location string             `json:"location"`
	Level    string             `json:"level"`
}

type RegeoRequest struct {
	Location string // "lng,lat"

	Poitype    string
	Radius     int
	Extensions string // base/all
	Roadlevel  int
	Homeorcorp int

	Output   string
	Sig      string
	Callback string
}

type RegeoResponse struct {
	openAPIStatus
	Regeocode any `json:"regeocode"`
}

// Geocode converts a structured address into lng/lat: v3 /geocode/geo.
func (s *GeoService) Geocode(ctx context.Context, req *GeocodeRequest) (*GeocodeResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("amap geocode: nil request")
	}
	if stringsTrim(req.Address) == "" {
		return nil, fmt.Errorf("amap geocode: empty address")
	}

	query := map[string]string{
		"address": req.Address,
	}
	if stringsTrim(req.City) != "" {
		query["city"] = req.City
	}
	if stringsTrim(req.Output) != "" {
		query["output"] = req.Output
	}
	if stringsTrim(req.Sig) != "" {
		query["sig"] = req.Sig
	}
	if stringsTrim(req.Callback) != "" {
		query["callback"] = req.Callback
	}

	var out GeocodeResponse
	if err := s.client.doGetV3(ctx, "/geocode/geo", query, &out); err != nil {
		return nil, fmt.Errorf("amap geocode: %w", err)
	}
	if !out.ok() {
		return nil, fmt.Errorf("amap geocode failed: info=%s infocode=%s", out.Info, out.Infocode)
	}
	return &out, nil
}

// Regeo converts lng/lat into an address: v3 /geocode/regeo.
func (s *GeoService) Regeo(ctx context.Context, req *RegeoRequest) (*RegeoResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("amap regeo: nil request")
	}
	if stringsTrim(req.Location) == "" {
		return nil, fmt.Errorf("amap regeo: empty location")
	}

	query := map[string]string{
		"location": req.Location,
	}
	if stringsTrim(req.Poitype) != "" {
		query["poitype"] = req.Poitype
	}
	if req.Radius > 0 {
		query["radius"] = strconv.Itoa(req.Radius)
	}
	if stringsTrim(req.Extensions) != "" {
		query["extensions"] = req.Extensions
	}
	if req.Roadlevel != 0 {
		query["roadlevel"] = strconv.Itoa(req.Roadlevel)
	}
	if req.Homeorcorp != 0 {
		query["homeorcorp"] = strconv.Itoa(req.Homeorcorp)
	}
	if stringsTrim(req.Output) != "" {
		query["output"] = req.Output
	}
	if stringsTrim(req.Sig) != "" {
		query["sig"] = req.Sig
	}
	if stringsTrim(req.Callback) != "" {
		query["callback"] = req.Callback
	}

	var out RegeoResponse
	if err := s.client.doGetV3(ctx, "/geocode/regeo", query, &out); err != nil {
		return nil, fmt.Errorf("amap regeo: %w", err)
	}
	if !out.ok() {
		return nil, fmt.Errorf("amap regeo failed: info=%s infocode=%s", out.Info, out.Infocode)
	}
	return &out, nil
}
