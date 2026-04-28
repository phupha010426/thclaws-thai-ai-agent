package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Publisher wraps a NATS JetStream connection and enforces the subject naming
// contract: every published subject must end with a non-wildcard namespace tail
// so consumers can filter on a single LINE userId without receiving payloads
// belonging to another tenant.
type Publisher struct {
	conn       *nats.Conn
	js         jetstream.JetStream
	streamName string
	logger     *slog.Logger
}

// NewPublisher connects to NATS, ensures the shared stream exists, and returns
// a Publisher ready to use. streamName is the JetStream stream that must cover
// all subjects the relay will publish to.
func NewPublisher(ctx context.Context, natsURL, streamName string, logger *slog.Logger) (*Publisher, error) {
	if natsURL == "" {
		return nil, fmt.Errorf("events: nats url must not be empty")
	}
	conn, err := nats.Connect(natsURL,
		nats.Name("thaiagent-go-publisher"),
		nats.MaxReconnects(-1),
	)
	if err != nil {
		return nil, fmt.Errorf("events: nats connect: %w", err)
	}
	js, err := jetstream.New(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("events: jetstream init: %w", err)
	}

	// ensureStream is shared with consumer.go so both sides produce an
	// identical stream config regardless of which boots first.
	if err := ensureStream(ctx, js); err != nil {
		logger.WarnContext(ctx, "ensureStream failed; publisher will continue", "err", err)
	}

	logger.InfoContext(ctx, "nats publisher ready", "url", natsURL, "stream", streamName)
	return &Publisher{conn: conn, js: js, streamName: streamName, logger: logger}, nil
}

// Publish serialises payload as JSON and publishes it to subject. The subject
// must end with a namespace tail (last dot-separated token, non-wildcard,
// non-empty) because the JetStream consumer filters on that tail to fence
// individual LINE users from seeing each other's messages.
func (p *Publisher) Publish(ctx context.Context, subject string, payload any) error {
	if !isNamespacedSubject(subject) {
		// Hard reject: we will never publish without a namespace because a
		// wildcard or empty tail would fan the payload out to every consumer.
		return fmt.Errorf("events: subject %q must end with a non-wildcard namespace tail", subject)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("events: marshal payload for %q: %w", subject, err)
	}
	if _, err := p.js.Publish(ctx, subject, data); err != nil {
		return fmt.Errorf("events: nats publish %q: %w", subject, err)
	}
	return nil
}

// Close drains the NATS connection, waiting for in-flight publishes to settle.
func (p *Publisher) Close() error {
	if p.conn != nil {
		if err := p.conn.Drain(); err != nil {
			return fmt.Errorf("events: nats drain: %w", err)
		}
	}
	return nil
}

// isNamespacedSubject mirrors the TypeScript helper in nats-publisher.ts.
// A valid subject has at least three dot-separated tokens and the last token
// must be a non-empty, non-wildcard string so it maps to exactly one namespace.
func isNamespacedSubject(subject string) bool {
	parts := strings.Split(subject, ".")
	if len(parts) < 3 {
		return false
	}
	last := parts[len(parts)-1]
	return last != "" && last != "*" && last != ">"
}
