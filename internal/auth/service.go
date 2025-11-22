package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/gokatarajesh/quiz-platform/internal/auth/jwt"
	"github.com/gokatarajesh/quiz-platform/internal/db/repository"
	sqlcgen "github.com/gokatarajesh/quiz-platform/internal/db/sqlc"
)

// Service handles authentication and user management.
type Service struct {
	userRepo    *repository.UserRepository
	tokenMgr    *jwt.Manager
	redis       *redis.Client
	emailSvc    *EmailService
	logger      zerolog.Logger
}

// ServiceOptions configures the auth service.
type ServiceOptions struct {
	TokenConfig jwt.TokenConfig
	Redis       *redis.Client
	EmailSvc    *EmailService
}

// NewService creates an authentication service.
func NewService(userRepo *repository.UserRepository, opts ServiceOptions, logger zerolog.Logger) *Service {
	return &Service{
		userRepo: userRepo,
		tokenMgr: jwt.NewManager(opts.TokenConfig),
		redis:    opts.Redis,
		emailSvc: opts.EmailSvc,
		logger:   logger,
	}
}

// Register creates a new registered user account.
func (s *Service) Register(ctx context.Context, req RegisterRequest) (*User, *TokenPair, error) {
	// Validate email format (basic check)
	if req.Email == "" {
		return nil, nil, fmt.Errorf("email required")
	}

	// Check if email already exists
	pgEmail := pgtype.Text{}
	pgEmail.Scan(req.Email)
	existing, err := s.userRepo.GetByEmail(ctx, pgEmail)
	if err == nil && existing.UserID.Bytes != [16]byte{} {
		return nil, nil, fmt.Errorf("email already registered")
	}

	// Hash password
	passwordHash, err := HashPassword(req.Password)
	if err != nil {
		return nil, nil, fmt.Errorf("hash password: %w", err)
	}

	// Create user
	pgHash := pgtype.Text{}
	pgHash.Scan(passwordHash)
	pgDisplayName := pgtype.Text{}
	pgDisplayName.Scan(req.DisplayName)
	pgUserType := pgtype.Text{}
	pgUserType.Scan("registered")

	createParams := sqlcgen.CreateUserParams{
		Email:        pgEmail,
		PasswordHash: pgHash,
		DisplayName:  pgDisplayName,
		UserType:     pgUserType,
	}

	dbUser, err := s.userRepo.CreateRegisteredUser(ctx, createParams)
	if err != nil {
		return nil, nil, fmt.Errorf("create user: %w", err)
	}

	userID, _ := uuid.FromBytes(dbUser.UserID.Bytes[:])
	user := &User{
		ID:          userID,
		Email:       &req.Email,
		DisplayName: req.DisplayName,
		UserType:    "registered",
		IsGuest:     false,
	}

	// Generate tokens
	tokens, err := s.generateTokenPair(*user)
	if err != nil {
		return nil, nil, fmt.Errorf("generate tokens: %w", err)
	}

	s.logger.Info().Str("user_id", userID.String()).Str("email", req.Email).Msg("user registered")

	return user, tokens, nil
}

// Login authenticates a user with email/password.
func (s *Service) Login(ctx context.Context, req LoginRequest) (*User, *TokenPair, error) {
	pgEmail := pgtype.Text{}
	pgEmail.Scan(req.Email)

	dbUser, err := s.userRepo.GetByEmail(ctx, pgEmail)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid credentials")
	}

	// Verify password
	if dbUser.PasswordHash.Valid {
		if err := VerifyPassword(dbUser.PasswordHash.String, req.Password); err != nil {
			return nil, nil, fmt.Errorf("invalid credentials")
		}
	} else {
		return nil, nil, fmt.Errorf("invalid credentials")
	}

	userID, _ := uuid.FromBytes(dbUser.UserID.Bytes[:])
	user := &User{
		ID:          userID,
		DisplayName: dbUser.DisplayName,
		UserType:    dbUser.UserType,
		IsGuest:     dbUser.UserType == "guest",
	}

	if dbUser.Email.Valid {
		email := dbUser.Email.String
		user.Email = &email
	}

	// Update last login
	_ = s.userRepo.UpdateLogin(ctx, userID)

	// Generate tokens
	tokens, err := s.generateTokenPair(*user)
	if err != nil {
		return nil, nil, fmt.Errorf("generate tokens: %w", err)
	}

	s.logger.Info().Str("user_id", userID.String()).Msg("user logged in")

	return user, tokens, nil
}

