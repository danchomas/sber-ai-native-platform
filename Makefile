# Сборка и запуск платформы.
BINARY      := bin/aibi
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS     := -s -w -X main.version=$(VERSION)
TEST_PG_DSN ?= postgres://aibi:aibi@localhost:5432/aibi_test?sslmode=disable

.DEFAULT_GOAL := help

.PHONY: help
help: ## Список целей
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Собрать бинарники
	@mkdir -p bin
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/api
	go build -trimpath -ldflags "-s -w" -o bin/aibi-migrate ./cmd/migrate

.PHONY: run
run: ## Запустить API локально
	go run ./cmd/api

.PHONY: migrate
migrate: ## Применить миграции
	go run ./cmd/migrate

.PHONY: test
test: ## Юнит-тесты
	go test -race ./...

.PHONY: test-integration
test-integration: ## Интеграционные тесты (нужен PostgreSQL)
	TEST_DATABASE_URL="$(TEST_PG_DSN)" go test -race -tags=integration ./internal/integration/... -v

.PHONY: cover
cover: ## Покрытие тестами
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

.PHONY: fmt
fmt: ## Формат и статический анализ
	gofmt -l -w .
	go vet ./...

.PHONY: tidy
tidy: ## Привести go.mod в порядок
	go mod tidy

.PHONY: up
up: ## Поднять стенд (PostgreSQL + API)
	docker compose up -d --build

.PHONY: up-llm
up-llm: ## Поднять стенд вместе с локальной моделью
	docker compose --profile llm up -d --build

.PHONY: down
down: ## Остановить стенд
	docker compose down

.PHONY: logs
logs: ## Логи API
	docker compose logs -f api

.PHONY: psql
psql: ## Консоль PostgreSQL стенда
	docker compose exec postgres psql -U aibi -d aibi
