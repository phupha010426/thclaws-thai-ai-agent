// Package memory holds MongoDB document structs and collection-name constants.
// Every struct carries a "namespace" field — this is the tenant isolation key
// (LINE userId scope). Queries MUST always include namespace in the filter so
// two users can never observe each other's documents.
package memory

import (
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Collection name constants — single source of truth to prevent typo-driven
// cross-collection reads that would silently return empty results.
const (
	ColConversationMessages = "conversation_messages"
	ColConversationSnapshot = "conversation_snapshots"
	ColMemoryItems          = "memory_items"
	ColMemorySummaries      = "memory_summaries"
	ColEventLogs            = "event_logs"
	ColAgentProfiles        = "agent_profiles"
	ColPersonalWikiPages    = "personal_wiki_pages"
	ColStoredImages         = "stored_images"
	ColSlipRecords          = "slip_records"
	ColUserAccountIdentity  = "user_account_identities"
	ColPendingActions       = "pending_actions"
	ColAgentSessions        = "agent_sessions"
)

// ConversationMessage stores one turn of a LINE conversation. The compacted
// flag is set to true after the message has been folded into a MemorySummary so
// GetContext only returns the tail that has not yet been summarised.
type ConversationMessage struct {
	ID        primitive.ObjectID `bson:"_id,omitempty"`
	OwnerID   string             `bson:"ownerId"`
	AgentID   string             `bson:"agentId"`
	Namespace string             `bson:"namespace"`
	Role      string             `bson:"role"` // user | assistant | system
	Content   string             `bson:"content"`
	Metadata  bson.M             `bson:"metadata"`
	Compacted bool               `bson:"compacted"`
	CreatedAt time.Time          `bson:"createdAt"`
	UpdatedAt time.Time          `bson:"updatedAt"`
}

// ConversationSnapshot is an upsert-per-user record that holds the last
// resolved intent so the next message can continue mid-flow without re-reading
// all history.
type ConversationSnapshot struct {
	ID         primitive.ObjectID `bson:"_id,omitempty"`
	Namespace  string             `bson:"namespace"`
	LineUserID string             `bson:"lineUserId"`
	LastIntent string             `bson:"lastIntent"`
	Metadata   bson.M             `bson:"metadata"`
	CreatedAt  time.Time          `bson:"createdAt"`
	UpdatedAt  time.Time          `bson:"updatedAt"`
}

// MemoryItem is a single semantic fact extracted from the conversation.
// SensitivityLevel drives retention policy — "high" items are excluded from
// export unless the requester holds an explicit consent proof.
type MemoryItem struct {
	ID               primitive.ObjectID `bson:"_id,omitempty"`
	OwnerID          string             `bson:"ownerId"`
	AgentID          string             `bson:"agentId"`
	Namespace        string             `bson:"namespace"`
	Type             string             `bson:"type"`
	Content          string             `bson:"content"`
	SensitivityLevel string             `bson:"sensitivityLevel"` // low | medium | high
	Confidence       float64            `bson:"confidence"`
	Source           string             `bson:"source"`
	ExpiresAt        *time.Time         `bson:"expiresAt,omitempty"`
	Metadata         bson.M             `bson:"metadata"`
	CreatedAt        time.Time          `bson:"createdAt"`
	UpdatedAt        time.Time          `bson:"updatedAt"`
}

// MemorySummary is the compacted representation of a contiguous window of
// ConversationMessages. FromDate/ToDate let the replay logic know which
// window was folded so it can skip re-compacting the same range.
type MemorySummary struct {
	ID           primitive.ObjectID `bson:"_id,omitempty"`
	OwnerID      string             `bson:"ownerId"`
	AgentID      string             `bson:"agentId"`
	Namespace    string             `bson:"namespace"`
	Summary      string             `bson:"summary"`
	MessageCount int                `bson:"messageCount"`
	FromDate     *time.Time         `bson:"fromDate,omitempty"`
	ToDate       *time.Time         `bson:"toDate,omitempty"`
	CreatedAt    time.Time          `bson:"createdAt"`
	UpdatedAt    time.Time          `bson:"updatedAt"`
}

// EventLog is the Mongo-side audit trail for domain events originating from
// LINE messages. The Postgres LedgerEvent is the authoritative event source;
// this collection is the human-readable companion that includes raw content.
type EventLog struct {
	ID        primitive.ObjectID `bson:"_id,omitempty"`
	OwnerID   string             `bson:"ownerId"`
	AgentID   string             `bson:"agentId,omitempty"`
	Namespace string             `bson:"namespace"`
	EventType string             `bson:"eventType"`
	Source    string             `bson:"source"`
	Content   string             `bson:"content,omitempty"`
	// Metadata is sanitised before write — tokens, secrets, and base64 blobs
	// are stripped to prevent credential leakage into audit logs.
	Metadata  bson.M    `bson:"metadata"`
	CreatedAt time.Time `bson:"createdAt"`
	UpdatedAt time.Time `bson:"updatedAt"`
}

// AgentProfile stores per-agent preferences. There is at most one profile per
// agentId (unique index in Mongo). The namespace field allows bulk deletion
// during PDPA right-to-be-forgotten flows without scanning by agentId.
type AgentProfile struct {
	ID          primitive.ObjectID `bson:"_id,omitempty"`
	AgentID     string             `bson:"agentId"`
	Namespace   string             `bson:"namespace"`
	Preferences bson.M             `bson:"preferences"`
	CreatedAt   time.Time          `bson:"createdAt"`
	UpdatedAt   time.Time          `bson:"updatedAt"`
}

// AgentSession is the durable runtime session for one agent namespace. It lets
// thClaws keep a stable identity across LINE reply-token windows without
// spawning one OS process per user.
type AgentSession struct {
	ID         primitive.ObjectID `bson:"_id,omitempty"`
	OwnerID    string             `bson:"ownerId"`
	AgentID    string             `bson:"agentId"`
	Namespace  string             `bson:"namespace"`
	Runtime    string             `bson:"runtime"`
	Model      string             `bson:"model"`
	Status     string             `bson:"status"`
	TurnCount  int64              `bson:"turnCount"`
	LastIntent string             `bson:"lastIntent,omitempty"`
	CreatedAt  time.Time          `bson:"createdAt"`
	UpdatedAt  time.Time          `bson:"updatedAt"`
}

// PersonalWikiPage is the legacy Mongo-backed wiki. New writes go to the
// Postgres wiki_pages table via WikiBrainReader; this collection is kept for
// backward compatibility and is read-merged in GetContext.
type PersonalWikiPage struct {
	ID        primitive.ObjectID `bson:"_id,omitempty"`
	OwnerID   string             `bson:"ownerId"`
	AgentID   string             `bson:"agentId"`
	Namespace string             `bson:"namespace"`
	Title     string             `bson:"title"`
	Content   string             `bson:"content"`
	Source    string             `bson:"source"`
	CreatedAt time.Time          `bson:"createdAt"`
	UpdatedAt time.Time          `bson:"updatedAt"`
}

// SlipRecord holds the result of OCR parsing for a bank-transfer slip image.
// storedImageId links back to the stored_images collection.
type SlipRecord struct {
	ID                primitive.ObjectID `bson:"_id,omitempty"`
	OwnerID           string             `bson:"ownerId"`
	AgentID           string             `bson:"agentId"`
	Namespace         string             `bson:"namespace"`
	StoredImageID     primitive.ObjectID `bson:"storedImageId"`
	Status            string             `bson:"status"`    // PENDING_OCR | PARSED | NEEDS_CONFIRMATION
	Direction         string             `bson:"direction"` // INCOME | EXPENSE | TRANSFER | UNKNOWN
	Amount            *float64           `bson:"amount,omitempty"`
	FromAccountMasked string             `bson:"fromAccountMasked,omitempty"`
	ToAccountMasked   string             `bson:"toAccountMasked,omitempty"`
	RawText           string             `bson:"rawText,omitempty"`
	ParserVersion     string             `bson:"parserVersion"`
	CreatedAt         time.Time          `bson:"createdAt"`
	UpdatedAt         time.Time          `bson:"updatedAt"`
}

type StoredImage struct {
	ID            primitive.ObjectID `bson:"_id,omitempty"`
	OwnerID       string             `bson:"ownerId"`
	AgentID       string             `bson:"agentId"`
	Namespace     string             `bson:"namespace"`
	LineMessageID string             `bson:"lineMessageId"`
	Bucket        string             `bson:"bucket"`
	ObjectName    string             `bson:"objectName"`
	ContentType   string             `bson:"contentType"`
	Size          int                `bson:"size"`
	Source        string             `bson:"source"`
	CreatedAt     time.Time          `bson:"createdAt"`
	UpdatedAt     time.Time          `bson:"updatedAt"`
}

type UserAccountIdentity struct {
	ID              primitive.ObjectID `bson:"_id,omitempty"`
	OwnerID         string             `bson:"ownerId"`
	AccountNoMasked string             `bson:"accountNoMasked"`
	AccountName     string             `bson:"accountName,omitempty"`
	BankName        string             `bson:"bankName,omitempty"`
	IsOwnerAccount  bool               `bson:"isOwnerAccount"`
	CreatedAt       time.Time          `bson:"createdAt"`
	UpdatedAt       time.Time          `bson:"updatedAt"`
}

type PendingAction struct {
	ID               primitive.ObjectID `bson:"_id,omitempty"`
	OwnerID          string             `bson:"ownerId"`
	AgentID          string             `bson:"agentId"`
	Namespace        string             `bson:"namespace"`
	Kind             string             `bson:"kind"`
	Original         string             `bson:"original"`
	Direction        string             `bson:"direction,omitempty"`
	Amount           float64            `bson:"amount,omitempty"`
	Note             string             `bson:"note,omitempty"`
	Category         string             `bson:"category,omitempty"`
	CounterpartyName string             `bson:"counterpartyName,omitempty"`
	CounterpartyRole string             `bson:"counterpartyRole,omitempty"`
	Confidence       float64            `bson:"confidence,omitempty"`
	CausationID      string             `bson:"causationId,omitempty"`
	ExpiresAt        time.Time          `bson:"expiresAt"`
	CreatedAt        time.Time          `bson:"createdAt"`
	UpdatedAt        time.Time          `bson:"updatedAt"`
}
