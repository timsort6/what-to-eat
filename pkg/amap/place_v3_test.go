package amap

import (
	"encoding/json"
	"testing"
)

func TestPOI_Unmarshal_ToleratesArrayNullAndNumber(t *testing.T) {
	var p POI
	b := []byte(`{
		"id":"1",
		"name":"n",
		"tel":[],
		"distance": 123,
		"typecode": null
	}`)
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.ID != "1" || p.Name != "n" {
		t.Fatalf("unexpected fields: %+v", p)
	}
	if p.Tel != "" {
		t.Fatalf("expected empty tel, got %q", p.Tel)
	}
	if p.Distance != "123" {
		t.Fatalf("expected distance 123, got %q", p.Distance)
	}
	if p.TypeCode != "" {
		t.Fatalf("expected empty typecode, got %q", p.TypeCode)
	}
}
