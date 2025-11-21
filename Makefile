APP_NAME ?= quiz-platform
GOFLAGS ?= -trimpath
GOTESTFLAGS ?= -race -count=1

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: lint
lint:
	golangci-lint run ./...

.PHONY: build
build:
	go build $(GOFLAGS) ./cmd/...

.PHONY: run
run:
	APP_ENV=development go run $(GOFLAGS) ./cmd/api

.PHONY: test
test:
	go test $(GOFLAGS) $(GOTESTFLAGS) ./...

.PHONY: migrate-up
migrate-up:
	go run ./cmd/migrator up

.PHONY: migrate-down
migrate-down:
	go run ./cmd/migrator down

.PHONY: integration-test
integration-test:
	docker compose -f deploy/compose/docker-compose.yml -f deploy/compose/docker-compose.test.yml up -d --remove-orphans
	APP_ENV=test PG_HOST=localhost PG_PORT=5434 PG_DATABASE=quiz_test PG_USER=quiz PG_PASSWORD=quizpass go test $(GOFLAGS) $(GOTESTFLAGS) ./tests/integration/...
	docker compose -f deploy/compose/docker-compose.yml -f deploy/compose/docker-compose.test.yml down -v

.PHONY: compose-up
compose-up:
	docker compose -f deploy/compose/docker-compose.yml up -d --build

.PHONY: compose-down
compose-down:
	docker compose -f deploy/compose/docker-compose.yml down -v

.PHONY: compose-logs
compose-logs:
	docker compose -f deploy/compose/docker-compose.yml logs -f app

