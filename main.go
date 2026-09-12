package main

import (
	"context"
	"database/sql"
	"errors"
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

// initDatabases opens the three PostgreSQL pools. The whatsmeow and app URLs
// are mandatory; the outbox URL falls back to the app pool when unset.
func initDatabases() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL is not set")
	}
	database.InitWhatsmeow(dbURL)

	appDbURL := os.Getenv("APP_DATABASE_URL")
	if appDbURL == "" {
		log.Fatal("APP_DATABASE_URL is not set")
	}
	database.InitAppDB(appDbURL)

	database.InitOutboxDB(os.Getenv("OUTBOX_DATABASE_URL"))
}

// loadFeatureFlags reads the boolean feature switches and the warming
// auto-reply cooldown into the config package globals.
func loadFeatureFlags() {
	config.EnableWebsocketIncomingMessage = strings.ToLower(os.Getenv("CHARON_ENABLE_WEBSOCKET_INCOMING_MSG")) == "true"
	config.EnableWebhook = strings.ToLower(os.Getenv("CHARON_ENABLE_WEBHOOK")) == "true"
	config.WarmingAutoReplyEnabled = os.Getenv("WARMING_AUTO_REPLY_ENABLED") == "true"
	config.WarmingWorkerEnabled = os.Getenv("WARMING_WORKER_ENABLED") == "true"

	// Cookie security flag — default true, only disable for plain http localhost dev.
	config.CookieSecure = os.Getenv("COOKIE_SECURE") != "false"

	config.WarmingAutoReplyCooldown = helper.GetEnvAsPositiveInt("WARMING_AUTO_REPLY_COOLDOWN", 60)
}

// loadMessagingConfig reads the typing simulation and phone number tunables.
// A zero typing delay means "derive the delay from message length".
func loadMessagingConfig() {
	config.TypingDelayMin = helper.GetEnvAsPositiveInt("CHARON_TYPING_DELAY_MIN", 0)
	config.TypingDelayMax = helper.GetEnvAsPositiveInt("CHARON_TYPING_DELAY_MAX", 0)
	config.LoadPhoneConfig()
}

// loadAIConfig reads the AI provider settings and their defaults.
func loadAIConfig() {
	config.AIEnabled = os.Getenv("AI_ENABLED") == "true"

	config.AIDefaultProvider = os.Getenv("AI_DEFAULT_PROVIDER")
	if config.AIDefaultProvider == "" {
		config.AIDefaultProvider = "gemini" // default to free Gemini
	}

	config.GeminiAPIKey = os.Getenv("GEMINI_API_KEY")
	config.GeminiDefaultModel = os.Getenv("GEMINI_DEFAULT_MODEL")
	if config.GeminiDefaultModel == "" {
		config.GeminiDefaultModel = "gemini-flash-latest"
	}

	config.AIConversationHistoryLimit = helper.GetEnvAsPositiveInt("AI_CONVERSATION_HISTORY_LIMIT", 10)
	config.AIDefaultTemperature = helper.GetEnvAsFloatInRange("AI_DEFAULT_TEMPERATURE", 0.7, 0, 2)
	config.AIDefaultMaxTokens = helper.GetEnvAsPositiveInt("AI_DEFAULT_MAX_TOKENS", 150)
}

// bootstrapRuntime applies the schema, seeds the admin user and restores the
// stored WhatsApp devices. Neither the seed nor the restore is fatal.
func bootstrapRuntime() {
	log.Println("Ensuring database schema...")
	helper.InitCustomSchema()

	// Runs before the device loader, which writes instances.phone_number.
	helper.RunPhoneBackfill()

	if err := model.SeedAdminUser(); err != nil {
		log.Printf("Warning: Failed to seed admin user: %v", err)
	}

	log.Println("Loading existing devices...")
	if err := service.LoadAllDevices(); err != nil {
		log.Printf("Warning: Failed to load devices: %v", err)
	}
}

