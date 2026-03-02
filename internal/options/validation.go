package options

import (
	"errors"
	"fmt"
	"strings"

	"food-agent/pkg/defaults"
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
	if o.Auth == nil {
		errs = append(errs, fmt.Errorf("auth options is nil"))
	} else {
		if strings.TrimSpace(o.Auth.JWTSecret) == "" {
			errs = append(errs, fmt.Errorf("auth.jwt_secret is required"))
		}
		if o.Auth.JWTExpiresInSeconds <= 0 {
			errs = append(errs, fmt.Errorf("auth.jwt_expires_in_seconds is invalid"))
		}
		// Safety: fail fast in release mode if the secret is still the default.
		if o.Server != nil && strings.ToLower(strings.TrimSpace(o.Server.Mode)) == "release" &&
			strings.TrimSpace(o.Auth.JWTSecret) == defaults.DefaultAuthJWTSecret {
			errs = append(errs, fmt.Errorf("auth.jwt_secret must not be default in release mode"))
		}
	}
	if o.Log != nil {
		errs = append(errs, o.Log.Validate()...)
	}

	if o.MCP.Client.InitializeTimeoutSeconds <= 0 || o.MCP.Client.CallTimeoutSeconds <= 0 {
		errs = append(errs, fmt.Errorf("mcp client timeout seconds is invalid"))
	}

	// This web server requires AMap MCP server to be available.
	if !o.MCP.Servers.Amap.Enabled {
		errs = append(errs, fmt.Errorf("mcp.servers.amap.enabled must be true (required by server)"))
	} else {
		transport := strings.ToLower(strings.TrimSpace(o.MCP.Servers.Amap.Transport))
		if transport != "stdio" {
			errs = append(errs, fmt.Errorf("mcp.servers.amap.transport must be stdio (got %q)", o.MCP.Servers.Amap.Transport))
		}
		if strings.TrimSpace(o.MCP.Servers.Amap.Command) == "" {
			errs = append(errs, fmt.Errorf("mcp.servers.amap.command is required"))
		}
		if strings.TrimSpace(o.MCP.Servers.Amap.ConfigFile) == "" {
			errs = append(errs, fmt.Errorf("mcp.servers.amap.config_file is required"))
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
