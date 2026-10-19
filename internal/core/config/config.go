package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	App      App
	HTTP     HTTP
	Postgres Postgres
	LLM      LLM
	Ingest   Ingest
	Query    Query
}

type App struct {
	Env      string
	LogLevel string
	LogJSON  bool
}

type HTTP struct {
	Addr            string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
	RequestTimeout  time.Duration
	CORSOrigins     []string
}

type Postgres struct {
	DSN               string
	RawSchema         string
	MaxConns          int32
	MinConns          int32
	MaxConnLifetime   time.Duration
	MaxConnIdleTime   time.Duration
	HealthCheckPeriod time.Duration
	ConnectTimeout    time.Duration
	StatementTimeout  time.Duration
}

type LLM struct {
	BaseURL     string
	APIKey      string
	Model       string
	Timeout     time.Duration
	MaxTokens   int
	Temperature float64
	Retries     int
	Enabled     bool
}

type Ingest struct {
	MaxUploadBytes   int64
	CopyBatchRows    int
	TypeSampleRows   int
	ProfileTopValues int
}

type Query struct {
	MaxRows      int
	Timeout      time.Duration
	PreviewRows  int
	MaxRepairs   int
	HistoryDepth int
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		App: App{
			Env:      str("APP_ENV", "dev"),
			LogLevel: str("LOG_LEVEL", "info"),
			LogJSON:  boolean("LOG_JSON", false),
		},
		HTTP: HTTP{
			Addr:            str("HTTP_ADDR", ":8080"),
			ReadTimeout:     duration("HTTP_READ_TIMEOUT", 30*time.Second),
			WriteTimeout:    duration("HTTP_WRITE_TIMEOUT", 120*time.Second),
			IdleTimeout:     duration("HTTP_IDLE_TIMEOUT", 90*time.Second),
			ShutdownTimeout: duration("HTTP_SHUTDOWN_TIMEOUT", 15*time.Second),
			RequestTimeout:  duration("HTTP_REQUEST_TIMEOUT", 300*time.Second),
			CORSOrigins:     list("HTTP_CORS_ORIGINS", []string{"*"}),
		},
		Postgres: Postgres{
			DSN:               str("DATABASE_URL", "postgres://aibi:aibi@localhost:5432/aibi?sslmode=disable"),
			RawSchema:         str("RAW_SCHEMA", "raw"),
			MaxConns:          int32(integer("PG_MAX_CONNS", 16)),
			MinConns:          int32(integer("PG_MIN_CONNS", 2)),
			MaxConnLifetime:   duration("PG_MAX_CONN_LIFETIME", time.Hour),
			MaxConnIdleTime:   duration("PG_MAX_CONN_IDLE_TIME", 30*time.Minute),
			HealthCheckPeriod: duration("PG_HEALTHCHECK_PERIOD", time.Minute),
			ConnectTimeout:    duration("PG_CONNECT_TIMEOUT", 10*time.Second),
			StatementTimeout:  duration("PG_STATEMENT_TIMEOUT", 60*time.Second),
		},
		LLM: LLM{
			BaseURL:     strings.TrimRight(str("LLM_BASE_URL", "http://localhost:11434/v1"), "/"),
			APIKey:      str("LLM_API_KEY", ""),
			Model:       str("LLM_MODEL", "qwen2.5-coder:14b"),
			Timeout:     duration("LLM_TIMEOUT", 120*time.Second),
			MaxTokens:   integer("LLM_MAX_TOKENS", 2048),
			Temperature: float("LLM_TEMPERATURE", 0.1),
			Retries:     integer("LLM_RETRIES", 2),
			Enabled:     boolean("LLM_ENABLED", true),
		},
		Ingest: Ingest{
			MaxUploadBytes:   int64(integer("INGEST_MAX_UPLOAD_MB", 512)) << 20,
			CopyBatchRows:    integer("INGEST_COPY_BATCH_ROWS", 5000),
			TypeSampleRows:   integer("INGEST_TYPE_SAMPLE_ROWS", 2000),
			ProfileTopValues: integer("INGEST_PROFILE_TOP_VALUES", 10),
		},
		Query: Query{
			MaxRows:      integer("QUERY_MAX_ROWS", 5000),
			Timeout:      duration("QUERY_TIMEOUT", 60*time.Second),
			PreviewRows:  integer("QUERY_PREVIEW_ROWS", 50),
			MaxRepairs:   integer("QUERY_MAX_REPAIRS", 2),
			HistoryDepth: integer("QUERY_HISTORY_DEPTH", 12),
		},
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) validate() error {
	var errs []error
	if c.Postgres.DSN == "" {
		errs = append(errs, errors.New("DATABASE_URL обязателен"))
	}
	if !isIdent(c.Postgres.RawSchema) {
		errs = append(errs, fmt.Errorf("RAW_SCHEMA %q не является корректным идентификатором", c.Postgres.RawSchema))
	}
	if c.Postgres.MaxConns < 1 {
		errs = append(errs, errors.New("PG_MAX_CONNS должен быть >= 1"))
	}
	if c.Postgres.MinConns < 0 || c.Postgres.MinConns > c.Postgres.MaxConns {
		errs = append(errs, errors.New("PG_MIN_CONNS должен быть в диапазоне [0, PG_MAX_CONNS]"))
	}
	if c.Query.MaxRows < 1 {
		errs = append(errs, errors.New("QUERY_MAX_ROWS должен быть >= 1"))
	}
	if c.LLM.Enabled && c.LLM.BaseURL == "" {
		errs = append(errs, errors.New("LLM_BASE_URL обязателен при LLM_ENABLED=true"))
	}
	return errors.Join(errs...)
}

func (c *Config) IsDev() bool { return c.App.Env == "dev" }

func isIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r == '_':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

func str(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func integer(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return def
}

func float(key string, def float64) float64 {
	if v, ok := os.LookupEnv(key); ok {
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return f
		}
	}
	return def
}

func boolean(key string, def bool) bool {
	if v, ok := os.LookupEnv(key); ok {
		if b, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil {
			return b
		}
	}
	return def
}

func duration(key string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok {
		if d, err := time.ParseDuration(strings.TrimSpace(v)); err == nil {
			return d
		}
	}
	return def
}

func list(key string, def []string) []string {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return def
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return def
	}
	return out
}
