.PHONY: tools generate frontend build dev clean \
       build-linux-amd64 build-darwin-arm64 build-windows-amd64 build-all

BINARY   := media-gate
DIST_DIR := dist

## tools: Install required Go dev tools (air, oapi-codegen)
tools:
	go install github.com/air-verse/air@latest
	go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest

## generate: Run oapi-codegen (Go) and openapi-typescript (frontend) code generation
generate:
	go generate ./...
	cd frontend && npm run generate:api

## frontend: Build the Vue SPA into frontend/dist/
frontend:
	cd frontend && npm ci && npm run build

## build: Full build — generate code, build frontend, compile Go binary
build: generate frontend
	go build -o $(BINARY) ./cmd/server/

## dev: Start Air (Go backend with hot-reload) and Vite (frontend dev server) in parallel
dev:
	@if ! command -v air >/dev/null 2>&1; then \
		echo "air not found. Run 'make tools' to install dev dependencies."; \
		exit 1; \
	fi
	@trap 'kill 0' EXIT; \
	air & \
	cd frontend && npm run dev & \
	wait

## clean: Remove build artifacts
clean:
	rm -f $(BINARY)
	rm -rf frontend/dist tmp/ $(DIST_DIR)/

## build-linux-amd64: Cross-compile prod binary for Linux x86_64
build-linux-amd64:
	docker build -f Dockerfile.build \
		--build-arg TARGETOS=linux --build-arg TARGETARCH=amd64 \
		--output $(DIST_DIR)/ .
	@mv $(DIST_DIR)/media-gate $(DIST_DIR)/media-gate-linux-amd64

## build-darwin-arm64: Cross-compile prod binary for macOS Apple Silicon
build-darwin-arm64:
	docker build -f Dockerfile.build \
		--build-arg TARGETOS=darwin --build-arg TARGETARCH=arm64 \
		--output $(DIST_DIR)/ .
	@mv $(DIST_DIR)/media-gate $(DIST_DIR)/media-gate-darwin-arm64

## build-windows-amd64: Cross-compile prod binary for Windows x86_64
build-windows-amd64:
	docker build -f Dockerfile.build \
		--build-arg TARGETOS=windows --build-arg TARGETARCH=amd64 \
		--output $(DIST_DIR)/ .
	@mv $(DIST_DIR)/media-gate.exe $(DIST_DIR)/media-gate-windows-amd64.exe

## build-all: Build prod binaries for all platforms
build-all: build-linux-amd64 build-darwin-arm64 build-windows-amd64
