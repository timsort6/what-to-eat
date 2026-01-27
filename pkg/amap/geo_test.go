package amap

import (
	"encoding/json"
	"testing"
)

func TestGeocodeItem_NumberCanBeEmptyArray(t *testing.T) {
	raw := []byte(`{
		"formatted_address":"x",
		"country":"中国",
		"province":"四川省",
		"citycode":"028",
		"city":"成都市",
		"district":"武侯区",
		"township":[],
		"adcode":"510107",
		"street":"锦城大道",
		"number":[],
		"location":"104.055145,30.575345",
		"level":"门牌号"
	}`)

	var item GeocodeItem
	if err := json.Unmarshal(raw, &item); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(item.Number) != "" {
		t.Fatalf("expected empty number, got %q", string(item.Number))
	}
	if string(item.Street) != "锦城大道" {
		t.Fatalf("expected street, got %q", string(item.Street))
	}
	if string(item.City) != "成都市" {
		t.Fatalf("expected city, got %q", string(item.City))
	}
	if string(item.Township) != "" {
		t.Fatalf("expected empty township, got %q", string(item.Township))
	}
}
