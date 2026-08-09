SHELL := /bin/sh

.DEFAULT_GOAL := help

GO ?= go
COMPOSE ?= docker compose
ENV_FILE ?= .env
BIN_DIR ?= bin
APP_BINARY ?= $(BIN_DIR)/app
MIGRATE_BINARY ?= $(BIN_DIR)/migrate
HEALTHCHECK_BINARY ?= $(BIN_DIR)/healthcheck
GOLANGCI_LINT ?= golangci-lint
GOVULNCHECK_VERSION ?= v1.6.0

.PHONY: help env-check fmt fmt-check tidy-check verify vet test test-race lint vuln build check template-smoke \
	compose-config compose-build compose-up compose-down compose-logs logs migrate migrate-up \
	migrate-down migrate-version migrate-step

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z0-9_-]+:.*##/ { printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

env-check: ## Check that the Compose environment file exists
	@test -f "$(ENV_FILE)" || { \
		echo "Missing $(ENV_FILE). Copy .env.example to $(ENV_FILE) first." >&2; \
		exit 1; \
	}

fmt: ## Format all Go packages
	$(GO) fmt ./...

fmt-check: ## Fail when Go files are not gofmt-formatted
	@unformatted="$$(git ls-files --cached --others --exclude-standard -z -- '*.go' | xargs -0 gofmt -l)"; \
	if [ -n "$$unformatted" ]; then \
		echo "Unformatted Go files:" >&2; \
		echo "$$unformatted" >&2; \
		exit 1; \
	fi

tidy-check: ## Verify that go.mod and go.sum need no tidy changes
	$(GO) mod tidy -diff

verify: ## Verify downloaded module checksums
	$(GO) mod verify

vet: ## Run go vet across all packages
	$(GO) vet ./...

test: ## Run unit tests with coverage
	$(GO) test -shuffle=on -cover ./...

test-race: ## Run the test suite with the race detector
	CGO_ENABLED=1 $(GO) test -race -shuffle=on ./...

lint: ## Run golangci-lint (v2 configuration)
	$(GOLANGCI_LINT) run --timeout=5m ./...

vuln: ## Scan reachable Go code for known vulnerabilities
	GOTOOLCHAIN="$$($(GO) env GOVERSION)" GOFLAGS= $(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

build: ## Build the application and migration binaries
	mkdir -p "$(BIN_DIR)"
	CGO_ENABLED=0 $(GO) build -trimpath -buildvcs=false -o "$(APP_BINARY)" ./cmd/app
	CGO_ENABLED=0 $(GO) build -trimpath -buildvcs=false -o "$(MIGRATE_BINARY)" ./cmd/migrate
	CGO_ENABLED=0 $(GO) build -trimpath -buildvcs=false -o "$(HEALTHCHECK_BINARY)" ./cmd/healthcheck

check: fmt-check tidy-check verify vet test test-race lint vuln build ## Run the complete local quality gate

template-smoke: ## Exercise bootstrap in a disposable downstream repository
	bash scripts/template-smoke.sh

compose-config: env-check ## Validate the canonical Compose file
	$(COMPOSE) --env-file "$(ENV_FILE)" config --quiet

compose-build: env-check ## Build the application image (also used by the migration job)
	$(COMPOSE) --env-file "$(ENV_FILE)" build app

compose-up: env-check ## Start PostgreSQL, apply migrations, and run the app
	$(COMPOSE) --env-file "$(ENV_FILE)" up --build --force-recreate --detach app

compose-down: env-check ## Stop Compose services (keeps the database volume)
	$(COMPOSE) --env-file "$(ENV_FILE)" down --remove-orphans

compose-logs: env-check ## Follow application and database logs
	$(COMPOSE) --env-file "$(ENV_FILE)" logs --follow --tail=200 app migrate postgres

logs: compose-logs ## Alias for compose-logs

migrate: migrate-up ## Apply pending database migrations

migrate-up: compose-build ## Apply pending database migrations in the Compose job
	$(COMPOSE) --env-file "$(ENV_FILE)" run --rm migrate up

migrate-down: compose-build ## Roll back one migration (requires CONFIRM=1)
	@test "$(CONFIRM)" = "1" || { \
		echo "Refusing destructive migration-down; rerun with CONFIRM=1." >&2; \
		exit 1; \
	}
	$(COMPOSE) --env-file "$(ENV_FILE)" run --rm migrate down

migrate-version: compose-build ## Show the current migration version
	$(COMPOSE) --env-file "$(ENV_FILE)" run --rm migrate version

migrate-step: compose-build ## Apply or roll back N migrations (set STEP=N)
	@test -n "$(STEP)" || { \
		echo "Set STEP to a non-zero integer, for example: make migrate-step STEP=1" >&2; \
		exit 1; \
	}
	$(COMPOSE) --env-file "$(ENV_FILE)" run --rm migrate step "$(STEP)"
