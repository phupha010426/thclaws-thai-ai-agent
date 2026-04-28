package line

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/line/line-bot-sdk-go/v8/linebot/webhook"
)

const maxBodyBytes = 1 << 20 // 1 MB

// WebhookHandler is the chi-compatible HTTP handler for the LINE webhook route.
// It owns signature verification, rate limiting, and event enqueue.  Business
// logic lives in the worker, not here.
type WebhookHandler struct {
	channelSecret string
	client        *Client
	rateLimiter   *RateLimiter
	enqueuer      Enqueuer
	logger        *slog.Logger
}

// NewWebhookHandler constructs a WebhookHandler.
func NewWebhookHandler(
	channelSecret string,
	client *Client,
	rateLimiter *RateLimiter,
	enqueuer Enqueuer,
	logger *slog.Logger,
) *WebhookHandler {
	return &WebhookHandler{
		channelSecret: channelSecret,
		client:        client,
		rateLimiter:   rateLimiter,
		enqueuer:      enqueuer,
		logger:        logger,
	}
}

// Handler is the http.HandlerFunc for POST /webhook/line.
func (h *WebhookHandler) Handler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// 1. Read body with hard cap — protects against large payload DoS before
	//    we do any JSON work.
	limitedBody := io.LimitReader(r.Body, maxBodyBytes)
	rawBody, err := io.ReadAll(limitedBody)
	if err != nil {
		h.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read body"})
		return
	}

	// 2. Verify HMAC-SHA256 signature — non-negotiable before parsing.
	headerSig := r.Header.Get("X-Line-Signature")
	if !VerifySignature(h.channelSecret, rawBody, headerSig) {
		h.logger.Warn("line webhook signature invalid", "bodyBytes", len(rawBody))
		h.writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid signature"})
		return
	}
	h.logger.Info("line webhook signature verified", "bodyBytes", len(rawBody))

	// 3. Parse only the fields we route on from raw JSON. The LINE SDK event
	// interface is still used as a fallback, but raw parsing keeps local tests
	// and future SDK changes from dropping replyToken/message fields.
	envelopes := h.parseRawEnvelopes(rawBody)
	if len(envelopes) > 0 {
		h.logger.Info("line webhook parsed raw envelopes", "count", len(envelopes))
	}
	if len(envelopes) == 0 {
		r.Body = io.NopCloser(bytes.NewReader(rawBody))
		cb, err := webhook.ParseRequest(h.channelSecret, r)
		if err != nil {
			// ParseRequest re-reads body via r.Body which is already drained.
			// Fall back to manual JSON parse of the raw bytes we captured.
			cb, err = h.parseRawCallback(rawBody)
			if err != nil {
				h.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
				return
			}
		}
		for _, ev := range cb.Events {
			envelope, ok := h.buildEnvelope(ev, rawBody)
			if ok {
				envelopes = append(envelopes, envelope)
			}
		}
		h.logger.Info("line webhook parsed sdk envelopes", "count", len(envelopes))
	}

	enqueued := 0
	for _, envelope := range envelopes {
		if envelope.LineUserID == "" {
			continue
		}
		h.logger.Info("line webhook event ready",
			"eventType", envelope.EventType,
			"messageType", envelope.MessageType,
			"messageId", envelope.MessageID,
			"textLength", len([]rune(envelope.Text)),
			"hasReplyToken", envelope.ReplyToken != "",
			"lineUserIdHash", redact(envelope.LineUserID),
		)

		// 4. Per-user rate limit check — after signature, before enqueue.
		result := h.rateLimiter.Check(ctx, envelope.LineUserID)
		if !result.Allowed {
			h.logger.Info("rate limit blocked; sending friendly reply",
				"lineUserIdHash", redact(envelope.LineUserID),
			)
			if envelope.ReplyToken != "" && h.client != nil {
				_ = h.client.ReplyText(ctx, envelope.ReplyToken,
					"ขณะนี้ส่งข้อความมาถี่เกินไปครับ กรุณารอสักครู่แล้วลองใหม่อีกครั้ง")
			}
			continue
		}
		h.logger.Info("line webhook rate limit passed",
			"lineUserIdHash", redact(envelope.LineUserID),
			"remaining", result.Remaining,
			"resetSeconds", result.ResetSeconds,
		)

		if err := h.enqueuer.EnqueueLineEvent(ctx, envelope); err != nil {
			h.logger.Error("failed to enqueue line event",
				"err", err,
				"lineUserIdHash", redact(envelope.LineUserID),
			)
			continue
		}
		h.logger.Info("line webhook event enqueued",
			"lineUserIdHash", redact(envelope.LineUserID),
			"messageType", envelope.MessageType,
			"messageId", envelope.MessageID,
		)
		enqueued++
	}

	h.logger.Info("line webhook processed", "total", len(envelopes), "enqueued", enqueued)
	h.writeJSON(w, http.StatusOK, map[string]int{"enqueued": enqueued})
}