// startHub starts the WebSocket hub and wires the per-user broadcast filter.
func startHub() *ws.Hub {
	hub := ws.NewHub()
	go hub.Run()

	service.Realtime = hub

	ws.SetInstanceAccessChecker(func(userID int, instanceID string) bool {
		hasAccess, err := model.HasInstanceAccess(userID, instanceID)
		if err != nil {
			return false
		}
		return hasAccess
	})

	return hub
}

// parseCORSOrigins reads CORS_ALLOW_ORIGINS and validates every entry. The
// rules are strict because the CORS middleware runs with AllowCredentials=true:
// a wildcard or a scheme-less origin aborts startup.
func parseCORSOrigins() []string {
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
		validateCORSOrigin(origin)
		allowOrigins = append(allowOrigins, origin)
	}

	if len(allowOrigins) == 0 {
		log.Fatal("CORS_ALLOW_ORIGINS yielded no valid origins after validation")
	}
	return allowOrigins
}

// validateCORSOrigin aborts startup when a single origin entry is unusable
// with credentialed requests.
func validateCORSOrigin(origin string) {
	if origin == "*" {
		log.Fatalf("CORS_ALLOW_ORIGINS rejects wildcard '*' when cookies/credentials are allowed")
	}
	if !strings.HasPrefix(origin, "http://") && !strings.HasPrefix(origin, "https://") {
		// origin comes from CORS_ALLOW_ORIGINS, an operator-set environment
		// variable read once at startup, never from a request.
		// #nosec G706
		log.Fatalf("CORS_ALLOW_ORIGINS entry %q must include scheme (http:// or https://)", origin)
	}
}

// pingDatabase records the health of one pool and reports whether it is up.
// A nil pool is skipped and counts as healthy.
func pingDatabase(ctx context.Context, checks map[string]string, name string, db *sql.DB) bool {
	if db == nil {
		return true
	}
	if err := db.PingContext(ctx); err != nil {
		checks[name] = "unhealthy: " + err.Error()
		return false
	}
	checks[name] = "ok"
	return true
}

// healthCheckHandler serves GET / as the readiness and liveness probe. It pings
// every configured pool with a shared 3-second budget and reports the build info.
func healthCheckHandler(c echo.Context) error {
	ctx, cancel := context.WithTimeout(c.Request().Context(), 3*time.Second)
	defer cancel()

	checks := map[string]string{}
	allHealthy := pingDatabase(ctx, checks, "app_db", database.AppDB)

	// Only ping the outbox pool when it is a distinct connection.
	if database.OutboxDB != database.AppDB {
		allHealthy = pingDatabase(ctx, checks, "outbox_db", database.OutboxDB) && allHealthy
	}
	allHealthy = pingDatabase(ctx, checks, "whatsmeow_db", database.WhatsmeowDB) && allHealthy

	status := http.StatusOK
	if !allHealthy {
		status = http.StatusServiceUnavailable
	}
	return c.JSON(status, map[string]any{
		"success":    allHealthy,
		"message":    "WhatsApp API is running",
		"version":    resolveVersion(),
		"commit":     commit,
		"build_date": buildDate,
		"checks":     checks,
	})
}