// CreateGuest creates an ephemeral guest account.
func (s *Service) CreateGuest(ctx context.Context, req GuestRequest) (*User, *TokenPair, error) {
	userID := uuid.New()

	// Create guest user
	pgDisplayName := pgtype.Text{}
	pgDisplayName.Scan(req.DisplayName)
	pgUserType := pgtype.Text{}
	pgUserType.Scan("guest")

	metadata, _ := json.Marshal(map[string]string{
		"device_fingerprint": req.DeviceFingerprint,
	})

	createParams := sqlcgen.CreateUserParams{
		Email:        pgtype.Text{}, // null for guests
		PasswordHash: pgtype.Text{}, // null for guests
		DisplayName:  pgDisplayName,
		UserType:     pgUserType,
		Metadata:     metadata,
	}

	_, err := s.userRepo.CreateRegisteredUser(ctx, createParams)
	if err != nil {
		return nil, nil, fmt.Errorf("create guest: %w", err)
	}

	user := &User{
		ID:          userID,
		DisplayName: req.DisplayName,
		UserType:    "guest",
		IsGuest:     true,
	}

	// Generate tokens
	tokens, err := s.generateTokenPair(*user)
	if err != nil {
		return nil, nil, fmt.Errorf("generate tokens: %w", err)
	}

	s.logger.Info().Str("user_id", userID.String()).Msg("guest created")

	return user, tokens, nil
}

// ConvertGuest upgrades a guest account to registered.
func (s *Service) ConvertGuest(ctx context.Context, req ConvertGuestRequest) (*User, *TokenPair, error) {
	// Hash password
	passwordHash, err := HashPassword(req.Password)
	if err != nil {
		return nil, nil, fmt.Errorf("hash password: %w", err)
	}

	pgGuestID := pgtype.UUID{}
	pgGuestID.Scan(req.GuestID)
	pgEmail := pgtype.Text{}
	pgEmail.Scan(req.Email)
	pgHash := pgtype.Text{}
	pgHash.Scan(passwordHash)

	convertParams := sqlcgen.PromoteGuestToRegisteredParams{
		UserID:       pgGuestID,
		Email:        pgEmail,
		PasswordHash: pgHash,
	}

	dbUser, err := s.userRepo.PromoteGuest(ctx, convertParams)
	if err != nil {
		return nil, nil, fmt.Errorf("convert guest: %w", err)
	}

	userID, _ := uuid.FromBytes(dbUser.UserID.Bytes[:])
	user := &User{
		ID:          userID,
		Email:       &req.Email,
		DisplayName: dbUser.DisplayName,
		UserType:    "registered",
		IsGuest:     false,
	}

	// Generate new tokens
	tokens, err := s.generateTokenPair(*user)
	if err != nil {
		return nil, nil, fmt.Errorf("generate tokens: %w", err)
	}

	s.logger.Info().Str("user_id", userID.String()).Msg("guest converted to registered")

	return user, tokens, nil
}

// RefreshToken generates a new access token from a refresh token.
func (s *Service) RefreshToken(ctx context.Context, refreshToken string) (*TokenPair, error) {
	claims, err := s.tokenMgr.ValidateRefreshToken(refreshToken)
	if err != nil {
		return nil, fmt.Errorf("invalid refresh token: %w", err)
	}

	// Fetch user to ensure still exists
	pgUserID := pgtype.UUID{}
	pgUserID.Scan(claims.UserID)
	dbUser, err := s.userRepo.GetByID(ctx, pgUserID)
	if err != nil {
		return nil, fmt.Errorf("user not found")
	}

	userID, _ := uuid.FromBytes(dbUser.UserID.Bytes[:])
	user := &User{
		ID:          userID,
		DisplayName: dbUser.DisplayName,
		UserType:    dbUser.UserType,
		IsGuest:     dbUser.UserType == "guest",
	}

	if dbUser.Email.Valid {
		email := dbUser.Email.String
		user.Email = &email
	}

	return s.generateTokenPair(*user)
}

