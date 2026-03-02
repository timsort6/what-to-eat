package config

// Config is the YAML configuration schema for food-agent.
//
// It is intentionally "file-only": env/flags merging is handled by internal/options
// through a single viper instance to guarantee priority:
// flag(Changed) > env(FOOD_AGENT_*) > config file > defaults.
type Config struct {
	Server ServerConfig `mapstructure:"server" yaml:"server"`
	Auth   AuthConfig   `mapstructure:"auth" yaml:"auth"`
	Data   DataConfig   `mapstructure:"data" yaml:"data"`

	// LLM is OpenAI-compatible LLM config (base_url/api_key/model...).
	LLM LLMConfig `mapstructure:"llm" yaml:"llm"`

	// MCP holds Model Context Protocol configuration (clients + servers).
	MCP MCPConfig `mapstructure:"mcp" yaml:"mcp"`

	Log LogConfig `mapstructure:"log" yaml:"log"`
}

type ServerConfig struct {
	BindAddress string `mapstructure:"bind_address" yaml:"bind_address"`
	Port        int    `mapstructure:"port" yaml:"port"`
	// Mode is gin mode: debug/release/test.
	Mode string `mapstructure:"mode" yaml:"mode"`
}

type AuthConfig struct {
	JWTSecret           string `mapstructure:"jwt_secret" yaml:"jwt_secret"`
	JWTExpiresInSeconds int    `mapstructure:"jwt_expires_in_seconds" yaml:"jwt_expires_in_seconds"`
	Issuer              string `mapstructure:"issuer" yaml:"issuer"`
}

type DataConfig struct {
	Database DatabaseConfig `mapstructure:"database" yaml:"database"`
	Redis    RedisConfig    `mapstructure:"redis" yaml:"redis"`
}

type DatabaseConfig struct {
	// Type is "pgsql" or "mysql". If empty, defaults to "pgsql".
	Type  string      `mapstructure:"type" yaml:"type"`
	PgSQL PgSQLConfig `mapstructure:"pgsql" yaml:"pgsql"`
	MySQL MySQLConfig `mapstructure:"mysql" yaml:"mysql"`

	// Pool is optional connection pool config (if/when we wire it to gorm/sql.DB).
	Pool DBPoolConfig `mapstructure:"pool" yaml:"pool"`
}

type DBPoolConfig struct {
	MaxOpenConns        int `mapstructure:"max_open_conns" yaml:"max_open_conns"`
	MaxIdleConns        int `mapstructure:"max_idle_conns" yaml:"max_idle_conns"`
	ConnMaxLifetimeSecs int `mapstructure:"conn_max_lifetime_seconds" yaml:"conn_max_lifetime_seconds"`
}

type PgSQLConfig struct {
	Host     string `mapstructure:"host" yaml:"host"`
	Port     int    `mapstructure:"port" yaml:"port"`
	User     string `mapstructure:"user" yaml:"user"`
	Password string `mapstructure:"password" yaml:"password"`
	DBName   string `mapstructure:"dbname" yaml:"dbname"`
	SSLMode  string `mapstructure:"sslmode" yaml:"sslmode"`
	TimeZone string `mapstructure:"timezone" yaml:"timezone"`

	// LogLevel supports "silent/error/warn/info" or an integer compatible with gorm logger levels.
	LogLevel any `mapstructure:"log_level" yaml:"log_level"`
}

type MySQLConfig struct {
	Host     string `mapstructure:"host" yaml:"host"`
	Port     int    `mapstructure:"port" yaml:"port"`
	User     string `mapstructure:"user" yaml:"user"`
	Password string `mapstructure:"password" yaml:"password"`
	DBName   string `mapstructure:"dbname" yaml:"dbname"`

	// Params is appended as raw query string (e.g. "charset=utf8mb4&parseTime=True&loc=Local").
	// If empty, a safe default is used by our mysql client.
	Params string `mapstructure:"params" yaml:"params"`

	// LogLevel supports "silent/error/warn/info" or an integer compatible with gorm logger levels.
	LogLevel any `mapstructure:"log_level" yaml:"log_level"`
}

type RedisConfig struct {
	Addr     string `mapstructure:"addr" yaml:"addr"`
	Password string `mapstructure:"password" yaml:"password"`
	DB       int    `mapstructure:"db" yaml:"db"`
}

type LLMConfig struct {
	BaseURL        string `mapstructure:"base_url" yaml:"base_url"`
	APIKey         string `mapstructure:"api_key" yaml:"api_key"`
	Model          string `mapstructure:"model" yaml:"model"`
	Language       string `mapstructure:"language" yaml:"language"`
	TimeoutSeconds int    `mapstructure:"timeout_seconds" yaml:"timeout_seconds"`
	MaxRetries     int    `mapstructure:"max_retries" yaml:"max_retries"`
}

type AmapConfig struct {
	Key            string `mapstructure:"key" yaml:"key"`
	BaseURL        string `mapstructure:"base_url" yaml:"base_url"`
	TimeoutSeconds int    `mapstructure:"timeout_seconds" yaml:"timeout_seconds"`
}

type MCPConfig struct {
	Client  MCPClientConfig            `mapstructure:"client" yaml:"client"`
	Servers map[string]MCPServerConfig `mapstructure:"servers" yaml:"servers"`
}

type MCPClientConfig struct {
	InitializeTimeoutSeconds int `mapstructure:"initialize_timeout_seconds" yaml:"initialize_timeout_seconds"`
	CallTimeoutSeconds       int `mapstructure:"call_timeout_seconds" yaml:"call_timeout_seconds"`
}

// MCPServerConfig is a generic MCP server definition.
// Currently only "stdio" transport is implemented in internal/mcp.
type MCPServerConfig struct {
	Enabled    bool              `mapstructure:"enabled" yaml:"enabled"`
	Transport  string            `mapstructure:"transport" yaml:"transport"` // stdio/http/sse (future)
	Command    string            `mapstructure:"command" yaml:"command"`
	Args       []string          `mapstructure:"args" yaml:"args"`
	Env        map[string]string `mapstructure:"env" yaml:"env"`
	WorkDir    string            `mapstructure:"work_dir" yaml:"work_dir"`
	ConfigFile string            `mapstructure:"config_file" yaml:"config_file"`
}

type LogConfig struct {
	// Level supports "debug/info/warn/error/dpanic/panic/fatal" or an integer zap level.
	Level string `mapstructure:"level" yaml:"level"`

	Filename      string `mapstructure:"filename" yaml:"filename"`
	MaxSize       int    `mapstructure:"max_size" yaml:"max_size"`
	MaxBackups    int    `mapstructure:"max_backups" yaml:"max_backups"`
	MaxAge        int    `mapstructure:"max_age" yaml:"max_age"`
	Compress      bool   `mapstructure:"compress" yaml:"compress"`
	AddStackLevel string `mapstructure:"add_stack_level" yaml:"add_stack_level"`
}
