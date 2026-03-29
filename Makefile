.PHONY: help dev build test lint clean docker-up docker-down migrate

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

build-api: ## Build API binary
	cd api && go build -o bin/crucible-iap ./cmd/crucible-iap

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
	docker compose -f deploy/docker-compose.yml up -d

docker-down: ## Stop full stack
	docker compose -f deploy/docker-compose.yml down

docker-build: ## Build Docker images
	docker compose -f deploy/docker-compose.yml build

docker-logs: ## Follow logs
	docker compose -f deploy/docker-compose.yml logs -f

# ── Cleanup ───────────────────────────────────────────────────────────────────

clean: ## Remove build artifacts
	rm -rf api/bin ui/build ui/.svelte-kit
