.PHONY: build build-api build-worker build-linux build-windows build-darwin build-all clean run dev dev-build fmt vet lint check-zig help

BUILD_DIR=bin
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS=-s -w -X 'main.version=$(VERSION)' -X 'main.commit=$(COMMIT)' -X 'main.buildDate=$(BUILD_DATE)'

# Build both binaries for current OS/arch
build: build-api build-worker

build-api:
	@mkdir -p $(BUILD_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/charon .

build-worker:
	@mkdir -p $(BUILD_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/worker ./cmd/worker/

# Cross-platform builds (requires zig: brew install zig)
check-zig:
	@which zig > /dev/null 2>&1 || (echo "ERROR: zig is required for cross-compilation. Install with: brew install zig" && exit 1)

build-linux: check-zig
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/charon_linux_amd64 .
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/worker_linux_amd64 ./cmd/worker/
	CGO_ENABLED=1 GOOS=linux GOARCH=arm64 CC="zig cc -target aarch64-linux" CXX="zig c++ -target aarch64-linux" go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/charon_linux_arm64 .
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/worker_linux_arm64 ./cmd/worker/

build-windows: check-zig
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC="zig cc -target x86_64-windows" CXX="zig c++ -target x86_64-windows" go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/charon_windows_amd64.exe .
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/worker_windows_amd64.exe ./cmd/worker/

build-darwin: check-zig
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 CC="zig cc -target x86_64-macos" CXX="zig c++ -target x86_64-macos" go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/charon_darwin_amd64 .
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/worker_darwin_amd64 ./cmd/worker/
	CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 CC="zig cc -target aarch64-macos" CXX="zig c++ -target aarch64-macos" go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/charon_darwin_arm64 .
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/worker_darwin_arm64 ./cmd/worker/

build-all: build-linux build-windows build-darwin

clean:
	rm -rf $(BUILD_DIR)
	go clean

run: build
	./$(BUILD_DIR)/charon

# Local development: the host compiles the Linux binaries, the containers only
# run them. DEV_ARCH must match the Docker engine architecture.
DEV_ARCH ?= arm64

# The API links against musl, because the dev containers run on alpine.
dev-build: check-zig
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=1 GOOS=linux GOARCH=$(DEV_ARCH) CC="zig cc -target aarch64-linux-musl" CXX="zig c++ -target aarch64-linux-musl" go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/charon_linux_$(DEV_ARCH) .
	CGO_ENABLED=0 GOOS=linux GOARCH=$(DEV_ARCH) go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/worker_linux_$(DEV_ARCH) ./cmd/worker/

# Rebuild on the host, then restart the containers that run the binaries.
dev: dev-build
	docker compose -f docker-compose.local.yml restart api worker

fmt:
	go fmt ./...

vet:
	go vet ./...

test:
	go test ./internal/... -v -count=1

lint: fmt vet

help:
	@echo "Available targets:"
	@echo "  build          - Build both binaries (current OS/arch)"
	@echo "  build-api      - Build API server only"
	@echo "  build-worker   - Build worker only"
	@echo "  build-linux    - Cross-compile for Linux (amd64 + arm64)"
	@echo "  build-windows  - Cross-compile for Windows (amd64)"
	@echo "  build-darwin   - Cross-compile for macOS (amd64 + arm64)"
	@echo "  build-all      - Cross-compile for all platforms"
	@echo "  clean          - Remove build artifacts"
	@echo "  run            - Build and run the API server"
	@echo "  dev-build      - Cross-compile the Linux dev binaries on the host"
	@echo "  dev            - dev-build, then restart the api and worker containers"
	@echo "  fmt            - Format code"
	@echo "  vet            - Run go vet"
	@echo "  lint           - Run fmt and vet"
	@echo ""
	@echo "Cross-compilation requires zig: brew install zig"
	@echo "CI releases use goreleaser-cross (no zig needed)."
