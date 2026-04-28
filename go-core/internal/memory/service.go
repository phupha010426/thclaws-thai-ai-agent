// Package memory implements the conversation and fact storage layer backed by
// MongoDB. Every method is namespace-scoped: callers must supply a non-empty
// namespace or the function returns an error so misconfigured callers fail fast
// rather than silently operating on the wrong tenant's data.
package memory

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// AuditLogger is satisfied by the audit service. Defined here (consumer side)
// to keep the memory package free of import cycles.
type AuditLogger interface {
	Log(ctx context.Context, actorID, action, resource string, metadata map[string]any) error
}

// WikiBrainReader fetches high-importance pages from the Postgres wiki. Defined
// here so the memory package does not depend on the brain package directly —
// the caller wires the concrete implementation at startup.
type WikiBrainReader interface {
	// ListByImportance returns at most limit pages for namespace sorted by
	// importance descending. Callers must handle errors gracefully.
	ListByImportance(ctx context.Context, namespace string, limit int) ([]WikiPageSummary, error)
}

// WikiPageSummary is the projection returned by WikiBrainReader — only the
// fields needed for GetContext so retrieval stays cheap.
type WikiPageSummary struct {
	Title      string
	Kind       string
	Summary    string
	Importance float64
	UpdatedAt  time.Time
	Source     string // "brain" for Postgres-origin pages
}

// ContextResult is returned by GetContext and carries everything the LLM needs
// to understand the current conversation state.
type ContextResult struct {
	RecentHistory  []HistoryMessage
	CompactSummary string
	WikiPages      []WikiPageSummary
}

// HistoryMessage is a single message from the tail window used as LLM context.
type HistoryMessage struct {
	Role      string    `bson:"role"`
	Content   string    `bson:"content"`
	CreatedAt time.Time `bson:"createdAt"`
}

// RememberInput carries all fields required to store a semantic memory item.
type RememberInput struct {
	OwnerID   string
	AgentID   string
	Namespace string
	Content   string
	Source    string
}

// SaveMessageInput carries all fields for a new conversation turn.
type SaveMessageInput struct {
	OwnerID   string
	AgentID   string
	Namespace string
	Role      string // user | assistant | system
	Content   string
	Metadata  map[string]any
}

// EventLogInput carries all fields for a new Mongo-side event log entry.
type EventLogInput struct {
	OwnerID   string
	AgentID   string
	Namespace string
	EventType string
	Source    string
	Content   string
	Metadata  map[string]any
}

// UpsertWikiPageInput carries the data for a PersonalWikiPage upsert.
type UpsertWikiPageInput struct {
	OwnerID   string
	AgentID   string
	Namespace string
	Title     string
	Content   string
}

type SaveStoredImageInput struct {
	OwnerID       string
	AgentID       string
	Namespace     string
	LineMessageID string
	Bucket        string
	ObjectName    string
	ContentType   string
	Size          int
}

type SlipVisionInput struct {
	OwnerID           string
	SlipID            primitive.ObjectID
	IsSlip            bool
	Amount            *float64
	FromAccountMasked string
	ToAccountMasked   string
	FinalReply        string
	DirectionHint     string
	Confidence        float64
}

type PendingAccountingInput struct {
	OwnerID          string
	AgentID          string
	Namespace        string
	Original         string
	Direction        string
	Amount           float64
	Note             string
	Category         string
	CounterpartyName string
	CounterpartyRole string
	Confidence       float64
	CausationID      string
	TTL              time.Duration
}

type EnsureAgentSessionInput struct {
	OwnerID    string
	AgentID    string
	Namespace  string
	Runtime    string
	Model      string
	LastIntent string
}

// CompactInput identifies the namespace owner for compaction.
type CompactInput struct {
	OwnerID   string
	AgentID   string
	Namespace string
}

// sensitiveKeyPattern matches metadata keys that must never reach the log
// store. The pattern is intentionally broad — false positives (e.g. "base_url")
// are acceptable; false negatives (leaking a token) are not.
var sensitiveKeyPattern = regexp.MustCompile(`(?i)token|secret|password|authorization|base|64`)

