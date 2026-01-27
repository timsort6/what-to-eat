package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"food-agent/internal/apis/helper"
	"food-agent/internal/mcp"
	"food-agent/internal/options"
	"food-agent/pkg/client/amap"
	"food-agent/pkg/client/cache"
	dbmysql "food-agent/pkg/client/db/mysql"
	dbpgsql "food-agent/pkg/client/db/pgsql"
	"food-agent/pkg/client/llm"
	"food-agent/pkg/logger"
	"food-agent/pkg/model"
	"food-agent/pkg/server"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ConsoleServer owns all runtime dependencies and wires them into the HTTP router.
// It mirrors tiansuoVM's ConsoleServer "composition root" style.
type ConsoleServer struct {
	Opts   *options.ServerRunOptions
	StopCh <-chan struct{}

	HTTP   *http.Server
	Router *gin.Engine

	// Core components
	DB    *gorm.DB
	Cache cache.Interface
	LLM   *llm.Client
	Amap  *amap.Client

	// Optional MCP clients (subprocess + stdio).
	AmapMCP *mcp.StdioClient

	closeFns []func(context.Context) error
}

func NewConsoleServer(opts *options.ServerRunOptions, stopCh <-chan struct{}) (*ConsoleServer, error) {
	if opts == nil {
		return nil, fmt.Errorf("nil options")
	}
	if stopCh == nil {
		return nil, fmt.Errorf("nil stop channel")
	}
	return &ConsoleServer{Opts: opts, StopCh: stopCh}, nil
}

// Prepare initializes dependencies, router, and HTTP server.
func (s *ConsoleServer) Prepare(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	// Initialize global zap logger (zap.L()) as early as possible.
	logger.Init(s.Opts.Log)

	if err := s.initGinMode(); err != nil {
		return err
	}
	s.initRouter()

	if err := s.initClients(ctx); err != nil {
		s.closeAll(context.Background())
		return err
	}

	s.registerRoutes()
	s.initHTTPServer()
	return nil
}

func (s *ConsoleServer) Run() error {
	if s.HTTP == nil || s.Router == nil {
		return fmt.Errorf("server is not prepared")
	}

	errCh := make(chan error, 1)
	go func() {
		if s.Opts.Server.TlsCertFile != "" && s.Opts.Server.TlsPrivateKey != "" {
			errCh <- s.HTTP.ListenAndServeTLS(s.Opts.Server.TlsCertFile, s.Opts.Server.TlsPrivateKey)
			return
		}
		errCh <- s.HTTP.ListenAndServe()
	}()

	zap.L().Info("http server started", zap.String("addr", s.HTTP.Addr))

	select {
	case <-s.StopCh:
		zap.L().Info("shutting down http server")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.HTTP.Shutdown(ctx)
		s.closeAll(ctx)
		return nil
	case err := <-errCh:
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			s.closeAll(context.Background())
			return nil
		}
		s.closeAll(context.Background())
		return err
	}
}

func (s *ConsoleServer) initGinMode() error {
	mode := strings.ToLower(strings.TrimSpace(s.Opts.Server.Mode))
	switch mode {
	case "", "release":
		gin.SetMode(gin.ReleaseMode)
	case "debug":
		gin.SetMode(gin.DebugMode)
	case "test":
		gin.SetMode(gin.TestMode)
	default:
		return fmt.Errorf("invalid gin mode: %s", s.Opts.Server.Mode)
	}
	return nil
}

func (s *ConsoleServer) initRouter() {
	r := gin.New()
	r.ContextWithFallback = true
	r.Use(gin.Recovery())
	r.Use(logger.GinLogger())
	r.Use(cors.New(cors.Config{
		AllowAllOrigins:  true,
		AllowCredentials: true,
		AllowMethods:     []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
	}))
	s.Router = r
}

func (s *ConsoleServer) registerRoutes() {
	v1 := s.Router.Group("/api/v1")
	helper.RegisterRoutes(v1)

	// TODO: user/login/favorite/recommend routes will be wired here later.
}

func (s *ConsoleServer) initHTTPServer() {
	addr := fmt.Sprintf("%s:%d", s.Opts.Server.BindAddress, s.Opts.Server.Port)
	s.HTTP = &http.Server{
		Addr:         addr,
		Handler:      s.Router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}
}

