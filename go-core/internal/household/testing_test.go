package household

import (
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// testPool returns a Postgres pool from TEST_POSTGRES_URL or falls back to
// the local development Postgres exposed at localhost:5433.
func testPool(t testing.TB) *pgxpool.Pool {
	t.Helper()

	url := os.Getenv("TEST_POSTGRES_URL")
	if url == "" {
		url = "postgres://thaiaiagent:thaiaiagent@localhost:5433/thaiaiagent?sslmode=disable"
	}

	ctx := t.Context()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("testPool: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("testPool ping: %v (is Postgres running at localhost:5433?)", err)
	}
	return pool
}

// truncateTables removes test data. Call in tests that need a clean state.
func truncateTables(t testing.TB, pool *pgxpool.Pool) {
	t.Helper()
	for _, table := range []string{"household_members", "households", "agents", "users"} {
		_, err := pool.Exec(t.Context(), fmt.Sprintf("DELETE FROM %s", table))
		if err != nil {
			t.Logf("truncate %s: %v", table, err)
		}
	}
}