// httpErrorHandler renders every Echo error in the project's response envelope.
func httpErrorHandler(err error, c echo.Context) {
	code := http.StatusInternalServerError
	message := "Internal Server Error"

	if he, ok := errors.AsType[*echo.HTTPError](err); ok {
		code = he.Code
		message = fmt.Sprintf("%v", he.Message)
	}

	response := map[string]any{
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

	if err := c.JSON(code, response); err != nil {
		log.Printf("failed to write error response: %v", err)
	}
}

// requestLogValues emits one JSON line per request. latency_ms is milliseconds
// with microsecond resolution, so sub-millisecond requests do not round to zero.
func requestLogValues(_ echo.Context, v middleware.RequestLoggerValues) error {
	bytesIn := v.ContentLength
	if bytesIn == "" {
		bytesIn = "0"
	}
	log.Printf(`{"time":"%s","request_id":"%s","remote_ip":"%s","method":"%s","uri":"%s","status":%d,"latency_ms":%.3f,"bytes_in":%s,"bytes_out":%d}`,
		v.StartTime.Format(time.RFC3339Nano), v.RequestID, v.RemoteIP,
		v.Method, v.URI, v.Status, float64(v.Latency.Microseconds())/1000, bytesIn, v.ResponseSize)
	return nil
}

// logPanic records a recovered panic with the request context that caused it.
func logPanic(c echo.Context, err error, stack []byte) error {
	requestID := c.Response().Header().Get(echo.HeaderXRequestID)
	if requestID == "" {
		requestID = c.Request().Header.Get(echo.HeaderXRequestID)
	}
	userID, _ := c.Get("user_id").(int64)
	log.Printf("PANIC: request_id=%s method=%s uri=%s user_id=%d err=%v\n%s",
		requestID, c.Request().Method, c.Request().RequestURI, userID, err, stack)
	return err
}

// rateLimitDenyHandler returns 429 with Retry-After plus X-RateLimit-* headers.
func rateLimitDenyHandler(limit int, windowSeconds int) func(echo.Context, string, error) error {
	return func(c echo.Context, _ string, _ error) error {
		reset := time.Now().Add(time.Duration(windowSeconds) * time.Second).Unix()
		c.Response().Header().Set("Retry-After", strconv.Itoa(windowSeconds))
		c.Response().Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
		c.Response().Header().Set("X-RateLimit-Remaining", "0")
		c.Response().Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset, 10))
		return c.JSON(http.StatusTooManyRequests, map[string]any{
			"success": false,
			"message": "Rate limit exceeded. Please slow down.",
			"error": map[string]any{
				"code":        "RATE_LIMITED",
				"retry_after": windowSeconds,
			},
		})
	}
}

// uploadSecurityHeaders hardens the static /uploads tree against MIME sniffing
// and against SVG files executing their embedded scripts as a top-level document.
func uploadSecurityHeaders(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if strings.HasPrefix(c.Request().URL.Path, "/uploads/") {
			c.Response().Header().Set("X-Content-Type-Options", "nosniff")
			c.Response().Header().Set("Content-Security-Policy", "default-src 'none'")
			if strings.HasSuffix(strings.ToLower(c.Request().URL.Path), ".svg") {
				c.Response().Header().Set("Content-Disposition", "attachment")
			}
		}
		return next(c)
	}
}

// Cache-Control values, one per response class.
const (
	cacheImmutableAsset = "public, max-age=31536000, immutable"
	cacheStaticFile     = "public, max-age=3600"
	cacheSPAShell       = "public, max-age=300"
	cachePrivateUpload  = "private, no-cache"
	cacheNoStore        = "no-store"
)

// Paths that must never be stored: the health probe, the WebSocket upgrade and
// the two authentication endpoints.
var noStorePaths = map[string]bool{
	"/":       true,
	"/ws":     true,
	"/login":  true,
	"/logout": true,
}

// cacheControlForPath classifies a request path into a Cache-Control value.
// Vite emits content-hashed names under /assets, so those are immutable. Every
// unmatched path falls through to the SPA catch-all, which serves index.html.
func cacheControlForPath(path string) string {
	switch {
	case strings.HasPrefix(path, "/assets/"):
		return cacheImmutableAsset
	case path == "/favicon.svg":
		return cacheStaticFile
	case strings.HasPrefix(path, "/uploads/"):
		return cachePrivateUpload
	case noStorePaths[path] || strings.HasPrefix(path, "/api/"):
		return cacheNoStore
	}
	return cacheSPAShell
}

// cacheHeaders sets Cache-Control on every response. Only GET and HEAD are
// cacheable; anything else is no-store. middleware.Gzip() already adds
// Vary: Accept-Encoding, so this does not repeat it.
func cacheHeaders(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		req := c.Request()
		if req.Method != http.MethodGet && req.Method != http.MethodHead {
			c.Response().Header().Set("Cache-Control", cacheNoStore)
			return next(c)
		}
		c.Response().Header().Set("Cache-Control", cacheControlForPath(req.URL.Path))
		return next(c)
	}
}

