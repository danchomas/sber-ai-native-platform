# Сборка и запуск платформы.
BINARY      := bin/aibi
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS     := -s -w -X main.version=$(VERSION)
TEST_PG_DSN ?= postgres://aibi:aibi@localhost:5432/aibi_test?sslmode=disable
LLM_MODEL   ?= qwen2.5-coder:7b

.DEFAULT_GOAL := help

.PHONY: help
help: ## Список целей
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Собрать бинарник API
	@mkdir -p bin
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/api

.PHONY: run
run: ## Запустить API локально (интерфейс — отдельно, make front)
	go run ./cmd/api

.PHONY: front
front: ## Запустить интерфейс в режиме разработки на :5173
	cd frontend && npm install && npm run dev

.PHONY: front-build
front-build: ## Собрать интерфейс
	cd frontend && npm ci && npm run build

.PHONY: front-test
front-test: ## Тесты интерфейса
	cd frontend && npm ci && npm test

.PHONY: front-shots
front-shots: ## Снимки интерфейса на разных ширинах (нужен поднятый стенд)
	cd frontend && mkdir -p .screenshots && npm run shots -- http://localhost:3000/ .screenshots

.PHONY: migrate
migrate: ## Применить миграции (контейнер migrate поверх ./migrations)
	docker compose run --rm migrate

.PHONY: migrate-down
migrate-down: ## Откатить одну миграцию
	docker compose run --rm migrate -path=/migrations \
		-database=postgres://aibi:aibi@postgres:5432/aibi?sslmode=disable down 1

.PHONY: migrate-new
migrate-new: ## Создать пару файлов миграции: make migrate-new NAME=add_x
	@test -n "$(NAME)" || (echo "укажите NAME, например: make migrate-new NAME=add_x" && exit 1)
	docker compose run --rm --entrypoint migrate migrate \
		create -ext sql -dir /migrations -seq $(NAME)

.PHONY: test
test: ## Юнит-тесты (Go)
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
up: ## Поднять стенд (PostgreSQL + API + интерфейс на :3000)
	docker compose up -d --build

.PHONY: up-llm
up-llm: ## Поднять стенд вместе с локальной моделью
	docker compose --profile llm up -d --build
	@echo "Жду готовности Ollama…"
	@until docker compose exec -T ollama ollama list >/dev/null 2>&1; do sleep 2; done
	@echo "Тяну модель $(LLM_MODEL) — это долго при первом запуске."
	docker compose exec -T ollama ollama pull $(LLM_MODEL)

.PHONY: down
down: ## Остановить стенд
	docker compose down

.PHONY: logs
logs: ## Логи API
	docker compose logs -f api

.PHONY: logs-front
logs-front: ## Логи интерфейса
	docker compose logs -f frontend

.PHONY: psql
psql: ## Консоль PostgreSQL стенда
	docker compose exec postgres psql -U aibi -d aibi