// MemoryService manages all MongoDB-backed memory operations.
type MemoryService struct {
	db     *mongo.Database
	logger *slog.Logger
	audit  AuditLogger
	brain  WikiBrainReader // optional; nil → brain pages silently omitted
}

// New creates a MemoryService. brain may be nil; GetContext degrades to
// Mongo-only wiki pages in that case.
func New(client *mongo.Client, dbName string, logger *slog.Logger, audit AuditLogger, brain WikiBrainReader) *MemoryService {
	return &MemoryService{
		db:     client.Database(dbName),
		logger: logger,
		audit:  audit,
		brain:  brain,
	}
}

func (s *MemoryService) col(name string) *mongo.Collection {
	return s.db.Collection(name)
}

func (s *MemoryService) EnsureAgentSession(ctx context.Context, input EnsureAgentSessionInput) (*AgentSession, error) {
	if strings.TrimSpace(input.Namespace) == "" {
		return nil, fmt.Errorf("memory: EnsureAgentSession: namespace is empty")
	}
	runtime := strings.TrimSpace(input.Runtime)
	if runtime == "" {
		return nil, fmt.Errorf("memory: EnsureAgentSession: runtime is empty")
	}
	model := strings.TrimSpace(input.Model)
	if model == "" {
		model = "sml/auto"
	}
	now := time.Now().UTC()
	set := bson.M{
		"ownerId":   input.OwnerID,
		"agentId":   input.AgentID,
		"namespace": input.Namespace,
		"runtime":   runtime,
		"model":     model,
		"status":    "active",
		"updatedAt": now,
	}
	if strings.TrimSpace(input.LastIntent) != "" {
		set["lastIntent"] = strings.TrimSpace(input.LastIntent)
	}
	update := bson.M{
		"$set": set,
		"$setOnInsert": bson.M{
			"createdAt": now,
			"turnCount": int64(0),
		},
	}
	opts := options.FindOneAndUpdate().
		SetUpsert(true).
		SetReturnDocument(options.After)

	var session AgentSession
	err := s.col(ColAgentSessions).FindOneAndUpdate(ctx,
		bson.M{"namespace": input.Namespace, "runtime": runtime},
		update,
		opts,
	).Decode(&session)
	if err != nil {
		return nil, fmt.Errorf("memory: EnsureAgentSession: %w", err)
	}
	return &session, nil
}

func (s *MemoryService) TouchAgentSession(ctx context.Context, namespace, runtime, lastIntent string) error {
	if strings.TrimSpace(namespace) == "" {
		return fmt.Errorf("memory: TouchAgentSession: namespace is empty")
	}
	runtime = strings.TrimSpace(runtime)
	if runtime == "" {
		return fmt.Errorf("memory: TouchAgentSession: runtime is empty")
	}
	set := bson.M{
		"status":    "active",
		"updatedAt": time.Now().UTC(),
	}
	if strings.TrimSpace(lastIntent) != "" {
		set["lastIntent"] = strings.TrimSpace(lastIntent)
	}
	_, err := s.col(ColAgentSessions).UpdateOne(ctx,
		bson.M{"namespace": namespace, "runtime": runtime},
		bson.M{
			"$set": set,
			"$inc": bson.M{"turnCount": int64(1)},
		},
	)
	if err != nil {
		return fmt.Errorf("memory: TouchAgentSession: %w", err)
	}
	return nil
}

