package locations

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"food-agent/pkg/amap"
)

type AmapMCP interface {
	CallToolText(ctx context.Context, name string, arguments map[string]any) (string, error)
}

type Service struct {
	mcp AmapMCP
}

func NewService(mcp AmapMCP) (*Service, error) {
	if mcp == nil {
		return nil, errDependency
	}
	return &Service{mcp: mcp}, nil
}

type toolCall struct {
	name string
	args map[string]any
}

func (s *Service) Search(ctx context.Context, input SearchInput) ([]Location, error) {
	searchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	args := map[string]any{"keywords": input.query, "page_size": 10, "page_num": 1}
	if input.city != "" {
		args["region"] = input.city
	}
	records, err := s.call(searchCtx, toolCall{name: "amap.poi2_text", args: args})
	if err != nil {
		return nil, err
	}
	items := make([]Location, 0, 10)
	for _, raw := range records {
		var poi amap.POI2POI
		if err := json.Unmarshal(raw, &poi); err != nil {
			continue
		}
		location, ok := parseLocation(poi.Location)
		if !ok || strings.TrimSpace(poi.Name) == "" {
			continue
		}
		items = append(items, Location{
			ID: strings.TrimSpace(poi.ID), Name: strings.TrimSpace(poi.Name),
			Address: strings.TrimSpace(poi.Address), Location: location, City: strings.TrimSpace(poi.Cityname),
		})
	}
	if len(items) > 0 {
		return items, nil
	}
	geoArgs := map[string]any{"address": input.query}
	if input.city != "" {
		geoArgs["city"] = input.city
	}
	records, err = s.call(searchCtx, toolCall{name: "amap.geocode", args: geoArgs})
	if err != nil {
		return nil, err
	}
	for _, raw := range records {
		var geo amap.GeocodeItem
		if err := json.Unmarshal(raw, &geo); err != nil {
			continue
		}
		location, ok := parseLocation(geo.Location)
		if !ok {
			continue
		}
		items = append(items, Location{
			Name: input.query, Address: strings.TrimSpace(geo.FormattedAddress),
			Location: location, City: strings.TrimSpace(string(geo.City)),
		})
	}
	return items, nil
}

func (s *Service) call(ctx context.Context, call toolCall) ([]json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	text, err := s.mcp.CallToolText(ctx, call.name, call.args)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	if err != nil {
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			return nil, context.DeadlineExceeded
		case errors.Is(err, context.Canceled):
			return nil, context.Canceled
		default:
			return nil, ErrUpstream
		}
	}
	var envelope struct {
		Status   string          `json:"status"`
		Infocode string          `json:"infocode"`
		Pois     json.RawMessage `json:"pois"`
		Geocodes json.RawMessage `json:"geocodes"`
	}
	if err := json.Unmarshal([]byte(text), &envelope); err != nil || envelope.Status != "1" ||
		(envelope.Infocode != "" && envelope.Infocode != "10000") {
		return nil, ErrUpstream
	}
	raw := envelope.Pois
	if call.name == "amap.geocode" {
		raw = envelope.Geocodes
	}
	var records []json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &records) != nil {
		return nil, ErrUpstream
	}
	return records[:min(len(records), 10)], nil
}

func parseLocation(raw string) (string, bool) {
	parts := strings.Split(raw, ",")
	if len(parts) != 2 {
		return "", false
	}
	lng, lngErr := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	lat, latErr := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if lngErr != nil || latErr != nil || math.IsNaN(lng) || math.IsNaN(lat) ||
		math.IsInf(lng, 0) || math.IsInf(lat, 0) || lng < -180 || lng > 180 || lat < -90 || lat > 90 {
		return "", false
	}
	return strconv.FormatFloat(lng, 'f', -1, 64) + "," + strconv.FormatFloat(lat, 'f', -1, 64), true
}
