package liff

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/thaiaiagent/go-core/internal/ledger"
	"github.com/thaiaiagent/go-core/internal/media"
	"github.com/thaiaiagent/go-core/internal/memory"
	"github.com/thaiaiagent/go-core/internal/thaitime"
	"github.com/thaiaiagent/go-core/internal/users"
)

type Handler struct {
	users      *users.Service
	ledger     *ledger.Writer
	memory     *memory.MemoryService
	secret     string
	liffID     string
	channelID  string
	httpClient *http.Client
	logger     *slog.Logger
}

func NewHandler(usersSvc *users.Service, ledgerWriter *ledger.Writer, memorySvc *memory.MemoryService, secret, liffID, channelID string, logger *slog.Logger) *Handler {
	liffID = strings.TrimSpace(liffID)
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		channelID = deriveChannelID(liffID)
	}
	return &Handler{
		users:      usersSvc,
		ledger:     ledgerWriter,
		memory:     memorySvc,
		secret:     strings.TrimSpace(secret),
		liffID:     liffID,
		channelID:  channelID,
		httpClient: &http.Client{Timeout: 5 * time.Second},
		logger:     logger,
	}
}

func (h *Handler) Register(r chi.Router) {
	r.Get("/", h.index)
	r.Get("/dashboard", h.index)
	r.Get("/dashboard/", h.index)
	r.Get("/api/dashboard", h.dashboard)
	r.Get("/api/projects", h.projects)
	r.Post("/api/projects", h.createProject)
	r.Patch("/api/projects/{projectID}", h.updateProject)
	r.Delete("/api/projects/{projectID}", h.deleteProject)
	r.Patch("/api/transactions/{transactionID}", h.updateTransaction)
	r.Delete("/api/transactions/{transactionID}", h.deleteTransaction)
	r.Get("/*", h.index)
}

func (h *Handler) IssueToken(identity users.Identity) (string, error) {
	return Sign(identity, h.secret, thaitime.Now())
}

func (h *Handler) index(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	html := strings.ReplaceAll(indexHTML, "__LIFF_ID_JSON__", strconv.Quote(h.liffID))
	_, _ = w.Write([]byte(html))
}

func (h *Handler) dashboard(w http.ResponseWriter, r *http.Request) {
	identity, err := h.identityFromHTTP(r)
	if err != nil {
		h.logger.Warn("liff: rejected dashboard auth", "err", err)
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
		return
	}
	now := thaitime.Now()
	rangeOpt, err := parseDashboardRange(r, now)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "bad_range"})
		return
	}
	data, err := h.ledger.DashboardWithRange(r.Context(), identity.UserID, identity.Namespace, ledger.DashboardOptions{
		Now:    now,
		From:   rangeOpt.From,
		To:     rangeOpt.To,
		Preset: rangeOpt.Preset,
		Label:  rangeOpt.Label,
	})
	if err != nil {
		h.logger.Error("liff: dashboard failed", "err", err, "namespace", identity.Namespace)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "dashboard_failed"})
		return
	}
	docs, err := h.dashboardDocuments(r.Context(), identity, rangeOpt.From, rangeOpt.To)
	if err != nil {
		h.logger.Warn("liff: documents failed", "err", err, "namespace", identity.Namespace)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"data":       dashboardResponse{DashboardData: data, Documents: docs},
		"serverTime": thaitime.ShortDateTime(time.Now()),
	})
}

func (h *Handler) projects(w http.ResponseWriter, r *http.Request) {
	identity, err := h.identityFromHTTP(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
		return
	}
	rangeOpt, err := parseDashboardRange(r, thaitime.Now())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "bad_range"})
		return
	}
	projects, err := h.ledger.ListProjects(
		r.Context(), identity.UserID, identity.Namespace,
		rangeOpt.From.UTC().Format(time.RFC3339Nano), rangeOpt.To.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		h.logger.Error("liff: projects failed", "err", err, "namespace", identity.Namespace)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "projects_failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "projects": projects})
}

type projectRequest struct {
	Name         string  `json:"name"`
	Description  string  `json:"description"`
	Color        string  `json:"color"`
	BudgetAmount float64 `json:"budgetAmount"`
	TargetDate   string  `json:"targetDate"`
}

