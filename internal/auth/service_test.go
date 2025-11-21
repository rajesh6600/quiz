package auth

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog"

	"github.com/gokatarajesh/quiz-platform/internal/auth/jwt"
	sqlcgen "github.com/gokatarajesh/quiz-platform/internal/db/sqlc"
)

type mockUserRepo struct {
	mock.Mock
}

func (m *mockUserRepo) CreateRegisteredUser(ctx context.Context, params sqlcgen.CreateUserParams) (sqlcgen.User, error) {
	args := m.Called(ctx, params)
	return args.Get(0).(sqlcgen.User), args.Error(1)
}

func (m *mockUserRepo) GetByEmail(ctx context.Context, email interface{}) (sqlcgen.User, error) {
	args := m.Called(ctx, email)
	return args.Get(0).(sqlcgen.User), args.Error(1)
}

func (m *mockUserRepo) GetByID(ctx context.Context, userID interface{}) (sqlcgen.User, error) {
	args := m.Called(ctx, userID)
	return args.Get(0).(sqlcgen.User), args.Error(1)
}

func (m *mockUserRepo) PromoteGuest(ctx context.Context, params sqlcgen.PromoteGuestToRegisteredParams) (sqlcgen.User, error) {
	args := m.Called(ctx, params)
	return args.Get(0).(sqlcgen.User), args.Error(1)
}

func (m *mockUserRepo) UpdateLogin(ctx context.Context, userID uuid.UUID) error {
	return m.Called(ctx, userID).Error(0)
}

func TestHashPassword(t *testing.T) {
	hash, err := HashPassword("testpassword123")
	assert.NoError(t, err)
	assert.NotEmpty(t, hash)
	assert.True(t, len(hash) > 20) // bcrypt hashes are long
}

func TestVerifyPassword(t *testing.T) {
	hash, _ := HashPassword("testpassword123")

	err := VerifyPassword(hash, "testpassword123")
	assert.NoError(t, err)

	err = VerifyPassword(hash, "wrongpassword")
	assert.Error(t, err)
}

func TestPasswordTooShort(t *testing.T) {
	_, err := HashPassword("short")
	assert.Error(t, err)
	assert.Equal(t, ErrPasswordTooShort, err)
}

func TestService_Register(t *testing.T) {
	// Skip integration test for now - requires proper repository interface
	t.Skip("requires repository interface refactoring")

	repo := new(mockUserRepo)
	logger := zerolog.Nop()

	cfg := jwt.TokenConfig{
		AccessSecret:  []byte("test-access-secret"),
		RefreshSecret: []byte("test-refresh-secret"),
	}

	// This test would need a repository interface to work properly
	_ = repo
	_ = cfg
	_ = logger

	// Mock: email doesn't exist (return error to simulate not found)
	repo.On("GetByEmail", mock.Anything, mock.Anything).Return(sqlcgen.User{}, assert.AnError)

	// Mock: user creation succeeds
	userID := uuid.New()
	pgUserID := pgtype.UUID{}
	pgUserID.Scan(userID)

	createdUser := sqlcgen.User{
		UserID:      pgUserID,
		DisplayName: "Test User",
		UserType:    "registered",
	}
	repo.On("CreateRegisteredUser", mock.Anything, mock.Anything).Return(createdUser, nil)

	// TODO: Add integration test with proper repository interface
}
