package options

import (
	"errors"
	"fmt"
	"strings"
)

func (o *ServerRunOptions) Validate() []error {
	var errs []error

	if o.Server == nil {
		errs = append(errs, fmt.Errorf("server options is nil"))
	} else {
		errs = append(errs, o.Server.Validate()...)

		// Gin mode lives on server options.
		switch strings.ToLower(strings.TrimSpace(o.Server.Mode)) {
		case "", "debug", "release", "test":
		default:
			errs = append(errs, fmt.Errorf("server.mode must be debug/release/test"))
		}
	}

	switch o.EffectiveDBType() {
	case "pgsql":
		if o.Data.Database.PgSQL != nil {
			errs = append(errs, o.Data.Database.PgSQL.Validate()...)
		}
	case "mysql":
		if o.Data.Database.MySQL != nil {
			errs = append(errs, o.Data.Database.MySQL.Validate()...)
		}
	default:
		errs = append(errs, fmt.Errorf("data.database.type must be pgsql or mysql"))
	}

	if o.Data.Redis != nil {
		errs = append(errs, o.Data.Redis.Validate()...)
	}
	if o.LLM != nil {
		errs = append(errs, o.LLM.Validate()...)
	}
	if o.Log != nil {
		errs = append(errs, o.Log.Validate()...)
	}

	if o.Amap != nil && o.Amap.TimeoutSeconds < 0 {
		errs = append(errs, fmt.Errorf("amap.timeout_seconds is invalid"))
	}
	if o.MCP.Client.InitializeTimeoutSeconds <= 0 || o.MCP.Client.CallTimeoutSeconds <= 0 {
		errs = append(errs, fmt.Errorf("mcp client timeout seconds is invalid"))
	}

	if o.MCP.Servers.Amap.Enabled && strings.ToLower(strings.TrimSpace(o.MCP.Servers.Amap.Transport)) == "stdio" {
		if strings.TrimSpace(o.MCP.Servers.Amap.Command) == "" {
			// Not required for cmd/mcp-server itself, but required when we run stdio child process.
			// Keep it strict at options level for now.
			errs = append(errs, fmt.Errorf("mcp.servers.amap.command is required when enabled stdio"))
		}
		if strings.TrimSpace(o.MCP.Servers.Amap.ConfigFile) == "" {
			errs = append(errs, fmt.Errorf("mcp.servers.amap.config_file is required when enabled stdio"))
		}
	}

	return errs
}

func joinErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	if len(errs) == 1 {
		return errs[0]
	}
	var b strings.Builder
	for i, e := range errs {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(e.Error())
	}
	return errors.New(b.String())
}