func (h *Handler) createProject(w http.ResponseWriter, r *http.Request) {
	identity, err := h.identityFromHTTP(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
		return
	}
	var req projectRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	targetDate, err := parseOptionalLocalDate(req.TargetDate)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "bad_target_date"})
		return
	}
	project, err := h.ledger.CreateProject(r.Context(), ledger.ProjectInput{
		UserID: identity.UserID, Namespace: identity.Namespace, Name: req.Name, Description: req.Description, Color: req.Color,
		BudgetAmount: req.BudgetAmount, TargetDate: targetDate,
	})
	if err != nil {
		h.logger.Warn("liff: create project failed", "err", err, "namespace", identity.Namespace)
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "project_create_failed"})
		return
	}
	h.logLiffEvent(r.Context(), identity, "liff.project.created", project.Name, map[string]any{"projectId": project.ID})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "project": project})
}

func (h *Handler) updateProject(w http.ResponseWriter, r *http.Request) {
	identity, err := h.identityFromHTTP(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
		return
	}
	var req projectRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	targetDate, err := parseOptionalLocalDate(req.TargetDate)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "bad_target_date"})
		return
	}
	projectID := chi.URLParam(r, "projectID")
	project, err := h.ledger.UpdateProject(r.Context(), projectID, ledger.ProjectInput{
		UserID: identity.UserID, Namespace: identity.Namespace, Name: req.Name, Description: req.Description, Color: req.Color,
		BudgetAmount: req.BudgetAmount, TargetDate: targetDate,
	})
	if err != nil {
		h.logger.Warn("liff: update project failed", "err", err, "namespace", identity.Namespace)
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "project_update_failed"})
		return
	}
	h.logLiffEvent(r.Context(), identity, "liff.project.updated", project.Name, map[string]any{"projectId": project.ID})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "project": project})
}

func (h *Handler) deleteProject(w http.ResponseWriter, r *http.Request) {
	identity, err := h.identityFromHTTP(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
		return
	}
	projectID := chi.URLParam(r, "projectID")
	if err := h.ledger.DeleteProject(r.Context(), identity.UserID, identity.Namespace, projectID); err != nil {
		h.logger.Warn("liff: delete project failed", "err", err, "namespace", identity.Namespace)
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "project_delete_failed"})
		return
	}
	h.logLiffEvent(r.Context(), identity, "liff.project.deleted", projectID, map[string]any{"projectId": projectID})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type transactionRequest struct {
	Type             string                   `json:"type"`
	Amount           float64                  `json:"amount"`
	Category         string                   `json:"category"`
	Note             string                   `json:"note"`
	CounterpartyName string                   `json:"counterpartyName"`
	CounterpartyRole string                   `json:"counterpartyRole"`
	HappenedAt       string                   `json:"happenedAt"`
	Allocations      []ledger.AllocationInput `json:"allocations"`
}

func (h *Handler) updateTransaction(w http.ResponseWriter, r *http.Request) {
	identity, err := h.identityFromHTTP(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
		return
	}
	var req transactionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	happenedAt, err := parseLocalDateTime(req.HappenedAt)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "bad_happened_at"})
		return
	}
	txID := chi.URLParam(r, "transactionID")
	err = h.ledger.UpdateTransaction(r.Context(), ledger.TransactionUpdateInput{
		UserID: identity.UserID, AgentID: identity.AgentID, Namespace: identity.Namespace, TransactionID: txID,
		Type: req.Type, Amount: req.Amount, Category: req.Category, Note: req.Note,
		CounterpartyName: req.CounterpartyName, CounterpartyRole: req.CounterpartyRole,
		HappenedAt: happenedAt, Allocations: req.Allocations,
	})
	if err != nil {
		h.logger.Warn("liff: update transaction failed", "err", err, "namespace", identity.Namespace, "transactionId", txID)
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "transaction_update_failed"})
		return
	}
	h.logLiffEvent(r.Context(), identity, "liff.transaction.updated", txID, map[string]any{"transactionId": txID, "amount": req.Amount})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) deleteTransaction(w http.ResponseWriter, r *http.Request) {
	identity, err := h.identityFromHTTP(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
		return
	}
	txID := chi.URLParam(r, "transactionID")
	if err := h.ledger.DeleteTransaction(r.Context(), identity.UserID, identity.Namespace, txID); err != nil {
		h.logger.Warn("liff: delete transaction failed", "err", err, "namespace", identity.Namespace, "transactionId", txID)
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "transaction_delete_failed"})
		return
	}
	h.logLiffEvent(r.Context(), identity, "liff.transaction.deleted", txID, map[string]any{"transactionId": txID})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) identityFromHTTP(r *http.Request) (users.Identity, error) {
	return h.identityFromRequest(r.Context(), strings.TrimSpace(r.URL.Query().Get("t")), bearerToken(r.Header.Get("Authorization")))
}

