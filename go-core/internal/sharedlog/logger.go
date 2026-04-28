// Package sharedlog provides a single slog logger configuration so every
// command (api, worker, outbox, server) emits identical JSON lines that the
// existing Caddy/log shipper pipeline already parses.
package sharedlog

import (
	"log/slog"
	"os"
	"strings"

	"github.com/thaiaiagent/go-core/internal/thaitime"
)

func New(level string) *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLevel(level),
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey && a.Value.Kind() == slog.KindTime {
				a.Value = slog.StringValue(thaitime.Log(a.Value.Time()))
			}
			return a
		},
	}))
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
