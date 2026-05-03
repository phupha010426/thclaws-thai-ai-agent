package liff

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/thaiaiagent/go-core/internal/ledger"
	"github.com/thaiaiagent/go-core/internal/thaitime"
)

func (h *Handler) createHousehold(w http.ResponseWriter, r *http.Request) {
	identity, err := h.identityFromHTTP(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	household, err := h.ledger.CreateHousehold(r.Context(), identity.UserID, strings.TrimSpace(req.Name))
	if err != nil {
		h.logger.Warn("liff: create household failed", "err", err, "namespace", identity.Namespace)
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "household_create_failed"})
		return
	}
	h.logLiffEvent(r.Context(), identity, "liff.household.created", household.Name, map[string]any{"householdId": household.ID})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "household": household})
}

func (h *Handler) listMyHouseholds(w http.ResponseWriter, r *http.Request) {
	identity, err := h.identityFromHTTP(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
		return
	}
	households, err := h.ledger.ListMyHouseholds(r.Context(), identity.UserID)
	if err != nil {
		h.logger.Warn("liff: list households failed", "err", err, "namespace", identity.Namespace)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "households_list_failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "households": households})
}

func (h *Handler) addHouseholdMember(w http.ResponseWriter, r *http.Request) {
	identity, err := h.identityFromHTTP(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
		return
	}
	householdID := chi.URLParam(r, "householdID")
	var req struct {
		UserID string `json:"userId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := h.ledger.AddHouseholdMember(r.Context(), identity.UserID, householdID, req.UserID); err != nil {
		h.logger.Warn("liff: add household member failed", "err", err, "namespace", identity.Namespace)
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "member_add_failed"})
		return
	}
	h.logLiffEvent(r.Context(), identity, "liff.household.member_added", householdID, map[string]any{"householdId": householdID, "memberUserId": req.UserID})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) householdDashboard(w http.ResponseWriter, r *http.Request) {
	identity, err := h.identityFromHTTP(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
		return
	}
	householdID := chi.URLParam(r, "householdID")
	now := thaitime.Now()
	rangeOpt, err := parseDashboardRange(r, now)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "bad_range"})
		return
	}
	data, err := h.ledger.HouseholdDashboard(r.Context(), identity.UserID, householdID, rangeOpt.From, rangeOpt.To)
	if err != nil {
		h.logger.Warn("liff: household dashboard failed", "err", err, "namespace", identity.Namespace)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "household_dashboard_failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"data":       data,
		"serverTime": thaitime.ShortDateTime(now),
	})
}

func (h *Handler) createHouseholdTransaction(w http.ResponseWriter, r *http.Request) {
	identity, err := h.identityFromHTTP(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
		return
	}
	householdID := chi.URLParam(r, "householdID")
	var req struct {
		Type       string  `json:"type"`
		Amount     float64 `json:"amount"`
		Category   string  `json:"category"`
		Note       string  `json:"note"`
		HappenedAt string  `json:"happenedAt"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	happenedAt, err := parseLocalDateTime(req.HappenedAt)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "bad_happened_at"})
		return
	}
	txID, err := h.ledger.CreateHouseholdTransaction(r.Context(), identity.UserID, householdID, ledger.HouseholdTransactionInput{
		Type:       req.Type,
		Amount:     req.Amount,
		Category:   req.Category,
		Note:       req.Note,
		HappenedAt: happenedAt,
	})
	if err != nil {
		h.logger.Warn("liff: create household transaction failed", "err", err, "namespace", identity.Namespace)
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "household_transaction_create_failed"})
		return
	}
	h.logLiffEvent(r.Context(), identity, "liff.household.transaction_created", txID, map[string]any{"householdId": householdID, "transactionId": txID, "amount": req.Amount})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "transactionId": txID})
}
