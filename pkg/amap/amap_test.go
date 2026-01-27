package amap

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

type callRecord struct {
	mu    sync.Mutex
	paths []string
	query []url.Values
}

func (r *callRecord) add(path string, q url.Values) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.paths = append(r.paths, path)
	r.query = append(r.query, q)
}

func newTestServer(t *testing.T, rec *callRecord) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r.URL.Path, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/v5/place/text":
			if r.URL.Query().Get("keywords") == "fail" {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"status": "0", "info": "INVALID_USER_KEY", "infocode": "10001",
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "1", "info": "OK", "infocode": "10000", "count": "0", "pois": []any{},
			})
		case "/v5/place/around":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "1", "info": "OK", "infocode": "10000", "count": "0", "pois": []any{},
			})
		case "/v3/geocode/geo":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "1", "info": "OK", "infocode": "10000", "count": "1",
				"geocodes": []any{map[string]any{"location": "116.0,39.0"}},
			})
		case "/v3/geocode/regeo":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "1", "info": "OK", "infocode": "10000", "regeocode": map[string]any{},
			})
		case "/v3/place/around":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "1", "info": "OK", "infocode": "10000", "count": "0", "pois": []any{},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "not found"})
		}
	}))
}

func TestPOI2Text_UsesV5AndAddsKey(t *testing.T) {
	rec := &callRecord{}
	srv := newTestServer(t, rec)
	defer srv.Close()

	c := NewClient("k", WithBaseURL(srv.URL)) // base url can be root

	_, err := c.POI2().Text(context.Background(), &POI2TextRequest{Keywords: "beijing"})
	if err != nil {
		t.Fatalf("Text() error: %v", err)
	}

	if len(rec.paths) != 1 || rec.paths[0] != "/v5/place/text" {
		t.Fatalf("unexpected path: %+v", rec.paths)
	}
	if rec.query[0].Get("key") != "k" || rec.query[0].Get("keywords") != "beijing" {
		t.Fatalf("unexpected query: %v", rec.query[0].Encode())
	}
}

func TestGeoGeocode_UsesV3AndAddsKey(t *testing.T) {
	rec := &callRecord{}
	srv := newTestServer(t, rec)
	defer srv.Close()

	c := NewClient("k", WithBaseURL(srv.URL+"/v3")) // base url can be /v3

	_, err := c.Geo().Geocode(context.Background(), &GeocodeRequest{Address: "test address"})
	if err != nil {
		t.Fatalf("Geocode() error: %v", err)
	}

	if len(rec.paths) != 1 || rec.paths[0] != "/v3/geocode/geo" {
		t.Fatalf("unexpected path: %+v", rec.paths)
	}
	if rec.query[0].Get("key") != "k" || rec.query[0].Get("address") != "test address" {
		t.Fatalf("unexpected query: %v", rec.query[0].Encode())
	}
}

func TestPOI2Text_StatusNotOK_ReturnsError(t *testing.T) {
	rec := &callRecord{}
	srv := newTestServer(t, rec)
	defer srv.Close()

	c := NewClient("k", WithBaseURL(srv.URL))

	_, err := c.POI2().Text(context.Background(), &POI2TextRequest{Keywords: "fail"})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "INVALID_USER_KEY") || !strings.Contains(err.Error(), "10001") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPOI2POI_Unmarshal_PreservesExtra(t *testing.T) {
	var p POI2POI
	b := []byte(`{"id":"1","name":"n","location":"1,2","foo":"bar","nested":{"x":1}}`)
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.ID != "1" || p.Name != "n" || p.Location != "1,2" {
		t.Fatalf("unexpected known fields: %+v", p)
	}
	if _, ok := p.Extra["foo"]; !ok {
		t.Fatalf("missing extra foo")
	}
	if _, ok := p.Extra["nested"]; !ok {
		t.Fatalf("missing extra nested")
	}
}

func TestPOI2POI_Unmarshal_ToleratesNonStringFields(t *testing.T) {
	var p POI2POI
	b := []byte(`{
		"id":"1",
		"name":"n",
		"tel":[],
		"rating":4.8,
		"cost":null,
		"parking_type": {"x": 1},
		"foo":"bar"
	}`)
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.ID != "1" || p.Name != "n" {
		t.Fatalf("unexpected known fields: %+v", p)
	}
	if p.Tel != "" {
		t.Fatalf("expected empty tel, got %q", p.Tel)
	}
	if p.Rating != "4.8" {
		t.Fatalf("expected rating 4.8, got %q", p.Rating)
	}
	if p.Cost != "" {
		t.Fatalf("expected empty cost, got %q", p.Cost)
	}
	if p.ParkingType == "" {
		t.Fatalf("expected parking_type to be non-empty stringified json")
	}
	if _, ok := p.Extra["foo"]; !ok {
		t.Fatalf("missing extra foo")
	}
}
