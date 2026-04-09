# ============================================
# Stage 1: Frontend Build
# ============================================
FROM node:22.15.0-alpine3.21 AS frontend

WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm install
COPY web/ .
RUN npm run build

# ============================================
# Stage 2: Go Build
# ============================================
FROM golang:1.26.2-bookworm AS builder

WORKDIR /src

# Install CGO build dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc libc6-dev \
    && rm -rf /var/lib/apt/lists/*

# Cache Go modules
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .

ENV CGO_ENABLED=1
RUN go build -ldflags "-s -w" -o /out/charon .
RUN go build -ldflags "-s -w" -o /out/worker ./cmd/worker/

# ============================================
# Stage 3: Runtime
# ============================================
FROM debian:bookworm-20250317-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy binaries
COPY --from=builder /out/charon /app/charon
COPY --from=builder /out/worker /app/worker

# Copy frontend build
COPY --from=frontend /web/dist /app/web/dist

# Copy static assets
COPY uploads/ /app/uploads/
COPY .env.example /app/.env.example

# Create uploads directory and non-root user
RUN mkdir -p /app/uploads/avatars /app/uploads/system \
    && useradd --no-create-home --shell /bin/false charon \
    && chown -R charon:charon /app

USER charon

EXPOSE 2121

HEALTHCHECK --interval=10s --timeout=3s --retries=3 \
    CMD curl -sf http://localhost:2121/ || exit 1

CMD ["./charon"]
