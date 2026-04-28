package line

import (
	"context"
	"encoding/json"
)

// EventEnvelope is the canonical message shape that the webhook handler
// places onto the queue and the worker dequeues.  Every field that a worker
// needs to route and process a LINE event is present here so workers never
// need to call LINE APIs to reconstruct context.
//
// Tenant isolation: LineUserID is always set before enqueue.  Workers must
// use this field — not any field derived from the raw body — as the namespace
// key for all downstream calls (memory, ledger, MinIO, etc.).
type EventEnvelope struct {
	// LineUserID is the LINE userId extracted from event.Source.UserId.
	// It is the primary tenant key.  Workers derive namespaces from it.
	LineUserID string `json:"lineUserId"`

	// ReplyToken is the one-time token to send a reply back to the user.
	// It expires after a short window; workers must use it as soon as possible.
	ReplyToken string `json:"replyToken,omitempty"`

	// EventType mirrors the LINE webhook event type (e.g. "message").
	EventType string `json:"eventType"`

	// MessageType is the LINE message content type (e.g. "text", "image").
	// Empty for non-message events.
	MessageType string `json:"messageType,omitempty"`

	// MessageID is the LINE message ID, used to download content for
	// image/video events via the blob API.
	MessageID string `json:"messageId,omitempty"`

	// Text holds the message text for text events.
	Text string `json:"text,omitempty"`

	// Raw contains the original JSON bytes of the LINE event for event types
	// that the dispatcher does not yet handle explicitly.
	Raw json.RawMessage `json:"raw,omitempty"`
}

// Enqueuer is the interface the WebhookHandler uses to put events onto a
// queue.  Keeping it as an interface lets tests inject a no-op stub and
// prevents the line package from importing the queue package (cycle risk).
type Enqueuer interface {
	EnqueueLineEvent(ctx context.Context, envelope EventEnvelope) error
}
