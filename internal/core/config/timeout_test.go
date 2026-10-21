package config

import (
	"strings"
	"testing"
	"time"
)

func TestValidateTimeoutOrder(t *testing.T) {
	base := func() *Config {
		return &Config{
			HTTP:     HTTP{RequestTimeout: 300 * time.Second, WriteTimeout: 330 * time.Second},
			Postgres: Postgres{DSN: "postgres://x", RawSchema: "raw", MaxConns: 4, MinConns: 1},
		}
	}

	t.Run("запись переживает обработку", func(t *testing.T) {
		if err := base().validate(); err != nil && strings.Contains(err.Error(), "HTTP_WRITE_TIMEOUT") {
			t.Errorf("корректный порядок отклонён: %v", err)
		}
	})

	t.Run("запись короче обработки — отказ", func(t *testing.T) {
		c := base()
		c.HTTP.WriteTimeout = 120 * time.Second

		err := c.validate()
		if err == nil || !strings.Contains(err.Error(), "HTTP_WRITE_TIMEOUT") {
			t.Fatalf("ожидался отказ про HTTP_WRITE_TIMEOUT, получено: %v", err)
		}
	})

	t.Run("запись равна обработке — тоже отказ", func(t *testing.T) {
		c := base()
		c.HTTP.WriteTimeout = c.HTTP.RequestTimeout

		if err := c.validate(); err == nil || !strings.Contains(err.Error(), "HTTP_WRITE_TIMEOUT") {
			t.Fatalf("равные значения не оставляют запаса, ожидался отказ: %v", err)
		}
	})

	t.Run("запись без предела допустима", func(t *testing.T) {
		c := base()
		c.HTTP.WriteTimeout = 0

		if err := c.validate(); err != nil && strings.Contains(err.Error(), "HTTP_WRITE_TIMEOUT") {
			t.Errorf("нулевое значение означает «без предела» и отказом быть не должно: %v", err)
		}
	})
}
