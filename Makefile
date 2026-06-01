.PHONY: all build proto sqlc web test lint migrate dev-master dev-agent clean release

# ─── Build targets ────────────────────────────────────────────────────────────

LDFLAGS := -X github.com/egt/orkestra/internal/shared/version.Version=$(shell git describe --tags --always 2>/dev/null || echo dev) \
           -X github.com/egt/orkestra/internal/shared/version.Commit=$(shell git rev-parse --short HEAD 2>/dev/null || echo unknown) \
           -X github.com/egt/orkestra/internal/shared/version.BuildDate=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)

all: proto sqlc web build

build: ## Build both binaries (embeds web/dist if present)
	go build -ldflags "$(LDFLAGS)" -o bin/orkestra-master ./cmd/orkestra-master
	go build -ldflags "$(LDFLAGS)" -o bin/orkestra-agent  ./cmd/orkestra-agent

build-dev: ## Build without web embedding (dev tag)
	go build -tags dev -ldflags "$(LDFLAGS)" -o bin/orkestra-master ./cmd/orkestra-master
	go build -tags dev -ldflags "$(LDFLAGS)" -o bin/orkestra-agent  ./cmd/orkestra-agent

# ─── Code generation ──────────────────────────────────────────────────────────

proto: ## Regenerate Go + TypeScript from .proto files
	buf generate

sqlc: ## Regenerate Go DB layer from SQL queries
	sqlc generate

web: ## Build React SPA into web/dist/ (embedded in master binary)
	cd web && npm ci && npm run build

# ─── Testing & quality ────────────────────────────────────────────────────────

test: ## Run unit tests
	go test ./...

test-integration: ## Run integration tests (requires Docker daemon)
	go test -tags integration -timeout 5m ./...

lint: ## Run Go linter + buf lint
	golangci-lint run ./...
	buf lint

vet: ## Run go vet
	go vet ./...

# ─── Database ─────────────────────────────────────────────────────────────────

MIGRATE_DSN ?= orkestra-dev.db

migrate: ## Apply pending migrations (uses MIGRATE_DSN env var or orkestra-dev.db)
	goose -dir internal/master/store/migrations sqlite3 "$(MIGRATE_DSN)" up

migrate-down: ## Roll back last migration
	goose -dir internal/master/store/migrations sqlite3 "$(MIGRATE_DSN)" down

migrate-status: ## Show migration status
	goose -dir internal/master/store/migrations sqlite3 "$(MIGRATE_DSN)" status

# ─── Development ──────────────────────────────────────────────────────────────

dev-master: build-dev ## Run Master with dev proxy to Vite (:5173)
	./bin/orkestra-master --log-level debug

dev-agent: build-dev ## Run Agent (pointed at local Master)
	./bin/orkestra-agent serve --log-level debug \
		--data-dir /tmp/orkestra-agent-dev

# ─── Release ──────────────────────────────────────────────────────────────────

release: ## Build release binaries + Docker images via goreleaser
	goreleaser release --clean

release-snapshot: ## Dry-run release (no publish)
	goreleaser release --snapshot --clean

# ─── Housekeeping ─────────────────────────────────────────────────────────────

clean: ## Remove build artifacts
	rm -rf bin/ web/dist/ dist/

deps: ## Download all Go dependencies
	go mod download
	go mod tidy

deps-web: ## Install web dependencies
	cd web && npm ci

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'
