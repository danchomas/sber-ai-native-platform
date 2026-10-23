# Сборка и запуск платформы.
BINARY      := bin/aibi
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS     := -s -w -X main.version=$(VERSION)
# Настройки берутся из .env — того же файла, что читает docker compose.
# Зашитых учётных данных в проекте нет ни одного места.
-include .env
export

# Строка подключения к тестовой базе собирается из тех же переменных.
TEST_PG_DSN ?= postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@localhost:$(POSTGRES_PORT)/$(POSTGRES_DB)_test?sslmode=disable
COMPOSE_DSN := postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@postgres:5432/$(POSTGRES_DB)?sslmode=disable

.DEFAULT_GOAL := help

.PHONY: env
env: ## Создать .env из шаблона, если его нет
	@test -f .env || (cp .env.example .env && echo "создан .env из .env.example")
	@test -f .env && echo ".env на месте"

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
		-database=$(COMPOSE_DSN) down 1

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
up: env ## Поднять стенд (PostgreSQL + API + интерфейс)
	docker compose up -d --build

.PHONY: up-llm
up-llm: env ## Поднять стенд вместе с локальной моделью
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

.PHONY: datasets
datasets: ## Загрузить наборы из ./datasets (можно указать: make datasets ONLY="01 09")
	scripts/load-datasets.sh $(ONLY)

.PHONY: psql
psql: ## Консоль PostgreSQL стенда
	docker compose exec postgres psql -U aibi -d aibi
