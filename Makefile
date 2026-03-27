.PHONY: tools generate frontend build dev clean

BINARY := media-gate

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
	rm -rf frontend/dist tmp/
