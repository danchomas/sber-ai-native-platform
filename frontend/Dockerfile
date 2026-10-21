# Сборка интерфейса и отдача через nginx.
#
# Многоступенчатая: node нужен только на сборку, в итоговый образ попадают
# статические файлы и конфигурация nginx.

FROM node:24-alpine AS build
WORKDIR /app

# Зависимости ставятся отдельным слоем: они меняются реже исходников.
COPY package.json package-lock.json ./
RUN npm ci

COPY . .
RUN npm run build

FROM nginx:1.29-alpine
COPY --from=build /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80
# Адрес именно 127.0.0.1, а не localhost: в контейнере localhost
# резолвится сперва в ::1, а nginx слушает только IPv4 — проверка
# получала «connection refused» на живом сервере.
HEALTHCHECK --interval=10s --timeout=3s --retries=5 \
    CMD wget -qO- http://127.0.0.1/ >/dev/null || exit 1