// weakFileETag builds a validator from the file mtime and size. It is weak on
// two counts: mtime plus size is not a bytewise identity proof, and
// middleware.Gzip() re-encodes the body after the handler has run.
func weakFileETag(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return fmt.Sprintf(`W/"%x-%x"`, info.ModTime().UnixNano(), info.Size())
}

// serveCachedFile serves a file with a weak ETag. c.File delegates to
// http.ServeContent, which answers If-None-Match (comma lists, "*" and weak
// validators) and If-Modified-Since with 304 on its own.
func serveCachedFile(c echo.Context, path string) error {
	if etag := weakFileETag(path); etag != "" {
		c.Response().Header().Set("ETag", etag)
	}
	return c.File(path)
}

// staticGetHead serves a directory tree on both GET and HEAD. e.Static
// registers GET only, so a HEAD on a static path would answer 405 and defeat
// cache revalidation by HEAD.
func staticGetHead(e *echo.Echo, pathPrefix, fsRoot string) {
	h := echo.StaticDirectoryHandler(echo.MustSubFS(e.Filesystem, fsRoot), false)
	e.GET(pathPrefix+"*", h)
	e.HEAD(pathPrefix+"*", h)
}

// setupCORS validates the allowed origins and installs the CORS middleware.
// AllowCredentials is on, so the origin list must be explicit.
func setupCORS(e *echo.Echo) {
	allowOrigins := parseCORSOrigins()
	config.CorsAllowOrigins = allowOrigins
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
}

// setupRateLimiter installs the global rate limiter and returns the stricter
// limiter reserved for the authentication endpoints.
func setupRateLimiter(e *echo.Echo) echo.MiddlewareFunc {
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
		DenyHandler: rateLimitDenyHandler(rateBurst, rateWindow*60),
	}))

	// Stricter rate limit for auth endpoints (5 requests per minute per IP)
	return middleware.RateLimiterWithConfig(middleware.RateLimiterConfig{
		Store: middleware.NewRateLimiterMemoryStoreWithConfig(
			middleware.RateLimiterMemoryStoreConfig{
				Rate:      rate.Limit(5.0 / 60.0),
				Burst:     5,
				ExpiresIn: 5 * time.Minute,
			},
		),
		DenyHandler: rateLimitDenyHandler(5, 60),
	})
}

// setupMiddleware installs the global middleware chain in order and returns the
// auth-endpoint rate limiter for the caller to attach to /login and /logout.
func setupMiddleware(e *echo.Echo) echo.MiddlewareFunc {
	e.Use(middleware.RequestID())
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogRequestID:     true,
		LogRemoteIP:      true,
		LogMethod:        true,
		LogURI:           true,
		LogStatus:        true,
		LogLatency:       true,
		LogContentLength: true,
		LogResponseSize:  true,
		LogValuesFunc:    requestLogValues,
	}))
	e.Use(middleware.RecoverWithConfig(middleware.RecoverConfig{
		StackSize:         4 << 10,
		DisableStackAll:   false,
		DisablePrintStack: false,
		LogLevel:          log2.ERROR,
		LogErrorFunc:      logPanic,
	}))
	e.Use(middleware.BodyLimit("100M"))
	e.Use(middleware.Gzip())
	e.Use(cacheHeaders)
	e.Use(middleware.SecureWithConfig(middleware.SecureConfig{
		XFrameOptions:         "DENY",
		ContentSecurityPolicy: "frame-ancestors 'none'",
		ContentTypeNosniff:    "nosniff",
		HSTSMaxAge:            31536000,
	}))

	setupCORS(e)
	return setupRateLimiter(e)
}

