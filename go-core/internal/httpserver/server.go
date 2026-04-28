// Package httpserver exposes the Go core parsers over HTTP so the Node service
// can offload deterministic logic (transaction parsing, vision JSON
// reshaping) without re-implementing it in TypeScript.
package httpserver

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/thaiaiagent/go-core/internal/parser"
)

const maxBodyBytes = 256 * 1024 // 256KB is plenty for a vision-model reply.

func New(logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handleHealth)
	mux.HandleFunc("POST /v1/parse/transaction", handleParseTransaction(logger))
	mux.HandleFunc("POST /v1/parse/vision", handleParseVision(logger))
	return logRequests(logger, mux)
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "service": "thaiagent-core"})
}

type parseTextRequest struct {
	Namespace string `json:"namespace"`
	Text      string `json:"text"`
}

func handleParseTransaction(logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req parseTextRequest
		if err := readJSON(r, &req); err != nil {
			logger.Warn("invalid transaction request", "err", err)
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		// Tenant isolation is mandatory: every call must declare a namespace
		// (the LINE userId scope) so we cannot accidentally serve or log a
		// payload that crosses users.
		if req.Namespace == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "namespace is required"})
			return
		}
		logger.Debug("parse transaction", "namespace", req.Namespace, "len", len(req.Text))
		writeJSON(w, http.StatusOK, parser.Parse(req.Text))
	}
}

type parseVisionRequest struct {
	Namespace string `json:"namespace"`
	Content   string `json:"content"`
}

func handleParseVision(logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req parseVisionRequest
		if err := readJSON(r, &req); err != nil {
			logger.Warn("invalid vision request", "err", err)
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		if req.Namespace == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "namespace is required"})
			return
		}
		logger.Debug("parse vision", "namespace", req.Namespace, "len", len(req.Content))
		writeJSON(w, http.StatusOK, parser.ParseVision(req.Content))
	}
}

func readJSON(r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, maxBodyBytes)
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func logRequests(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		logger.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"durationMs", time.Since(start).Milliseconds(),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}