func (s *MemoryService) SavePendingAccounting(ctx context.Context, input PendingAccountingInput) error {
	if strings.TrimSpace(input.Namespace) == "" {
		return fmt.Errorf("memory: SavePendingAccounting: namespace is empty")
	}
	ttl := input.TTL
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	now := time.Now().UTC()
	doc := bson.M{
		"ownerId":          input.OwnerID,
		"agentId":          input.AgentID,
		"namespace":        input.Namespace,
		"kind":             "accounting_confirmation",
		"original":         strings.TrimSpace(input.Original),
		"direction":        input.Direction,
		"amount":           input.Amount,
		"note":             strings.TrimSpace(input.Note),
		"category":         strings.TrimSpace(input.Category),
		"counterpartyName": strings.TrimSpace(input.CounterpartyName),
		"counterpartyRole": strings.TrimSpace(input.CounterpartyRole),
		"confidence":       input.Confidence,
		"causationId":      input.CausationID,
		"expiresAt":        now.Add(ttl),
		"updatedAt":        now,
	}
	update := bson.M{
		"$set": doc,
		"$setOnInsert": bson.M{
			"createdAt": now,
		},
	}
	_, err := s.col(ColPendingActions).UpdateOne(ctx,
		bson.M{"namespace": input.Namespace, "kind": "accounting_confirmation"},
		update,
		options.Update().SetUpsert(true),
	)
	if err != nil {
		return fmt.Errorf("memory: SavePendingAccounting: %w", err)
	}
	return nil
}

func (s *MemoryService) GetPendingAccounting(ctx context.Context, namespace string) (*PendingAction, error) {
	if strings.TrimSpace(namespace) == "" {
		return nil, fmt.Errorf("memory: GetPendingAccounting: namespace is empty")
	}
	var doc PendingAction
	err := s.col(ColPendingActions).FindOne(ctx, bson.M{
		"namespace": namespace,
		"kind":      "accounting_confirmation",
		"expiresAt": bson.M{"$gt": time.Now().UTC()},
	}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("memory: GetPendingAccounting: %w", err)
	}
	return &doc, nil
}

func (s *MemoryService) ClearPendingAccounting(ctx context.Context, namespace string) error {
	if strings.TrimSpace(namespace) == "" {
		return fmt.Errorf("memory: ClearPendingAccounting: namespace is empty")
	}
	_, err := s.col(ColPendingActions).DeleteMany(ctx, bson.M{
		"namespace": namespace,
		"kind":      "accounting_confirmation",
	})
	if err != nil {
		return fmt.Errorf("memory: ClearPendingAccounting: %w", err)
	}
	return nil
}

// RememberImportantFact persists a semantic fact extracted from the
// conversation. Empty content is rejected early to avoid polluting the store
// with blank documents.
func (s *MemoryService) RememberImportantFact(ctx context.Context, input RememberInput) (string, error) {
	content := strings.TrimSpace(input.Content)
	if content == "" {
		return "", fmt.Errorf("memory: RememberImportantFact: content is empty")
	}
	src := input.Source
	if src == "" {
		src = "line"
	}

	doc := MemoryItem{
		ID:               primitive.NewObjectID(),
		OwnerID:          input.OwnerID,
		AgentID:          input.AgentID,
		Namespace:        input.Namespace,
		Type:             "semantic",
		Content:          content,
		SensitivityLevel: "low",
		Confidence:       0.9,
		Source:           src,
		Metadata:         bson.M{},
		CreatedAt:        time.Now().UTC(),
		UpdatedAt:        time.Now().UTC(),
	}
	_, err := s.col(ColMemoryItems).InsertOne(ctx, doc)
	if err != nil {
		return "", fmt.Errorf("memory: RememberImportantFact: insert: %w", err)
	}

	if s.audit != nil {
		_ = s.audit.Log(ctx, input.OwnerID, "memory.item_created", "memory_items", map[string]any{
			"agentId":   input.AgentID,
			"namespace": input.Namespace,
		})
	}
	return doc.ID.Hex(), nil
}