func (h *Handler) identityFromRequest(ctx context.Context, signedToken, idToken string) (users.Identity, error) {
	if signedToken != "" {
		session, err := Verify(signedToken, h.secret, thaitime.Now())
		if err != nil {
			return users.Identity{}, err
		}
		identity, err := h.users.ResolveIdentity(ctx, session.UserID, session.AgentID, session.Namespace)
		if err != nil {
			return users.Identity{}, fmt.Errorf("liff session identity: %w", err)
		}
		return identity, nil
	}
	if idToken == "" {
		return users.Identity{}, errors.New("liff auth: missing token")
	}
	lineUserID, err := h.verifyIDToken(ctx, idToken)
	if err != nil {
		return users.Identity{}, err
	}
	return h.users.ResolveOrCreateLineUser(ctx, lineUserID)
}

func (h *Handler) verifyIDToken(ctx context.Context, idToken string) (string, error) {
	if h.channelID == "" {
		return "", errors.New("liff auth: LIFF_CHANNEL_ID is not configured")
	}
	form := url.Values{}
	form.Set("id_token", idToken)
	form.Set("client_id", h.channelID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.line.me/oauth2/v2.1/verify", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := h.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("liff auth: verify request: %w", err)
	}
	defer resp.Body.Close()
	var out struct {
		Sub string `json:"sub"`
		Aud string `json:"aud"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("liff auth: verify decode: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("liff auth: verify status %d", resp.StatusCode)
	}
	if out.Sub == "" {
		return "", errors.New("liff auth: missing line user id")
	}
	if out.Aud != "" && out.Aud != h.channelID {
		return "", errors.New("liff auth: audience mismatch")
	}
	return out.Sub, nil
}

func bearerToken(header string) string {
	header = strings.TrimSpace(header)
	if header == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(header), "bearer ") {
		return strings.TrimSpace(header[7:])
	}
	return ""
}

func deriveChannelID(liffID string) string {
	liffID = strings.TrimSpace(liffID)
	i := strings.Index(liffID, "-")
	if i <= 0 {
		return ""
	}
	prefix := liffID[:i]
	for _, r := range prefix {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return prefix
}

type dashboardResponse struct {
	ledger.DashboardData
	Documents dashboardDocuments `json:"documents"`
}

type dashboardDocuments struct {
	Items           []dashboardDocument `json:"items"`
	SlipCount       int                 `json:"slipCount"`
	ImageCount      int                 `json:"imageCount"`
	OtherImageCount int                 `json:"otherImageCount"`
}

type dashboardDocument struct {
	ID                string  `json:"id"`
	ImageID           string  `json:"imageId"`
	Kind              string  `json:"kind"`
	KindLabel         string  `json:"kindLabel"`
	Status            string  `json:"status"`
	Direction         string  `json:"direction"`
	DirectionLabel    string  `json:"directionLabel"`
	Amount            float64 `json:"amount,omitempty"`
	AmountText        string  `json:"amountText,omitempty"`
	FromAccountMasked string  `json:"fromAccountMasked,omitempty"`
	ToAccountMasked   string  `json:"toAccountMasked,omitempty"`
	ImageURL          string  `json:"imageUrl,omitempty"`
	ContentType       string  `json:"contentType,omitempty"`
	SizeText          string  `json:"sizeText,omitempty"`
	CreatedAt         string  `json:"createdAt"`
	Summary           string  `json:"summary,omitempty"`
}

type dashboardRange struct {
	Preset string
	Label  string
	From   time.Time
	To     time.Time
}

func parseDashboardRange(r *http.Request, now time.Time) (dashboardRange, error) {
	loc := thaitime.Location()
	now = now.In(loc)
	if now.IsZero() {
		now = thaitime.Now()
	}
	q := r.URL.Query()
	fromText := strings.TrimSpace(q.Get("from"))
	toText := strings.TrimSpace(q.Get("to"))
	if fromText != "" || toText != "" {
		from, err := parseLocalDate(fromText)
		if err != nil {
			return dashboardRange{}, err
		}
		toDay, err := parseLocalDate(toText)
		if err != nil {
			return dashboardRange{}, err
		}
		to := endOfLocalDay(toDay)
		if to.Before(from) {
			return dashboardRange{}, errors.New("range: to before from")
		}
		return dashboardRange{
			Preset: "custom",
			Label:  rangeLabel(from, to),
			From:   from,
			To:     to,
		}, nil
	}

	preset := strings.TrimSpace(q.Get("preset"))
	if preset == "" {
		preset = "this_month"
	}
	startToday := localDayStart(now)
	var from, to time.Time
	label := ""
	switch preset {
	case "today":
		from, to, label = startToday, endOfLocalDay(startToday), "วันนี้"
	case "yesterday":
		day := startToday.AddDate(0, 0, -1)
		from, to, label = day, endOfLocalDay(day), "เมื่อวาน"
	case "last_7_days":
		from, to, label = startToday.AddDate(0, 0, -6), endOfLocalDay(startToday), "7 วันล่าสุด"
	case "last_30_days":
		from, to, label = startToday.AddDate(0, 0, -29), endOfLocalDay(startToday), "30 วันล่าสุด"
	case "this_week":
		offset := int(now.Weekday())
		if offset == 0 {
			offset = 7
		}
		from, to, label = startToday.AddDate(0, 0, -(offset-1)), endOfLocalDay(startToday), "สัปดาห์นี้"
	case "this_quarter":
		month := int(now.Month())
		quarterStartMonth := time.Month(((month-1)/3)*3 + 1)
		from = time.Date(now.Year(), quarterStartMonth, 1, 0, 0, 0, 0, loc)
		to, label = endOfLocalDay(startToday), "ไตรมาสนี้"
	case "last_month":
		firstThisMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
		from = firstThisMonth.AddDate(0, -1, 0)
		to, label = firstThisMonth.Add(-time.Nanosecond), "เดือนที่แล้ว"
	case "this_year":
		from = time.Date(now.Year(), 1, 1, 0, 0, 0, 0, loc)
		to, label = endOfLocalDay(startToday), "ปีนี้"
	case "this_month":
		fallthrough
	default:
		preset = "this_month"
		from = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
		to, label = endOfLocalDay(startToday), "เดือนนี้"
	}
	return dashboardRange{Preset: preset, Label: label, From: from, To: to}, nil
}

func parseLocalDate(text string) (time.Time, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return time.Time{}, errors.New("range: date is empty")
	}
	t, err := time.ParseInLocation("2006-01-02", text, thaitime.Location())
	if err != nil {
		return time.Time{}, err
	}
	return localDayStart(t), nil
}

func parseOptionalLocalDate(text string) (time.Time, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return time.Time{}, nil
	}
	return parseLocalDate(text)
}

func localDayStart(t time.Time) time.Time {
	t = t.In(thaitime.Location())
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, thaitime.Location())
}

func endOfLocalDay(t time.Time) time.Time {
	return localDayStart(t).AddDate(0, 0, 1).Add(-time.Nanosecond)
}

func rangeLabel(from, to time.Time) string {
	return thaitime.ShortDate(from) + " - " + thaitime.ShortDate(to)
}

func (h *Handler) dashboardDocuments(ctx context.Context, identity users.Identity, from, to time.Time) (dashboardDocuments, error) {
	out := dashboardDocuments{Items: []dashboardDocument{}}
	if h.memory == nil {
		return out, nil
	}
	images, err := h.memory.ListStoredImages(ctx, identity.UserID, identity.Namespace, from, to, 500)
	if err != nil {
		return out, err
	}
	slips, err := h.memory.ListSlipRecords(ctx, identity.UserID, identity.Namespace, from, to, 500)
	if err != nil {
		return out, err
	}
	slipByImage := map[string]memory.SlipRecord{}
	for _, slip := range slips {
		slipByImage[slip.StoredImageID.Hex()] = slip
	}
	for _, img := range images {
		id := img.ID.Hex()
		doc := dashboardDocument{
			ID:          id,
			ImageID:     id,
			Kind:        "image",
			KindLabel:   "รูปภาพ",
			ImageURL:    h.signedImagePath(id),
			ContentType: img.ContentType,
			SizeText:    sizeText(img.Size),
			CreatedAt:   thaitime.ShortDateTime(img.CreatedAt),
		}
		if slip, ok := slipByImage[id]; ok {
			doc.ID = slip.ID.Hex()
			doc.Kind = "slip"
			doc.KindLabel = "สลิป/เอกสารเงิน"
			doc.Status = slip.Status
			doc.Direction = slip.Direction
			doc.DirectionLabel = directionLabel(slip.Direction)
			doc.FromAccountMasked = slip.FromAccountMasked
			doc.ToAccountMasked = slip.ToAccountMasked
			doc.Summary = slip.RawText
			if slip.Amount != nil {
				doc.Amount = *slip.Amount
				doc.AmountText = ledger.FormatMoney(*slip.Amount)
			}
			out.SlipCount++
		} else {
			out.OtherImageCount++
		}
		out.Items = append(out.Items, doc)
		out.ImageCount++
	}
	return out, nil
}

func (h *Handler) signedImagePath(id string) string {
	if id == "" || h.secret == "" {
		return ""
	}
	exp := strconv.FormatInt(time.Now().Add(15*time.Minute).Unix(), 10)
	sig, err := media.Sign(id, exp, h.secret)
	if err != nil {
		return ""
	}
	return "/v2/media/images/" + id + "?exp=" + url.QueryEscape(exp) + "&sig=" + url.QueryEscape(sig)
}

func directionLabel(direction string) string {
	switch strings.ToUpper(strings.TrimSpace(direction)) {
	case "INCOME":
		return "เงินเข้า"
	case "EXPENSE":
		return "เงินออก"
	case "TRANSFER":
		return "โอนระหว่างบัญชี"
	default:
		return "รอตรวจ"
	}
}

func sizeText(size int) string {
	if size <= 0 {
		return ""
	}
	if size >= 1024*1024 {
		return ledger.FormatMoney(float64(size)/(1024*1024)) + " MB"
	}
	return ledger.FormatMoney(float64(size)/1024) + " KB"
}

func decodeJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "bad_json"})
		return false
	}
	return true
}

func parseLocalDateTime(text string) (time.Time, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return time.Time{}, errors.New("datetime empty")
	}
	layouts := []string{"2006-01-02T15:04", time.RFC3339, "2006-01-02 15:04"}
	for _, layout := range layouts {
		var (
			t   time.Time
			err error
		)
		if layout == time.RFC3339 {
			t, err = time.Parse(layout, text)
		} else {
			t, err = time.ParseInLocation(layout, text, thaitime.Location())
		}
		if err == nil {
			return t, nil
		}
	}
	return time.Time{}, errors.New("datetime invalid")
}

func (h *Handler) logLiffEvent(ctx context.Context, identity users.Identity, eventType, content string, metadata map[string]any) {
	if h.memory == nil {
		return
	}
	if err := h.memory.SaveEventLog(ctx, memory.EventLogInput{
		OwnerID: identity.UserID, AgentID: identity.AgentID, Namespace: identity.Namespace,
		EventType: eventType, Source: "liff", Content: content, Metadata: metadata,
	}); err != nil {
		h.logger.Warn("liff: event log failed", "err", err, "namespace", identity.Namespace, "eventType", eventType)
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
