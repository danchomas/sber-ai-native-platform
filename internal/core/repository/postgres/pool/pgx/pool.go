package pgx

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/heinousness/aibi/internal/core/config"
)

const defaultPingTimeout = 10 * time.Second

type PoolOptions struct {
	DSN string

	User              string
	Password          string
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

	if opts.User != "" {
		pgCfg.ConnConfig.User = opts.User
		pgCfg.ConnConfig.Password = opts.Password
	}
	if opts.MaxConns > 0 {
		pgCfg.MaxConns = opts.MaxConns
	}
	if opts.MinConns > 0 {
		pgCfg.MinConns = opts.MinConns
	}
	if opts.MaxConnLifetime > 0 {
		pgCfg.MaxConnLifetime = opts.MaxConnLifetime
	}
	if opts.MaxConnIdleTime > 0 {
		pgCfg.MaxConnIdleTime = opts.MaxConnIdleTime
	}
	if opts.HealthCheckPeriod > 0 {
		pgCfg.HealthCheckPeriod = opts.HealthCheckPeriod
	}
	if opts.ConnectTimeout > 0 {
		pgCfg.ConnConfig.ConnectTimeout = opts.ConnectTimeout
	}

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

	pingTimeout := opts.ConnectTimeout
	if pingTimeout <= 0 {
		pingTimeout = defaultPingTimeout
	}
	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
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
