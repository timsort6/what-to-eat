package options

import (
	"strings"

	"food-agent/pkg/auth"
	"food-agent/pkg/client/cache"
	dbmysql "food-agent/pkg/client/db/mysql"
	dbpgsql "food-agent/pkg/client/db/pgsql"
	"food-agent/pkg/client/llm"
	"food-agent/pkg/defaults"
	"food-agent/pkg/logger"
	serveroptions "food-agent/pkg/server/options" // generic server options (bind/port/tls/config)

	"github.com/spf13/pflag"
)

// ServerRunOptions is the single runtime options root for both cmd/server and cmd/mcp-server.
//
// Final value priority (high -> low):
// cmd-flag(Changed) > env(FOOD_AGENT_*) > config file > defaults.
type ServerRunOptions struct {
	// ConfigFile is used to locate the config file (not read from config file itself).
	ConfigFile string

	// Generic server options used by the web server (bind/port/tls...).
	Server *serveroptions.ServerRunOptions `mapstructure:"server" yaml:"server"`

	Auth *auth.Options `mapstructure:"auth" yaml:"auth"`

	Data DataOptions `mapstructure:"data" yaml:"data"`

	LLM *llm.Options `mapstructure:"llm" yaml:"llm"`

	MCP MCPOptions `mapstructure:"mcp" yaml:"mcp"`

	// Logger
	Log *logger.Options `mapstructure:"log" yaml:"log"`
}

type DataOptions struct {
	Database DatabaseOptions `mapstructure:"database" yaml:"database"`
	Redis    *cache.Options  `mapstructure:"redis" yaml:"redis"`
}

type DatabaseOptions struct {
	Type  string           `mapstructure:"type" yaml:"type"`
	PgSQL *dbpgsql.Options `mapstructure:"pgsql" yaml:"pgsql"`
	MySQL *dbmysql.Options `mapstructure:"mysql" yaml:"mysql"`
}

type MCPClientOptions struct {
	InitializeTimeoutSeconds int `mapstructure:"initialize_timeout_seconds" yaml:"initialize_timeout_seconds"`
	CallTimeoutSeconds       int `mapstructure:"call_timeout_seconds" yaml:"call_timeout_seconds"`
}

type MCPOptions struct {
	Client  MCPClientOptions  `mapstructure:"client" yaml:"client"`
	Servers MCPServersOptions `mapstructure:"servers" yaml:"servers"`
}

type MCPServersOptions struct {
	Amap MCPAmapServerOptions `mapstructure:"amap" yaml:"amap"`
}

type MCPAmapServerOptions struct {
	Enabled   bool     `mapstructure:"enabled" yaml:"enabled"`
	Transport string   `mapstructure:"transport" yaml:"transport"`
	Command   string   `mapstructure:"command" yaml:"command"`
	Args      []string `mapstructure:"args" yaml:"args"`
	WorkDir   string   `mapstructure:"work_dir" yaml:"work_dir"`
	// ConfigFile points to cmd/mcp-server's independent config file.
	// The web server uses it only to start/connect the MCP server process.
	ConfigFile string            `mapstructure:"config_file" yaml:"config_file"`
	Env        map[string]string `mapstructure:"env" yaml:"env"`
}

