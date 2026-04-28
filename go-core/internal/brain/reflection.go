package brain

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// ConversationCollection is the minimal surface ReflectionService needs from
// MongoDB. Defining it here lets the worker inject the real *mongo.Collection
// while keeping the brain package free of memory-package coupling.
type ConversationCollection interface {
	Distinct(ctx context.Context, fieldName string, filter any, opts ...any) ([]any, error)
	FindMessages(ctx context.Context, namespace string, since time.Time, limit int) ([]ConversationDoc, error)
}

type ConversationDoc struct {
	OwnerID string
	AgentID string
	Content string
}

type ReflectionService struct {
	conv      ConversationCollection
	extractor *ExtractionService
	logger    *slog.Logger
}

func NewReflectionService(conv ConversationCollection, extractor *ExtractionService, logger *slog.Logger) *ReflectionService {
	return &ReflectionService{conv: conv, extractor: extractor, logger: logger}
}

// ReflectNamespace summarises the most recent conversation for one LINE
// userId namespace into wiki facts. Replaces noisy per-message extraction
// with one high-quality pass per user per night.
func (r *ReflectionService) ReflectNamespace(ctx context.Context, namespace string) error {
	if namespace == "" {
		return fmt.Errorf("reflection: namespace required")
	}
	const sinceHours = 24
	since := time.Now().Add(-sinceHours * time.Hour)
	docs, err := r.conv.FindMessages(ctx, namespace, since, 200)
	if err != nil {
		return fmt.Errorf("reflection: find messages: %w", err)
	}
	if len(docs) == 0 {
		return nil
	}

	blob := concatMessages(docs, 6000)
	owner := ""
	agent := ""
	if len(docs) > 0 {
		owner = docs[0].OwnerID
		agent = docs[0].AgentID
	}
	pages, edges, err := r.extractor.ExtractAndStore(ctx, ExtractInput{
		Namespace: namespace,
		OwnerID:   owner,
		AgentID:   agent,
		Text:      RedactPii(blob),
	})
	if err != nil {
		return fmt.Errorf("reflection: extract: %w", err)
	}
	r.logger.Info("reflection cycle done", "namespace", namespace, "pages", pages, "edges", edges)
	return nil
}

// ReflectAllActive iterates every namespace that produced a conversation
// message in the last 24h and reflects it in turn. Failures on one namespace
// must not block the rest.
func (r *ReflectionService) ReflectAllActive(ctx context.Context) error {
	const sinceHours = 24
	since := time.Now().Add(-sinceHours * time.Hour)
	values, err := r.conv.Distinct(ctx, "namespace", bson.M{"createdAt": bson.M{"$gte": since}})
	if err != nil {
		return fmt.Errorf("reflection: distinct: %w", err)
	}
	success := 0
	for _, v := range values {
		ns, ok := v.(string)
		if !ok || ns == "" {
			continue
		}
		if err := r.ReflectNamespace(ctx, ns); err != nil {
			r.logger.Warn("reflection failed for namespace", "namespace", ns, "err", err)
			continue
		}
		success++
	}
	r.logger.Info("reflection batch done", "active", len(values), "success", success)
	return nil
}

func concatMessages(docs []ConversationDoc, maxLen int) string {
	out := ""
	for _, d := range docs {
		if d.Content == "" {
			continue
		}
		out += d.Content + "\n"
		if len(out) >= maxLen {
			return out[:maxLen]
		}
	}
	return out
}

// MongoConversationAdapter wraps a *mongo.Collection so a real Mongo client
// satisfies ConversationCollection without leaking the bson types into the
// reflection logic.
type MongoConversationAdapter struct {
	Coll *mongo.Collection
}

func (m *MongoConversationAdapter) Distinct(ctx context.Context, field string, filter any, _ ...any) ([]any, error) {
	return m.Coll.Distinct(ctx, field, filter)
}

func (m *MongoConversationAdapter) FindMessages(ctx context.Context, namespace string, since time.Time, limit int) ([]ConversationDoc, error) {
	cur, err := m.Coll.Find(ctx, bson.M{
		"namespace": namespace,
		"createdAt": bson.M{"$gte": since},
	})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var raw []struct {
		OwnerID string `bson:"ownerId"`
		AgentID string `bson:"agentId"`
		Content string `bson:"content"`
	}
	if err := cur.All(ctx, &raw); err != nil {
		return nil, err
	}
	if len(raw) > limit {
		raw = raw[:limit]
	}
	out := make([]ConversationDoc, len(raw))
	for i, r := range raw {
		out[i] = ConversationDoc{OwnerID: r.OwnerID, AgentID: r.AgentID, Content: r.Content}
	}
	return out, nil
}