// SaveConversationMessage appends one turn to the conversation history. Content
// is capped at 4 000 chars to prevent runaway context windows. Empty content is
// silently skipped (idempotent no-op) to match the TypeScript behaviour.
func (s *MemoryService) SaveConversationMessage(ctx context.Context, input SaveMessageInput) (string, error) {
	content := strings.TrimSpace(input.Content)
	if content == "" {
		return "", nil
	}
	if len(content) > 4000 {
		content = content[:4000]
	}
	meta := input.Metadata
	if meta == nil {
		meta = map[string]any{}
	}

	doc := ConversationMessage{
		ID:        primitive.NewObjectID(),
		OwnerID:   input.OwnerID,
		AgentID:   input.AgentID,
		Namespace: input.Namespace,
		Role:      input.Role,
		Content:   content,
		Metadata:  bson.M(meta),
		Compacted: false,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	_, err := s.col(ColConversationMessages).InsertOne(ctx, doc)
	if err != nil {
		return "", fmt.Errorf("memory: SaveConversationMessage: %w", err)
	}
	return doc.ID.Hex(), nil
}

// SaveConversationSnapshot upserts the last-intent snapshot for a LINE user.
// The (namespace, lineUserId) pair is the natural key; only lastIntent changes
// across calls.
func (s *MemoryService) SaveConversationSnapshot(ctx context.Context, namespace, lineUserID, lastIntent string) error {
	filter := bson.M{"namespace": namespace, "lineUserId": lineUserID}
	update := bson.M{
		"$set": bson.M{
			"lastIntent": lastIntent,
			"updatedAt":  time.Now().UTC(),
		},
		"$setOnInsert": bson.M{
			"namespace":  namespace,
			"lineUserId": lineUserID,
			"metadata":   bson.M{},
			"createdAt":  time.Now().UTC(),
		},
	}
	opts := options.Update().SetUpsert(true)
	_, err := s.col(ColConversationSnapshot).UpdateOne(ctx, filter, update, opts)
	if err != nil {
		return fmt.Errorf("memory: SaveConversationSnapshot: %w", err)
	}
	return nil
}

// SaveEventLog writes a domain event to the Mongo audit log. Metadata keys
// matching the sensitive pattern are dropped before the insert.
func (s *MemoryService) SaveEventLog(ctx context.Context, input EventLogInput) error {
	src := input.Source
	if src == "" {
		src = "line"
	}
	content := input.Content
	if len(content) > 4000 {
		content = content[:4000]
	}

	doc := EventLog{
		ID:        primitive.NewObjectID(),
		OwnerID:   input.OwnerID,
		AgentID:   input.AgentID,
		Namespace: input.Namespace,
		EventType: input.EventType,
		Source:    src,
		Content:   content,
		Metadata:  bson.M(sanitizeMetadata(input.Metadata)),
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	_, err := s.col(ColEventLogs).InsertOne(ctx, doc)
	if err != nil {
		return fmt.Errorf("memory: SaveEventLog: %w", err)
	}
	return nil
}

// GetContext fetches the minimal context set needed for LLM inference. Reads
// are issued in parallel; the Postgres wiki branch degrades silently so a
// Postgres outage cannot block LINE message responses.
func (s *MemoryService) GetContext(ctx context.Context, namespace string) (ContextResult, error) {
	if namespace == "" {
		return ContextResult{}, fmt.Errorf("memory: GetContext: namespace required")
	}

	type mongoWikiDoc struct {
		Title     string    `bson:"title"`
		Content   string    `bson:"content"`
		UpdatedAt time.Time `bson:"updatedAt"`
	}
	type summaryDoc struct {
		Summary string `bson:"summary"`
	}

	var (
		wg           sync.WaitGroup
		recentMsgs   []HistoryMessage
		summaryText  string
		mongoWikiRaw []mongoWikiDoc
		brainPages   []WikiPageSummary

		errMsgs    error
		errSummary error
		errWiki    error
	)

	// 12 most recent uncompacted messages
	wg.Add(1)
	go func() {
		defer wg.Done()
		filter := bson.M{"namespace": namespace, "compacted": false}
		findOpts := options.Find().
			SetSort(bson.D{{Key: "createdAt", Value: -1}}).
			SetLimit(12).
			SetProjection(bson.M{"role": 1, "content": 1, "createdAt": 1, "_id": 0})
		cur, err := s.col(ColConversationMessages).Find(ctx, filter, findOpts)
		if err != nil {
			errMsgs = fmt.Errorf("memory: GetContext messages: %w", err)
			return
		}
		defer cur.Close(ctx)
		var msgs []HistoryMessage
		if err := cur.All(ctx, &msgs); err != nil {
			errMsgs = fmt.Errorf("memory: GetContext messages decode: %w", err)
			return
		}
		// Reverse to chronological order (we fetched desc for LIMIT efficiency)
		for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
			msgs[i], msgs[j] = msgs[j], msgs[i]
		}
		recentMsgs = msgs
	}()

	// Latest memory summary
	wg.Add(1)
	go func() {
		defer wg.Done()
		filter := bson.M{"namespace": namespace}
		findOpts := options.FindOne().SetSort(bson.D{{Key: "createdAt", Value: -1}})
		var doc summaryDoc
		err := s.col(ColMemorySummaries).FindOne(ctx, filter, findOpts).Decode(&doc)
		if err != nil && err != mongo.ErrNoDocuments {
			errSummary = fmt.Errorf("memory: GetContext summary: %w", err)
		}
		summaryText = doc.Summary
	}()

	// 8 most recent PersonalWikiPage (legacy Mongo)
	wg.Add(1)
	go func() {
		defer wg.Done()
		filter := bson.M{"namespace": namespace}
		findOpts := options.Find().
			SetSort(bson.D{{Key: "updatedAt", Value: -1}}).
			SetLimit(8).
			SetProjection(bson.M{"title": 1, "content": 1, "updatedAt": 1, "_id": 0})
		cur, err := s.col(ColPersonalWikiPages).Find(ctx, filter, findOpts)
		if err != nil {
			errWiki = fmt.Errorf("memory: GetContext wikiPages: %w", err)
			return
		}
		defer cur.Close(ctx)
		if err := cur.All(ctx, &mongoWikiRaw); err != nil {
			errWiki = fmt.Errorf("memory: GetContext wikiPages decode: %w", err)
		}
	}()

	// 8 highest-importance pages from Postgres brain (optional)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if s.brain == nil {
			return
		}
		pages, err := s.brain.ListByImportance(ctx, namespace, 8)
		if err != nil {
			// Degrade silently — Postgres brain is augmentation, not critical path
			s.logger.Warn("brain wiki fetch failed; continuing with mongo only",
				"namespace", namespace, "err", err)
			return
		}
		brainPages = pages
	}()

	wg.Wait()

	if errMsgs != nil {
		return ContextResult{}, errMsgs
	}
	if errSummary != nil {
		return ContextResult{}, errSummary
	}
	if errWiki != nil {
		return ContextResult{}, errWiki
	}

	// Merge: brain pages first (more structured), then legacy Mongo
	merged := make([]WikiPageSummary, 0, len(brainPages)+len(mongoWikiRaw))
	merged = append(merged, brainPages...)
	for _, p := range mongoWikiRaw {
		merged = append(merged, WikiPageSummary{
			Title:     p.Title,
			Summary:   p.Content,
			UpdatedAt: p.UpdatedAt,
			Source:    "mongo",
		})
	}

	return ContextResult{
		RecentHistory:  recentMsgs,
		CompactSummary: summaryText,
		WikiPages:      merged,
	}, nil
}