func NewServerRunOptions() *ServerRunOptions {
	return &ServerRunOptions{
		ConfigFile: defaults.DefaultConfigFile,
		Server: serveroptions.NewServerRunOptions(
			serveroptions.WithDefaultBindAddress(defaults.DefaultServerBindAddress),
			serveroptions.WithDefaultPort(defaults.DefaultServerPort),
			serveroptions.WithDefaultMode(defaults.DefaultServerMode),
		),
		Auth: auth.NewOptions(
			auth.SetDefaultJWTSecret(defaults.DefaultAuthJWTSecret),
			auth.SetDefaultJWTExpiresInSeconds(defaults.DefaultAuthJWTExpiresInSeconds),
			auth.SetDefaultIssuer(defaults.DefaultAuthIssuer),
		),
		Data: DataOptions{
			Database: DatabaseOptions{
				Type: defaults.DefaultDBType,
				PgSQL: dbpgsql.NewPgsqlOptions(
					dbpgsql.SetDefaultHost(defaults.DefaultPgHost),
					dbpgsql.SetDefaultPort(defaults.DefaultPgPort),
					dbpgsql.SetDefaultUser(defaults.DefaultPgUser),
					dbpgsql.SetDefaultPassword(defaults.DefaultPgPassword),
					dbpgsql.SetDefaultDBName(defaults.DefaultPgDBName),
					dbpgsql.SetDefaultSSLMode(defaults.DefaultPgSSLMode),
					dbpgsql.SetDefaultTimeZone(defaults.DefaultPgTimeZone),
					dbpgsql.SetDefaultLogLevel(defaults.DefaultPgLogLevel),
				),
				MySQL: dbmysql.NewMysqlOptions(
					dbmysql.SetDefaultHost(defaults.DefaultMyHost),
					dbmysql.SetDefaultPort(defaults.DefaultMyPort),
					dbmysql.SetDefaultUser(defaults.DefaultMyUser),
					dbmysql.SetDefaultPassword(defaults.DefaultMyPassword),
					dbmysql.SetDefaultDBName(defaults.DefaultMyDBName),
					dbmysql.SetDefaultParams(defaults.DefaultMyParams),
					dbmysql.SetDefaultLogLevel(defaults.DefaultMyLogLevel),
				),
			},
			Redis: cache.NewRedisOptions(
				cache.SetDefaultAddr(defaults.DefaultRedisAddr),
				cache.SetDefaultDB(defaults.DefaultRedisDB),
			),
		},
		LLM: llm.NewLLMOptions(
			llm.SetDefaultBaseURL(defaults.DefaultLLMBaseURL),
			llm.SetDefaultLanguage(defaults.DefaultLLMLanguage),
			llm.SetDefaultTimeoutSeconds(defaults.DefaultLLMTimeoutSeconds),
			llm.SetDefaultMaxRetries(defaults.DefaultLLMMaxRetries),
		),
		MCP: MCPOptions{
			Client: MCPClientOptions{
				InitializeTimeoutSeconds: defaults.DefaultMCPInitTimeoutSeconds,
				CallTimeoutSeconds:       defaults.DefaultMCPCallTimeoutSeconds,
			},
			Servers: MCPServersOptions{
				Amap: MCPAmapServerOptions{
					Enabled:    false,
					Transport:  "stdio",
					Command:    "",
					Args:       nil,
					WorkDir:    "",
					ConfigFile: "",
					Env:        map[string]string{},
				},
			},
		},
		Log: logger.NewLoggerOptions(
			logger.SetDefaultLevel(defaults.DefaultLogLevel),
			logger.SetDefaultAddStackLevel(defaults.DefaultLogAddStackLevel),
			logger.SetDefaultRotation(defaults.DefaultLogMaxSize, defaults.DefaultLogMaxBackups, defaults.DefaultLogMaxAge),
		),
	}
}

