package defaults

const (
	// Common
	DefaultConfigFile = "configs/config.yaml"

	// Server
	DefaultServerBindAddress = "0.0.0.0"
	DefaultServerPort        = 8080
	DefaultServerMode        = "release"

	// Database
	DefaultDBType = "pgsql"

	DefaultPgHost     = "127.0.0.1"
	DefaultPgPort     = 5432
	DefaultPgUser     = "postgres"
	DefaultPgPassword = "postgres"
	DefaultPgDBName   = "food_agent"
	DefaultPgSSLMode  = "disable"
	DefaultPgTimeZone = "Asia/Shanghai"
	DefaultPgLogLevel = "warn" // silent/error/warn/info

	DefaultMyHost     = "127.0.0.1"
	DefaultMyPort     = 3306
	DefaultMyUser     = "root"
	DefaultMyPassword = "123456"
	DefaultMyDBName   = "food_agent"
	DefaultMyParams   = "charset=utf8mb4&parseTime=True&loc=Local"
	DefaultMyLogLevel = "warn"

	// Redis
	DefaultRedisAddr = "127.0.0.1:6379"
	DefaultRedisDB   = 0

	// LLM
	DefaultLLMBaseURL        = "https://api.openai.com/v1"
	DefaultLLMLanguage       = "zh"
	DefaultLLMTimeoutSeconds = 60
	DefaultLLMMaxRetries     = 2

	// AMap
	DefaultAmapBaseURL        = "https://restapi.amap.com/v3"
	DefaultAmapTimeoutSeconds = 5

	// MCP
	DefaultMCPInitTimeoutSeconds = 5
	DefaultMCPCallTimeoutSeconds = 10

	// Auth
	DefaultAuthJWTSecret           = "change-me"
	DefaultAuthJWTExpiresInSeconds = 86400
	DefaultAuthIssuer              = "food-agent"

	// Log
	DefaultLogLevel         = "info"
	DefaultLogAddStackLevel = "fatal"
	DefaultLogMaxSize       = 10
	DefaultLogMaxBackups    = 10
	DefaultLogMaxAge        = 30
)
