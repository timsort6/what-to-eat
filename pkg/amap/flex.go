package amap

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// flexString decodes a JSON value into a "best-effort" string.
//
// AMap APIs sometimes return fields as strings when present, but return [] or null
// when absent. Some fields may also be numbers/bools depending on the POI type.
//
// Rules:
// - "string" => string
// - null => ""
// - [] => "" (empty array)
// - non-empty array/object => compact JSON string
// - number/bool => fmt.Sprint(value)
func flexString(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}

	// Fast path: JSON string.
	if len(raw) > 0 && raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			return s
		}
	}

	// Empty array means "missing" for some AMap fields.
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err == nil {
		if len(arr) == 0 {
			return ""
		}
		return string(raw)
	}

	// Fallback: try decoding into any and stringify.
	var v any
	if err := json.Unmarshal(raw, &v); err == nil {
		return fmt.Sprint(v)
	}

	// Last resort.
	return string(raw)
}
