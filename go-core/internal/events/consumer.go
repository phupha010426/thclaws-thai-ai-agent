// Package events wires the Go core to NATS JetStream so it can react to
// LINE / ledger / wiki events emitted by the Node service.
//
// Tenant safety: every subject in the stream is shaped as `<family>.<*>.<ns>`
// where `<ns>` is the LINE userId-scoped namespace. Consumers receive only
// the namespace tail of each subject and use it to route logic; there is no
// path that can blur two LINE userIds together.
package events

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const StreamName = "thaiagent"

type Handler func(ctx context.Context, namespace string, subject string, data []byte) error

type Consumer struct {
	logger  *slog.Logger
	conn    *nats.Conn
	js      jetstream.JetStream
	consumer jetstream.Consumer
	stop    context.CancelFunc
}

type Options struct {
	URL          string
	Durable      string
	FilterSubjects []string
	Handler      Handler
}

func Connect(parent context.Context, logger *slog.Logger, opts Options) (*Consumer, error) {
	if opts.URL == "" {
		return nil, errors.New("nats url empty")
	}
	if opts.Handler == nil {
		return nil, errors.New("handler required")
	}
	conn, err := nats.Connect(opts.URL,
		nats.Name("thaiagent-core"),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
	)
	if err != nil {
		return nil, err
	}
	js, err := jetstream.New(conn)
	if err != nil {
		conn.Close()
		return nil, err
	}

	// Ensure the stream exists. Either side (Node publisher or Go consumer)
	// can boot first; whichever wins creates the stream so the loser's call
	// becomes a no-op. Subject filters are intentionally identical to the
	// Node publisher's `ensureStream` config.
	if err := ensureStream(parent, js); err != nil {
		logger.Warn("ensureStream failed; consumer will retry", "err", err)
	}

	cfg := jetstream.ConsumerConfig{
		Durable:        opts.Durable,
		AckPolicy:      jetstream.AckExplicitPolicy,
		MaxAckPending:  256,
		FilterSubjects: opts.FilterSubjects,
	}
	cons, err := js.CreateOrUpdateConsumer(parent, StreamName, cfg)
	if err != nil {
		conn.Close()
		return nil, err
	}

	ctx, cancel := context.WithCancel(parent)
	c := &Consumer{logger: logger, conn: conn, js: js, consumer: cons, stop: cancel}

	go c.run(ctx, opts.Handler)
	logger.Info("nats consumer ready", "durable", opts.Durable, "filter", opts.FilterSubjects)
	return c, nil
}

func (c *Consumer) Close() {
	if c.stop != nil {
		c.stop()
	}
	if c.conn != nil {
		_ = c.conn.Drain()
	}
}

func (c *Consumer) run(ctx context.Context, handler Handler) {
	for {
		if ctx.Err() != nil {
			return
		}
		msgs, err := c.consumer.Fetch(32, jetstream.FetchMaxWait(2*time.Second))
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				c.logger.Warn("consumer fetch failed", "err", err)
			}
			time.Sleep(time.Second)
			continue
		}
		for msg := range msgs.Messages() {
			subject := msg.Subject()
			ns := namespaceOf(subject)
			if ns == "" {
				c.logger.Warn("dropping message with no namespace tail", "subject", subject)
				_ = msg.Term()
				continue
			}
			if err := handler(ctx, ns, subject, msg.Data()); err != nil {
				c.logger.Warn("handler failed; will retry", "subject", subject, "err", err)
				_ = msg.Nak()
				continue
			}
			_ = msg.Ack()
		}
	}
}

// ensureStream creates the shared "thaiagent" stream if it does not yet
// exist. Subject filters mirror the Node publisher so either side can boot
// first without producing a divergent stream config.
func ensureStream(ctx context.Context, js jetstream.JetStream) error {
	subjects := []string{
		"line.event.>",
		"thclaws.plan.>",
		"ledger.tx.>",
		"wiki.fact.>",
		"slip.image.>",
	}
	cfg := jetstream.StreamConfig{Name: StreamName, Subjects: subjects}
	if _, err := js.Stream(ctx, StreamName); err == nil {
		_, err := js.UpdateStream(ctx, cfg)
		return err
	}
	_, err := js.CreateStream(ctx, cfg)
	return err
}

// namespaceOf extracts the namespace tail of a subject (the last token)
// after validating that it is not a wildcard. We never operate on a message
// whose namespace cannot be statically pinned to a single LINE userId.
func namespaceOf(subject string) string {
	idx := strings.LastIndex(subject, ".")
	if idx < 0 || idx == len(subject)-1 {
		return ""
	}
	tail := subject[idx+1:]
	if tail == "*" || tail == ">" {
		return ""
	}
	return tail
}
