# Changelog

## [1.2.2] - 2026-04-04

### Fixed
- Use SSRFSafeDialContext in incoming message webhook delivery to prevent DNS rebinding attacks
- Use SSRFSafeDialContext in worker outbox webhook delivery
- Restrict instance creation to admin and user roles (block viewer)
- Block viewer role from write operations in RequireInstanceAccess and RequirePhoneNumberAccess
- Restrict API key creation and worker config writes to user and admin roles
- Add X-Content-Type-Options nosniff and Content-Security-Policy headers to uploads route
- Serve SVG uploads with Content-Disposition attachment to prevent stored XSS
- Validate system identity old-file path stays within uploads directory before deletion
- Make instance creation limit check atomic with PostgreSQL advisory lock
- Make daily outbox limit check atomic with advisory lock; fail closed on DB error
- Require room ownership for warming logs with null created_by
- Validate room ownership before filtering warming logs by room_id
- Enforce script ownership check in warming room creation
- Scope available-circles and available-applications queries to the requesting user

## [1.2.1] - 2026-04-04

### Added
- Production docker-compose for Coolify deployment with external network and named volumes

### Changed
- Renamed `docker-compose.yml` to `docker-compose.local.yml` for local development
- Updated deployment and authentication documentation
- Added API key management section to API docs

### Fixed
- Use `127.0.0.1` instead of `localhost` for Docker healthcheck (Alpine IPv6 resolution issue)
- Handle nested spintax braces correctly with inside-out processing
- Store rotated refresh token correctly in outbox worker

## [1.2.0] - 2026-03-30

### Added
- API key authentication system for external application integrations (SHA-256 hashed)
- Outbox REST API for external message enqueueing (single + batch, max 1000)
- Outbox monitoring page with filters, pagination, and detail panel
- API key management section in profile page
- Initial test infrastructure with phone, spintax, and API key tests
- Shared typing delay helper (ApplyTypingDelay) replacing 13 duplicate blocks

### Fixed
- SQL injection in worker application filter (parameterized queries)
- Path traversal in file manager prefix check
- Dashboard connected instances count using wrong column name
- JWT algorithm validation (HS256 only) to prevent algorithm confusion attacks
- Per-user token invalidation on account disable
- Data race on shared worker API client auth fields (mutex protection)
- Shutdown cancelled context for database writes (background context with timeout)
- Worker stuck messages at status 3 on error paths
- API key last_used_at cancelled context for async update
- X-API-Key missing from CORS allowed headers
- WebSocket /ws endpoint now requires JWT auth and validates origin
- Refresh token rotation on each use (consumed and replaced)
- Refresh token moved from localStorage to in-memory storage
- BlacklistAllUserTokens broken due to incorrect int-to-string conversion
- WebSocket reconnect loop after logout
- NULL circle crash in GetAvailableCircles query
- Superadmin dead role check in outbox handler
- CORS wildcard when CORS_ALLOW_ORIGINS env unset (now fatal)
- Outbox error_count never incremented on failure
- Warming lastReplyTime memory leak on room finish/delete

### Changed
- Dropped PM2 support in favor of Docker-only deployment
- Config values (typing delay, feature flags) read at startup, not per-request
- Worker graceful shutdown with WaitGroup synchronization
- Resolved instance ID stored in middleware context to avoid duplicate DB queries
- Removed dead code: unused audit log functions, Config struct, instancesCall variable, FetchPendingOutbox/UpdateOutboxStatus
- Removed debug print statements from production code

## [1.1.0] - 2026-03-30

### Added
- By-phone-number message sending mode with toggle in Messages page
- Group listing in phone mode via GET /api/groups/by-number endpoint
- Script edit side panel in warming page (title, description, category)
- Room edit side panel with full AI configuration support
- Template edit side panel with JSON structure editing
- Warming log detail side panel with full message and error info
- Per-instance status refresh button in instance detail panel
- Blast config edit side panel with webhook URL/secret
- Standalone Contacts page with paginated table, detail panel, mutual groups, XLSX/CSV export
- Instance detail panel with edit form, device info, and webhook config
- Messages page rewrite with contacts/groups tabs, media send (file+URL), group messaging, number check
- Warming system wizard (3-step room create), inline script lines (add/edit/delete/AI gen/reorder), templates tab
- Auto-seed admin user (admin/admin123) on startup if none exists

### Fixed
- API response parsing for instances, warming rooms, and scripts (nested objects not flat arrays)
- Blast outbox interval field name (interval_seconds not interval_min_seconds)
- Vite proxy bypass for GET /login route conflict with SPA
- Docker web dev port changed to 5174 to avoid conflict
- Docker Vite proxy target uses service name (api:2121) not localhost
- Docker init-db.sql made idempotent with pg_database check
- Healthchecks added to all Docker containers

### Changed
- WarmingRoom type extended with AI and reply delay fields
- npm dependencies updated

## [1.0.0] - 2026-03-30

### Added
- WhatsApp multi-instance automation REST API (Go + Echo v4 + whatsmeow)
- JWT authentication with access/refresh tokens
- WebSocket real-time events (QR, status changes, incoming messages)
- WhatsApp warming system with BOT_VS_BOT and HUMAN_VS_BOT modes
- Google Gemini AI integration for warming conversations
- Standalone outbox worker for blast messaging with atomic claiming
- Admin API endpoints for user management and instance assignment
- File manager API for uploads directory browsing and deletion
- Dashboard stats API for system-wide metrics
- React 19 web UI with cyberpunk dark theme (TailwindCSS v4)
- Login/Register pages with JWT auth flow
- Dashboard with live WebSocket event feed and admin stats
- Instance management with real-time QR code scanning
- Chat-like messaging UI with per-instance WebSocket listeners
- File manager with breadcrumbs, preview panel, and admin delete
- Warming system UI (rooms, scripts, logs) with play/pause/stop controls
- Blast outbox worker config management with circle/app selectors
- Admin user management with role change and instance assignment
- Profile page with avatar upload and password change
- System identity settings with company info and logo uploads
- Docker development environment (PostgreSQL + API + Worker + Web)
- Production Dockerfile with 3-stage build (Node + Go + Debian runtime)
- SPA static serve from Go binary (web/dist catch-all)
- Cross-platform build support via zig CC and goreleaser-cross

### Changed
- Replaced chai2010/webp (abandoned) with vegidio/webp-go (active, bundled static libs)
- Removed MySQL support, PostgreSQL-only for all databases
- Adapted build tooling from btk-sorgu to hermeswa project
- Docker volumes use docker-data/ bind mounts instead of named volumes
- README rewritten with professional emoji-free design
