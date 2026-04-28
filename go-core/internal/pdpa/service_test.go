package pdpa_test

import (
	"context"
	"testing"

	"github.com/thaiaiagent/go-core/internal/pdpa"
	"log/slog"
	"os"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// TestDeleteAll_NamespaceRequired verifies that an empty namespace is rejected
// before any store is touched. All three store parameters are nil; any attempt
// to reach them would panic, proving the guard runs first.
func TestDeleteAll_NamespaceRequired(t *testing.T) {
	t.Parallel()
	svc := pdpa.New(nil, nil, "testdb", nil, testLogger())
	_, err := svc.DeleteAll(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty namespace, got nil")
	}
}

// TestExportAll_NamespaceRequired is the same guard check for ExportAll.
func TestExportAll_NamespaceRequired(t *testing.T) {
	t.Parallel()
	svc := pdpa.New(nil, nil, "testdb", nil, testLogger())
	_, err := svc.ExportAll(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty namespace, got nil")
	}
}

// TestDeleteAll_Integration runs against real stores when all three env vars
// are set. Full wiring lives in the cmd/ integration suite; this stub ensures
// the test binary compiles and the env-guard is exercised.
func TestDeleteAll_Integration(t *testing.T) {
	pgDSN := os.Getenv("POSTGRES_TEST_URL")
	mongoURL := os.Getenv("MONGODB_TEST_URL")
	redisURL := os.Getenv("REDIS_TEST_URL")
	if pgDSN == "" || mongoURL == "" || redisURL == "" {
		t.Skip("POSTGRES_TEST_URL / MONGODB_TEST_URL / REDIS_TEST_URL not set; skipping integration test")
	}
	// Full store wiring belongs in cmd/ to avoid circular imports.
	t.Skip("full integration covered in cmd/ layer tests")
}

// Compile-time surface check: Service must expose DeleteAll and ExportAll.
var _ interface {
	DeleteAll(context.Context, string) (pdpa.DeleteReport, error)
	ExportAll(context.Context, string) (pdpa.ExportData, error)
} = (*pdpa.Service)(nil)
