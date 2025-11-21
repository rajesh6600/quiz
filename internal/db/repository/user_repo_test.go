package repository

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	sqlcgen "github.com/gokatarajesh/quiz-platform/internal/db/sqlc"
)

type mockUserStore struct {
	mock.Mock
}

func (m *mockUserStore) CreateUser(ctx context.Context, arg sqlcgen.CreateUserParams) (sqlcgen.User, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(sqlcgen.User), args.Error(1)
}

func (m *mockUserStore) GetUserByEmail(ctx context.Context, email pgtype.Text) (sqlcgen.User, error) {
	args := m.Called(ctx, email)
	return args.Get(0).(sqlcgen.User), args.Error(1)
}

func (m *mockUserStore) PromoteGuestToRegistered(ctx context.Context, arg sqlcgen.PromoteGuestToRegisteredParams) (sqlcgen.User, error) {
	args := m.Called(ctx, arg)
	return args.Get(0).(sqlcgen.User), args.Error(1)
}

func (m *mockUserStore) GetUserByID(ctx context.Context, userID pgtype.UUID) (sqlcgen.User, error) {
	args := m.Called(ctx, userID)
	return args.Get(0).(sqlcgen.User), args.Error(1)
}

func (m *mockUserStore) UpdateUserLogin(ctx context.Context, userID pgtype.UUID) error {
	return m.Called(ctx, userID).Error(0)
}

func TestUserRepository_CreateRegisteredUser(t *testing.T) {
	store := new(mockUserStore)
	repo := NewUserRepository(store)

	params := sqlcgen.CreateUserParams{
		Email:        pgtype.Text{String: "user@example.com", Valid: true},
		PasswordHash: pgtype.Text{String: "hashed", Valid: true},
		DisplayName:  pgtype.Text{String: "Ace", Valid: true},
		UserType:     pgtype.Text{String: "registered", Valid: true},
	}
	expect := sqlcgen.User{
		UserID:      uuidFromByte(1),
		DisplayName: "Ace",
		UserType:    "registered",
	}

	store.On("CreateUser", mock.Anything, params).Return(expect, nil)

	got, err := repo.CreateRegisteredUser(context.Background(), params)

	assert.NoError(t, err)
	assert.Equal(t, expect, got)
	store.AssertExpectations(t)
}

func TestUserRepository_GetByEmail(t *testing.T) {
	store := new(mockUserStore)
	repo := NewUserRepository(store)

	email := pgtype.Text{String: "user@example.com", Valid: true}
	expect := sqlcgen.User{UserID: uuidFromByte(2), DisplayName: "Ace"}

	store.On("GetUserByEmail", mock.Anything, email).Return(expect, nil)

	got, err := repo.GetByEmail(context.Background(), email)

	assert.NoError(t, err)
	assert.Equal(t, expect, got)
	store.AssertExpectations(t)
}

func TestUserRepository_PromoteGuest(t *testing.T) {
	store := new(mockUserStore)
	repo := NewUserRepository(store)

	params := sqlcgen.PromoteGuestToRegisteredParams{
		Email:        pgtype.Text{String: "upgraded@example.com", Valid: true},
		PasswordHash: pgtype.Text{String: "hashed", Valid: true},
		UserID:       uuidFromByte(3),
	}
	expect := sqlcgen.User{UserID: params.UserID, UserType: "registered"}

	store.On("PromoteGuestToRegistered", mock.Anything, params).Return(expect, nil)

	got, err := repo.PromoteGuest(context.Background(), params)

	assert.NoError(t, err)
	assert.Equal(t, expect, got)
	store.AssertExpectations(t)
}