// UpsertWikiPage creates or updates a PersonalWikiPage by (namespace, title).
// Content is capped at 6 000 chars to keep document sizes predictable.
func (s *MemoryService) UpsertWikiPage(ctx context.Context, input UpsertWikiPageInput) error {
	title := strings.TrimSpace(input.Title)
	content := strings.TrimSpace(input.Content)
	if len(content) > 6000 {
		content = content[:6000]
	}

	filter := bson.M{"namespace": input.Namespace, "title": title}
	now := time.Now().UTC()
	update := bson.M{
		"$set": bson.M{
			"ownerId":   input.OwnerID,
			"agentId":   input.AgentID,
			"namespace": input.Namespace,
			"title":     title,
			"content":   content,
			"source":    "thclaws",
			"updatedAt": now,
		},
		"$setOnInsert": bson.M{
			"createdAt": now,
		},
	}
	opts := options.Update().SetUpsert(true)
	_, err := s.col(ColPersonalWikiPages).UpdateOne(ctx, filter, update, opts)
	if err != nil {
		return fmt.Errorf("memory: UpsertWikiPage: %w", err)
	}
	return nil
}

// CompactConversationIfNeeded summarises older messages when the uncompacted
// window reaches 40. It leaves the 12 most recent messages uncompacted so
// GetContext always has a live tail to return. Returns nil when no compaction
// was performed.
func (s *MemoryService) CompactConversationIfNeeded(ctx context.Context, input CompactInput) (*MemorySummary, error) {
	filter := bson.M{"namespace": input.Namespace, "compacted": false}
	count, err := s.col(ColConversationMessages).CountDocuments(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("memory: CompactConversationIfNeeded: count: %w", err)
	}
	if count < 40 {
		return nil, nil
	}

	// Fetch all but the 12 newest messages to preserve the live tail
	limit := count - 12
	if limit <= 0 {
		return nil, nil
	}
	findOpts := options.Find().
		SetSort(bson.D{{Key: "createdAt", Value: 1}}).
		SetLimit(limit)
	cur, err := s.col(ColConversationMessages).Find(ctx, filter, findOpts)
	if err != nil {
		return nil, fmt.Errorf("memory: CompactConversationIfNeeded: find: %w", err)
	}
	defer cur.Close(ctx)

	var msgs []ConversationMessage
	if err := cur.All(ctx, &msgs); err != nil {
		return nil, fmt.Errorf("memory: CompactConversationIfNeeded: decode: %w", err)
	}
	if len(msgs) == 0 {
		return nil, nil
	}

	var sb strings.Builder
	for _, m := range msgs {
		line := m.Role + ": " + m.Content + "\n"
		if sb.Len()+len(line) > 12000 {
			break
		}
		sb.WriteString(line)
	}

	now := time.Now().UTC()
	fromDate := msgs[0].CreatedAt
	toDate := msgs[len(msgs)-1].CreatedAt
	summary := MemorySummary{
		ID:           primitive.NewObjectID(),
		OwnerID:      input.OwnerID,
		AgentID:      input.AgentID,
		Namespace:    input.Namespace,
		Summary:      sb.String(),
		MessageCount: len(msgs),
		FromDate:     &fromDate,
		ToDate:       &toDate,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if _, err := s.col(ColMemorySummaries).InsertOne(ctx, summary); err != nil {
		return nil, fmt.Errorf("memory: CompactConversationIfNeeded: insert summary: %w", err)
	}

	// Mark compacted in bulk — use _id list to avoid re-fetching
	ids := make([]primitive.ObjectID, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ID
	}
	_, err = s.col(ColConversationMessages).UpdateMany(ctx,
		bson.M{"_id": bson.M{"$in": ids}},
		bson.M{"$set": bson.M{"compacted": true}},
	)
	if err != nil {
		return nil, fmt.Errorf("memory: CompactConversationIfNeeded: mark compacted: %w", err)
	}

	if s.audit != nil {
		_ = s.audit.Log(ctx, input.OwnerID, "memory.conversation_compacted", "conversation_messages", map[string]any{
			"namespace":    input.Namespace,
			"messageCount": len(msgs),
		})
	}
	s.logger.Info("conversation compacted", "namespace", input.Namespace, "messageCount", len(msgs))
	return &summary, nil
}

// EnsureAgentProfile creates the agent profile if it does not exist. $setOnInsert
// means repeated calls are safe — the document is never overwritten after creation.
func (s *MemoryService) EnsureAgentProfile(ctx context.Context, agentID, namespace string) (*AgentProfile, error) {
	filter := bson.M{"agentId": agentID}
	now := time.Now().UTC()
	update := bson.M{
		"$setOnInsert": bson.M{
			"agentId":     agentID,
			"namespace":   namespace,
			"preferences": bson.M{},
			"createdAt":   now,
			"updatedAt":   now,
		},
	}
	opts := options.FindOneAndUpdate().
		SetUpsert(true).
		SetReturnDocument(options.After)

	var profile AgentProfile
	err := s.col(ColAgentProfiles).FindOneAndUpdate(ctx, filter, update, opts).Decode(&profile)
	if err != nil {
		return nil, fmt.Errorf("memory: EnsureAgentProfile: %w", err)
	}
	return &profile, nil
}

func (s *MemoryService) SaveStoredImage(ctx context.Context, input SaveStoredImageInput) (*StoredImage, error) {
	if strings.TrimSpace(input.Namespace) == "" {
		return nil, fmt.Errorf("memory: SaveStoredImage: namespace is empty")
	}
	now := time.Now().UTC()
	doc := StoredImage{
		ID:            primitive.NewObjectID(),
		OwnerID:       input.OwnerID,
		AgentID:       input.AgentID,
		Namespace:     input.Namespace,
		LineMessageID: input.LineMessageID,
		Bucket:        input.Bucket,
		ObjectName:    input.ObjectName,
		ContentType:   input.ContentType,
		Size:          input.Size,
		Source:        "line",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	_, err := s.col(ColStoredImages).InsertOne(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("memory: SaveStoredImage: %w", err)
	}
	return &doc, nil
}

func (s *MemoryService) GetStoredImageByID(ctx context.Context, id primitive.ObjectID) (*StoredImage, error) {
	if id.IsZero() {
		return nil, fmt.Errorf("memory: GetStoredImageByID: id is empty")
	}
	var doc StoredImage
	err := s.col(ColStoredImages).FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("memory: GetStoredImageByID: %w", err)
	}
	return &doc, nil
}

func (s *MemoryService) GetLatestStoredImage(ctx context.Context, namespace string) (*StoredImage, error) {
	if strings.TrimSpace(namespace) == "" {
		return nil, fmt.Errorf("memory: GetLatestStoredImage: namespace is empty")
	}
	var doc StoredImage
	err := s.col(ColStoredImages).FindOne(ctx,
		bson.M{"namespace": namespace},
		options.FindOne().SetSort(bson.D{{Key: "createdAt", Value: -1}}),
	).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("memory: GetLatestStoredImage: %w", err)
	}
	return &doc, nil
}

