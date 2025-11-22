APP_NAME ?= quiz-platform
GOFLAGS ?= -trimpath
GOTESTFLAGS ?= -race -count=1

DEV_COMPOSE = deploy/compose/docker-compose.dev.yml
PROD_COMPOSE = deploy/compose/docker-compose.prod.yml

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: sqlc-generate
sqlc-generate:
	@echo "Generating sqlc code..."
	@export PATH=$$PATH:$$(go env GOPATH)/bin && sqlc generate

.PHONY: sqlc-install
sqlc-install:
	@echo "Installing sqlc..."
	@go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest

.PHONY: migrate-up
migrate-up:
	@echo "Running database migrations..."
	@if [ -f .env ]; then \
		export $$(grep -v '^#' .env | xargs) && ./migrator up; \
	else \
		echo "Error: .env file not found. Copy configs/env.example to .env and configure it."; \
		exit 1; \
	fi

.PHONY: migrate-down
migrate-down:
	@echo "Rolling back database migrations..."
	@if [ -f .env ]; then \
		export $$(grep -v '^#' .env | xargs) && ./migrator -command=down; \
	else \
		echo "Error: .env file not found. Copy configs/env.example to .env and configure it."; \
		exit 1; \
	fi

.PHONY: migrate-status
migrate-status:
	@echo "Checking migration status..."
	@if [ -f .env ]; then \
		export $$(grep -v '^#' .env | xargs) && ./migrator -command=status; \
	else \
		echo "Error: .env file not found. Copy configs/env.example to .env and configure it."; \
		exit 1; \
	fi

.PHONY: lint
lint:
	golangci-lint run ./...

.PHONY: build
build:
	go build $(GOFLAGS) ./cmd/...

.PHONY: run-local
run-local:
	APP_ENV=development go run $(GOFLAGS) ./cmd/api

# -----------------------------
# Docker Compose (Development)
# -----------------------------

.PHONY: compose-dev-up
compose-dev-up:
	docker compose -f $(DEV_COMPOSE) up -d --build

.PHONY: compose-dev-down
compose-dev-down:
	docker compose -f $(DEV_COMPOSE) down -v

.PHONY: compose-dev-logs
compose-dev-logs:
	docker compose -f $(DEV_COMPOSE) logs -f app

# -----------------------------
# Docker Compose (Production)
# -----------------------------

.PHONY: compose-prod-up
compose-prod-up:
	docker compose -f $(PROD_COMPOSE) up -d --build

.PHONY: compose-prod-down
compose-prod-down:
	docker compose -f $(PROD_COMPOSE) down -v

.PHONY: compose-prod-logs
compose-prod-logs:
	docker compose -f $(PROD_COMPOSE) logs -f app

.PHONY: wrapper-up
wrapper-up:
	docker compose -f deploy/compose/docker-compose.yml up -d --build wrapper

.PHONY: wrapper-logs
wrapper-logs:
	docker compose -f deploy/compose/docker-compose.yml logs -f wrapper

# -----------------------------
# Integration Tests
# -----------------------------

.PHONY: integration-test
integration-test:
	docker compose -f $(DEV_COMPOSE) -f deploy/compose/docker-compose.test.yml up -d --remove-orphans
	APP_ENV=test PG_HOST=localhost PG_PORT=5434 PG_DATABASE=quiz_test PG_USER=quiz PG_PASSWORD=quizpass \
	go test $(GOFLAGS) $(GOTESTFLAGS) ./tests/integration/...
	docker compose -f $(DEV_COMPOSE) -f deploy/compose/docker-compose.test.yml down -v
