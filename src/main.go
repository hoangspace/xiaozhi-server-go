// @title Xiaozhi Server API Documentation
// @version 1.0
// @description Xiaozhi server, including OTA and Vision interfaces
// @host localhost:8080
// @BasePath /api
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"xiaozhi-server-go/src/configs"
	"xiaozhi-server-go/src/configs/database"
	cfg "xiaozhi-server-go/src/configs/server"
	"xiaozhi-server-go/src/core/auth"
	"xiaozhi-server-go/src/core/auth/store"
	"xiaozhi-server-go/src/core/pool"
	"xiaozhi-server-go/src/core/transport"
	"xiaozhi-server-go/src/core/transport/websocket"
	"xiaozhi-server-go/src/core/utils"
	_ "xiaozhi-server-go/src/docs"
	"xiaozhi-server-go/src/ota"
	"xiaozhi-server-go/src/task"
	"xiaozhi-server-go/src/vision"

	"github.com/gin-contrib/cors"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	// Import all providers to ensure init functions are called
	_ "xiaozhi-server-go/src/core/providers/asr/deepgram"
	_ "xiaozhi-server-go/src/core/providers/asr/doubao"
	_ "xiaozhi-server-go/src/core/providers/asr/gosherpa"
	_ "xiaozhi-server-go/src/core/providers/asr/stepfun"
	_ "xiaozhi-server-go/src/core/providers/llm/coze"
	_ "xiaozhi-server-go/src/core/providers/llm/ollama"
	_ "xiaozhi-server-go/src/core/providers/llm/openai"
	_ "xiaozhi-server-go/src/core/providers/tts/deepgram"
	_ "xiaozhi-server-go/src/core/providers/tts/doubao"
	_ "xiaozhi-server-go/src/core/providers/tts/edge"
	_ "xiaozhi-server-go/src/core/providers/tts/gosherpa"
	_ "xiaozhi-server-go/src/core/providers/vlllm/ollama"
	_ "xiaozhi-server-go/src/core/providers/vlllm/openai"

	"github.com/gin-gonic/gin"
	"golang.org/x/sync/errgroup"
)

func LoadConfigAndLogger() (*configs.Config, *utils.Logger, error) {
	// Initialize database connection
	_, _, err := database.InitDB()
	if err != nil {
		fmt.Println("Database connection failed: %v", err)
	}
	// Load configuration, default using .config.yaml
	config, configPath, err := configs.LoadConfig(database.GetServerConfigDB())
	if err != nil {
		return nil, nil, err
	}

	// Initialize logging system
	logger, err := utils.NewLogger((*utils.LogCfg)(&config.Log))
	if err != nil {
		return nil, nil, err
	}
	logger.Info("Logging system initialized successfully, config file path: %s", configPath)
	utils.DefaultLogger = logger

	database.SetLogger(logger)

	return config, logger, nil
}

// initAuthManager initializes the authentication manager
func initAuthManager(config *configs.Config, logger *utils.Logger) (*auth.AuthManager, error) {
	if !config.Server.Auth.Enabled {
		logger.Info("Authentication feature not enabled")
		return nil, nil
	}

	// Create storage configuration
	storeConfig := &store.StoreConfig{
		Type:     config.Server.Auth.Store.Type,
		ExpiryHr: config.Server.Auth.Store.Expiry,
		Config:   make(map[string]interface{}),
	}

	// Create authentication manager
	authManager, err := auth.NewAuthManager(storeConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize authentication manager: %v", err)
	}

	return authManager, nil
}

func StartTransportServer(
	config *configs.Config,
	logger *utils.Logger,
	authManager *auth.AuthManager,
	g *errgroup.Group,
	groupCtx context.Context,
) (*transport.TransportManager, error) {
	// Initialize resource pool manager
	poolManager, err := pool.NewPoolManager(config, logger)
	if err != nil {
		logger.Error(fmt.Sprintf("failed to initialize resource pool manager: %v", err))
		return nil, fmt.Errorf("failed to initialize resource pool manager: %v", err)
	}

	// Initialize task manager
	taskMgr := task.NewTaskManager(task.ResourceConfig{
		MaxWorkers:        12,
		MaxTasksPerClient: 20,
	})
	taskMgr.Start()

	// Create transport manager
	transportManager := transport.NewTransportManager(config, logger)

	// Create connection handler factory
	handlerFactory := transport.NewDefaultConnectionHandlerFactory(
		config,
		poolManager,
		taskMgr,
		logger,
	)

	// Enable different transport layers based on configuration
	enabledTransports := make([]string, 0)

	// Check WebSocket transport layer configuration
	if config.Transport.WebSocket.Enabled {
		wsTransport := websocket.NewWebSocketTransport(config, logger)
		wsTransport.SetConnectionHandler(handlerFactory)
		transportManager.RegisterTransport("websocket", wsTransport)
		enabledTransports = append(enabledTransports, "WebSocket")
		logger.Debug("WebSocket transport layer registered")
	}

	if len(enabledTransports) == 0 {
		return nil, fmt.Errorf("no transport layer enabled")
	}

	logger.Info("Enabled transport layers: %v", enabledTransports)

	// Start transport layer service
	g.Go(func() error {
		// Listen for shutdown signals
		go func() {
			<-groupCtx.Done()
			logger.Info("Received shutdown signal, starting to close all transport layers...")
			if err := transportManager.StopAll(); err != nil {
				logger.Error("Failed to close transport layers: %v", err)
			} else {
				logger.Info("All transport layers gracefully closed")
			}
		}()

		// Use transport manager to start service
		if err := transportManager.StartAll(groupCtx); err != nil {
			if groupCtx.Err() != nil {
				return nil // Normal shutdown
			}
			logger.Error("Transport layer failed to run: %v", err)
			return err
		}
		return nil
	})

	logger.Debug("Transport layer service started successfully")
	return transportManager, nil
}

