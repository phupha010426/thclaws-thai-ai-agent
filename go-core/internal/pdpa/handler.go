// Package pdpa (handler.go) exposes the PDPA HTTP endpoints via chi.
// Authentication uses a shared admin token because consent verification happens
// out-of-band — these routes are called by ops tooling or an authenticated LIFF
// dashboard, not by anonymous LINE users.
package pdpa

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// HandlerConfig carries the dependencies that the HTTP handler needs.
type HandlerConfig struct {
	Service      *Service
	GatewayAPIKey string
	Logger       *slog.Logger
}

// NewHandler returns a chi-compatible http.Handler that mounts the PDPA routes.
func NewHandler(cfg HandlerConfig) http.Handler {
	mux := http.NewServeMux()

	auth := adminTokenMiddleware(cfg.GatewayAPIKey)

	mux.Handle("POST /pdpa/delete", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ns := parseNamespace(r)
		if ns == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "namespace required"})
			return
		}
		report, err := cfg.Service.DeleteAll(r.Context(), ns)
		if err != nil {
			cfg.Logger.Error("pdpa delete failed", "namespace", ns, "err", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "delete failed"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "report": report})
	})))

	mux.Handle("POST /pdpa/export", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ns := parseNamespace(r)
		if ns == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "namespace required"})
			return
		}
		data, err := cfg.Service.ExportAll(r.Context(), ns)
		if err != nil {
			cfg.Logger.Error("pdpa export failed", "namespace", ns, "err", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "export failed"})
			return
		}
		// Populate exportedAt here because the service struct cannot import time
		// formatting without coupling to the HTTP layer.
		type exportResp struct {
			OK         bool       `json:"ok"`
			ExportedAt string     `json:"exportedAt"`
			Data       ExportData `json:"data"`
		}
		writeJSON(w, http.StatusOK, exportResp{
			OK:         true,
			ExportedAt: time.Now().UTC().Format(time.RFC3339),
			Data:       data,
		})
	})))

	return mux
}

// adminTokenMiddleware rejects requests whose X-Admin-Token header does not
// match the expected key. A timing-safe comparison is not needed here because
// the token space is large and the endpoint is not publicly routable.
func adminTokenMiddleware(expectedKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			provided := r.Header.Get("X-Admin-Token")
			if provided == "" || provided != expectedKey {
				writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// parseNamespace decodes the request body and returns the trimmed namespace
// value. Returns empty string if the body is malformed or namespace is absent.
func parseNamespace(r *http.Request) string {
	var body struct {
		Namespace string `json:"namespace"`
	}
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return ""
	}
	// Trim space to prevent namespace=" foo" from creating a different tenant key
	ns := body.Namespace
	for len(ns) > 0 && (ns[0] == ' ' || ns[0] == '\t') {
		ns = ns[1:]
	}
	for len(ns) > 0 && (ns[len(ns)-1] == ' ' || ns[len(ns)-1] == '\t') {
		ns = ns[:len(ns)-1]
	}
	return ns
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
