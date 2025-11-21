package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	sqlcgen "github.com/gokatarajesh/quiz-platform/internal/db/sqlc"
)

type userStore interface {
	CreateUser(ctx context.Context, arg sqlcgen.CreateUserParams) (sqlcgen.User, error)
	GetUserByEmail(ctx context.Context, email pgtype.Text) (sqlcgen.User, error)
	GetUserByID(ctx context.Context, userID pgtype.UUID) (sqlcgen.User, error)
	PromoteGuestToRegistered(ctx context.Context, arg sqlcgen.PromoteGuestToRegisteredParams) (sqlcgen.User, error)
	UpdateUserLogin(ctx context.Context, userID pgtype.UUID) error
}

// UserRepository exposes typed DB operations required by auth flows.
type UserRepository struct {
	store userStore
}

// NewUserRepository wraps sqlc Queries for user-specific operations.
func NewUserRepository(store userStore) *UserRepository {
	return &UserRepository{store: store}
}

// CreateRegisteredUser inserts a fully registered account (non-guest).
func (r *UserRepository) CreateRegisteredUser(ctx context.Context, params sqlcgen.CreateUserParams) (sqlcgen.User, error) {
	return r.store.CreateUser(ctx, params)
}

// GetByEmail fetches a user by email if present.
func (r *UserRepository) GetByEmail(ctx context.Context, email pgtype.Text) (sqlcgen.User, error) {
	return r.store.GetUserByEmail(ctx, email)
}

// PromoteGuest upgrades a guest to registered, ensuring atomic update.
func (r *UserRepository) PromoteGuest(ctx context.Context, params sqlcgen.PromoteGuestToRegisteredParams) (sqlcgen.User, error) {
	return r.store.PromoteGuestToRegistered(ctx, params)
}

// GetByID fetches a user by ID.
func (r *UserRepository) GetByID(ctx context.Context, userID pgtype.UUID) (sqlcgen.User, error) {
	return r.store.GetUserByID(ctx, userID)
}

// UpdateLogin records the last login timestamp.
func (r *UserRepository) UpdateLogin(ctx context.Context, userID uuid.UUID) error {
	pgUserID := pgtype.UUID{}
	pgUserID.Scan(userID)
	return r.store.UpdateUserLogin(ctx, pgUserID)
}