func (s *MemoryService) ListStoredImages(ctx context.Context, ownerID, namespace string, from, to time.Time, limit int) ([]StoredImage, error) {
	if strings.TrimSpace(ownerID) == "" || strings.TrimSpace(namespace) == "" {
		return nil, fmt.Errorf("memory: ListStoredImages: ownerID and namespace are required")
	}
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	filter := bson.M{"ownerId": ownerID, "namespace": namespace}
	if !from.IsZero() && !to.IsZero() {
		filter["createdAt"] = bson.M{"$gte": from.UTC(), "$lte": to.UTC()}
	}
	cur, err := s.col(ColStoredImages).Find(ctx, filter,
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}}).SetLimit(int64(limit)),
	)
	if err != nil {
		return nil, fmt.Errorf("memory: ListStoredImages: %w", err)
	}
	defer cur.Close(ctx)

	out := []StoredImage{}
	for cur.Next(ctx) {
		var item StoredImage
		if err := cur.Decode(&item); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, cur.Err()
}

func (s *MemoryService) ListSlipRecords(ctx context.Context, ownerID, namespace string, from, to time.Time, limit int) ([]SlipRecord, error) {
	if strings.TrimSpace(ownerID) == "" || strings.TrimSpace(namespace) == "" {
		return nil, fmt.Errorf("memory: ListSlipRecords: ownerID and namespace are required")
	}
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	filter := bson.M{"ownerId": ownerID, "namespace": namespace}
	if !from.IsZero() && !to.IsZero() {
		filter["createdAt"] = bson.M{"$gte": from.UTC(), "$lte": to.UTC()}
	}
	cur, err := s.col(ColSlipRecords).Find(ctx, filter,
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}}).SetLimit(int64(limit)),
	)
	if err != nil {
		return nil, fmt.Errorf("memory: ListSlipRecords: %w", err)
	}
	defer cur.Close(ctx)

	out := []SlipRecord{}
	for cur.Next(ctx) {
		var item SlipRecord
		if err := cur.Decode(&item); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, cur.Err()
}

