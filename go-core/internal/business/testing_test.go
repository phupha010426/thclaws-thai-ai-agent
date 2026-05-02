package business

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

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

func createTestUser(t testing.TB, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	userID := uuid.NewString()
	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, "lineUserId", language, "createdAt", "updatedAt")
		VALUES ($1, 'test-line-'||$1, 'th', NOW(), NOW())
		ON CONFLICT (id) DO NOTHING`, userID)
	if err != nil {
		t.Fatalf("createTestUser: %v", err)
	}
	return userID
}
