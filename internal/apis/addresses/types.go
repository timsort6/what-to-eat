package addresses

import "encoding/json"

type Address struct {
	ID        uint   `json:"id"`
	Label     string `json:"label"`
	IsDefault bool   `json:"is_default"`

	// Common POI fields
	AmapID   string `json:"amap_id,omitempty"`
	Name     string `json:"name,omitempty"`
	Type     string `json:"type,omitempty"`
	TypeCode string `json:"typecode,omitempty"`
	Tel      string `json:"tel,omitempty"`
	Tag      string `json:"tag,omitempty"`

	FormattedAddress string `json:"formatted_address,omitempty"`
	Address          string `json:"address,omitempty"`

	Location  string  `json:"location"`
	Longitude float64 `json:"longitude"`
	Latitude  float64 `json:"latitude"`

	Pname    string `json:"pname,omitempty"`
	Cityname string `json:"cityname,omitempty"`
	Adname   string `json:"adname,omitempty"`
	Citycode string `json:"citycode,omitempty"`
	Adcode   string `json:"adcode,omitempty"`

	AMapPOI json.RawMessage `json:"amap_poi,omitempty"`
}

type CreateAddressRequest struct {
	Label string `json:"label"`

	// Only supports "set default". Omit or false means "do not change default".
	IsDefault *bool `json:"is_default,omitempty"`

	AmapID   string `json:"amap_id,omitempty"`
	Name     string `json:"name,omitempty"`
	Type     string `json:"type,omitempty"`
	TypeCode string `json:"typecode,omitempty"`
	Tel      string `json:"tel,omitempty"`
	Tag      string `json:"tag,omitempty"`

	FormattedAddress string `json:"formatted_address,omitempty"`
	Address          string `json:"address,omitempty"`

	Location string `json:"location"` // required: "lng,lat"

	Pname    string `json:"pname,omitempty"`
	Cityname string `json:"cityname,omitempty"`
	Adname   string `json:"adname,omitempty"`
	Citycode string `json:"citycode,omitempty"`
	Adcode   string `json:"adcode,omitempty"`

	AMapPOI json.RawMessage `json:"amap_poi,omitempty"`
}

type UpdateAddressRequest struct {
	// Optional updates; if set, Label must be non-empty.
	Label *string `json:"label,omitempty"`

	// Only supports "set default". If true => make it default. If false/omitted => no-op.
	IsDefault *bool `json:"is_default,omitempty"`

	AmapID   *string `json:"amap_id,omitempty"`
	Name     *string `json:"name,omitempty"`
	Type     *string `json:"type,omitempty"`
	TypeCode *string `json:"typecode,omitempty"`
	Tel      *string `json:"tel,omitempty"`
	Tag      *string `json:"tag,omitempty"`

	FormattedAddress *string `json:"formatted_address,omitempty"`
	Address          *string `json:"address,omitempty"`

	Location *string `json:"location,omitempty"` // if set, must be "lng,lat"

	Pname    *string `json:"pname,omitempty"`
	Cityname *string `json:"cityname,omitempty"`
	Adname   *string `json:"adname,omitempty"`
	Citycode *string `json:"citycode,omitempty"`
	Adcode   *string `json:"adcode,omitempty"`

	AMapPOI *json.RawMessage `json:"amap_poi,omitempty"`
}
