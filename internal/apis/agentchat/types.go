package agentchat

import "encoding/json"

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Messages []ChatMessage `json:"messages"`

	// Location context (required: at least one).
	AddressID *uint  `json:"address_id,omitempty"`
	Location  string `json:"location,omitempty"` // "lng,lat"

	// Optional search preferences passed to AMap POI2 around.
	Keywords  string `json:"keywords,omitempty"`
	Types     string `json:"types,omitempty"`
	Radius    int    `json:"radius,omitempty"`
	SortRule  string `json:"sortrule,omitempty"`
	Region    string `json:"region,omitempty"`
	CityLimit *bool  `json:"city_limit,omitempty"`
	Limit     int    `json:"limit,omitempty"` // how many places to return (default 10)
}

type Place struct {
	// Common fields (best-effort extraction from AMap POI2).
	ID       string `json:"id,omitempty"`
	Name     string `json:"name,omitempty"`
	Location string `json:"location,omitempty"` // "lng,lat"
	Address  string `json:"address,omitempty"`
	Distance string `json:"distance,omitempty"`

	Type     string `json:"type,omitempty"`
	TypeCode string `json:"typecode,omitempty"`

	Pname    string `json:"pname,omitempty"`
	Cityname string `json:"cityname,omitempty"`
	Adname   string `json:"adname,omitempty"`
	Citycode string `json:"citycode,omitempty"`
	Adcode   string `json:"adcode,omitempty"`

	Tel string `json:"tel,omitempty"`
	Tag string `json:"tag,omitempty"`

	// Raw keeps the original POI object (as much as we can preserve).
	Raw json.RawMessage `json:"raw,omitempty"`
}

type DoneData struct {
	AnswerText    string  `json:"answer_text"`
	Places        []Place `json:"places"`
	PlacesCount   int     `json:"places_count"`
	Location      string  `json:"location,omitempty"`
	UsedAddressID *uint   `json:"address_id,omitempty"`
}