// ValidateToken validates an access token and returns user claims.
func (s *Service) ValidateToken(tokenString string) (*jwt.Claims, error) {
	return s.tokenMgr.ValidateAccessToken(tokenString)
}

// RequestPasswordReset generates a reset token and sends reset email.
func (s *Service) RequestPasswordReset(ctx context.Context, email string) error {
	if s.redis == nil {
		return fmt.Errorf("redis not configured for password reset")
	}
	if s.emailSvc == nil {
		return fmt.Errorf("email service not configured")
	}

	// Find user by email
	pgEmail := pgtype.Text{}
	pgEmail.Scan(email)
	dbUser, err := s.userRepo.GetByEmail(ctx, pgEmail)
	if err != nil || dbUser.UserID.Bytes == [16]byte{} {
		// Don't reveal if user exists - security best practice
		return nil
	}

	// Generate secure random token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return fmt.Errorf("generate token: %w", err)
	}
	token := base64.URLEncoding.EncodeToString(tokenBytes)

	// Store token in Redis with 1 hour TTL
	userID, _ := uuid.FromBytes(dbUser.UserID.Bytes[:])
	tokenData := map[string]string{
		"user_id": userID.String(),
		"email":   email,
	}
	tokenJSON, _ := json.Marshal(tokenData)

	key := fmt.Sprintf("password_reset:%s", token)
	if err := s.redis.Set(ctx, key, tokenJSON, time.Hour).Err(); err != nil {
		return fmt.Errorf("store reset token: %w", err)
	}

	// Send email
	if err := s.emailSvc.SendPasswordResetEmail(ctx, email, token); err != nil {
		return fmt.Errorf("send email: %w", err)
	}

	s.logger.Info().Str("email", email).Msg("password reset requested")
	return nil
}

// ResetPassword validates token and updates password.
func (s *Service) ResetPassword(ctx context.Context, token, newPassword string) error {
	if s.redis == nil {
		return fmt.Errorf("redis not configured for password reset")
	}

	// Validate password
	if len(newPassword) < minPasswordLength {
		return ErrPasswordTooShort
	}

	// Get token from Redis
	key := fmt.Sprintf("password_reset:%s", token)
	tokenDataJSON, err := s.redis.Get(ctx, key).Result()
	if err == redis.Nil {
		return fmt.Errorf("invalid or expired reset token")
	}
	if err != nil {
		return fmt.Errorf("get reset token: %w", err)
	}

	var tokenData map[string]string
	if err := json.Unmarshal([]byte(tokenDataJSON), &tokenData); err != nil {
		return fmt.Errorf("decode token data: %w", err)
	}

	userIDStr, ok := tokenData["user_id"]
	if !ok {
		return fmt.Errorf("invalid token data")
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		return fmt.Errorf("invalid user ID: %w", err)
	}

	// Hash new password
	passwordHash, err := HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	// Update user password
	if err := s.userRepo.UpdatePassword(ctx, userID, passwordHash); err != nil {
		return fmt.Errorf("update password: %w", err)
	}

	// Delete token (single-use)
	if err := s.redis.Del(ctx, key).Err(); err != nil {
		s.logger.Warn().Err(err).Msg("failed to delete reset token")
	}

	s.logger.Info().Str("user_id", userID.String()).Msg("password reset completed")
	return nil
}

func (s *Service) generateTokenPair(user User) (*TokenPair, error) {
	jwtUser := jwt.User{
		ID:          user.ID,
		Email:       user.Email,
		DisplayName: user.DisplayName,
		UserType:    user.UserType,
		IsGuest:     user.IsGuest,
	}

	accessToken, err := s.tokenMgr.GenerateAccessToken(jwtUser)
	if err != nil {
		return nil, err
	}

	refreshToken, err := s.tokenMgr.GenerateRefreshToken(jwtUser)
	if err != nil {
		return nil, err
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int64(1 * 3600), // 1 hour in seconds
	}, nil
}
