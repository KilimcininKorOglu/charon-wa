package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"charon/config"
	"charon/database"
	"charon/internal/handler"
	warmingHandler "charon/internal/handler/warming"
	"charon/internal/helper"
	customMiddleware "charon/internal/middleware"
	"charon/internal/model"
	"charon/internal/service"
	"charon/internal/worker"

	log2 "github.com/labstack/gommon/log"

	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"golang.org/x/time/rate"

	"charon/internal/ws"
)

// Populated via -ldflags at build time (see Makefile). Fallbacks read VERSION at startup.
var (
	version   = ""
	commit    = ""
	buildDate = ""
)

func resolveVersion() string {
	if version != "" {
		return version
	}
	if data, err := os.ReadFile("VERSION"); err == nil {
		v := strings.TrimSpace(string(data))
		if v != "" {
			return v
		}
	}
	return "dev"
}

func main() {

	// Load .env (ignore error if file doesn't exist, e.g. in production)
	_ = godotenv.Load()

	// Database: whatsmeow
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL is not set")
	}
	database.InitWhatsmeow(dbURL)

	// Database: custom app DB
	appDbURL := os.Getenv("APP_DATABASE_URL")
	if appDbURL == "" {
		log.Fatal("APP_DATABASE_URL is not set")
	}
	database.InitAppDB(appDbURL)

	// Initialize separate Outbox DB if set
	outboxDbURL := os.Getenv("OUTBOX_DATABASE_URL")
	database.InitOutboxDB(outboxDbURL)

	// feature flags (WEBHOOK & WEBSOCKET)
	wsEnv := strings.ToLower(os.Getenv("CHARON_ENABLE_WEBSOCKET_INCOMING_MSG"))
	webhookEnv := strings.ToLower(os.Getenv("CHARON_ENABLE_WEBHOOK"))

	config.EnableWebsocketIncomingMessage = (wsEnv == "true")
	config.EnableWebhook = (webhookEnv == "true")

	autoReplyEnv := os.Getenv("WARMING_AUTO_REPLY_ENABLED")
	config.WarmingAutoReplyEnabled = (autoReplyEnv == "true")

	cooldownStr := os.Getenv("WARMING_AUTO_REPLY_COOLDOWN")
	if cooldownStr != "" {
		if cooldown, err := strconv.Atoi(cooldownStr); err == nil && cooldown > 0 {
			config.WarmingAutoReplyCooldown = cooldown
		} else {
			config.WarmingAutoReplyCooldown = 60 // default 60 seconds
		}
	} else {
		config.WarmingAutoReplyCooldown = 60 // default 60 seconds
	}

	// Typing Delay Configuration (read once at startup)
	if v, err := strconv.Atoi(os.Getenv("CHARON_TYPING_DELAY_MIN")); err == nil && v > 0 {
		config.TypingDelayMin = v
	}
	if v, err := strconv.Atoi(os.Getenv("CHARON_TYPING_DELAY_MAX")); err == nil && v > 0 {
		config.TypingDelayMax = v
	}

	// Phone Number Configuration
	config.PhoneCountryCode = strings.TrimSpace(os.Getenv("PHONE_COUNTRY_CODE"))

	// AI Configuration
	config.AIEnabled = os.Getenv("AI_ENABLED") == "true"
	config.AIDefaultProvider = os.Getenv("AI_DEFAULT_PROVIDER")
	if config.AIDefaultProvider == "" {
		config.AIDefaultProvider = "gemini" // default to free Gemini
	}

	config.GeminiAPIKey = os.Getenv("GEMINI_API_KEY")
	config.GeminiDefaultModel = os.Getenv("GEMINI_DEFAULT_MODEL")
	if config.GeminiDefaultModel == "" {
		config.GeminiDefaultModel = "gemini-1.5-flash"
	}

	if histLimit := os.Getenv("AI_CONVERSATION_HISTORY_LIMIT"); histLimit != "" {
		if limit, err := strconv.Atoi(histLimit); err == nil && limit > 0 {
			config.AIConversationHistoryLimit = limit
		} else {
			config.AIConversationHistoryLimit = 10
		}
	} else {
		config.AIConversationHistoryLimit = 10
	}

	if temp := os.Getenv("AI_DEFAULT_TEMPERATURE"); temp != "" {
		if t, err := strconv.ParseFloat(temp, 64); err == nil && t >= 0 && t <= 1 {
			config.AIDefaultTemperature = t
		} else {
			config.AIDefaultTemperature = 0.7
		}
	} else {
		config.AIDefaultTemperature = 0.7
	}

	if maxTokens := os.Getenv("AI_DEFAULT_MAX_TOKENS"); maxTokens != "" {
		if tokens, err := strconv.Atoi(maxTokens); err == nil && tokens > 0 {
			config.AIDefaultMaxTokens = tokens
		} else {
			config.AIDefaultMaxTokens = 150
		}
	} else {
		config.AIDefaultMaxTokens = 150
	}

	log.Printf("feature flags -> websocket_incoming_msg: %v, webhook: %v, warming_auto_reply: %v, ai_enabled: %v",
		config.EnableWebsocketIncomingMessage, config.EnableWebhook, config.WarmingAutoReplyEnabled, config.AIEnabled)

	// Initialize session-based authentication
	service.InitSessionConfig()

	// **************************
	// Main process
	//***************************

	// 6. Auto-Migration (Ensure database schema is up to date)
	log.Println("Ensuring database schema...")
	helper.InitCustomSchema()

	// 7. Seed default admin user if none exists
	if err := model.SeedAdminUser(); err != nil {
		log.Printf("Warning: Failed to seed admin user: %v", err)
	}

	// Load all existing devices from database
	log.Println("Loading existing devices...")
	err := service.LoadAllDevices()
	if err != nil {
		log.Printf("Warning: Failed to load devices: %v", err)
	}

	// Initialize WebSocket Hub
	hub := ws.NewHub()
	go hub.Run()

	service.Realtime = hub

	// Configure WebSocket broadcast filtering by user instance access
	ws.SetInstanceAccessChecker(func(userID int, instanceID string) bool {
		hasAccess, err := model.HasInstanceAccess(userID, instanceID)
		if err != nil {
			return false
		}
		return hasAccess
	})

	// Setup Echo
	e := echo.New()

	// Configure IP extraction for rate limiting
	if os.Getenv("BEHIND_PROXY") == "true" {
		e.IPExtractor = echo.ExtractIPFromRealIPHeader()
	} else {
		e.IPExtractor = echo.ExtractIPDirect()
	}

	e.Use(middleware.RequestID())
	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Format: `{"time":"${time_rfc3339_nano}","request_id":"${id}","remote_ip":"${remote_ip}",` +
			`"method":"${method}","uri":"${uri}","status":${status},` +
			`"latency_ms":${latency},"bytes_in":${bytes_in},"bytes_out":${bytes_out}}` + "\n",
	}))
	e.Use(middleware.RecoverWithConfig(middleware.RecoverConfig{
		StackSize:         4 << 10,
		DisableStackAll:   false,
		DisablePrintStack: false,
		LogLevel:          log2.ERROR,
		LogErrorFunc: func(c echo.Context, err error, stack []byte) error {
			requestID := c.Response().Header().Get(echo.HeaderXRequestID)
			if requestID == "" {
				requestID = c.Request().Header.Get(echo.HeaderXRequestID)
			}
			userID, _ := c.Get("user_id").(int64)
			log.Printf("PANIC: request_id=%s method=%s uri=%s user_id=%d err=%v\n%s",
				requestID, c.Request().Method, c.Request().RequestURI, userID, err, stack)
			return err
		},
	}))
	e.Use(middleware.BodyLimit("100M"))
	e.Use(middleware.SecureWithConfig(middleware.SecureConfig{
		XFrameOptions:         "DENY",
		ContentSecurityPolicy: "frame-ancestors 'none'",
		ContentTypeNosniff:    "nosniff",
		HSTSMaxAge:            31536000,
	}))

	// CORS allowed origins — validated strictly because AllowCredentials=true.
	originsEnv := os.Getenv("CORS_ALLOW_ORIGINS")
	if originsEnv == "" {
		log.Fatal("CORS_ALLOW_ORIGINS must be set (comma-separated origins, e.g. http://localhost:5173)")
	}
	rawOrigins := strings.Split(originsEnv, ",")
	allowOrigins := make([]string, 0, len(rawOrigins))
	for _, o := range rawOrigins {
		origin := strings.TrimSpace(o)
		if origin == "" {
			continue
		}
		if origin == "*" {
			log.Fatalf("CORS_ALLOW_ORIGINS rejects wildcard '*' when cookies/credentials are allowed")
		}
		if !strings.HasPrefix(origin, "http://") && !strings.HasPrefix(origin, "https://") {
			log.Fatalf("CORS_ALLOW_ORIGINS entry %q must include scheme (http:// or https://)", origin)
		}
		allowOrigins = append(allowOrigins, origin)
	}
	if len(allowOrigins) == 0 {
		log.Fatal("CORS_ALLOW_ORIGINS yielded no valid origins after validation")
	}
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: allowOrigins,
		AllowMethods: []string{
			echo.GET,
			echo.POST,
			echo.PUT,
			echo.PATCH,
			echo.DELETE,
			echo.OPTIONS,
		},
		AllowHeaders: []string{
			echo.HeaderOrigin,
			echo.HeaderContentType,
			echo.HeaderAccept,
			echo.HeaderXRequestedWith,
			echo.HeaderAuthorization,
			"X-API-Key",
		},
		AllowCredentials: true,
	}))
	e.OPTIONS("/*", func(c echo.Context) error {
		return c.NoContent(http.StatusOK)
	})

	// Rate limiter configuration from env
	rateLimit := helper.GetEnvAsInt("RATE_LIMIT_PER_SECOND", 10)
	rateBurst := helper.GetEnvAsInt("RATE_LIMIT_BURST", 10)
	rateWindow := helper.GetEnvAsInt("RATE_LIMIT_WINDOW_MINUTES", 3)

	e.Use(middleware.RateLimiterWithConfig(middleware.RateLimiterConfig{
		Store: middleware.NewRateLimiterMemoryStoreWithConfig(
			middleware.RateLimiterMemoryStoreConfig{
				Rate:      rate.Limit(rateLimit),
				Burst:     rateBurst,
				ExpiresIn: time.Duration(rateWindow) * time.Minute,
			},
		),
	}))

	// =====================================================
	// PUBLIC ROUTES (No authentication required)
	// =====================================================

	// Stricter rate limit for auth endpoints (5 requests per minute per IP)
	authLimiter := middleware.RateLimiterWithConfig(middleware.RateLimiterConfig{
		Store: middleware.NewRateLimiterMemoryStoreWithConfig(
			middleware.RateLimiterMemoryStoreConfig{
				Rate:      rate.Limit(5.0 / 60.0),
				Burst:     5,
				ExpiresIn: 5 * time.Minute,
			},
		),
	})

	// New user authentication endpoints
	e.POST("/login", handler.LoginUser, authLimiter)
	e.POST("/logout", handler.LogoutUser)

	// Static file serving for uploaded files — with security headers to prevent MIME sniffing
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if strings.HasPrefix(c.Request().URL.Path, "/uploads/") {
				c.Response().Header().Set("X-Content-Type-Options", "nosniff")
				c.Response().Header().Set("Content-Security-Policy", "default-src 'none'")
				// Force SVG files to download rather than render inline — SVG can
				// execute embedded scripts when opened as a top-level document.
				if strings.HasSuffix(strings.ToLower(c.Request().URL.Path), ".svg") {
					c.Response().Header().Set("Content-Disposition", "attachment")
				}
			}
			return next(c)
		}
	})
	e.Static("/uploads", "./uploads")

	// WebSocket and health check
	e.GET("/ws", handler.WebSocketHandler(hub)) // WebSocket listener (Gorilla)
	e.GET("/", func(c echo.Context) error { // Health check
		ctx, cancel := context.WithTimeout(c.Request().Context(), 3*time.Second)
		defer cancel()

		checks := map[string]string{}
		allHealthy := true

		if database.AppDB != nil {
			if err := database.AppDB.PingContext(ctx); err != nil {
				checks["app_db"] = "unhealthy: " + err.Error()
				allHealthy = false
			} else {
				checks["app_db"] = "ok"
			}
		}
		if database.OutboxDB != nil && database.OutboxDB != database.AppDB {
			if err := database.OutboxDB.PingContext(ctx); err != nil {
				checks["outbox_db"] = "unhealthy: " + err.Error()
				allHealthy = false
			} else {
				checks["outbox_db"] = "ok"
			}
		}
		if database.WhatsmeowDB != nil {
			if err := database.WhatsmeowDB.PingContext(ctx); err != nil {
				checks["whatsmeow_db"] = "unhealthy: " + err.Error()
				allHealthy = false
			} else {
				checks["whatsmeow_db"] = "ok"
			}
		}

		status := 200
		if !allHealthy {
			status = 503
		}
		return c.JSON(status, map[string]interface{}{
			"success":    allHealthy,
			"message":    "WhatsApp API is running",
			"version":    resolveVersion(),
			"commit":     commit,
			"build_date": buildDate,
			"checks":     checks,
		})
	})

	// Route group accepting session cookie (UI) or X-API-Key (outbox worker, external integrations)
	api := e.Group("/api", customMiddleware.SessionOrAPIKeyMiddleware())

	e.HTTPErrorHandler = func(err error, c echo.Context) {
		code := http.StatusInternalServerError
		message := "Internal Server Error"

		if he, ok := err.(*echo.HTTPError); ok {
			code = he.Code
			message = fmt.Sprintf("%v", he.Message)
		}
		// Custom response format
		response := map[string]interface{}{
			"success": false,
			"error":   message,
		}
		// Custom message for specific errors
		switch code {
		case http.StatusUnauthorized:
			response["message"] = "Authentication required. Please login first."
		case http.StatusMethodNotAllowed:
			response["message"] = "Method not allowed for this endpoint"
		case http.StatusNotFound:
			response["message"] = "Endpoint not found"
		}

		c.JSON(code, response)
	}

	// =====================================================
	// USER PROFILE ROUTES (session required)
	// =====================================================
	api.GET("/me", handler.GetCurrentUser)
	api.PUT("/me", handler.UpdateCurrentUser)
	api.PUT("/me/password", handler.ChangePassword)

	// File upload
	api.POST("/me/avatar", handler.UploadAvatar)

	// =====================================================
	// SYSTEM IDENTITY ROUTES (Admin Only)
	// =====================================================
	api.GET("/system/identity", handler.GetSystemIdentityHandler)                                 // Publicly accessible via API token
	api.POST("/system/identity", handler.UpdateSystemIdentityFull, customMiddleware.RequireAdmin) // Unified: Text + Logos (Admin Only)

	// =====================================================
	// ADMIN ROUTES (Admin Only)
	// =====================================================
	admin := api.Group("/admin", customMiddleware.RequireAdmin)
	admin.POST("/users", handler.AdminCreateUser)
	admin.GET("/users", handler.ListUsers)
	admin.GET("/users/:id", handler.GetUser)
	admin.PATCH("/users/:id", handler.UpdateUser)
	admin.DELETE("/users/:id", handler.DeleteUser)
	admin.GET("/users/:id/instances", handler.GetUserInstances)
	admin.POST("/users/:id/instances", handler.AssignInstance)
	admin.DELETE("/users/:id/instances/:instanceId", handler.RevokeInstance)
	admin.GET("/stats", handler.GetStats)

	// =====================================================
	// FILE MANAGER ROUTES (admin only — listing exposes all tenant directories)
	// =====================================================
	api.GET("/files", handler.ListFiles, customMiddleware.RequireAdmin)
	api.DELETE("/files", handler.DeleteFile, customMiddleware.RequireAdmin)

	// =====================================================
	// WHATSAPP INSTANCE ROUTES (session required)
	// =====================================================

	// Routes
	api.POST("/login", handler.Login)
	api.GET("/qr/:instanceId", handler.GetQR, customMiddleware.RequireInstanceAccess())
	api.GET("/status/:instanceId", handler.GetStatus, customMiddleware.RequireInstanceAccess())
	api.POST("/logout/:instanceId", handler.Logout, customMiddleware.RequireInstanceAccess())
	api.DELETE("/instances/:instanceId", handler.DeleteInstance, customMiddleware.RequireInstanceAccess())
	api.DELETE("/qr-cancel/:instanceId", handler.CancelQR, customMiddleware.RequireInstanceAccess())

	// Get all instances (requires authentication, filtered by user role)
	api.GET("/instances", handler.GetAllInstances) // session already applied to 'api' group
	// Update instance fields (used, description, circle)
	api.PATCH("/instances/:instanceId", handler.UpdateInstanceFields, customMiddleware.RequireInstanceAccess())

	// Message routes by instance id
	api.POST("/send/:instanceId", handler.SendMessage, customMiddleware.RequireInstanceAccess())
	api.POST("/check/:instanceId", handler.CheckNumber, customMiddleware.RequireInstanceAccess())

	// Contact routes
	api.GET("/contacts/:instanceId", handler.GetContactList, customMiddleware.RequireInstanceAccess())
	api.GET("/contacts/:instanceId/export", handler.ExportContacts, customMiddleware.RequireInstanceAccess())
	api.GET("/contacts/:instanceId/:jid", handler.GetContactDetail, customMiddleware.RequireInstanceAccess())
	api.GET("/contacts/:instanceId/:jid/mutual-groups", handler.GetMutualGroups, customMiddleware.RequireInstanceAccess())

	// Media routes by instance id
	api.POST("/send/:instanceId/media", handler.SendMediaFile, customMiddleware.RequireInstanceAccess())
	api.POST("/send/:instanceId/media-url", handler.SendMediaURL, customMiddleware.RequireInstanceAccess())

	// Message by phone number (requires phone number access)
	api.POST("/by-number/:phoneNumber", handler.SendMessageByNumber, customMiddleware.RequirePhoneNumberAccess())
	api.POST("/by-number/:phoneNumber/media-url", handler.SendMediaURLByNumber, customMiddleware.RequirePhoneNumberAccess())
	api.POST("/by-number/:phoneNumber/media-file", handler.SendMediaFileByNumber, customMiddleware.RequirePhoneNumberAccess())

	// Group routes
	api.GET("/groups/:instanceId", handler.GetGroups, customMiddleware.RequireInstanceAccess())
	api.POST("/send-group/:instanceId", handler.SendGroupMessage, customMiddleware.RequireInstanceAccess())
	api.POST("/send-group/:instanceId/media", handler.SendGroupMedia, customMiddleware.RequireInstanceAccess())
	api.POST("/send-group/:instanceId/media-url", handler.SendGroupMediaURL, customMiddleware.RequireInstanceAccess())

	// Group by phone number (requires phone number access)
	api.GET("/groups/by-number/:phoneNumber", handler.GetGroupsByNumber, customMiddleware.RequirePhoneNumberAccess())
	api.POST("/send-group/by-number/:phoneNumber", handler.SendGroupMessageByNumber, customMiddleware.RequirePhoneNumberAccess())
	api.POST("/send-group/by-number/:phoneNumber/media", handler.SendGroupMediaByNumber, customMiddleware.RequirePhoneNumberAccess())
	api.POST("/send-group/by-number/:phoneNumber/media-url", handler.SendGroupMediaURLByNumber, customMiddleware.RequirePhoneNumberAccess())

	// Get account info
	api.GET("/info-device/:instanceId", handler.GetDeviceInfo, customMiddleware.RequireInstanceAccess())

	//----------------------------
	// WEBSOCKET AND WEBHOOK
	//----------------------------
	// Listen for incoming messages via WebSocket
	api.GET("/listen/:instanceId", handler.ListenMessages(hub), customMiddleware.RequireInstanceAccess())
	// Webhook
	api.POST("/instances/:instanceId/webhook-setconfig", handler.SetWebhookConfig, customMiddleware.RequireInstanceAccess())

	//----------------------------
	// WORKER BLAST OUTBOX
	//----------------------------
	blastOutbox := api.Group("/blast-outbox")
	blastOutbox.GET("/configs", handler.GetWorkerConfigs)
	blastOutbox.POST("/configs", handler.CreateWorkerConfig, customMiddleware.RequireRole("admin", "user"))
	blastOutbox.GET("/configs/:id", handler.GetWorkerConfig)
	blastOutbox.PUT("/configs/:id", handler.UpdateWorkerConfig, customMiddleware.RequireRole("admin", "user"))
	blastOutbox.DELETE("/configs/:id", handler.DeleteWorkerConfig, customMiddleware.RequireRole("admin", "user"))
	blastOutbox.POST("/configs/:id/toggle", handler.ToggleWorkerConfig, customMiddleware.RequireRole("admin", "user"))

	// Helper endpoints for frontend
	blastOutbox.GET("/available-circles", handler.GetAvailableCircles)
	blastOutbox.GET("/available-applications", handler.GetAvailableApplications)

	//----------------------------
	// API KEY MANAGEMENT (session protected)
	//----------------------------
	apiKeys := api.Group("/api-keys")
	apiKeys.POST("", handler.CreateAPIKey, customMiddleware.RequireRole("admin", "user"))
	apiKeys.GET("", handler.ListAPIKeys)
	apiKeys.DELETE("/:id", handler.DeleteAPIKey, customMiddleware.RequireRole("admin", "user"))

	//----------------------------
	// OUTBOX API (API Key protected)
	//----------------------------
	outbox := e.Group("/api/outbox", customMiddleware.APIKeyAuthMiddleware())
	outbox.POST("/enqueue", handler.EnqueueOutbox)
	outbox.POST("/enqueue-batch", handler.EnqueueOutboxBatch)
	outbox.GET("/status/:id", handler.GetOutboxStatus)
	outbox.GET("/messages", handler.ListOutboxMessages)

	// Outbox messages also accessible via session (for admin UI)
	api.GET("/outbox/messages", handler.ListOutboxMessages)
	api.GET("/outbox/status/:id", handler.GetOutboxStatus)

	//----------------------------
	// WARMING SYSTEM
	//----------------------------
	warming := api.Group("/warming")
	warmingWrite := customMiddleware.RequireRole("admin", "user")
	warming.POST("/scripts", warmingHandler.CreateWarmingScript, warmingWrite)
	warming.GET("/scripts", warmingHandler.GetAllWarmingScripts)
	warming.GET("/scripts/:id", warmingHandler.GetWarmingScriptByID)
	warming.PUT("/scripts/:id", warmingHandler.UpdateWarmingScript, warmingWrite)
	warming.DELETE("/scripts/:id", warmingHandler.DeleteWarmingScript, warmingWrite)

	// Script Lines (Dialog/Script)
	// IMPORTANT: Specific routes must come BEFORE parameterized routes to avoid conflicts
	warming.POST("/scripts/:scriptId/lines/generate", warmingHandler.GenerateWarmingScriptLines, warmingWrite)
	warming.PUT("/scripts/:scriptId/lines/reorder", warmingHandler.ReorderWarmingScriptLines, warmingWrite)
	warming.POST("/scripts/:scriptId/lines", warmingHandler.CreateWarmingScriptLine, warmingWrite)
	warming.GET("/scripts/:scriptId/lines", warmingHandler.GetAllWarmingScriptLines)
	warming.GET("/scripts/:scriptId/lines/:id", warmingHandler.GetWarmingScriptLineByID)
	warming.PUT("/scripts/:scriptId/lines/:id", warmingHandler.UpdateWarmingScriptLine, warmingWrite)
	warming.DELETE("/scripts/:scriptId/lines/:id", warmingHandler.DeleteWarmingScriptLine, warmingWrite)

	// Templates (Manage Conversation Templates)
	warming.POST("/templates", warmingHandler.CreateWarmingTemplate, warmingWrite)
	warming.GET("/templates", warmingHandler.GetAllWarmingTemplates)
	warming.GET("/templates/:id", warmingHandler.GetWarmingTemplateByID)
	warming.PUT("/templates/:id", warmingHandler.UpdateWarmingTemplate, warmingWrite)
	warming.DELETE("/templates/:id", warmingHandler.DeleteWarmingTemplate, warmingWrite)

	// Rooms (Execution Management)
	warming.POST("/rooms", warmingHandler.CreateWarmingRoom, warmingWrite)
	warming.GET("/rooms", warmingHandler.GetAllWarmingRooms)
	warming.GET("/rooms/:id", warmingHandler.GetWarmingRoomByID)
	warming.PUT("/rooms/:id", warmingHandler.UpdateWarmingRoom, warmingWrite)
	warming.DELETE("/rooms/:id", warmingHandler.DeleteWarmingRoom, warmingWrite)
	warming.PATCH("/rooms/:id/status", warmingHandler.UpdateRoomStatus, warmingWrite)
	warming.POST("/rooms/:id/restart", warmingHandler.RestartWarmingRoom, warmingWrite)

	// Logs (Execution History - Read Only)
	warming.GET("/logs", warmingHandler.GetAllWarmingLogs)
	warming.GET("/logs/:id", warmingHandler.GetWarmingLogByID)

	port := os.Getenv("PORT")
	if port == "" {
		port = "2121" // safe default
	}

	// Start warming worker if enabled — shared context cancelled during shutdown
	warmingCtx, cancelWarming := context.WithCancel(context.Background())
	defer cancelWarming()
	if os.Getenv("WARMING_WORKER_ENABLED") == "true" {
		log.Println("🚀 Starting Warming Worker...")
		go worker.StartWarmingWorker(warmingCtx, hub)
	} else {
		log.Println("⏸️  Warming Worker disabled (set WARMING_WORKER_ENABLED=true to enable)")
	}

	// =====================================================
	// WEB UI STATIC FILES (SPA)
	// =====================================================
	e.Static("/assets", "./web/dist/assets")
	e.File("/favicon.svg", "./web/dist/favicon.svg")
	// SPA catch-all: serve index.html for all non-API routes
	e.GET("/*", func(c echo.Context) error {
		return c.File("./web/dist/index.html")
	})

	baseURL := os.Getenv("BASEURL")
	if baseURL == "" {
		log.Fatal("BASEURL is not set")
	}

	// Log info to verify config
	log.Printf("Server starting on port %s, baseURL=%s", port, baseURL)

	// Bind to all interfaces, not just 127.0.0.1
	srv := &http.Server{
		Addr:              ":" + port,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	go func() {
		if err := e.StartServer(srv); err != nil && err != http.ErrServerClosed {
			log.Fatalf("API server failed: %v", err)
		}
	}()

	// Wait for termination signal for graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down API server...")
	cancelWarming()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := e.Shutdown(ctx); err != nil {
		log.Printf("Graceful shutdown failed: %v", err)
	} else {
		log.Println("API server shutdown complete.")
	}
}
