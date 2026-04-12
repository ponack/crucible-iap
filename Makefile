.PHONY: help dev build test lint clean docker-up docker-down migrate release

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -s -w -X github.com/ponack/crucible-iap/internal/server.version=$(VERSION)

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

# ── Development ───────────────────────────────────────────────────────────────

dev-deps: ## Start dev dependencies (postgres, minio)
	docker compose -f deploy/docker-compose.dev.yml up -d

dev-api: ## Run API in development mode
	cd api && go run ./cmd/crucible-iap

dev-ui: ## Run UI in development mode
	cd ui && pnpm dev

# ── Build ─────────────────────────────────────────────────────────────────────

build-api: ## Build API binary (version injected from git tag)
	cd api && go build -ldflags="$(LDFLAGS)" -o bin/crucible-iap ./cmd/crucible-iap

build-ui: ## Build UI for production
	cd ui && pnpm build

build: build-api build-ui ## Build everything

# ── Test ──────────────────────────────────────────────────────────────────────

test-api: ## Run API tests
	cd api && go test ./...

test-ui: ## Run UI tests
	cd ui && pnpm test

test: test-api test-ui ## Run all tests

# ── Lint ──────────────────────────────────────────────────────────────────────

lint-api: ## Lint Go code
	cd api && golangci-lint run ./...

lint-ui: ## Lint UI code
	cd ui && pnpm lint

lint: lint-api lint-ui ## Lint everything

# ── Database ──────────────────────────────────────────────────────────────────

migrate: ## Run database migrations
	cd api && go run ./cmd/crucible-iap migrate

migrate-down: ## Roll back last migration
	cd api && go run ./cmd/crucible-iap migrate --down

# ── Docker ────────────────────────────────────────────────────────────────────

docker-up: ## Start full stack with Docker Compose
	docker network create crucible-runner 2>/dev/null || true
	docker compose up -d

docker-down: ## Stop full stack
	docker compose down

docker-build: ## Build Docker images (version injected from git tag)
	docker compose build --build-arg VERSION=$(VERSION)

docker-logs: ## Follow logs
	docker compose logs -f

# ── Release ───────────────────────────────────────────────────────────────────

release: build ## Build everything with version stamp (use after git tag)
	@echo "Built $(VERSION)"

# ── Cleanup ───────────────────────────────────────────────────────────────────

clean: ## Remove build artifacts
	rm -rf api/bin ui/build ui/.svelte-kit
