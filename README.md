# Charon

**WhatsApp Multi-Instance Automation API**

REST API for WhatsApp Web automation, multi-instance management, and real-time messaging built with Go, Echo v4, and whatsmeow. Includes a cyberpunk-themed React web UI and a standalone blast outbox worker.

---

## Table of Contents

- [Key Features](#key-features)
- [Tech Stack](#tech-stack)
- [Getting Started](#getting-started)
- [Authentication](#authentication)
- [Docker Development](#docker-development)
  - [Local Development](#local-development)
  - [Production (Coolify)](#production-coolify)
- [Web UI](#web-ui)
- [Environment Variables](#environment-variables)
- [Deployment](#deployment)
- [User Profile API](#user-profile-api)
- [Instance Management API](#instance-management-api)
- [Messaging API](#messaging-api)
- [Group Messaging API](#group-messaging-api)
- [File Manager API](#file-manager-api)
- [System Identity API](#system-identity-api)
- [Warming System API](#warming-system-api)
- [Worker Config API](#worker-config-api)
- [Outbox REST API](#outbox-rest-api)
- [WebSocket Events](#websocket-events)
- [Webhook Integration](#webhook-integration)
- [Admin API](#admin-api)
- [API Key Management](#api-key-management)
- [Contacts API](#contacts-api)
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
- Per-instance status refresh via REST API

### Messaging

- Send text messages by instance ID or by phone number
- Send media (image, video, document, audio) from URL or file upload
- Group messaging by instance ID or by phone number
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
- AI-powered script line generation

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

| Mode               | Value                     | Behavior                 |
|:-------------------|:--------------------------|:-------------------------|
| Single Application | `application = "App1"`    | Dedicated worker for one |
| Multi-Application  | `application = "A, B, C"` | Sequential processing    |
| Wildcard           | `application = "*"`       | Process all pending      |

**Processing cycle:**

1. Worker polls database for pending messages (`status = 0`)
2. Atomically claims one message (`status = 3`)
3. Fetches available instances from configured circle
4. Sends message via WhatsApp API
5. Updates status to success (`1`) or failed (`2`)
6. Sleeps for configured interval (with random jitter if `interval_max` set)
7. Repeats

### API Key Authentication

- API key system for external application integrations
- Keys are SHA-256 hashed in database (raw key shown once on creation)
- Per-key application scope locking (optional)
- Key management via session-cookie-protected endpoints
- Last-used tracking for audit purposes

---

## Tech Stack

| Component | Technology                                                   |
|:----------|:-------------------------------------------------------------|
| Language  | Go 1.26.2+                                                   |
| Framework | [Echo v4](https://echo.labstack.com/)                        |
| WhatsApp  | [whatsmeow](https://github.com/tulir/whatsmeow)              |
| Database  | PostgreSQL 12+                                               |
| WebSocket | [Gorilla WebSocket](https://github.com/gorilla/websocket)    |
| AI        | Google Gemini API                                            |
| Frontend  | React 19 + Vite 8 + TypeScript + TailwindCSS v4 + Zustand v5 |
| Container | Docker / Docker Compose                                      |

---

## Getting Started

### Prerequisites

- Go 1.26.2 or later
- Node.js 22+ and npm (for frontend)
- PostgreSQL 12 or later
- Make (build tool)
- Docker and Docker Compose
- (Cross-compilation only) Zig (`brew install zig`)

### Build

All build operations go through the Makefile:

```bash
# Build both API server and worker (GOFLAGS=-mod=mod required if docker-data/go-mod exists locally)
GOFLAGS=-mod=mod make build

# Build frontend
cd web && npm install && npm run build

# Lint (format + vet)
make lint

# Clean build artifacts
make clean
```

### Run

```bash
# Copy and configure environment
cp .env.example .env
# Edit .env with your settings

# Start API server
./bin/charon

# Start worker (separate terminal)
./bin/worker
```

On first startup, if no admin user exists in the database, Charon automatically creates a default admin account with credentials `admin` / `admin123`. Change the password immediately after first login.

---

## Authentication

Charon uses **server-side sessions with httpOnly cookies** for user/UI authentication. API keys are available for external integrations.

### Roles

| Role     | Description                                                             |
|:---------|:------------------------------------------------------------------------|
| `admin`  | Full access to all resources and admin endpoints                        |
| `user`   | Standard access, scoped to assigned instances                           |
| `viewer` | Read-only access, blocked from all write operations on instances/phones |

### Session Flow

1. **Login** with username/password (user accounts are created by admins via `POST /api/admin/users`)
2. The server creates a DB-backed session and sets an HttpOnly, Secure, SameSite=Strict `session` cookie
3. The browser sends the cookie automatically on all subsequent requests — no `Authorization` header needed
4. Sessions use **sliding expiry** (7 days default, extended on each request)
5. Admin role change or user deactivation triggers instant session revocation across all devices

### Login

```http
POST /login
Content-Type: application/json
```

```json
{
  "username": "johndoe",
  "password": "securePassword123"
}
```

**Response** (cookie set by server):

```json
{
  "success": true,
  "message": "Login successful",
  "data": {
    "user": {
      "id": 1,
      "username": "johndoe",
      "role": "user",
      "is_active": true
    }
  }
}
```

After login, the browser stores the `session` cookie and sends it automatically. Non-browser clients should enable cookie jar support (axios: `withCredentials: true`; curl: `-c cookies.txt -b cookies.txt`).

### Logout

```http
POST /logout
```

Destroys the session server-side and clears the cookie. No body required — the `session` cookie identifies the session to destroy. Available on a public route so expired sessions can still log out cleanly.

### Account Lockout

Five consecutive failed logins lock the account for 15 minutes (`failed_login_count` + `locked_until` columns on the users table). Successful login resets the counter.

---

## Docker Development

Two separate Docker Compose files are provided:

### Local Development

```bash
# Start all services (PostgreSQL + API + Worker + Web)
docker compose -f docker-compose.local.yml up -d

# View logs
docker compose -f docker-compose.local.yml logs -f

# Stop
docker compose -f docker-compose.local.yml down
```

| Service    | Port | Description                |
|:-----------|:-----|:---------------------------|
| `postgres` | 5432 | PostgreSQL database        |
| `api`      | 2121 | Go API server (hot-reload) |
| `worker`   | --   | Blast outbox worker        |
| `web`      | 5174 | Vite dev server (React UI) |

All volumes bind-mount to `docker-data/` directory. The API server uses `air` for hot-reload during development.

### Production (Coolify)

```bash
# docker-compose.yml is designed for Coolify deployment
# Services: db-init (one-shot) + api + worker
# PostgreSQL is managed separately in Coolify
# Environment variables are set via Coolify UI
# Traefik proxy handles domain routing and SSL
```

| Service   | Description                                           |
|:----------|:------------------------------------------------------|
| `db-init` | One-shot container that creates required databases    |
| `api`     | Production API server (built from Dockerfile)         |
| `worker`  | Blast outbox worker (compiled binary, same image)     |

---

## Web UI

A cyberpunk-themed dark web interface built with React 19, Vite 8, TypeScript, TailwindCSS v4, Zustand v5, Lucide React, and React Hot Toast.

### Pages

| Page        | Description                                                   |
|:------------|:--------------------------------------------------------------|
| Dashboard   | Admin stats + live WebSocket event feed                       |
| Instances   | CRUD, QR scan, detail panel (edit, device info, webhook)      |
| Messages    | By-instance and by-phone modes, contacts/groups tabs, media   |
| Contacts    | Paginated table, detail panel, mutual groups, XLSX/CSV export |
| Files       | Upload browser, breadcrumbs, preview, admin delete            |
| Warming     | Rooms (play/pause/stop), scripts, templates, logs             |
| Blast       | Worker config CRUD with circle/app selectors                  |
| Outbox      | Message queue monitoring with filters and detail panel        |
| Admin Users | User management, role change, instance assignment             |
| Profile     | Edit name, password, avatar upload, API key management        |
| System      | Company identity and logo uploads                             |

### Build Frontend

```bash
cd web
npm install
npm run build
```

The built frontend is served as static files from the Go binary (`web/dist/`).

---

## Environment Variables

Configure these in your `.env` file.

### Core Configuration

| Variable              | Description                                     | Default | Example                                      |
|:----------------------|:------------------------------------------------|:--------|:---------------------------------------------|
| `DATABASE_URL`        | PostgreSQL URL for whatsmeow session storage    | --      | `postgres://user:pass@localhost:5432/db`     |
| `APP_DATABASE_URL`    | PostgreSQL URL for application data             | --      | `postgres://user:pass@localhost:5432/app`    |
| `OUTBOX_DATABASE_URL` | PostgreSQL URL for outbox (optional)            | --      | `postgres://user:pass@localhost:5432/outbox` |
| `PORT`                | Server listening port                           | `2121`  | `3000`                                       |
| `BASEURL`             | Public host/IP of the server (without protocol) | --      | `127.0.0.1`                                  |
| `CORS_ALLOW_ORIGINS`  | Allowed origins for CORS (required)             | --      | `http://localhost:3000`                      |
| `BEHIND_PROXY`        | Enable `X-Real-IP` extraction behind reverse proxy | `false` | `true`                                    |

### Features

| Variable                                 | Description                                 | Default | Example |
|:-----------------------------------------|:--------------------------------------------|:--------|:--------|
| `CHARON_ENABLE_WEBSOCKET_INCOMING_MSG` | Enable incoming message WebSocket broadcast | `false` | `true`  |
| `CHARON_ENABLE_WEBHOOK`                | Enable global incoming message webhooks     | `false` | `true`  |
| `CHARON_TYPING_DELAY_MIN`              | Minimum typing simulation delay (seconds)   | `1`     | `2`     |
| `CHARON_TYPING_DELAY_MAX`              | Maximum typing simulation delay (seconds)   | `3`     | `5`     |
| `PHONE_COUNTRY_CODE`                     | Country code for phone number formatting    | --      | `90`    |
| `ALLOW_9_DIGIT_PHONE_NUMBER`             | Skip IsOnWhatsApp check for leading-0, no-cc-prefix, or <10 digit numbers | `false` | `true`  |

### Session Configuration

| Variable         | Description                                            | Default | Example |
|:-----------------|:-------------------------------------------------------|:--------|:--------|
| `SESSION_EXPIRY` | Session validity duration (sliding, extended per request) | `168h`  | `720h`  |
| `COOKIE_SECURE`  | Set `Secure` flag on session cookie (disable for localhost dev) | `true` | `false` |

### Avatar Upload Configuration

| Variable               | Description                      | Default                | Example          |
|:-----------------------|:---------------------------------|:-----------------------|:-----------------|
| `UPLOAD_DIR`           | Directory for uploaded files     | `./uploads`            | `/data/uploads`  |
| `MAX_AVATAR_SIZE_MB`   | Maximum avatar file size (MB)    | `1`                    | `2`              |
| `MAX_AVATAR_SIZE_KB`   | Maximum avatar size (KB)         | `500`                  | `1024`           |
| `ALLOWED_AVATAR_TYPES` | Allowed image formats            | `jpg,jpeg,png,webp`   | `jpg,png`        |
| `AVATAR_OUTPUT_FORMAT` | Output format after processing   | `webp`                 | `png`            |
| `AVATAR_MAX_DIMENSION` | Maximum dimension in pixels      | `1024`                 | `2048`           |
| `AVATAR_MIN_DIMENSION` | Minimum dimension in pixels      | `100`                  | `50`             |

### Phone Number Format

Phone numbers are automatically formatted using the `PHONE_COUNTRY_CODE` environment variable:

| Format | Conversion |
|:-------|:-----------|
| `0XXXXXXXXX` | `PHONE_COUNTRY_CODE` prefix prepended (e.g. `0555...` → `90555...`) |
| `XXXXXXXXX` (no prefix) | `PHONE_COUNTRY_CODE` prefix prepended |
| Country code already present | Passed through unchanged |

If `PHONE_COUNTRY_CODE` is empty, full international format is required with no auto-conversion.

### Rate Limiting

| Variable                    | Description                     | Default | Example |
|:----------------------------|:--------------------------------|:--------|:--------|
| `RATE_LIMIT_PER_SECOND`     | API requests allowed per second | `10`    | `20`    |
| `RATE_LIMIT_BURST`          | Max burst of requests           | `10`    | `20`    |
| `RATE_LIMIT_WINDOW_MINUTES` | Rate limit expiration window    | `3`     | `5`     |

### File Upload Limits (MB)

| Variable                    | Description              | Default | Example |
|:----------------------------|:-------------------------|:--------|:--------|
| `MAX_FILE_SIZE_IMAGE_MB`    | Max image upload size    | `5`     | `10`    |
| `MAX_FILE_SIZE_VIDEO_MB`    | Max video upload size    | `16`    | `32`    |
| `MAX_FILE_SIZE_AUDIO_MB`    | Max audio upload size    | `16`    | `32`    |
| `MAX_FILE_SIZE_DOCUMENT_MB` | Max document upload size | `100`   | `200`   |

### Warming System

| Variable                          | Description                             | Default | Example |
|:----------------------------------|:----------------------------------------|:--------|:--------|
| `WARMING_WORKER_ENABLED`          | Enable conversation simulation          | `false` | `true`  |
| `WARMING_WORKER_INTERVAL_SECONDS` | Interval between worker checks          | `5`     | `10`    |
| `WARMING_AUTO_REPLY_ENABLED`      | Enable AI/Auto-reply in warming rooms   | `false` | `true`  |
| `WARMING_AUTO_REPLY_COOLDOWN`     | Cooldown between auto-replies (seconds) | `60`    | `10`    |
| `DEFAULT_REPLY_DELAY_MIN`         | Min delay before auto-reply (seconds)   | `10`    | `5`     |
| `DEFAULT_REPLY_DELAY_MAX`         | Max delay before auto-reply (seconds)   | `60`    | `30`    |

### AI Configuration (Gemini)

| Variable                        | Description                      | Default            | Example      |
|:--------------------------------|:---------------------------------|:-------------------|:-------------|
| `AI_ENABLED`                    | Enable AI-powered features       | `false`            | `true`       |
| `AI_DEFAULT_PROVIDER`           | AI provider                      | `gemini`           | `openai`     |
| `GEMINI_API_KEY`                | Google Gemini API Key            | --                 | `AIzaSy...`  |
| `GEMINI_DEFAULT_MODEL`          | Default Gemini model             | `gemini-flash-latest` | `gemini-pro` |
| `AI_CONVERSATION_HISTORY_LIMIT` | Previous messages for context    | `10`               | `20`         |
| `AI_DEFAULT_TEMPERATURE`        | Response randomness (0.0 to 1.0) | `0.7`              | `0.5`        |
| `AI_DEFAULT_MAX_TOKENS`         | Max tokens for AI response       | `150`              | `300`        |

### Worker Blast Outbox

| Variable             | Description                            | Default                 | Example                   |
|:---------------------|:---------------------------------------|:------------------------|:--------------------------|
| `OUTBOX_API_BASEURL` | Base URL for WhatsApp API (worker)     | `http://localhost:2121` | `https://api.example.com` |
| `OUTBOX_API_USER`    | Username for worker API authentication | --                      | `worker_user`             |
| `OUTBOX_API_PASS`    | Password for worker API authentication | --                      | `worker_pass`             |

The worker runs as a standalone binary and communicates with the main API to send messages. It reads configurations from `APP_DATABASE_URL` and processes messages from `OUTBOX_DATABASE_URL` (falls back to `APP_DATABASE_URL` if not set).

### Resource Limits

| Variable                    | Description                                   | Default | Example |
|:----------------------------|:----------------------------------------------|:--------|:--------|
| `MAX_INSTANCES_PER_USER`    | Max WhatsApp instances per non-admin user     | `10`    | `20`    |
| `OUTBOX_MAX_DAILY_PER_USER` | Max outbox messages per user per day          | `10000` | `50000` |

---

## Deployment

### Docker Production Build

The production image uses a 3-stage build (Node frontend + Go backend + Debian runtime). The image contains both the API server binary and the worker binary, but `CMD` only starts the API server. Run the worker as a separate container from the same image.

```bash
# Build production image
docker build -t charon:latest .

# Run API server
docker run -d --name charon-api \
  --env-file .env \
  -p 2121:2121 \
  charon:latest

# Run worker (same image, different entrypoint)
docker run -d --name charon-worker \
  --env-file .env \
  charon:latest ./worker
```

### Cross-compilation

```bash
# Using Makefile (requires zig for CGO cross-compilation)
make build-all
```

**Note:** Windows cross-compile is currently broken with Go 1.26 + zig (0.14–0.15): Go passes a `-tsaware` linker flag unsupported by zig. CI builds Linux only (amd64 + arm64). Release binaries for all platforms are produced by `goreleaser-cross` on tag pushes matching `v*`.

### Auto-migration

The application automatically updates the database schema on startup:

- Creates missing tables
- Adds missing columns
- Expands column types (e.g., VARCHAR to TEXT)
- Preserves existing data and custom columns

No manual migration commands are needed.

---

## User Profile API

Authenticated user management endpoints (requires session cookie):

| Method | Endpoint           | Description                          |
|:-------|:-------------------|:-------------------------------------|
| GET    | `/api/me`          | Get current user profile             |
| PUT    | `/api/me`          | Update profile (name, avatar)        |
| PUT    | `/api/me/password` | Change password (destroys all sessions) |
| POST   | `/logout`          | Logout (public route, destroys session) |
| POST   | `/api/me/avatar`   | Upload avatar image                  |

---

## Instance Management API

WhatsApp instance lifecycle endpoints (requires session cookie + instance access):

| Method | Endpoint                        | Description                          |
|:-------|:--------------------------------|:-------------------------------------|
| POST   | `/api/login`                    | Create new WhatsApp instance         |
| GET    | `/api/instances`                | List all instances (role-filtered)   |
| PATCH  | `/api/instances/:instanceId`    | Update instance fields               |
| DELETE | `/api/instances/:instanceId`    | Delete instance                      |
| GET    | `/api/qr/:instanceId`          | Get QR code for pairing              |
| DELETE | `/api/qr-cancel/:instanceId`   | Cancel QR code generation            |
| GET    | `/api/status/:instanceId`      | Get instance connection status       |
| POST   | `/api/logout/:instanceId`      | Logout WhatsApp session              |
| GET    | `/api/info-device/:instanceId` | Get device info (JID, phone, platform)|

### Create Instance

```http
POST /api/login
Cookie: session={session_cookie}
Content-Type: application/json
```

```json
{
  "circle": "production"
}
```

The `circle` field is required and used for instance grouping. After creation, call `GET /api/qr/:instanceId` to retrieve the QR code for WhatsApp pairing.

### Update Instance

```http
PATCH /api/instances/:instanceId
Cookie: session={session_cookie}
Content-Type: application/json
```

```json
{
  "used": true,
  "description": "Marketing line A",
  "circle": "marketing"
}
```

All fields are optional. At least one must be provided.

---

## Messaging API

Send messages via instance ID or phone number (requires session cookie):

### By Instance ID

| Method | Endpoint                              | Description                    |
|:-------|:--------------------------------------|:-------------------------------|
| POST   | `/api/send/:instanceId`               | Send text message              |
| POST   | `/api/send/:instanceId/media`         | Send media file (upload)       |
| POST   | `/api/send/:instanceId/media-url`     | Send media from URL            |
| POST   | `/api/check/:instanceId`              | Check if number is on WhatsApp |

### By Phone Number

Routes resolve the phone number to an instance, then check user access:

| Method | Endpoint                                    | Description                    |
|:-------|:--------------------------------------------|:-------------------------------|
| POST   | `/api/by-number/:phoneNumber`               | Send text message              |
| POST   | `/api/by-number/:phoneNumber/media-url`     | Send media from URL            |
| POST   | `/api/by-number/:phoneNumber/media-file`    | Send media file (upload)       |

### Send Text Message

```http
POST /api/send/:instanceId
Cookie: session={session_cookie}
Content-Type: application/json
```

```json
{
  "to": "905xxxxxxxxx",
  "message": "Hello from Charon!"
}
```

### Send Media from URL

```http
POST /api/send/:instanceId/media-url
Cookie: session={session_cookie}
Content-Type: application/json
```

```json
{
  "to": "905xxxxxxxxx",
  "mediaUrl": "https://example.com/image.jpg",
  "caption": "Check this out",
  "mediaType": "image"
}
```

`mediaType` is optional (auto-detected from URL). Valid values: `image`, `video`, `document`, `audio`.

### Send Media File

```http
POST /api/send/:instanceId/media
Cookie: session={session_cookie}
Content-Type: multipart/form-data
```

Form fields: `to` (required), `file` (required), `caption` (optional).

### Check Phone Number

```http
POST /api/check/:instanceId
Cookie: session={session_cookie}
Content-Type: application/json
```

```json
{
  "phone": "905xxxxxxxxx"
}
```

---

## Group Messaging API

Group operations via instance ID or phone number (requires session cookie):

### By Instance ID

| Method | Endpoint                                     | Description              |
|:-------|:---------------------------------------------|:-------------------------|
| GET    | `/api/groups/:instanceId`                    | List all groups          |
| POST   | `/api/send-group/:instanceId`                | Send text to group       |
| POST   | `/api/send-group/:instanceId/media`          | Send media file to group |
| POST   | `/api/send-group/:instanceId/media-url`      | Send media URL to group  |

### By Phone Number

| Method | Endpoint                                              | Description              |
|:-------|:------------------------------------------------------|:-------------------------|
| GET    | `/api/groups/by-number/:phoneNumber`                  | List all groups          |
| POST   | `/api/send-group/by-number/:phoneNumber`              | Send text to group       |
| POST   | `/api/send-group/by-number/:phoneNumber/media`        | Send media file to group |
| POST   | `/api/send-group/by-number/:phoneNumber/media-url`    | Send media URL to group  |

---

## File Manager API

File browser for the uploads directory (requires session cookie):

| Method | Endpoint      | Description                     |
|:-------|:--------------|:--------------------------------|
| GET    | `/api/files`  | List uploaded files             |
| DELETE | `/api/files`  | Delete a file (admin only)      |

---

## System Identity API

Company branding configuration (requires session cookie):

| Method | Endpoint               | Description                           |
|:-------|:-----------------------|:--------------------------------------|
| GET    | `/api/system/identity` | Get system identity (name, logos)     |
| POST   | `/api/system/identity` | Update identity (admin only, multipart)|

---

## Warming System API

WhatsApp conversation simulation management (requires session cookie):

### Scripts

| Method | Endpoint                    | Description         |
|:-------|:----------------------------|:--------------------|
| POST   | `/api/warming/scripts`      | Create script       |
| GET    | `/api/warming/scripts`      | List all scripts    |
| GET    | `/api/warming/scripts/:id`  | Get script by ID    |
| PUT    | `/api/warming/scripts/:id`  | Update script       |
| DELETE | `/api/warming/scripts/:id`  | Delete script       |

### Script Lines

| Method | Endpoint                                            | Description              |
|:-------|:----------------------------------------------------|:-------------------------|
| POST   | `/api/warming/scripts/:scriptId/lines`              | Create line              |
| GET    | `/api/warming/scripts/:scriptId/lines`              | List all lines           |
| GET    | `/api/warming/scripts/:scriptId/lines/:id`          | Get line by ID           |
| PUT    | `/api/warming/scripts/:scriptId/lines/:id`          | Update line              |
| DELETE | `/api/warming/scripts/:scriptId/lines/:id`          | Delete line              |
| POST   | `/api/warming/scripts/:scriptId/lines/generate`     | AI-generate lines        |
| PUT    | `/api/warming/scripts/:scriptId/lines/reorder`      | Reorder lines            |

### Templates

| Method | Endpoint                       | Description         |
|:-------|:-------------------------------|:--------------------|
| POST   | `/api/warming/templates`       | Create template     |
| GET    | `/api/warming/templates`       | List all templates  |
| GET    | `/api/warming/templates/:id`   | Get template by ID  |
| PUT    | `/api/warming/templates/:id`   | Update template     |
| DELETE | `/api/warming/templates/:id`   | Delete template     |

### Rooms (Execution)

| Method | Endpoint                              | Description            |
|:-------|:--------------------------------------|:-----------------------|
| POST   | `/api/warming/rooms`                  | Create room            |
| GET    | `/api/warming/rooms`                  | List all rooms         |
| GET    | `/api/warming/rooms/:id`              | Get room by ID         |
| PUT    | `/api/warming/rooms/:id`              | Update room            |
| DELETE | `/api/warming/rooms/:id`              | Delete room            |
| PATCH  | `/api/warming/rooms/:id/status`       | Update room status     |
| POST   | `/api/warming/rooms/:id/restart`      | Restart room execution |

### Logs

| Method | Endpoint                  | Description         |
|:-------|:--------------------------|:--------------------|
| GET    | `/api/warming/logs`       | List all logs       |
| GET    | `/api/warming/logs/:id`   | Get log by ID       |

---

## Worker Config API

Blast outbox worker configuration (requires session cookie):

| Method | Endpoint                                        | Description                    |
|:-------|:------------------------------------------------|:-------------------------------|
| POST   | `/api/blast-outbox/configs`                     | Create worker config           |
| GET    | `/api/blast-outbox/configs`                     | List all configs               |
| GET    | `/api/blast-outbox/configs/:id`                 | Get config by ID               |
| PUT    | `/api/blast-outbox/configs/:id`                 | Update config                  |
| DELETE | `/api/blast-outbox/configs/:id`                 | Delete config                  |
| POST   | `/api/blast-outbox/configs/:id/toggle`          | Toggle config enabled/disabled |
| GET    | `/api/blast-outbox/available-circles`           | List available circles         |
| GET    | `/api/blast-outbox/available-applications`      | List available applications    |

---

## Outbox REST API

External applications can enqueue WhatsApp messages via REST API using API keys instead of direct database access.

### Generate an API Key

Create an API key from the Profile page in the web UI, or via the API:

```http
POST /api/api-keys
Cookie: session={session_cookie}
Content-Type: application/json
```

```json
{
  "name": "My CRM Integration",
  "application": "marketing"
}
```

The raw key (`hwa_...`) is returned once and cannot be retrieved again. Store it securely.

### Enqueue a Single Message

```http
POST /api/outbox/enqueue
X-API-Key: hwa_your_api_key_here
Content-Type: application/json
```

```json
{
  "destination": "905xxxxxxxxx",
  "message": "Hello from my app!",
  "application": "marketing",
  "table_id": "order_12345",
  "file": "https://example.com/receipt.pdf"
}
```

**Response:**

```json
{
  "success": true,
  "message": "Message enqueued",
  "data": { "id_outbox": 42 }
}
```

### Enqueue a Batch

```http
POST /api/outbox/enqueue-batch
X-API-Key: hwa_your_api_key_here
Content-Type: application/json
```

```json
{
  "messages": [
    { "destination": "905xxx", "message": "Message 1", "application": "marketing" },
    { "destination": "905yyy", "message": "Message 2", "application": "marketing" }
  ]
}
```

Maximum 1000 messages per batch.

**Response:**

```json
{
  "success": true,
  "message": "Batch enqueued",
  "data": { "ids": [42, 43], "count": 2 }
}
```

### Check Message Status

```http
GET /api/outbox/status/42
X-API-Key: hwa_your_api_key_here
```

Status codes: `0` = Pending, `1` = Sent, `2` = Failed, `3` = Processing

### List Messages

```http
GET /api/outbox/messages?application=marketing&status=0&page=1&limit=50
X-API-Key: hwa_your_api_key_here
```

---

## WebSocket Events

### Global WebSocket -- System Events (Session Required)

```
ws://{host}:{port}/ws
```

The browser sends the `session` cookie automatically on same-origin WebSocket upgrade — no ticket exchange or query token. Origin is validated against `CORS_ALLOW_ORIGINS`. Non-admin clients only receive events for instances they own (user-scoped filtering via the `user_instances` table).

Monitors QR code generation, login/logout events, connection status changes, and system-wide notifications.

### Instance-Specific WebSocket -- Incoming Messages (Session Required)

```
ws://{host}:{port}/api/listen/{instanceId}
```

Cookie-based auth (same as the global endpoint). Only streams messages for the specified instance, after the access check passes.

**Event payload:**

```json
{
  "event": "incoming_message",
  "timestamp": "2025-12-07T23:22:00Z",
  "data": {
    "instance_id": "instance123",
    "from": "905123456789@s.whatsapp.net",
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
Cookie: session={session_cookie}
Content-Type: application/json
```

```json
{
  "url": "https://your-app.com/wa-webhook",
  "secret": "your-webhook-secret"
}
```

When a secret is configured, Charon signs every outgoing webhook using HMAC-SHA256:

| Detail    | Value                              |
|:----------|:-----------------------------------|
| Header    | `X-Charon-Signature`             |
| Algorithm | HMAC-SHA256                        |
| Message   | Raw HTTP request body              |
| Key       | Instance-specific `webhook_secret` |

**Webhook payload** follows the same format as the WebSocket `incoming_message` event shown above.

### Worker Outbox Callback

When the blast outbox worker processes a message, it sends a webhook callback to the `webhook_url` configured in the worker config:

```json
{
  "event": "outbox.processed",
  "timestamp": "2026-03-30T12:00:00Z",
  "data": {
    "id_outbox": 42,
    "status": 1,
    "status_text": "success",
    "destination": "905xxxxxxxxx",
    "from_number": "905111111111",
    "application": "marketing",
    "table_id": "order_12345",
    "error_msg": ""
  }
}
```

Signed with `X-Charon-Signature` (HMAC-SHA256) if `webhook_secret` is configured.

---

## Admin API

Admin-only endpoints (requires session cookie with `admin` role):

| Method | Endpoint                                     | Description                    |
|:-------|:---------------------------------------------|:-------------------------------|
| GET    | `/api/admin/stats`                           | System-wide statistics         |
| POST   | `/api/admin/users`                           | Create new user (admin only)   |
| GET    | `/api/admin/users`                           | List all users (paginated)     |
| GET    | `/api/admin/users/:id`                       | Get user details               |
| PATCH  | `/api/admin/users/:id`                       | Update user (role, active)     |
| DELETE | `/api/admin/users/:id`                       | Delete user                    |
| GET    | `/api/admin/users/:id/instances`             | List user's assigned instances |
| POST   | `/api/admin/users/:id/instances`             | Assign instance to user        |
| DELETE | `/api/admin/users/:id/instances/:instanceId` | Revoke instance from user      |

Admin self-deletion is blocked. The last remaining admin cannot be deleted.

## API Key Management

Manage API keys for external integrations (requires session cookie):

| Method | Endpoint              | Description                     |
|:-------|:----------------------|:--------------------------------|
| POST   | `/api/api-keys`       | Create a new API key            |
| GET    | `/api/api-keys`       | List all API keys for your user |
| DELETE | `/api/api-keys/:id`   | Revoke and delete an API key    |

API keys use the `X-API-Key` header and are scoped per user. The raw key (`hwa_...32hex`) is shown only once on creation — it is stored as a SHA-256 hash. An optional `application` field locks the key to a specific outbox application.

---

## Contacts API

Contact management endpoints (requires session cookie + instance access):

| Method | Endpoint                                       | Description                       |
|:-------|:-----------------------------------------------|:----------------------------------|
| GET    | `/api/contacts/:instanceId`                    | List contacts (paginated, search) |
| GET    | `/api/contacts/:instanceId/export?format=xlsx` | Export contacts (xlsx or csv)     |
| GET    | `/api/contacts/:instanceId/:jid`               | Contact detail by JID             |
| GET    | `/api/contacts/:instanceId/:jid/mutual-groups` | Mutual groups with contact        |

---

## API Reference

An OpenAPI 3.0 specification is included in `api_docs/openapi.json`.

## Disclaimer

This project is intended for educational and research purposes only. Use at your own risk.

---

[![Go Version](https://img.shields.io/github/go-mod/go-version/KilimcininKorOglu/charon-wa)](https://github.com/KilimcininKorOglu/charon-wa)
[![GitHub issues](https://img.shields.io/github/issues/KilimcininKorOglu/charon-wa)](https://github.com/KilimcininKorOglu/charon-wa/issues)
[![GitHub stars](https://img.shields.io/github/stars/KilimcininKorOglu/charon-wa)](https://github.com/KilimcininKorOglu/charon-wa)