func (o *ServerRunOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.ConfigFile, "config", o.ConfigFile, "config file path. env FOOD_AGENT_CONFIG")

	// server.*
	o.Server.AddFlagsWithPrefix(fs, "server")

	// auth.*
	if o.Auth != nil {
		o.Auth.AddFlagsWithPrefix(fs, "auth")
	}

	// data.database.*
	fs.StringVar(&o.Data.Database.Type, "data.database.type", o.Data.Database.Type, "database type: pgsql/mysql")
	o.Data.Database.PgSQL.AddFlagsWithPrefix(fs, "data.database.pgsql")
	o.Data.Database.MySQL.AddFlagsWithPrefix(fs, "data.database.mysql")

	// data.redis.*
	o.Data.Redis.AddFlagsWithPrefix(fs, "data.redis")

	// llm.*
	o.LLM.AddFlagsWithPrefix(fs, "llm")

	// mcp.client.*
	fs.IntVar(&o.MCP.Client.InitializeTimeoutSeconds, "mcp.client.initialize_timeout_seconds", o.MCP.Client.InitializeTimeoutSeconds, "mcp initialize timeout seconds")
	fs.IntVar(&o.MCP.Client.CallTimeoutSeconds, "mcp.client.call_timeout_seconds", o.MCP.Client.CallTimeoutSeconds, "mcp call timeout seconds")

	// mcp.servers.amap.*
	fs.BoolVar(&o.MCP.Servers.Amap.Enabled, "mcp.servers.amap.enabled", o.MCP.Servers.Amap.Enabled, "enable amap mcp server")
	fs.StringVar(&o.MCP.Servers.Amap.Transport, "mcp.servers.amap.transport", o.MCP.Servers.Amap.Transport, "mcp transport: stdio/http/sse")
	fs.StringVar(&o.MCP.Servers.Amap.Command, "mcp.servers.amap.command", o.MCP.Servers.Amap.Command, "amap mcp server command")
	fs.StringSliceVar(&o.MCP.Servers.Amap.Args, "mcp.servers.amap.args", o.MCP.Servers.Amap.Args, "amap mcp server args")
	fs.StringVar(&o.MCP.Servers.Amap.WorkDir, "mcp.servers.amap.work_dir", o.MCP.Servers.Amap.WorkDir, "amap mcp server working directory")
	fs.StringVar(&o.MCP.Servers.Amap.ConfigFile, "mcp.servers.amap.config_file", o.MCP.Servers.Amap.ConfigFile, "amap mcp server config file (passed via FOOD_AGENT_MCP_CONFIG)")
	fs.StringToStringVar(&o.MCP.Servers.Amap.Env, "mcp.servers.amap.env", o.MCP.Servers.Amap.Env, "amap mcp server env map, e.g. KEY=VALUE")

	// log.*
	o.Log.AddFlagsWithPrefix(fs, "log")
}

// LoadEnv applies environment variables to the already-unmarshaled options struct.
// It is invoked by Complete() to preserve priority:
// flag(Changed) > env > config file > defaults.
func (o *ServerRunOptions) LoadEnv(envPrefix string, fs *pflag.FlagSet) []error {
	var errs []error

	if o.Server != nil {
		errs = append(errs, o.Server.LoadEnv(envPrefix, fs, "server")...)
	}
	if o.Auth != nil {
		errs = append(errs, o.Auth.LoadEnv(envPrefix, fs, "auth")...)
	}
	// data.database.*
	if o.Data.Database.PgSQL != nil {
		errs = append(errs, o.Data.Database.PgSQL.LoadEnv(envPrefix, fs, "data.database.pgsql")...)
	}
	if o.Data.Database.MySQL != nil {
		errs = append(errs, o.Data.Database.MySQL.LoadEnv(envPrefix, fs, "data.database.mysql")...)
	}
	// data.redis.*
	if o.Data.Redis != nil {
		errs = append(errs, o.Data.Redis.LoadEnv(envPrefix, fs, "data.redis")...)
	}
	// llm.*
	if o.LLM != nil {
		errs = append(errs, o.LLM.LoadEnv(envPrefix, fs, "llm")...)
	}
	// log.*
	if o.Log != nil {
		errs = append(errs, o.Log.LoadEnv(envPrefix, fs, "log")...)
	}

	return errs
}

func (o *ServerRunOptions) EffectiveDBType() string {
	t := strings.ToLower(strings.TrimSpace(o.Data.Database.Type))
	switch t {
	case "", "pgsql", "postgres", "postgresql":
		return "pgsql"
	case "mysql":
		return "mysql"
	default:
		return t
	}
}
