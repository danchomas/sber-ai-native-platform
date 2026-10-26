package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// EnsureReaderRole готовит роль, под которой выполняются аналитические
// запросы к собственной базе платформы. Это последний слой защиты после
// компилятора запросов и sqlguard: у роли есть только SELECT на схему
// сырых данных. Даже если запрос обойдёт обе проверки, база не даст ни
// изменить данные, ни прочитать метаданные платформы (ключи провайдеров,
// строки подключения), ни вызвать функции суперпользователя вроде
// pg_read_file.
//
// Пароль генерируется заново при каждом старте и нигде не хранится:
// знает его только этот процесс.
func EnsureReaderRole(ctx context.Context, pool *pgxpool.Pool, role, schema string, timeout time.Duration) (string, error) {
	if role == "" || schema == "" {
		return "", fmt.Errorf("не задана роль чтения или схема данных")
	}

	secret := make([]byte, 24)
	if _, err := rand.Read(secret); err != nil {
		return "", fmt.Errorf("генерация пароля роли чтения: %w", err)
	}
	password := hex.EncodeToString(secret)

	var owner string
	var exists bool
	if err := pool.QueryRow(ctx,
		`SELECT current_user, EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)`, role,
	).Scan(&owner, &exists); err != nil {
		return "", fmt.Errorf("проверка роли чтения: %w", MapError(err))
	}
	if owner == role {
		return "", fmt.Errorf("роль чтения %q совпадает с владельцем базы", role)
	}

	verb := "CREATE"
	if exists {
		verb = "ALTER"
	}
	r, s := QuoteIdent(role), QuoteIdent(schema)
	// Пароль — hex, поэтому литерал безопасен; параметры в DDL не поддерживаются.
	statements := []string{
		fmt.Sprintf(`%s ROLE %s WITH LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS PASSWORD %s`,
			verb, r, QuoteLiteral(password)),
		fmt.Sprintf(`ALTER ROLE %s SET default_transaction_read_only = on`, r),
		fmt.Sprintf(`ALTER ROLE %s SET statement_timeout = %d`, r, timeout.Milliseconds()),
		fmt.Sprintf(`GRANT USAGE ON SCHEMA %s TO %s`, s, r),
		fmt.Sprintf(`GRANT SELECT ON ALL TABLES IN SCHEMA %s TO %s`, s, r),
		fmt.Sprintf(`ALTER DEFAULT PRIVILEGES FOR ROLE %s IN SCHEMA %s GRANT SELECT ON TABLES TO %s`,
			QuoteIdent(owner), s, r),
	}
	for _, stmt := range statements {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			return "", fmt.Errorf("настройка роли чтения %q: %w", role, MapError(err))
		}
	}
	return password, nil
}