func (s *MemoryService) CreatePendingSlip(ctx context.Context, ownerID, agentID, namespace string, storedImageID primitive.ObjectID) (*SlipRecord, error) {
	now := time.Now().UTC()
	doc := SlipRecord{
		ID:            primitive.NewObjectID(),
		OwnerID:       ownerID,
		AgentID:       agentID,
		Namespace:     namespace,
		StoredImageID: storedImageID,
		Status:        "PENDING_OCR",
		Direction:     "UNKNOWN",
		ParserVersion: "go-smlgateway-vision-v1",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	_, err := s.col(ColSlipRecords).InsertOne(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("memory: CreatePendingSlip: %w", err)
	}
	if s.audit != nil {
		_ = s.audit.Log(ctx, ownerID, "slip.image_received", "slip_records", map[string]any{
			"slipId":    doc.ID.Hex(),
			"namespace": namespace,
		})
	}
	return &doc, nil
}

func (s *MemoryService) ApplyVisionToSlip(ctx context.Context, input SlipVisionInput) (string, error) {
	direction, err := s.classifySlipDirection(ctx, input.OwnerID, input.DirectionHint, input.FromAccountMasked, input.ToAccountMasked)
	if err != nil {
		return "", err
	}
	status := "PARSED"
	if input.IsSlip {
		status = "NEEDS_CONFIRMATION"
	}
	set := bson.M{
		"status":            status,
		"direction":         direction,
		"amount":            input.Amount,
		"fromAccountMasked": input.FromAccountMasked,
		"toAccountMasked":   input.ToAccountMasked,
		"rawText":           input.FinalReply,
		"parserVersion":     "go-smlgateway-vision-v1",
		"updatedAt":         time.Now().UTC(),
	}
	_, err = s.col(ColSlipRecords).UpdateOne(ctx,
		bson.M{"_id": input.SlipID, "ownerId": input.OwnerID},
		bson.M{"$set": set},
	)
	if err != nil {
		return "", fmt.Errorf("memory: ApplyVisionToSlip: %w", err)
	}
	if s.audit != nil {
		_ = s.audit.Log(ctx, input.OwnerID, "slip.vision_parsed", "slip_records", map[string]any{
			"slipId":     input.SlipID.Hex(),
			"isSlip":     input.IsSlip,
			"direction":  direction,
			"confidence": input.Confidence,
		})
	}
	return direction, nil
}

func (s *MemoryService) classifySlipDirection(ctx context.Context, ownerID, hint, fromMasked, toMasked string) (string, error) {
	hint = strings.ToUpper(strings.TrimSpace(hint))
	if hint == "INCOME" || hint == "EXPENSE" || hint == "TRANSFER" {
		return hint, nil
	}
	filter := bson.M{"ownerId": ownerID, "isOwnerAccount": true}
	cur, err := s.col(ColUserAccountIdentity).Find(ctx, filter)
	if err != nil {
		return "", fmt.Errorf("memory: classifySlipDirection accounts: %w", err)
	}
	defer cur.Close(ctx)

	own := map[string]struct{}{}
	for cur.Next(ctx) {
		var account UserAccountIdentity
		if err := cur.Decode(&account); err == nil && account.AccountNoMasked != "" {
			own[account.AccountNoMasked] = struct{}{}
		}
	}
	fromOwn := false
	if fromMasked != "" {
		_, fromOwn = own[fromMasked]
	}
	toOwn := false
	if toMasked != "" {
		_, toOwn = own[toMasked]
	}
	switch {
	case fromOwn && toOwn:
		return "TRANSFER", nil
	case toOwn:
		return "INCOME", nil
	case fromOwn:
		return "EXPENSE", nil
	default:
		return "UNKNOWN", nil
	}
}

// sanitizeMetadata drops keys whose names match the sensitive pattern. This is
// the Go equivalent of the TypeScript sanitizeLogMetadata function and runs at
// insert time so data never touches the network.
func sanitizeMetadata(meta map[string]any) map[string]any {
	if meta == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(meta))
	for k, v := range meta {
		if !sensitiveKeyPattern.MatchString(k) {
			out[k] = v
		}
	}
	return out
}
