// Package postgres exposes a pgx connection pool for the Go services.
package postgres

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// New connects to Postgres and pings to fail fast on bad credentials. Prisma
// appends `?schema=public` to its DSN; pgx rejects that as an unknown server
// parameter, so we strip Prisma-only options before handing the URL to pgx.
func New(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	dsn = stripPrismaOnlyParams(dsn)
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres: parse: %w", err)
	}
	cfg.MaxConns = 16
	cfg.MinConns = 2
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: connect: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}
	return pool, nil
}

func stripPrismaOnlyParams(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return dsn
	}
	q := u.Query()
	for _, key := range []string{"schema", "connection_limit", "pool_timeout", "pgbouncer", "sslidentity", "sslpassword"} {
		q.Del(key)
	}
	u.RawQuery = q.Encode()
	return u.String()
}