func (s *ConsoleServer) initClients(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	// Database
	switch s.Opts.EffectiveDBType() {
	case "pgsql":
		if s.Opts.Data.Database.PgSQL == nil {
			return fmt.Errorf("pgsql options is nil")
		}
		db, err := dbpgsql.NewPgsqlClient(s.Opts.Data.Database.PgSQL)
		if err != nil {
			return fmt.Errorf("init pgsql: %w", err)
		}
		s.DB = db
	case "mysql":
		if s.Opts.Data.Database.MySQL == nil {
			return fmt.Errorf("mysql options is nil")
		}
		db, err := dbmysql.NewMysqlClient(s.Opts.Data.Database.MySQL)
		if err != nil {
			return fmt.Errorf("init mysql: %w", err)
		}
		s.DB = db
	default:
		return fmt.Errorf("unsupported database type: %s", s.Opts.Data.Database.Type)
	}

	// AutoMigrate by default (as requested).
	if err := model.AutoMigrate(s.DB); err != nil {
		return fmt.Errorf("auto migrate: %w", err)
	}
	s.closeFns = append(s.closeFns, func(ctx context.Context) error {
		sqlDB, err := s.DB.DB()
		if err != nil {
			return err
		}
		return sqlDB.Close()
	})

	// Cache: prefer redis if configured, otherwise in-memory cache.
	if s.Opts.Data.Redis != nil && strings.TrimSpace(s.Opts.Data.Redis.Host) != "" {
		c, err := cache.NewRedisClient(s.Opts.Data.Redis, s.StopCh)
		if err != nil {
			return fmt.Errorf("init redis: %w", err)
		}
		s.Cache = c
	} else {
		s.Cache = cache.NewSimpleCache()
	}

	// AMap SDK (used by web server for address -> location, etc.).
	if s.Opts.Amap == nil {
		return fmt.Errorf("amap options is nil")
	}
	amapKey := strings.TrimSpace(s.Opts.Amap.Key)
	amapBaseURL := strings.TrimSpace(s.Opts.Amap.BaseURL)
	amapTimeout := time.Duration(s.Opts.Amap.TimeoutSeconds) * time.Second
	s.Amap = amap.NewClient(
		amapKey,
		amap.WithBaseURL(amapBaseURL),
		amap.WithTimeout(amapTimeout),
	)

	// LLM (Eino) init: build ChatModel config.
	if s.Opts.LLM == nil {
		return fmt.Errorf("llm options is nil")
	}
	cli, err := llm.NewClient(s.Opts.LLM)
	if err != nil {
		return fmt.Errorf("init llm: %w", err)
	}
	s.LLM = cli

	// Optional MCP: start amap MCP server subprocess and connect via stdio.
	if s.Opts.MCP.Servers.Amap.Enabled {
		if err := s.initAmapMCP(ctx); err != nil {
			return err
		}
	}

	return nil
}

func (s *ConsoleServer) initAmapMCP(ctx context.Context) error {
	so := s.Opts.MCP.Servers.Amap
	if strings.TrimSpace(so.Command) == "" {
		return fmt.Errorf("mcp.servers.amap.enabled=true but command is empty")
	}

	env := make(map[string]string, len(so.Env)+8)
	for k, v := range so.Env {
		env[k] = v
	}

	// Pass the resolved AMap settings into the subprocess via unified env keys.
	if strings.TrimSpace(s.Opts.Amap.Key) != "" {
		env["FOOD_AGENT_AMAP_KEY"] = strings.TrimSpace(s.Opts.Amap.Key)
	}
	if strings.TrimSpace(s.Opts.Amap.BaseURL) != "" {
		env["FOOD_AGENT_AMAP_BASE_URL"] = strings.TrimSpace(s.Opts.Amap.BaseURL)
	}
	if s.Opts.Amap.TimeoutSeconds > 0 {
		env["FOOD_AGENT_AMAP_TIMEOUT_SECONDS"] = strconv.Itoa(s.Opts.Amap.TimeoutSeconds)
	}
	if strings.TrimSpace(s.Opts.ConfigFile) != "" {
		env["FOOD_AGENT_CONFIG"] = strings.TrimSpace(s.Opts.ConfigFile)
	}

	c, err := mcp.NewStdioClient(ctx, mcp.StdioClientOptions{
		Command: strings.TrimSpace(so.Command),
		Args:    so.Args,
		Env:     env,
	})
	if err != nil {
		return fmt.Errorf("init amap mcp client: %w", err)
	}
	s.AmapMCP = c
	s.closeFns = append(s.closeFns, func(ctx context.Context) error { return c.Close() })
	return nil
}

func (s *ConsoleServer) closeAll(ctx context.Context) {
	for i := len(s.closeFns) - 1; i >= 0; i-- {
		if s.closeFns[i] == nil {
			continue
		}
		if err := s.closeFns[i](ctx); err != nil {
			zap.L().Warn("close component failed", zap.Error(err))
		}
	}

	// Ensure the stop channel (used by redis client) is closed on error paths as well.
	// WARNING: don't call RequestShutdown if stopCh is already closed, otherwise the
	// signal handler treats it as a "second signal" and may os.Exit(1).
	if s.StopCh != nil {
		select {
		case <-s.StopCh:
			// already closed
		default:
			server.RequestShutdown()
		}
	}
}
