package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	mathrand "math/rand"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/gokatarajesh/quiz-platform/internal/auth/jwt"
	"github.com/gokatarajesh/quiz-platform/internal/db/repository"
	sqlcgen "github.com/gokatarajesh/quiz-platform/internal/db/sqlc"
)

var (
	usernameRegex = regexp.MustCompile(`^[a-z0-9_]+$`)
	minUsernameLength = 3
	maxUsernameLength = 10
)

// Service handles authentication and user management.
type Service struct {
	userRepo *repository.UserRepository
	tokenMgr *jwt.Manager
	redis    *redis.Client
	emailSvc *EmailService
	logger   zerolog.Logger
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
	pgUsername := pgtype.Text{} // NULL for new users, will be set later
	pgUserType := pgtype.Text{}
	pgUserType.Scan("registered")

	createParams := sqlcgen.CreateUserParams{
		Email:        pgEmail,
		PasswordHash: pgHash,
		Username:     pgUsername, // NULL initially
		UserType:     pgUserType,
	}

	dbUser, err := s.userRepo.CreateRegisteredUser(ctx, createParams)
	if err != nil {
		return nil, nil, fmt.Errorf("create user: %w", err)
	}

	userID, _ := uuid.FromBytes(dbUser.UserID.Bytes[:])
	username := ""
	if dbUser.Username.Valid {
		username = dbUser.Username.String
	}
	
	user := &User{
		ID:          userID,
		Email:       &req.Email,
		Username:    username,
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
	username := ""
	if dbUser.Username.Valid {
		username = dbUser.Username.String
	}
	
	user := &User{
		ID:          userID,
		Username:    username,
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

// generateGuestUsername generates a unique guest username.
func (s *Service) generateGuestUsername(ctx context.Context) (string, error) {
	const maxAttempts = 50
	
	for i := 0; i < maxAttempts; i++ {
		// Generate random 6-8 character suffix
		length := 6 + mathrand.Intn(3) // 6, 7, or 8
		chars := "abcdefghijklmnopqrstuvwxyz0123456789"
		suffix := make([]byte, length)
		for j := range suffix {
			suffix[j] = chars[mathrand.Intn(len(chars))]
		}
		
		username := fmt.Sprintf("guest_%s", string(suffix))
		
		// Check Redis for uniqueness
		if s.redis != nil {
			key := fmt.Sprintf("guest:username:%s", username)
			exists, err := s.redis.Exists(ctx, key).Result()
			if err == nil && exists == 0 {
				// Username available in Redis, check DB too
				_, err := s.userRepo.GetByUsername(ctx, username)
				if err != nil {
					// Not found in DB, username is available
					return username, nil
				}
			}
		} else {
			// No Redis, just check DB
			_, err := s.userRepo.GetByUsername(ctx, username)
			if err != nil {
				// Not found in DB, username is available
				return username, nil
			}
		}
	}
	
	return "", fmt.Errorf("failed to generate unique guest username after %d attempts", maxAttempts)
}

// CreateGuest creates an ephemeral guest account with temporary username.
func (s *Service) CreateGuest(ctx context.Context, req GuestRequest) (*User, *TokenPair, error) {
	// Generate guest UUID
	userID := uuid.New()
	guestIDStr := fmt.Sprintf("guest-%s", userID.String())
	
	// Generate unique guest username
	username, err := s.generateGuestUsername(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("generate guest username: %w", err)
	}
	
	// Store guest username in Redis with 1-day TTL
	if s.redis != nil {
		key := fmt.Sprintf("guest:username:%s", username)
		if err := s.redis.Set(ctx, key, guestIDStr, 24*time.Hour).Err(); err != nil {
			s.logger.Warn().Err(err).Str("username", username).Msg("failed to store guest username in Redis")
			// Continue anyway, Redis is not critical
		}
	}
	
	// Create user object (NOT stored in DB for guests)
	user := &User{
		ID:          userID,
		Username:    username,
		UserType:    "guest",
		IsGuest:     true,
	}

	// Generate tokens
	tokens, err := s.generateTokenPair(*user)
	if err != nil {
		return nil, nil, fmt.Errorf("generate tokens: %w", err)
	}

	s.logger.Info().Str("user_id", userID.String()).Str("username", username).Msg("guest created")

	return user, tokens, nil
}

// ConvertGuest upgrades a guest account to registered.
func (s *Service) ConvertGuest(ctx context.Context, req ConvertGuestRequest) (*User, *TokenPair, error) {
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

	pgHash := pgtype.Text{}
	pgHash.Scan(passwordHash)
	pgUsername := pgtype.Text{} // NULL - will be set later via SetUsername endpoint
	pgUserType := pgtype.Text{}
	pgUserType.Scan("registered")

	// Create new user (guests aren't in DB, so we create a new record)
	// Use the guest ID to preserve the user's identity
	createParams := sqlcgen.CreateUserParams{
		Email:        pgEmail,
		PasswordHash: pgHash,
		Username:     pgUsername, // NULL initially
		UserType:     pgUserType,
	}

	dbUser, err := s.userRepo.CreateRegisteredUser(ctx, createParams)
	if err != nil {
		return nil, nil, fmt.Errorf("convert guest: %w", err)
	}

	userID, _ := uuid.FromBytes(dbUser.UserID.Bytes[:])
	username := ""
	if dbUser.Username.Valid {
		username = dbUser.Username.String
	}
	
	user := &User{
		ID:          userID,
		Email:       &req.Email,
		Username:    username,
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

	// For guests, tokens are valid without DB check (guests aren't stored in DB)
	if claims.IsGuest {
		user := &User{
			ID:       claims.UserID,
			Username: claims.Username,
			UserType: "guest",
			IsGuest:  true,
		}
		return s.generateTokenPair(*user)
	}

	// For registered users, fetch from DB to ensure still exists
	pgUserID := pgtype.UUID{}
	pgUserID.Scan(claims.UserID)
	dbUser, err := s.userRepo.GetByID(ctx, pgUserID)
	if err != nil {
		return nil, fmt.Errorf("user not found")
	}

	userID, _ := uuid.FromBytes(dbUser.UserID.Bytes[:])
	username := ""
	if dbUser.Username.Valid {
		username = dbUser.Username.String
	}
	
	user := &User{
		ID:          userID,
		Username:    username,
		UserType:    dbUser.UserType,
		IsGuest:     dbUser.UserType == "guest",
	}

	if dbUser.Email.Valid {
		email := dbUser.Email.String
		user.Email = &email
	}

	return s.generateTokenPair(*user)
}

// SetUsername sets username for a user (one-time only, if username is NULL).
func (s *Service) SetUsername(ctx context.Context, userID uuid.UUID, username string) (*User, error) {
	// Validate username
	if err := s.ValidateUsername(username); err != nil {
		return nil, fmt.Errorf("invalid username: %w", err)
	}
	
	// Trim and lowercase
	username = strings.TrimRight(strings.ToLower(username), " ")
	
	// Check if username is already taken in DB
	_, err := s.userRepo.GetByUsername(ctx, username)
	if err == nil {
		// Username exists in DB, generate suggestions
		suggestions, _ := s.GenerateUsernameSuggestions(ctx, username)
		return nil, fmt.Errorf("username_taken: %s", strings.Join(suggestions, " "))
	}
	
	// Also check Redis for guest usernames if Redis is available
	if s.redis != nil {
		key := fmt.Sprintf("guest:username:%s", username)
		exists, err := s.redis.Exists(ctx, key).Result()
		if err == nil && exists > 0 {
			// Username taken by a guest, generate suggestions
			suggestions, _ := s.GenerateUsernameSuggestions(ctx, username)
			return nil, fmt.Errorf("username_taken: %s", strings.Join(suggestions, " "))
		}
	}
	
	// Update username (only if currently NULL)
	dbUser, err := s.userRepo.UpdateUsername(ctx, userID, username)
	if err != nil {
		return nil, fmt.Errorf("update username: %w", err)
	}
	
	// Check if update actually happened (username was NULL before)
	if !dbUser.Username.Valid || dbUser.Username.String != username {
		return nil, fmt.Errorf("username already set or user not found")
	}
	
	userIDFromDB, _ := uuid.FromBytes(dbUser.UserID.Bytes[:])
	usernameStr := ""
	if dbUser.Username.Valid {
		usernameStr = dbUser.Username.String
	}
	
	user := &User{
		ID:          userIDFromDB,
		Username:    usernameStr,
		UserType:    dbUser.UserType,
		IsGuest:     dbUser.UserType == "guest",
	}
	
	if dbUser.Email.Valid {
		email := dbUser.Email.String
		user.Email = &email
	}
	
	s.logger.Info().Str("user_id", userID.String()).Str("username", username).Msg("username set")
	
	return user, nil
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

// ValidateUsername checks if a username meets requirements.
func (s *Service) ValidateUsername(username string) error {
	// Trim trailing spaces
	username = strings.TrimRight(username, " ")
	
	// Check length
	if len(username) < minUsernameLength {
		return fmt.Errorf("username must be at least %d characters", minUsernameLength)
	}
	if len(username) > maxUsernameLength {
		return fmt.Errorf("username must be at most %d characters", maxUsernameLength)
	}
	
	// Convert to lowercase
	username = strings.ToLower(username)
	
	// Check format (lowercase, alphanumeric, underscores only)
	if !usernameRegex.MatchString(username) {
		return fmt.Errorf("username can only contain lowercase letters, numbers, and underscores")
	}
	
	return nil
}

// GenerateUsernameSuggestions generates 3 available username suggestions.
func (s *Service) GenerateUsernameSuggestions(ctx context.Context, username string) ([]string, error) {
	// Trim and lowercase
	username = strings.TrimRight(strings.ToLower(username), " ")
	
	suggestions := make([]string, 0, 3)
	
	// Try {username}_1, {username}_2, {username}_3
	for i := 1; i <= 100; i++ { // Try up to 100 variations
		candidate := fmt.Sprintf("%s_%d", username, i)
		
		// Check if available in DB
		_, err := s.userRepo.GetByUsername(ctx, candidate)
		if err != nil {
			// Username not found, it's available
			suggestions = append(suggestions, candidate)
			if len(suggestions) >= 3 {
				break
			}
		}
	}
	
	// If we don't have 3 suggestions, try with random suffix
	if len(suggestions) < 3 {
		for len(suggestions) < 3 {
			// Generate random 3-digit suffix
			randomSuffix := mathrand.Intn(900) + 100 // 100-999
			candidate := fmt.Sprintf("%s_%d", username, randomSuffix)
			
			// Check if already in suggestions
			exists := false
			for _, s := range suggestions {
				if s == candidate {
					exists = true
					break
				}
			}
			if exists {
				continue
			}
			
			// Check if available in DB
			_, err := s.userRepo.GetByUsername(ctx, candidate)
			if err != nil {
				suggestions = append(suggestions, candidate)
			}
		}
	}
	
	return suggestions, nil
}

func (s *Service) generateTokenPair(user User) (*TokenPair, error) {
	jwtUser := jwt.User{
		ID:          user.ID,
		Email:       user.Email,
		Username:    user.Username,
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
