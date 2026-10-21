package amap

// MCP "tools/list" response types.

type toolsListResult struct {
	Tools []toolDef `json:"tools"`
}

type toolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
}

// MCP "tools/call" request/response types.

type toolsCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type toolsCallResult struct {
	Content []toolContent `json:"content"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

func toolDefs() []toolDef {
	// Keep schema minimal but helpful for LLMs.
	return []toolDef{
		{
			Name:        "amap.geocode",
			Description: "AMap v3 geocode: address -> lng,lat",
			InputSchema: schemaObject(
				map[string]any{
					"address": schemaString("structured address, required"),
					"city":    schemaString("optional cityname/citycode/adcode"),
				},
				[]string{"address"},
			),
		},
		{
			Name:        "amap.regeo",
			Description: "AMap v3 reverse geocode: lng,lat -> address",
			InputSchema: schemaObject(
				map[string]any{
					"location":   schemaString("lng,lat, required"),
					"radius":     schemaInt("optional, meters"),
					"extensions": schemaString("optional, base/all"),
				},
				[]string{"location"},
			),
		},
		{
			Name:        "amap.poi2_text",
			Description: "AMap v5 Place Search 2.0 text search",
			InputSchema: schemaObject(
				map[string]any{
					"keywords":    schemaString("keywords (keywords or types is required)"),
					"types":       schemaString("typecode, optional; use | to join"),
					"region":      schemaString("optional, a region name"),
					"city_limit":  schemaBool("optional, true/false"),
					"show_fields": schemaString("optional, fields list"),
					"page_size":   schemaInt("optional, 1-25"),
					"page_num":    schemaInt("optional, >=1"),
				},
				nil,
			),
		},
		{
			Name:        "amap.poi2_around",
			Description: "AMap v5 Place Search 2.0 nearby search",
			InputSchema: schemaObject(
				map[string]any{
					"location":    schemaString("lng,lat, required"),
					"keywords":    schemaString("optional keywords"),
					"types":       schemaString("typecode, optional; use | to join"),
					"radius":      schemaInt("optional, meters"),
					"sortrule":    schemaString("optional, distance/weight"),
					"region":      schemaString("optional"),
					"city_limit":  schemaBool("optional, true/false"),
					"show_fields": schemaString("optional, fields list"),
					"page_size":   schemaInt("optional, 1-25"),
					"page_num":    schemaInt("optional, >=1"),
				},
				[]string{"location"},
			),
		},

		// Compatibility alias.
		{
			Name:        "amap.search_around",
			Description: "Alias of amap.poi2_around",
			InputSchema: schemaObject(
				map[string]any{
					"location":    schemaString("lng,lat, required"),
					"keywords":    schemaString("optional keywords"),
					"types":       schemaString("typecode, optional; use | to join"),
					"radius":      schemaInt("optional, meters"),
					"sortrule":    schemaString("optional, distance/weight"),
					"region":      schemaString("optional"),
					"city_limit":  schemaBool("optional, true/false"),
					"show_fields": schemaString("optional, fields list"),
					"page_size":   schemaInt("optional, 1-25"),
					"page_num":    schemaInt("optional, >=1"),
				},
				[]string{"location"},
			),
		},
	}
}

func schemaObject(props map[string]any, required []string) map[string]any {
	m := map[string]any{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

func schemaString(desc string) map[string]any {
	m := map[string]any{"type": "string"}
	if desc != "" {
		m["description"] = desc
	}
	return m
}

func schemaInt(desc string) map[string]any {
	m := map[string]any{"type": "integer"}
	if desc != "" {
		m["description"] = desc
	}
	return m
}

func schemaBool(desc string) map[string]any {
	m := map[string]any{"type": "boolean"}
	if desc != "" {
		m["description"] = desc
	}
	return m
}