// buildEnvelope converts a parsed LINE EventInterface into an EventEnvelope.
// Returns false when the event has no userID (cannot route).
func (h *WebhookHandler) buildEnvelope(ev webhook.EventInterface, rawBody []byte) (EventEnvelope, bool) {
	env := EventEnvelope{EventType: ev.GetType(), Raw: rawBody}

	switch e := ev.(type) {
	case *webhook.MessageEvent:
		if e.Source == nil {
			return env, false
		}
		userSource, ok := e.Source.(*webhook.UserSource)
		if !ok || userSource.UserId == "" {
			return env, false
		}
		env.LineUserID = userSource.UserId
		env.ReplyToken = e.ReplyToken

		switch msg := e.Message.(type) {
		case *webhook.TextMessageContent:
			env.MessageType = "text"
			env.MessageID = msg.Id
			env.Text = msg.Text
		case *webhook.ImageMessageContent:
			env.MessageType = "image"
			env.MessageID = msg.Id
		default:
			env.MessageType = e.Message.GetType()
		}

	default:
		// Non-message events (follow, unfollow, postback, etc.) have varying
		// source shapes.  We still need a userID for tenant routing.
		userID := extractUserID(ev)
		if userID == "" {
			return env, false
		}
		env.LineUserID = userID
	}

	return env, true
}

// parseRawCallback is the fallback when r.Body is already drained.
func (h *WebhookHandler) parseRawCallback(raw []byte) (*webhook.CallbackRequest, error) {
	var cb webhook.CallbackRequest
	if err := json.Unmarshal(raw, &cb); err != nil {
		return nil, fmt.Errorf("line webhook: json unmarshal: %w", err)
	}
	return &cb, nil
}

func (h *WebhookHandler) parseRawEnvelopes(raw []byte) []EventEnvelope {
	var body struct {
		Events []struct {
			Type       string `json:"type"`
			ReplyToken string `json:"replyToken"`
			Source     struct {
				Type   string `json:"type"`
				UserID string `json:"userId"`
			} `json:"source"`
			Message struct {
				Type string `json:"type"`
				ID   string `json:"id"`
				Text string `json:"text"`
			} `json:"message"`
		} `json:"events"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil
	}
	out := make([]EventEnvelope, 0, len(body.Events))
	for _, ev := range body.Events {
		if ev.Source.UserID == "" {
			continue
		}
		env := EventEnvelope{
			LineUserID:  ev.Source.UserID,
			ReplyToken:  ev.ReplyToken,
			EventType:   ev.Type,
			MessageType: ev.Message.Type,
			MessageID:   ev.Message.ID,
			Text:        ev.Message.Text,
			Raw:         raw,
		}
		out = append(out, env)
	}
	return out
}

func (h *WebhookHandler) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// extractUserID tries common source shapes for non-message events.
func extractUserID(ev webhook.EventInterface) string {
	type withUserSource interface {
		GetSource() webhook.SourceInterface
	}
	// The SDK does not define a generic GetSource on EventInterface, so we
	// use a JSON-round-trip approach only for the source field to keep this
	// path simple and test-friendly.
	type sourceHolder struct {
		Source struct {
			UserId string `json:"userId"`
		} `json:"source"`
	}
	b, err := json.Marshal(ev)
	if err != nil {
		return ""
	}
	var sh sourceHolder
	if err := json.Unmarshal(b, &sh); err != nil {
		return ""
	}
	return sh.Source.UserId
}

// redact returns the first 8 chars of a LINE userId for safe logging.
func redact(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

// contextKey is unexported to prevent collisions across packages.
type contextKey struct{ name string }

// LineUserIDKey can be used by middleware to store the verified LINE userId.
var LineUserIDKey = &contextKey{"lineUserID"}

// WithLineUserID returns a copy of ctx carrying the LINE userId.
func WithLineUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, LineUserIDKey, userID)
}

// LineUserIDFromContext retrieves the LINE userId stored by WithLineUserID.
func LineUserIDFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(LineUserIDKey).(string)
	return v, ok && v != ""
}
