package pgx

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/heinousness/aibi/internal/core/config"
)

type PoolOptions struct {
	DSN               string
	MaxConns          int32
	MinConns          int32
	MaxConnLifetime   time.Duration
	MaxConnIdleTime   time.Duration
	HealthCheckPeriod time.Duration
	ConnectTimeout    time.Duration
	StatementTimeout  time.Duration
	AppName           string
}

func NewPool(ctx context.Context, opts PoolOptions) (*pgxpool.Pool, error) {
	pgCfg, err := pgxpool.ParseConfig(opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("разбор DSN: %w", err)
	}

	pgCfg.MaxConns = opts.MaxConns
	pgCfg.MinConns = opts.MinConns
	pgCfg.MaxConnLifetime = opts.MaxConnLifetime
	pgCfg.MaxConnIdleTime = opts.MaxConnIdleTime
	pgCfg.HealthCheckPeriod = opts.HealthCheckPeriod
	pgCfg.ConnConfig.ConnectTimeout = opts.ConnectTimeout

	pgCfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeCacheStatement

	if pgCfg.ConnConfig.RuntimeParams == nil {
		pgCfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	appName := opts.AppName
	if appName == "" {
		appName = "aibi"
	}
	pgCfg.ConnConfig.RuntimeParams["application_name"] = appName
	if opts.StatementTimeout > 0 {
		pgCfg.ConnConfig.RuntimeParams["statement_timeout"] =
			fmt.Sprintf("%d", opts.StatementTimeout.Milliseconds())
	}
	pgCfg.ConnConfig.RuntimeParams["timezone"] = "UTC"

	pool, err := pgxpool.NewWithConfig(ctx, pgCfg)
	if err != nil {
		return nil, fmt.Errorf("создание пула: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, opts.ConnectTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("проверка соединения: %w", err)
	}
	return pool, nil
}

func PoolOptionsFromConfig(c config.Postgres) PoolOptions {
	return PoolOptions{
		DSN:               c.DSN,
		MaxConns:          c.MaxConns,
		MinConns:          c.MinConns,
		MaxConnLifetime:   c.MaxConnLifetime,
		MaxConnIdleTime:   c.MaxConnIdleTime,
		HealthCheckPeriod: c.HealthCheckPeriod,
		ConnectTimeout:    c.ConnectTimeout,
		StatementTimeout:  c.StatementTimeout,
		AppName:           "aibi-platform",
	}
}

type Stats struct {
	Total    int32 `json:"total_conns"`
	Idle     int32 `json:"idle_conns"`
	Acquired int32 `json:"acquired_conns"`
	MaxConns int32 `json:"max_conns"`
}

func PoolStats(p *pgxpool.Pool) Stats {
	s := p.Stat()
	return Stats{
		Total:    s.TotalConns(),
		Idle:     s.IdleConns(),
		Acquired: s.AcquiredConns(),
		MaxConns: s.MaxConns(),
	}
}

type HealthProbe struct {
	pool *pgxpool.Pool
}

func NewHealthProbe(pool *pgxpool.Pool) *HealthProbe {
	return &HealthProbe{pool: pool}
}

func (p *HealthProbe) Ping(ctx context.Context) error {
	return p.pool.Ping(ctx)
}

func (p *HealthProbe) PoolStats() map[string]int32 {
	s := PoolStats(p.pool)
	return map[string]int32{
		"total":    s.Total,
		"idle":     s.Idle,
		"acquired": s.Acquired,
		"max":      s.MaxConns,
	}
}
