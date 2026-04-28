// Package users resolves a LINE userId to an internal User and PERSONAL Agent.
// The Go worker owns this path for LINE traffic so first-time users are
// onboarded inside one Postgres transaction before any memory or ledger write.
package users

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrUnknown = errors.New("users: unknown LINE userId")

type Identity struct {
	UserID    string
	AgentID   string
	Namespace string
}

type Service struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

func (s *Service) ResolveLineUser(ctx context.Context, lineUserID string) (Identity, error) {
	if lineUserID == "" {
		return Identity{}, ErrUnknown
	}
	var id Identity
	err := s.pool.QueryRow(ctx, `
		SELECT u.id, a.id, a.namespace
		FROM users u
		JOIN agents a ON a."ownerId" = u.id AND a.type = 'PERSONAL' AND a."isActive" = true
		WHERE u."lineUserId" = $1
		LIMIT 1`, lineUserID).Scan(&id.UserID, &id.AgentID, &id.Namespace)
	if errors.Is(err, pgx.ErrNoRows) {
		return Identity{}, ErrUnknown
	}
	return id, err
}

func (s *Service) ResolveIdentity(ctx context.Context, userID, agentID, namespace string) (Identity, error) {
	if userID == "" || agentID == "" || namespace == "" {
		return Identity{}, ErrUnknown
	}
	var id Identity
	err := s.pool.QueryRow(ctx, `
	SELECT u.id, a.id, a.namespace
	FROM users u
	JOIN agents a ON a."ownerId" = u.id AND a.type = 'PERSONAL' AND a."isActive" = true
	WHERE u.id = $1 AND a.id = $2 AND a.namespace = $3
	LIMIT 1`, userID, agentID, namespace).Scan(&id.UserID, &id.AgentID, &id.Namespace)
	if errors.Is(err, pgx.ErrNoRows) {
		return Identity{}, ErrUnknown
	}
	return id, err
}

// ResolveOrCreateLineUser returns a namespace-isolated PERSONAL agent for the
// LINE user. It is race-safe: the unique constraints on users.lineUserId and
// agents.namespace collapse concurrent first messages into the same rows.
func (s *Service) ResolveOrCreateLineUser(ctx context.Context, lineUserID string) (Identity, error) {
	if lineUserID == "" {
		return Identity{}, ErrUnknown
	}
	id, err := s.ResolveLineUser(ctx, lineUserID)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, ErrUnknown) {
		return Identity{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Identity{}, fmt.Errorf("users: begin: %w", err)
	}
	defer tx.Rollback(ctx)

	userID := uuid.NewString()
	now := time.Now().UTC()
	err = tx.QueryRow(ctx, `
		INSERT INTO users (id, "lineUserId", language, "createdAt", "updatedAt")
		VALUES ($1, $2, 'th', $3, $3)
		ON CONFLICT ("lineUserId") DO UPDATE SET "updatedAt" = EXCLUDED."updatedAt"
		RETURNING id`, userID, lineUserID, now).Scan(&id.UserID)
	if err != nil {
		return Identity{}, fmt.Errorf("users: upsert user: %w", err)
	}

	id.Namespace = "memory:user:" + id.UserID
	agentID := uuid.NewString()
	err = tx.QueryRow(ctx, `
		INSERT INTO agents (id, "ownerId", type, name, namespace, "isActive", "createdAt", "updatedAt")
		VALUES ($1, $2, 'PERSONAL', 'Personal Agent', $3, true, $4, $4)
		ON CONFLICT (namespace) DO UPDATE SET "isActive" = true, "updatedAt" = EXCLUDED."updatedAt"
		RETURNING id`, agentID, id.UserID, id.Namespace, now).Scan(&id.AgentID)
	if err != nil {
		return Identity{}, fmt.Errorf("users: upsert agent: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Identity{}, fmt.Errorf("users: commit: %w", err)
	}
	return id, nil
}
