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
	corepg "github.com/heinousness/aibi/internal/core/repository/postgres"
	pgpool "github.com/heinousness/aibi/internal/core/repository/postgres/pool/pgx"
	corehttp "github.com/heinousness/aibi/internal/core/transport/http"
	"github.com/heinousness/aibi/internal/core/transport/http/health"
	"github.com/heinousness/aibi/internal/core/transport/http/middleware"
	"github.com/heinousness/aibi/internal/core/web"

	chatpg "github.com/heinousness/aibi/internal/features/chat/repository/postgres"
	chatsvc "github.com/heinousness/aibi/internal/features/chat/service"
	chathttp "github.com/heinousness/aibi/internal/features/chat/transport/http"
	dashpg "github.com/heinousness/aibi/internal/features/dashboards/repository/postgres"
	dashsvc "github.com/heinousness/aibi/internal/features/dashboards/service"
	dashhttp "github.com/heinousness/aibi/internal/features/dashboards/transport/http"
	datapg "github.com/heinousness/aibi/internal/features/datasets/repository/postgres"
	datasvc "github.com/heinousness/aibi/internal/features/datasets/service"
	datahttp "github.com/heinousness/aibi/internal/features/datasets/transport/http"
	queryhttp "github.com/heinousness/aibi/internal/features/query/transport/http"
	srcpg "github.com/heinousness/aibi/internal/features/sources/repository/postgres"
	srcsvc "github.com/heinousness/aibi/internal/features/sources/service"
	srchttp "github.com/heinousness/aibi/internal/features/sources/transport/http"
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

	applied, err := corepg.Migrate(ctx, pool, cfg.Postgres.RawSchema)
	if err != nil {
		return err
	}
	if len(applied) > 0 {
		log.Info("миграции применены", "count", len(applied), "versions", applied)
	}

	sourceRepo := srcpg.NewSourceRepository(pool)
	datasetRepo := datapg.NewDatasetRepository(pool)
	relationRepo := datapg.NewRelationRepository(pool)
	sessionRepo := chatpg.NewSessionRepository(pool)
	dashboardRepo := dashpg.NewDashboardRepository(pool)

	registry := pgpool.NewRegistry(pool, sourceRepo, pgpool.PoolOptionsFromConfig(cfg.Postgres))
	defer registry.Close()

	rawRepo := corepg.NewRawDataRepository(registry, cfg.Postgres.RawSchema, cfg.Query.Timeout)

	model := newModel(ctx, cfg, log)

	datasets := datasvc.New(datasetRepo, relationRepo, rawRepo,
		cfg.Postgres.RawSchema, cfg.Ingest, cfg.Query, log)
	sources := srcsvc.New(sourceRepo, datasetRepo, rawRepo,
		srcpg.NewIntrospector(registry), datasets, registry.Forget, log)
	dashboards := dashsvc.New(dashboardRepo, datasets, rawRepo, cfg.Query, log)
	chat := chatsvc.New(datasets, rawRepo, sessionRepo, dashboards, model, cfg.Query, log)

	router := corehttp.NewRouter(corehttp.RouterOptions{
		Config: cfg,
		Logger: log,
		Static: web.Handler(),
		Features: []corehttp.Registrar{
			health.New(pgpool.NewHealthProbe(pool), model, version),
			datahttp.New(datasets, middleware.MaxBody(cfg.Ingest.MaxUploadBytes+1<<20)),
			srchttp.New(sources),
			chathttp.New(chat),
			dashhttp.New(dashboards),
			queryhttp.New(datasets, rawRepo, cfg.Query),
		},
	})

	server := corehttp.NewServer(cfg.HTTP, router, log)
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
