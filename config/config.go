package config

var EnableWebsocketIncomingMessage bool
var EnableWebhook bool
var WarmingWorkerEnabled bool
var WarmingAutoReplyEnabled bool
var WarmingAutoReplyCooldown int // seconds

// CookieSecure drives the Secure flag on the session cookie. Populated once
// from the COOKIE_SECURE env var at startup so hot-path handlers don't shell
// out to os.Getenv on every request.
var CookieSecure bool

// CorsAllowOrigins is the validated, trimmed list of origins allowed by CORS
// and the WebSocket upgrade handshake. Populated once at startup in main.go.
var CorsAllowOrigins []string

// Typing Delay Configuration (read once at startup)
var TypingDelayMin int // 0 = disabled (use calculated delay)
var TypingDelayMax int

// Phone number configuration lives in phone.go, because both binaries load it.

// AI Configuration
var AIEnabled bool
var AIDefaultProvider string
var GeminiAPIKey string
var GeminiDefaultModel string
var AIConversationHistoryLimit int
var AIDefaultTemperature float64
var AIDefaultMaxTokens int
