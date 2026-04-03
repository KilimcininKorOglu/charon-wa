package config

var EnableWebsocketIncomingMessage bool
var EnableWebhook bool
var WarmingWorkerEnabled bool
var WarmingAutoReplyEnabled bool
var WarmingAutoReplyCooldown int // seconds

// Typing Delay Configuration (read once at startup)
var TypingDelayMin int // 0 = disabled (use calculated delay)
var TypingDelayMax int

// AI Configuration
var AIEnabled bool
var AIDefaultProvider string
var GeminiAPIKey string
var GeminiDefaultModel string
var AIConversationHistoryLimit int
var AIDefaultTemperature float64
var AIDefaultMaxTokens int
