package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// UnmarshalYAML parses a YAML document into out.
// This is used by internal/options to ensure the config file can be decoded by the declared schema.
func UnmarshalYAML(in []byte, out any) error {
	if out == nil {
		return fmt.Errorf("out is nil")
	}
	return yaml.Unmarshal(in, out)
}
