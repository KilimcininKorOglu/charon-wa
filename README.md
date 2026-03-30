# HERMESWA

**WhatsApp Multi-Instance Automation API**

REST API for WhatsApp Web automation, multi-instance management, and real-time messaging built with Go, Echo v4, and whatsmeow.

---

## Table of Contents

- [Key Features](#key-features)
- [Tech Stack](#tech-stack)
- [Getting Started](#getting-started)
- [Environment Variables](#environment-variables)
- [Deployment](#deployment)
- [WebSocket Events](#websocket-events)
- [Webhook Integration](#webhook-integration)
- [API Reference](#api-reference)
- [Disclaimer](#disclaimer)

---

## Key Features

### Authentication and Instance Management

- Multi-instance management for multiple WhatsApp numbers simultaneously
- QR Code authentication for device pairing
- Persistent sessions stored in PostgreSQL, survive restarts
- Auto-reconnect after server restart with staggered delays
- Instance reusability: logged out instances can re-scan QR without recreation
- Instance availability control via `used` flag and `description` field
- Graceful logout with complete cleanup (device store + session memory)
- Circle-based grouping to organize instances by category

### Messaging

- Send text messages by instance ID or by phone number
- Send media (image, video, document, audio) from URL or file upload
- Recipient number validation before sending
- Human-like typing simulation with variable speed, composing/paused presence, and random delays
- Real-time incoming message listener via WebSocket per instance

### WhatsApp Warming System

- **Two simulation modes:**
  - **AI Mode (Human vs Bot)** -- Automated natural interaction using Google Gemini AI
  - **Script Mode** -- Pre-defined conversation scripts with Spintax support for variety
- Bidirectional communication: Actor A and Actor B automatic message exchange
- Simulation mode for dry-run testing without sending real messages
- Real message mode with typing simulation
- Auto-pause on errors when instances disconnect
- Dynamic variables: `{TIME_GREETING}`, `{DAY_NAME}`, `{DATE}` for contextual messages
- Randomized interval control between messages (min/max seconds)
- Real-time monitoring via WebSocket events
- Drag-and-drop script line reordering

### Real-time Features (WebSocket)

- Global WebSocket (`/ws`) for QR events, status changes, and system notifications
- Instance-specific WebSocket (`/api/listen/:instanceId`) for incoming messages
- Warming events: real-time status updates (SUCCESS / FAILED / FINISHED / PAUSED)
- Ping-based keep-alive every 5 minutes
- Auto-cleanup of ghost connections after 15-minute timeout
- Configurable incoming message broadcast via environment variable

### Device and Presence

- Random device identity (OS + hex ID) per instance for privacy
- Presence heartbeat ("active now" status) every 5 minutes
- Real-time status tracking: `online`, `disconnected`, `logged_out`

### Worker Blast Outbox System

- Standalone worker process as a separate binary
- Multi-application support: one worker handles multiple applications sequentially
- Sequential FIFO queuing by `insertDateTime`
- Atomic message claiming with `FOR UPDATE SKIP LOCKED`
- Wildcard support (`*`) to process all applications
- Dynamic configuration with auto-reload every 30 seconds
- Interruptible sleep for graceful shutdown
- Circle-based routing to specific instance groups
- Webhook integration for optional status callbacks
- Auto-migration of database schema on startup

**Configuration modes:**

| Mode               | Value                    | Behavior                   |
| :----------------- | :----------------------- | :------------------------- |
| Single Application | `application = "App1"`   | Dedicated worker for one   |
| Multi-Application  | `application = "A, B, C"`| Sequential processing      |
| Wildcard           | `application = "*"`      | Process all pending        |

**Processing cycle:**

1. Worker polls database for pending messages (`status = 0`)
2. Atomically claims one message (`status = 3`)
3. Fetches available instances from configured circle
4. Sends message via WhatsApp API
5. Updates status to success (`1`) or failed (`2`)
6. Sleeps for configured interval (with random jitter if `interval_max` set)
7. Repeats

---

## Tech Stack

| Component  | Technology                                                  |
| :--------- | :---------------------------------------------------------- |
| Language   | Go 1.24+                                                    |
| Framework  | [Echo v4](https://echo.labstack.com/)                       |
| WhatsApp   | [whatsmeow](https://github.com/tulir/whatsmeow)            |
| Database   | PostgreSQL 12+                                              |
| WebSocket  | [Gorilla WebSocket](https://github.com/gorilla/websocket)   |
| AI         | Google Gemini API                                           |
| Process    | PM2 (production)                                            |

---

## Getting Started

### Prerequisites

- Go 1.24 or later
- PostgreSQL 12 or later
- (Optional) Separate PostgreSQL database for outbox
- (Optional) PM2 for production process management

### Build

```bash
# API Server
go build -o hermeswa main.go

# Worker
go build -o worker ./cmd/worker/

# Make executable (Linux/macOS)
chmod +x hermeswa worker
```

### Run

```bash
# Copy and configure environment
cp .env.example .env
# Edit .env with your settings

# Start API server
./hermeswa

# Start worker (separate terminal)
./worker
```

---

## Environment Variables

Configure these in your `.env` file.

### Core Configuration

| Variable             | Description                                         | Default | Example                                  |
| :------------------- | :-------------------------------------------------- | :------ | :--------------------------------------- |
| `DATABASE_URL`       | PostgreSQL URL for whatsmeow session storage        | --      | `postgres://user:pass@localhost:5432/db`  |
| `APP_DATABASE_URL`   | PostgreSQL URL for application data                 | --      | `postgres://user:pass@localhost:5432/app` |
| `OUTBOX_DATABASE_URL`| PostgreSQL URL for outbox (optional)                | --      | `postgres://user:pass@localhost:5432/outbox` |
| `JWT_SECRET`         | Secret key for JWT authentication                   | --      | `your-secret-key`                        |
| `APP_LOGIN_USERNAME` | Username for API login                              | --      | `admin`                                  |
| `APP_LOGIN_PASSWORD` | Password for API login                              | --      | `secure-password`                        |
| `PORT`               | Server listening port                               | `2121`  | `3000`                                   |
| `BASEURL`            | Base URL/Host of the server                         | --      | `127.0.0.1`                              |
| `CORS_ALLOW_ORIGINS` | Allowed origins for CORS                            | --      | `http://localhost:3000`                  |

### Features

| Variable                                   | Description                                  | Default | Example |
| :----------------------------------------- | :------------------------------------------- | :------ | :------ |
| `HERMESWA_ENABLE_WEBSOCKET_INCOMING_MSG`   | Enable incoming message WebSocket broadcast  | `false` | `true`  |
| `HERMESWA_ENABLE_WEBHOOK`                  | Enable global incoming message webhooks      | `false` | `true`  |
| `HERMESWA_TYPING_DELAY_MIN`               | Minimum typing simulation delay (seconds)    | `1`     | `2`     |
| `HERMESWA_TYPING_DELAY_MAX`               | Maximum typing simulation delay (seconds)    | `3`     | `5`     |
| `ALLOW_9_DIGIT_PHONE_NUMBER`              | Allow 9-digit numbers without validation     | `false` | `true`  |

### Rate Limiting

| Variable                   | Description                    | Default | Example |
| :------------------------- | :----------------------------- | :------ | :------ |
| `RATE_LIMIT_PER_SECOND`   | API requests allowed per second| `10`    | `20`    |
| `RATE_LIMIT_BURST`        | Max burst of requests          | `10`    | `20`    |
| `RATE_LIMIT_WINDOW_MINUTES`| Rate limit expiration window  | `3`     | `5`     |

### File Upload Limits (MB)

| Variable                   | Description            | Default | Example |
| :------------------------- | :--------------------- | :------ | :------ |
| `MAX_FILE_SIZE_IMAGE_MB`   | Max image upload size  | `5`     | `10`    |
| `MAX_FILE_SIZE_VIDEO_MB`   | Max video upload size  | `16`    | `32`    |
| `MAX_FILE_SIZE_AUDIO_MB`   | Max audio upload size  | `16`    | `32`    |
| `MAX_FILE_SIZE_DOCUMENT_MB`| Max document upload size| `100`  | `200`   |

### Warming System

| Variable                           | Description                              | Default | Example |
| :--------------------------------- | :--------------------------------------- | :------ | :------ |
| `WARMING_WORKER_ENABLED`           | Enable conversation simulation           | `false` | `true`  |
| `WARMING_WORKER_INTERVAL_SECONDS`  | Interval between worker checks           | `5`     | `10`    |
| `WARMING_AUTO_REPLY_ENABLED`       | Enable AI/Auto-reply in warming rooms    | `false` | `true`  |
| `WARMING_AUTO_REPLY_COOLDOWN`      | Cooldown between auto-replies (seconds)  | `60`    | `10`    |
| `DEFAULT_REPLY_DELAY_MIN`          | Min delay before auto-reply (seconds)    | `10`    | `5`     |
| `DEFAULT_REPLY_DELAY_MAX`          | Max delay before auto-reply (seconds)    | `60`    | `30`    |

### AI Configuration (Gemini)

| Variable                       | Description                              | Default          | Example        |
| :----------------------------- | :--------------------------------------- | :--------------- | :------------- |
| `AI_ENABLED`                   | Enable AI-powered features               | `false`          | `true`         |
| `AI_DEFAULT_PROVIDER`          | AI provider                              | `gemini`         | `openai`       |
| `GEMINI_API_KEY`               | Google Gemini API Key                    | --               | `AIzaSy...`    |
| `GEMINI_DEFAULT_MODEL`         | Default Gemini model                     | `gemini-1.5-flash`| `gemini-pro`  |
| `AI_CONVERSATION_HISTORY_LIMIT`| Previous messages for context            | `10`             | `20`           |
| `AI_DEFAULT_TEMPERATURE`       | Response randomness (0.0 to 1.0)         | `0.7`            | `0.5`          |
| `AI_DEFAULT_MAX_TOKENS`        | Max tokens for AI response               | `150`            | `300`          |

### Worker Blast Outbox

| Variable            | Description                             | Default                  | Example                    |
| :------------------ | :-------------------------------------- | :----------------------- | :------------------------- |
| `OUTBOX_API_BASEURL`| Base URL for WhatsApp API (worker)      | `http://localhost:2121`  | `https://api.example.com`  |
| `OUTBOX_API_USER`   | Username for worker API authentication  | --                       | `worker_user`              |
| `OUTBOX_API_PASS`   | Password for worker API authentication  | --                       | `worker_pass`              |

The worker runs as a standalone binary and communicates with the main API to send messages. It reads configurations from `APP_DATABASE_URL` and processes messages from `OUTBOX_DATABASE_URL` (falls back to `APP_DATABASE_URL` if not set).

---

## Deployment

### Running with PM2 (Production)

The project includes an `ecosystem.config.js` for PM2:

```bash
# Start both API and worker
pm2 start ecosystem.config.js

# Save and enable startup
pm2 save
pm2 startup
```

### Cross-compilation

```bash
# From macOS/Windows to Linux
GOOS=linux GOARCH=amd64 go build -o hermeswa main.go
GOOS=linux GOARCH=amd64 go build -o worker ./cmd/worker/
```

### Auto-migration

The application automatically updates the database schema on startup:

- Creates missing tables
- Adds missing columns
- Expands column types (e.g., VARCHAR to TEXT)
- Preserves existing data and custom columns

No manual migration commands are needed.

---

## WebSocket Events

### Global WebSocket -- System Events

```
ws://{host}:{port}/ws
```

Monitors QR code generation, login/logout events, connection status changes, and system-wide notifications for all instances.

### Instance-Specific WebSocket -- Incoming Messages

```
ws://{host}:{port}/api/listen/{instanceId}
```

Requires authentication:

```http
Authorization: Bearer {token}
```

**Event payload:**

```json
{
  "event": "incoming_message",
  "timestamp": "2025-12-07T23:22:00Z",
  "data": {
    "instance_id": "instance123",
    "from": "6281234567890@s.whatsapp.net",
    "from_me": false,
    "message": "Hello World",
    "timestamp": 1733587980,
    "is_group": false,
    "message_id": "3EB0ABC123DEF456",
    "push_name": "John Doe"
  }
}
```

---

## Webhook Integration

### Configure Webhook per Instance

```http
POST /api/instances/:instanceId/webhook-setconfig
Authorization: Bearer {token}
Content-Type: application/json
```

```json
{
  "url": "https://your-app.com/wa-webhook",
  "secret": "your-webhook-secret"
}
```

When a secret is configured, HERMESWA signs every outgoing webhook using HMAC-SHA256:

| Detail    | Value                              |
| :-------- | :--------------------------------- |
| Header    | `X-HERMESWA-Signature`             |
| Algorithm | HMAC-SHA256                        |
| Message   | Raw HTTP request body              |
| Key       | Instance-specific `webhook_secret` |

**Webhook payload** follows the same format as the WebSocket `incoming_message` event shown above.

---

## API Reference

Full API documentation is available at:

```
https://sudevwa.apidog.io/
```

An OpenAPI 3.0 specification is included in `api_docs/openapi.json`.

## Disclaimer

This project is intended for educational and research purposes only. Use at your own risk.

---

## License

See [LICENSE](LICENSE) for details.

---

[![Go Version](https://img.shields.io/github/go-mod/go-version/KilimcininKorOglu/hermes-wa)](https://github.com/KilimcininKorOglu/hermes-wa)
[![GitHub issues](https://img.shields.io/github/issues/KilimcininKorOglu/hermes-wa)](https://github.com/KilimcininKorOglu/hermes-wa/issues)
[![GitHub stars](https://img.shields.io/github/stars/KilimcininKorOglu/hermes-wa)](https://github.com/KilimcininKorOglu/hermes-wa)
