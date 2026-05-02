// Package household manages household groups and their members.
// Each household is owned by a LINE user who can invite other LINE users.
// All ledger writes are scoped by householdID through the ledger.Writer.
package household

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Household represents a shared ledger group (e.g. family, roommate).
type Household struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	OwnerID   string    `json:"ownerId"`
	CreatedAt time.Time `json:"createdAt"`
}

// Member represents a LINE user who belongs to a household.
type Member struct {
	ID          string    `json:"id"`
	HouseholdID string    `json:"householdId"`
	UserID      string    `json:"userId"`
	Role        string    `json:"role"` // owner | admin | member
	CreatedAt   time.Time `json:"createdAt"`
}

// Namespace returns the household namespace for ledger isolation.
func (h *Household) Namespace() string {
	return "memory:household:" + h.ID
}

// LedgerNamespace returns the ledger scope string.
func (h *Household) LedgerNamespace() string {
	return "ledger:household:" + h.ID
}

// Service handles household operations.
type Service struct {
	pool *pgxpool.Pool
}

// New creates a household Service backed by the given connection pool.
func New(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// Create creates a new household owned by the given user.
func (s *Service) Create(ctx context.Context, ownerID, name string) (*Household, error) {
	if ownerID == "" {
		return nil, fmt.Errorf("household: ownerID required")
	}
	if name == "" {
		return nil, fmt.Errorf("household: name required")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("household: begin: %w", err)
	}
	defer tx.Rollback(ctx)

	id := uuid.NewString()
	now := time.Now().UTC()

	_, err = tx.Exec(ctx, `
		INSERT INTO households (id, name, "ownerId", "createdAt")
		VALUES ($1, $2, $3, $4)`,
		id, name, ownerID, now)
	if err != nil {
		return nil, fmt.Errorf("household: insert: %w", err)
	}

	// Auto-add owner as first member with role "owner"
	memberID := uuid.NewString()
	_, err = tx.Exec(ctx, `
		INSERT INTO household_members (id, "householdId", "userId", role, "createdAt")
		VALUES ($1, $2, $3, 'owner', $4)`,
		memberID, id, ownerID, now)
	if err != nil {
		return nil, fmt.Errorf("household: add owner member: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("household: commit: %w", err)
	}

	return &Household{ID: id, Name: name, OwnerID: ownerID, CreatedAt: now}, nil
}

// AddMember adds a LINE user to an existing household.
// Only the owner or an admin can add members.
func (s *Service) AddMember(ctx context.Context, householdID, userID, role string, addedByUserID string) (*Member, error) {
	if householdID == "" || userID == "" {
		return nil, fmt.Errorf("household: householdID and userID required")
	}
	if role == "" {
		role = "member"
	}
	if !isValidRole(role) {
		return nil, fmt.Errorf("household: invalid role %q", role)
	}

	// Verify ownership
	hh, err := s.Get(ctx, householdID)
	if err != nil {
		return nil, err
	}
	if hh.OwnerID != addedByUserID {
		return nil, fmt.Errorf("household: only owner can add members")
	}

	// Check if already a member
	existing, err := s.FindMember(ctx, householdID, userID)
	if err == nil && existing != nil {
		return existing, nil
	}

	id := uuid.NewString()
	now := time.Now().UTC()
	_, err = s.pool.Exec(ctx, `
		INSERT INTO household_members (id, "householdId", "userId", role, "createdAt")
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT ("householdId", "userId") DO NOTHING`,
		id, householdID, userID, role, now)
	if err != nil {
		return nil, fmt.Errorf("household: add member: %w", err)
	}

	return &Member{ID: id, HouseholdID: householdID, UserID: userID, Role: role, CreatedAt: now}, nil
}

// RemoveMember removes a member from a household. The owner cannot be removed.
func (s *Service) RemoveMember(ctx context.Context, householdID, userID string, removedByUserID string) error {
	// Verify requester is owner
	hh, err := s.Get(ctx, householdID)
	if err != nil {
		return err
	}
	if hh.OwnerID != removedByUserID {
		return fmt.Errorf("household: only owner can remove members")
	}
	if userID == hh.OwnerID {
		return fmt.Errorf("household: cannot remove owner")
	}

	tag, err := s.pool.Exec(ctx, `
		DELETE FROM household_members
		WHERE "householdId" = $1 AND "userId" = $2`,
		householdID, userID)
	if err != nil {
		return fmt.Errorf("household: remove member: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("household: member not found")
	}
	return nil
}

// Get returns a household by ID.
func (s *Service) Get(ctx context.Context, householdID string) (*Household, error) {
	if householdID == "" {
		return nil, fmt.Errorf("household: id required")
	}
	var hh Household
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, "ownerId", "createdAt"
		FROM households WHERE id = $1`, householdID).
		Scan(&hh.ID, &hh.Name, &hh.OwnerID, &hh.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("household: not found")
	}
	return &hh, err
}

// ListByOwner returns all households owned by the given user.
func (s *Service) ListByOwner(ctx context.Context, ownerID string) ([]Household, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, "ownerId", "createdAt"
		FROM households WHERE "ownerId" = $1
		ORDER BY "createdAt" DESC`, ownerID)
	if err != nil {
		return nil, fmt.Errorf("household: list: %w", err)
	}
	defer rows.Close()

	var out []Household
	for rows.Next() {
		var hh Household
		if err := rows.Scan(&hh.ID, &hh.Name, &hh.OwnerID, &hh.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, hh)
	}
	return out, rows.Err()
}

// ListMembers returns all members of a household.
func (s *Service) ListMembers(ctx context.Context, householdID string) ([]Member, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, "householdId", "userId", role, "createdAt"
		FROM household_members WHERE "householdId" = $1
		ORDER BY "createdAt" ASC`, householdID)
	if err != nil {
		return nil, fmt.Errorf("household: list members: %w", err)
	}
	defer rows.Close()

	var out []Member
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.ID, &m.HouseholdID, &m.UserID, &m.Role, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// FindMember finds a specific member in a household.
func (s *Service) FindMember(ctx context.Context, householdID, userID string) (*Member, error) {
	var m Member
	err := s.pool.QueryRow(ctx, `
		SELECT id, "householdId", "userId", role, "createdAt"
		FROM household_members
		WHERE "householdId" = $1 AND "userId" = $2`,
		householdID, userID).
		Scan(&m.ID, &m.HouseholdID, &m.UserID, &m.Role, &m.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return &m, err
}

// BelongsTo returns true if the user is a member of the household.
func (s *Service) BelongsTo(ctx context.Context, householdID, userID string) (bool, error) {
	m, err := s.FindMember(ctx, householdID, userID)
	if err != nil {
		return false, err
	}
	return m != nil, nil
}

func isValidRole(role string) bool {
	switch role {
	case "owner", "admin", "member":
		return true
	}
	return false
}