func main() {

	// Load .env (ignore error if file doesn't exist, e.g. in production)
	_ = godotenv.Load()

	initDatabases()
	loadFeatureFlags()
	loadMessagingConfig()
	loadAIConfig()

	log.Printf("feature flags -> websocket_incoming_msg: %v, webhook: %v, warming_auto_reply: %v, ai_enabled: %v",
		config.EnableWebsocketIncomingMessage, config.EnableWebhook, config.WarmingAutoReplyEnabled, config.AIEnabled)

	// Initialize upload configuration from env vars
	helper.InitUploadConfig()

	// Initialize session-based authentication
	service.InitSessionConfig()

	// Start periodic sweep of expired webhook cache entries
	service.StartWebhookCacheSweeper()

	bootstrapRuntime()
	hub := startHub()

	// Setup Echo
	e := echo.New()

	// Configure IP extraction for rate limiting
	if os.Getenv("BEHIND_PROXY") == "true" {
		e.IPExtractor = echo.ExtractIPFromRealIPHeader()
	} else {
		e.IPExtractor = echo.ExtractIPDirect()
	}

	authLimiter := setupMiddleware(e)

	// =====================================================
	// PUBLIC ROUTES (No authentication required)
	// =====================================================

	// New user authentication endpoints
	e.POST("/login", handler.LoginUser, authLimiter)
	e.POST("/logout", handler.LogoutUser, authLimiter)

	// Static file serving for uploaded files — with security headers to prevent MIME sniffing
	e.Use(uploadSecurityHeaders)
	staticGetHead(e, "/uploads", "./uploads")

	// WebSocket and health check
	e.GET("/ws", handler.WebSocketHandler(hub)) // WebSocket listener (Gorilla)
	e.GET("/", healthCheckHandler)              // Health check

	// Route group accepting session cookie (UI) or X-API-Key (outbox worker, external integrations)
	api := e.Group("/api", customMiddleware.SessionOrAPIKeyMiddleware())

	e.HTTPErrorHandler = httpErrorHandler

	registerAPIRoutes(e, api, hub)
	runServer(e, hub)
}

// registerAPIRoutes wires every authenticated route group onto the /api group,
// plus the API-key-only outbox write group on the root Echo instance.
func registerAPIRoutes(e *echo.Echo, api *echo.Group, hub *ws.Hub) {
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
	// OUTBOX API
	// Writes are API-key only (worker clients). Reads accept either a session
	// cookie (admin UI) or an API key via the shared SessionOrAPIKey middleware
	// on the `api` group.
	//----------------------------
	outboxWrite := e.Group("/api/outbox", customMiddleware.APIKeyAuthMiddleware())
	outboxWrite.POST("/enqueue", handler.EnqueueOutbox)
	outboxWrite.POST("/enqueue-batch", handler.EnqueueOutboxBatch)

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
}

// registerSPARoutes serves the built frontend. The catch-all must be registered
// last so it never shadows an API route.
func registerSPARoutes(e *echo.Echo) {
	staticGetHead(e, "/assets", "./web/dist/assets")

	favicon := func(c echo.Context) error {
		return serveCachedFile(c, "./web/dist/favicon.svg")
	}
	e.GET("/favicon.svg", favicon)
	e.HEAD("/favicon.svg", favicon)

	shell := func(c echo.Context) error {
		return serveCachedFile(c, "./web/dist/index.html")
	}
	e.GET("/*", shell)
	e.HEAD("/*", shell)
}

// runServer starts the warming worker and the HTTP server, then blocks until a
// termination signal arrives and shuts both down gracefully.
func runServer(e *echo.Echo, hub *ws.Hub) {
	port := os.Getenv("PORT")
	if port == "" {
		port = "2121" // safe default
	}

	// Start warming worker if enabled — shared context cancelled during shutdown
	warmingCtx, cancelWarming := context.WithCancel(context.Background())
	defer cancelWarming()
	if config.WarmingWorkerEnabled {
		log.Println("🚀 Starting Warming Worker...")
		go worker.StartWarmingWorker(warmingCtx, hub)
	} else {
		log.Println("⏸️  Warming Worker disabled (set WARMING_WORKER_ENABLED=true to enable)")
	}

	registerSPARoutes(e)

	baseURL := os.Getenv("BASEURL")
	if baseURL == "" {
		log.Fatal("BASEURL is not set")
	}

	// Log info to verify config
	// port and baseURL come from PORT and BASEURL, read once at startup.
	// #nosec G706
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
