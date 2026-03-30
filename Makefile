.PHONY: build build-api build-worker clean run fmt vet lint help

BUILD_DIR=bin
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS=-s -w -X 'main.version=$(VERSION)' -X 'main.commit=$(COMMIT)' -X 'main.buildDate=$(BUILD_DATE)'

# Build both binaries for current OS/arch
build: build-api build-worker

build-api:
	@mkdir -p $(BUILD_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/hermeswa .

build-worker:
	@mkdir -p $(BUILD_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/worker ./cmd/worker/

clean:
	rm -rf $(BUILD_DIR)
	go clean

run: build
	./$(BUILD_DIR)/hermeswa

fmt:
	go fmt ./...

vet:
	go vet ./...

lint: fmt vet

help:
	@echo "Available targets:"
	@echo "  build          - Build both binaries (current OS/arch)"
	@echo "  build-api      - Build API server only"
	@echo "  build-worker   - Build worker only"
	@echo "  clean          - Remove build artifacts"
	@echo "  run            - Build and run the API server"
	@echo "  fmt            - Format code"
	@echo "  vet            - Run go vet"
	@echo "  lint           - Run fmt and vet"
	@echo ""
	@echo "Cross-platform builds are handled by GoReleaser (goreleaser-cross)."
	@echo "See: make release-snapshot (dry-run) or push a git tag for full release."
