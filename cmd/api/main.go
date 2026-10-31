package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/heinousness/aibi/internal/core/config"
	"github.com/heinousness/aibi/internal/core/domain"
	"github.com/heinousness/aibi/internal/core/llm"
	"github.com/heinousness/aibi/internal/core/logger"
	corepg "github.com/heinousness/aibi/internal/core/repository/postgres"
	pgpool "github.com/heinousness/aibi/internal/core/repository/postgres/pool/pgx"
	corehttp "github.com/heinousness/aibi/internal/core/transport/http"
	"github.com/heinousness/aibi/internal/core/transport/http/health"
	"github.com/heinousness/aibi/internal/core/transport/http/middleware"

	chatpg "github.com/heinousness/aibi/internal/features/chat/repository/postgres"
	chatsvc "github.com/heinousness/aibi/internal/features/chat/service"
	chathttp "github.com/heinousness/aibi/internal/features/chat/transport/http"
	dashpg "github.com/heinousness/aibi/internal/features/dashboards/repository/postgres"
	dashsvc "github.com/heinousness/aibi/internal/features/dashboards/service"
	dashhttp "github.com/heinousness/aibi/internal/features/dashboards/transport/http"
	datapg "github.com/heinousness/aibi/internal/features/datasets/repository/postgres"
	datasvc "github.com/heinousness/aibi/internal/features/datasets/service"
	datahttp "github.com/heinousness/aibi/internal/features/datasets/transport/http"
	mcphttp "github.com/heinousness/aibi/internal/features/mcp/transport/http"
	provpg "github.com/heinousness/aibi/internal/features/providers/repository/postgres"
	provsvc "github.com/heinousness/aibi/internal/features/providers/service"
	provhttp "github.com/heinousness/aibi/internal/features/providers/transport/http"
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

	if err := corepg.EnsureRawSchema(ctx, pool, cfg.Postgres.RawSchema); err != nil {
		return err
	}

	sourceRepo := srcpg.NewSourceRepository(pool)
	providerRepo := provpg.NewProviderRepository(pool)
	datasetRepo := datapg.NewDatasetRepository(pool)
	relationRepo := datapg.NewRelationRepository(pool)
	sessionRepo := chatpg.NewSessionRepository(pool)
	dashboardRepo := dashpg.NewDashboardRepository(pool)

	registry := pgpool.NewRegistry(pool, sourceRepo, pgpool.PoolOptionsFromConfig(cfg.Postgres))
	defer registry.Close()

	rawRepo := corepg.NewRawDataRepository(registry, cfg.Postgres.RawSchema, cfg.Query.Timeout)
	if cfg.Postgres.ReaderRole != "" {
		reader, err := newReaderPool(ctx, pool, cfg)
		if err != nil {
			return err
		}
		defer reader.Close()
		rawRepo.WithReader(reader)
		log.Info("аналитические запросы выполняются от роли только для чтения", "role", cfg.Postgres.ReaderRole)
	} else {
		log.Warn("PG_READER_ROLE не задан: аналитические запросы выполняются от владельца базы")
	}

	providers := provsvc.New(providerRepo, newModel(ctx, cfg, log), cfg.LLM, log)
	providers.Restore(ctx)

	datasets := datasvc.New(datasetRepo, relationRepo, rawRepo,
		cfg.Postgres.RawSchema, cfg.Ingest, cfg.Query, log)
	sources := srcsvc.New(sourceRepo, datasetRepo, rawRepo,
		srcpg.NewIntrospector(registry), datasets, registry.Forget, log)
	dashboards := dashsvc.New(dashboardRepo, datasets, rawRepo, cfg.Query, cfg.Agent.AllowRawSQL, log)
	chat := chatsvc.New(datasets, rawRepo, sessionRepo, dashboards, providers, cfg.Query, cfg.Agent, log)

	router := corehttp.NewRouter(corehttp.RouterOptions{
		Config: cfg,
		Logger: log,
		Features: []corehttp.Registrar{
			health.New(pgpool.NewHealthProbe(pool), providers, version),
			datahttp.New(datasets, middleware.MaxBody(cfg.Ingest.MaxUploadBytes+1<<20)),
			srchttp.New(sources),
			provhttp.New(providers),
			chathttp.New(chat),
			dashhttp.New(dashboards),
			queryhttp.New(datasets, rawRepo, cfg.Query, cfg.Agent.AllowRawSQL),
			mcphttp.New(chat, version),
		},
	})

	server := corehttp.NewServer(cfg.HTTP, router, log)
	if err := server.Run(ctx); err != nil {
		return err
	}
	log.Info("платформа остановлена")
	return nil
}

func newReaderPool(ctx context.Context, owner *pgxpool.Pool, cfg *config.Config) (*pgxpool.Pool, error) {
	password, err := corepg.EnsureReaderRole(ctx, owner,
		cfg.Postgres.ReaderRole, cfg.Postgres.RawSchema, cfg.Query.Timeout)
	if err != nil {
		return nil, fmt.Errorf("%w (чтобы запускаться без неё, оставьте PG_READER_ROLE пустым)", err)
	}

	opts := pgpool.PoolOptionsFromConfig(cfg.Postgres)
	opts.User, opts.Password = cfg.Postgres.ReaderRole, password
	opts.AppName = "aibi-reader"
	opts.MaxConns = max(cfg.Postgres.MaxConns/2, 2)
	opts.MinConns = 0
	reader, err := pgpool.NewPool(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("подключение ролью чтения %q: %w", cfg.Postgres.ReaderRole, err)
	}
	return reader, nil
}

func newModel(ctx context.Context, cfg *config.Config, log *slog.Logger) domain.LLM {
	if !cfg.LLM.Enabled {
		log.Warn("языковая модель отключена, доступны только детерминированные сценарии")
		return llm.NewFallback()
	}

	client := llm.New(cfg.LLM)
	if llm.IsOpenRouter(cfg.LLM.BaseURL) {
		client = llm.NewOpenRouter(cfg.LLM)
	}
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
