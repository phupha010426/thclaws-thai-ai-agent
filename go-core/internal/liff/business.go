package liff

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/thaiaiagent/go-core/internal/ledger"
	"github.com/thaiaiagent/go-core/internal/thaitime"
)

func (h *Handler) createBusiness(w http.ResponseWriter, r *http.Request) {
	identity, err := h.identityFromHTTP(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
		return
	}
	var req struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	business, err := h.ledger.CreateBusiness(r.Context(), identity.UserID, strings.TrimSpace(req.Name), strings.TrimSpace(req.Type))
	if err != nil {
		h.logger.Warn("liff: create business failed", "err", err, "namespace", identity.Namespace)
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "business_create_failed"})
		return
	}
	h.logLiffEvent(r.Context(), identity, "liff.business.created", business.Name, map[string]any{"businessId": business.ID})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "business": business})
}

func (h *Handler) listMyBusinesses(w http.ResponseWriter, r *http.Request) {
	identity, err := h.identityFromHTTP(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
		return
	}
	businesses, err := h.ledger.ListMyBusinesses(r.Context(), identity.UserID)
	if err != nil {
		h.logger.Warn("liff: list businesses failed", "err", err, "namespace", identity.Namespace)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "businesses_list_failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "businesses": businesses})
}

func (h *Handler) businessDashboard(w http.ResponseWriter, r *http.Request) {
	identity, err := h.identityFromHTTP(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
		return
	}
	businessID := chi.URLParam(r, "businessID")
	now := thaitime.Now()
	rangeOpt, err := parseDashboardRange(r, now)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "bad_range"})
		return
	}
	data, err := h.ledger.BusinessDashboard(r.Context(), identity.UserID, businessID, rangeOpt.From, rangeOpt.To)
	if err != nil {
		h.logger.Warn("liff: business dashboard failed", "err", err, "namespace", identity.Namespace)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "business_dashboard_failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"data":       data,
		"serverTime": thaitime.ShortDateTime(now),
	})
}

func (h *Handler) createBusinessSale(w http.ResponseWriter, r *http.Request) {
	identity, err := h.identityFromHTTP(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
		return
	}
	businessID := chi.URLParam(r, "businessID")
	var req struct {
		Product    string  `json:"product"`
		Amount     float64 `json:"amount"`
		Quantity   float64 `json:"quantity"`
		UnitPrice  float64 `json:"unitPrice"`
		HappenedAt string  `json:"happenedAt"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	txID, err := ledger.RecordBusinessSale(r.Context(), h.ledger, identity.UserID, businessID, req.Product, req.Amount, req.Quantity, req.UnitPrice)
	if err != nil {
		h.logger.Warn("liff: create business sale failed", "err", err, "namespace", identity.Namespace)
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "sale_create_failed"})
		return
	}
	h.logLiffEvent(r.Context(), identity, "liff.business.sale_created", txID, map[string]any{"businessId": businessID, "transactionId": txID, "amount": req.Amount})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "transactionId": txID})
}

func (h *Handler) createBusinessPurchase(w http.ResponseWriter, r *http.Request) {
	identity, err := h.identityFromHTTP(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
		return
	}
	businessID := chi.URLParam(r, "businessID")
	var req struct {
		Item       string  `json:"item"`
		Amount     float64 `json:"amount"`
		Quantity   float64 `json:"quantity"`
		UnitCost   float64 `json:"unitCost"`
		HappenedAt string  `json:"happenedAt"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	txID, err := ledger.RecordBusinessPurchase(r.Context(), h.ledger, identity.UserID, businessID, req.Item, req.Amount, req.Quantity, req.UnitCost)
	if err != nil {
		h.logger.Warn("liff: create business purchase failed", "err", err, "namespace", identity.Namespace)
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "purchase_create_failed"})
		return
	}
	h.logLiffEvent(r.Context(), identity, "liff.business.purchase_created", txID, map[string]any{"businessId": businessID, "transactionId": txID, "amount": req.Amount})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "transactionId": txID})
}

func (h *Handler) businessProfit(w http.ResponseWriter, r *http.Request) {
	identity, err := h.identityFromHTTP(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
		return
	}
	businessID := chi.URLParam(r, "businessID")
	now := thaitime.Now()
	rangeOpt, err := parseDashboardRange(r, now)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "bad_range"})
		return
	}
	profit, err := ledger.ComputeBusinessProfit(r.Context(), h.ledger, businessID, rangeOpt.From.UTC().Format("2006-01-02T15:04:05Z"), rangeOpt.To.UTC().Format("2006-01-02T15:04:05Z"))
	if err != nil {
		h.logger.Warn("liff: business profit failed", "err", err, "namespace", identity.Namespace)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "profit_calc_failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"profit": profit,
	})
}
