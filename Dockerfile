# ── Сборка ───────────────────────────────────────────────────────────
FROM golang:1.27-alpine AS build

WORKDIR /src
RUN apk add --no-cache git ca-certificates

# Зависимости кэшируются отдельным слоем.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=docker
# Миграции накатывает отдельный контейнер migrate поверх ./migrations,
# поэтому в образ попадает только API.
RUN CGO_ENABLED=0 GOOS=linux go build \
        -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
        -o /out/aibi ./cmd/api

# ── Запуск ───────────────────────────────────────────────────────────
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 10001 aibi
USER aibi

WORKDIR /app
COPY --from=build /out/aibi /usr/local/bin/

ENV HTTP_ADDR=:8080
EXPOSE 8080

HEALTHCHECK --interval=15s --timeout=3s --start-period=20s --retries=5 \
    CMD wget -qO- http://127.0.0.1:8080/api/v1/ready || exit 1

ENTRYPOINT ["aibi"]
