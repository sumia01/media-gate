.PHONY: tools generate frontend build dev clean \
        harness-up harness-status harness-logs harness-smoke harness-complete \
        harness-error harness-reset-fakes harness-down harness-destroy harness-ci \
        build-linux-amd64 build-darwin-arm64 build-windows-amd64 build-all

BINARY   := media-gate
DIST_DIR := dist
HARNESS_ID ?= default
HARNESS_MODE ?= local
SERVICE ?= all

## tools: Install required Go dev tools (air, oapi-codegen)
tools:
	cd backend && go install github.com/air-verse/air@latest
	cd backend && go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.7.0

## generate: Run oapi-codegen (Go) and openapi-typescript (frontend) code generation
generate:
	cd backend && go generate ./...
	cd frontend && npm run generate:api

## frontend: Build the Vue SPA into frontend/dist/ and copy to backend embed location
frontend:
	cd frontend && npm ci && npm run build
	rm -rf backend/frontend/dist
	cp -r frontend/dist backend/frontend/dist

## build: Full build — generate code, build frontend, compile Go binary
build: generate frontend
	cd backend && go build -ldflags "-X main.version=$$(git describe --tags --always --dirty 2>/dev/null || echo dev)" -o ../$(BINARY) ./cmd/server/

## dev: Start Air (Go backend with hot-reload) and Vite (frontend dev server) in parallel
dev:
	@if ! command -v air >/dev/null 2>&1; then \
		echo "air not found. Run 'make tools' to install dev dependencies."; \
		exit 1; \
	fi
	@trap 'kill 0' EXIT; \
	cd backend && air & \
	cd frontend && npm run dev & \
	wait

## harness-up: Start an isolated disposable instance (HARNESS_ID=name, HARNESS_MODE=local|ci)
harness-up:
	HARNESS_ID="$(HARNESS_ID)" HARNESS_MODE="$(HARNESS_MODE)" ./harness/harness.sh up

## harness-status: Show status and URLs for an isolated instance
harness-status:
	HARNESS_ID="$(HARNESS_ID)" ./harness/harness.sh status

## harness-logs: Read instance logs (SERVICE=all|backend|frontend|fakes, FOLLOW=1)
harness-logs:
	HARNESS_ID="$(HARNESS_ID)" FOLLOW="$(FOLLOW)" ./harness/harness.sh logs "$(SERVICE)"

## harness-smoke: Run deterministic API, torrent, and import smoke tests
harness-smoke:
	HARNESS_ID="$(HARNESS_ID)" ./harness/harness.sh smoke

harness-complete:
	HARNESS_ID="$(HARNESS_ID)" ./harness/harness.sh complete

harness-error:
	HARNESS_ID="$(HARNESS_ID)" ./harness/harness.sh error

harness-reset-fakes:
	HARNESS_ID="$(HARNESS_ID)" ./harness/harness.sh reset-fakes

## harness-down: Stop an instance while preserving its data and logs
harness-down:
	HARNESS_ID="$(HARNESS_ID)" ./harness/harness.sh down

## harness-destroy: Stop and delete an isolated instance
harness-destroy:
	HARNESS_ID="$(HARNESS_ID)" ./harness/harness.sh destroy

## harness-ci: Build and run the disposable release-gate smoke test
harness-ci:
	HARNESS_ID="$(HARNESS_ID)" ./harness/harness.sh ci

## clean: Remove build artifacts
clean:
	rm -f $(BINARY)
	rm -rf frontend/dist backend/frontend/dist backend/tmp/ $(DIST_DIR)/

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