func StartHttpServer(config *configs.Config, logger *utils.Logger, g *errgroup.Group, groupCtx context.Context) (*http.Server, error) {
	// Initialize Gin engine
	if config.Log.LogLevel == "debug" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
	router := gin.Default()
	router.SetTrustedProxies([]string{"0.0.0.0"})

	// Configure global CORS middleware
	corsConfig := cors.Config{
		AllowOrigins: []string{"*"}, // Production environment should specify specific domains
		AllowMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
		AllowHeaders: []string{
			"Origin",
			"Content-Type",
			"Accept",
			"Authorization",
			"X-Requested-With",
			"Cache-Control",
			"X-File-Name",
			"client-id",
			"device-id",
		},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}
	// Apply global CORS middleware
	router.Use(cors.New(corsConfig))

	logger.Debug("Global CORS middleware configured, supports OPTIONS preflight requests")
	// All API routes mounted under /api prefix
	apiGroup := router.Group("/api")
	// Start OTA service
	otaService := ota.NewDefaultOTAService(config.Web.Websocket)
	if err := otaService.Start(groupCtx, router, apiGroup); err != nil {
		logger.Error("OTA service startup failed", err)
		return nil, err
	}

	// Start Vision service
	visionService, err := vision.NewDefaultVisionService(config, logger)
	if err != nil {
		logger.Error("Vision service initialization failed: %v", err)
		// return nil, err
	}
	if visionService != nil {
		if err := visionService.Start(groupCtx, router, apiGroup); err != nil {
			logger.Error("Vision service startup failed: %v", err)
			// return nil, err
		}
	}

	cfgServer, err := cfg.NewDefaultCfgService(config, logger)
	if err != nil {
		logger.Error("Configuration service initialization failed: %v", err)
		return nil, err
	}
	if err := cfgServer.Start(groupCtx, router, apiGroup); err != nil {
		logger.Error("Configuration service startup failed", err)
		return nil, err
	}

	// HTTP Server (supports graceful shutdown)
	httpServer := &http.Server{
		Addr:    ":" + strconv.Itoa(config.Web.Port),
		Handler: router,
	}

	// Register Swagger documentation routes
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	g.Go(func() error {
		logger.Info(fmt.Sprintf("Gin service started, access address: http://0.0.0.0:%d", config.Web.Port))

		// Listen for shutdown signals in a separate goroutine
		go func() {
			<-groupCtx.Done()
			logger.Info("Received shutdown signal, starting to close HTTP service...")

			// Create shutdown timeout context
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			if err := httpServer.Shutdown(shutdownCtx); err != nil {
				logger.Error("HTTP service shutdown failed: %v", err)
			} else {
				logger.Info("HTTP service gracefully closed")
			}
		}()

		// ListenAndServe returns ErrServerClosed when it's a normal shutdown
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("HTTP service startup failed: %v", err)
			return err
		}
		return nil
	})

	return httpServer, nil
}

func GracefulShutdown(cancel context.CancelFunc, logger *utils.Logger, g *errgroup.Group) {
	// Listen for system signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	// Wait for signal
	sig := <-sigChan
	logger.Info("Received system signal: %v, starting graceful service shutdown", sig)

	// Cancel context to notify all services to start shutting down
	cancel()

	// Wait for all services to close, set timeout protection
	done := make(chan error, 1)
	go func() {
		done <- g.Wait()
	}()

	select {
	case err := <-done:
		if err != nil {
			logger.Error("Error occurred during service shutdown: %v", err)
			os.Exit(1)
		}
		logger.Info("All services gracefully closed")
	case <-time.After(15 * time.Second):
		logger.Error("Service shutdown timeout, forcing exit")
		os.Exit(1)
	}
}

func startServices(
	config *configs.Config,
	logger *utils.Logger,
	authManager *auth.AuthManager,
	g *errgroup.Group,
	groupCtx context.Context,
) error {
	// Start transport layer service
	if _, err := StartTransportServer(config, logger, authManager, g, groupCtx); err != nil {
		return fmt.Errorf("failed to start transport layer service: %w", err)
	}

	// Start HTTP service
	if _, err := StartHttpServer(config, logger, g, groupCtx); err != nil {
		return fmt.Errorf("failed to start HTTP service: %w", err)
	}

	return nil
}

func main() {
	// Load configuration and initialize logging system
	config, logger, err := LoadConfigAndLogger()
	if err != nil {
		fmt.Println("Failed to load configuration or initialize logging system:", err)
		os.Exit(1)
	}

	// Initialize authentication manager
	authManager, err := initAuthManager(config, logger)
	if err != nil {
		logger.Error("Failed to initialize authentication manager:", err)
		os.Exit(1)
	}

	// Create cancellable context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Use errgroup to manage two services
	g, groupCtx := errgroup.WithContext(ctx)

	// Start all services
	if err := startServices(config, logger, authManager, g, groupCtx); err != nil {
		logger.Error("Failed to start services: %v", err)
		cancel()
		os.Exit(1)
	}

	// Start graceful shutdown handling
	GracefulShutdown(cancel, logger, g)

	// Close authentication manager
	if authManager != nil {
		authManager.Close()
	}

	logger.Info("Program exited successfully")
	logger.Close()
}
