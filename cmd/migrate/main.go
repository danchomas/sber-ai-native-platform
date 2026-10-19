package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/heinousness/aibi/internal/core/config"
	"github.com/heinousness/aibi/internal/core/logger"
	"github.com/heinousness/aibi/internal/core/repository/postgres"
	pgpool "github.com/heinousness/aibi/internal/core/repository/postgres/pool/pgx"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "конфигурация: %v\n", err)
		os.Exit(1)
	}

	log := logger.New(cfg.App.LogLevel, cfg.App.LogJSON)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pool, err := pgpool.NewPool(ctx, pgpool.PoolOptionsFromConfig(cfg.Postgres))
	if err != nil {
		log.Error("подключение к базе данных", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	applied, err := postgres.Migrate(ctx, pool, cfg.Postgres.RawSchema)
	if err != nil {
		log.Error("применение миграций", "error", err)
		os.Exit(1)
	}
	if len(applied) == 0 {
		log.Info("новых миграций нет")
		return
	}
	log.Info("миграции применены", "versions", applied)
}
