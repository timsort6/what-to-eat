package config

// MCPServerAppConfig is the YAML configuration schema for cmd/mcp-server.
//
// It is intentionally minimal and independent from the main web server config.
type MCPServerAppConfig struct {
	Amap AmapConfig `mapstructure:"amap" yaml:"amap"`
	Log  LogConfig  `mapstructure:"log" yaml:"log"`
}
