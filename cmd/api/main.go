package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/heinousness/aibi/internal/core/config"
	"github.com/heinousness/aibi/internal/core/domain"
	"github.com/heinousness/aibi/internal/core/llm"
	"github.com/heinousness/aibi/internal/core/logger"
	"github.com/heinousness/aibi/internal/core/repository/postgres"
	pgpool "github.com/heinousness/aibi/internal/core/repository/postgres/pool/pgx"
	"github.com/heinousness/aibi/internal/service/agent"
	"github.com/heinousness/aibi/internal/service/catalog"
	"github.com/heinousness/aibi/internal/service/chat"
	"github.com/heinousness/aibi/internal/service/dashboard"
	"github.com/heinousness/aibi/internal/service/ingest"
	"github.com/heinousness/aibi/internal/service/source"
	transport "github.com/heinousness/aibi/internal/transport/http"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "не удалось запустить платформу: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := logger.New(cfg.App.LogLevel, cfg.App.LogJSON)
	slog.SetDefault(log)
	log.Info("запуск платформы", "version", version, "env", cfg.App.Env)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := pgpool.NewPool(ctx, pgpool.PoolOptionsFromConfig(cfg.Postgres))
	if err != nil {
		return err
	}
	defer pool.Close()
	log.Info("подключение к PostgreSQL установлено",
		"max_conns", cfg.Postgres.MaxConns, "schema", cfg.Postgres.RawSchema)

	applied, err := postgres.Migrate(ctx, pool, cfg.Postgres.RawSchema)
	if err != nil {
		return err
	}
	if len(applied) > 0 {
		log.Info("миграции применены", "count", len(applied), "versions", applied)
	}

	sourceRepo := postgres.NewSourceRepository(pool)
	datasetRepo := postgres.NewDatasetRepository(pool)
	relationRepo := postgres.NewRelationRepository(pool)
	sessionRepo := postgres.NewSessionRepository(pool)
	dashboardRepo := postgres.NewDashboardRepository(pool)

	registry := pgpool.NewRegistry(pool, sourceRepo, pgpool.PoolOptionsFromConfig(cfg.Postgres))
	defer registry.Close()

	rawRepo := postgres.NewRawDataRepository(registry, cfg.Postgres.RawSchema, cfg.Query.Timeout)

	model := newModel(ctx, cfg, log)

	catalogSvc := catalog.New(datasetRepo, relationRepo, rawRepo,
		cfg.Postgres.RawSchema, cfg.Ingest, cfg.Query, log)
	ingestSvc := ingest.New(datasetRepo, rawRepo, catalogSvc, cfg.Postgres.RawSchema, cfg.Ingest, log)
	sourceSvc := source.New(sourceRepo, datasetRepo, rawRepo,
		postgres.NewIntrospector(registry), catalogSvc, registry.Forget, log)
	dashboardSvc := dashboard.New(dashboardRepo, catalogSvc, rawRepo, cfg.Query, log)
	agentSvc := agent.New(catalogSvc, rawRepo, sessionRepo, dashboardSvc, model, cfg.Query, log)
	chatSvc := chat.New(sessionRepo, agentSvc, log)

	router := transport.NewRouter(transport.Deps{
		Config:     cfg,
		Logger:     log,
		Version:    version,
		Ingest:     ingestSvc,
		Catalog:    catalogSvc,
		Sources:    sourceSvc,
		Chat:       chatSvc,
		Dashboards: dashboardSvc,
		Executor:   rawRepo,
		DB:         pgpool.NewHealthProbe(pool),
		Model:      model,
	})

	server := transport.NewServer(cfg.HTTP, router, log)
	if err := server.Run(ctx); err != nil {
		return err
	}
	log.Info("платформа остановлена")
	return nil
}

func newModel(ctx context.Context, cfg *config.Config, log *slog.Logger) domain.LLM {
	if !cfg.LLM.Enabled {
		log.Warn("языковая модель отключена, доступны только детерминированные сценарии")
		return llm.NewFallback()
	}

	client := llm.New(cfg.LLM)
	if err := client.Ping(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			return client
		}
		log.Warn("языковая модель недоступна при старте, платформа продолжит работу без неё",
			"base_url", cfg.LLM.BaseURL, "model", cfg.LLM.Model, "error", err)
		return client
	}
	log.Info("языковая модель подключена", "base_url", cfg.LLM.BaseURL, "model", cfg.LLM.Model)
	return client
}
