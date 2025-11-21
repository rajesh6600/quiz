package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	sqlcgen "github.com/gokatarajesh/quiz-platform/internal/db/sqlc"
)

// OAuthProvider defines the interface for OAuth implementations.
type OAuthProvider interface {
	GetAuthURL(state string) (string, error)
	ExchangeCode(ctx context.Context, code string) (*OAuthUserInfo, error)
}

// OAuthUserInfo contains user data from OAuth provider.
type OAuthUserInfo struct {
	ProviderID string
	Email      string
	Name       string
	AvatarURL  string
}

// OAuthService handles OAuth flows with full token exchange.
type OAuthService struct {
	googleConfig *oauth2.Config
	logger       zerolog.Logger
	httpClient   *http.Client
}

// NewOAuthService creates an OAuth service with provider credentials.
func NewOAuthService(googleClientID, googleClientSecret, googleRedirectURI string, logger zerolog.Logger) *OAuthService {
	config := &oauth2.Config{
		ClientID:     googleClientID,
		ClientSecret: googleClientSecret,
		RedirectURL:  googleRedirectURI,
		Scopes:       []string{"openid", "email", "profile"},
		Endpoint:     google.Endpoint,
	}

	return &OAuthService{
		googleConfig: config,
		logger:       logger,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
	}
}

// StartOAuthFlow generates the authorization URL for Google OAuth.
func (s *OAuthService) StartOAuthFlow(provider, state string) (string, error) {
	if provider != OAuthProviderGoogle {
		return "", fmt.Errorf("unsupported provider: %s", provider)
	}

	if s.googleConfig == nil || s.googleConfig.ClientID == "" {
		return "", fmt.Errorf("OAuth not configured (missing GOOGLE_CLIENT_ID)")
	}

	// Generate authorization URL with state for CSRF protection
	authURL := s.googleConfig.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)
	return authURL, nil
}

// HandleOAuthCallback processes the OAuth callback and exchanges code for user info.
func (s *OAuthService) HandleOAuthCallback(ctx context.Context, provider, code, state string) (*OAuthUserInfo, error) {
	if provider != OAuthProviderGoogle {
		return nil, fmt.Errorf("unsupported provider: %s", provider)
	}

	if s.googleConfig == nil {
		return nil, fmt.Errorf("OAuth not configured")
	}

	// Exchange authorization code for access token
	token, err := s.googleConfig.Exchange(ctx, code)
	if err != nil {
		s.logger.Error().Err(err).Msg("OAuth token exchange failed")
		return nil, fmt.Errorf("token exchange failed: %w", err)
	}

	// Fetch user info using access token
	userInfoURL := "https://www.googleapis.com/oauth2/v2/userinfo"
	req, err := http.NewRequestWithContext(ctx, "GET", userInfoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token.AccessToken))

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch user info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("user info API returned status %d", resp.StatusCode)
	}

	var googleUser struct {
		ID      string `json:"id"`
		Email   string `json:"email"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&googleUser); err != nil {
		return nil, fmt.Errorf("decode user info: %w", err)
	}

	return &OAuthUserInfo{
		ProviderID: googleUser.ID,
		Email:      googleUser.Email,
		Name:       googleUser.Name,
		AvatarURL:  googleUser.Picture,
	}, nil
}

// CreateOrGetOAuthUser creates a user account from OAuth info or returns existing.
// This will be called after HandleOAuthCallback succeeds.
func (s *OAuthService) CreateOrGetOAuthUser(ctx context.Context, authSvc *Service, provider string, info *OAuthUserInfo) (*User, *TokenPair, error) {
	if info.Email == "" {
		return nil, nil, fmt.Errorf("OAuth provider did not return email")
	}

	// Check if user exists by email
	pgEmail := pgtype.Text{}
	pgEmail.Scan(info.Email)

	dbUser, err := authSvc.userRepo.GetByEmail(ctx, pgEmail)
	if err == nil && dbUser.UserID.Bytes != [16]byte{} {
		// User exists, return with tokens
		userID, _ := uuid.FromBytes(dbUser.UserID.Bytes[:])
		user := &User{
			ID:          userID,
			Email:       &info.Email,
			DisplayName: dbUser.DisplayName,
			UserType:    dbUser.UserType,
			IsGuest:     false,
		}

		tokens, err := authSvc.generateTokenPair(*user)
		if err != nil {
			return nil, nil, fmt.Errorf("generate tokens: %w", err)
		}

		s.logger.Info().Str("user_id", userID.String()).Str("provider", provider).Msg("OAuth user logged in")
		return user, tokens, nil
	}

	// Create new user with OAuth metadata
	metadata, _ := json.Marshal(map[string]string{
		"oauth_provider": provider,
		"oauth_id":       info.ProviderID,
		"avatar_url":     info.AvatarURL,
	})

	pgDisplayName := pgtype.Text{}
	if info.Name != "" {
		pgDisplayName.Scan(info.Name)
	} else {
		pgDisplayName.Scan(info.Email) // fallback to email
	}
	pgUserType := pgtype.Text{}
	pgUserType.Scan("registered")

	createParams := sqlcgen.CreateUserParams{
		Email:        pgEmail,
		PasswordHash: pgtype.Text{}, // null for OAuth users
		DisplayName:  pgDisplayName,
		UserType:     pgUserType,
		Metadata:     metadata,
	}

	dbUser, err = authSvc.userRepo.CreateRegisteredUser(ctx, createParams)
	if err != nil {
		return nil, nil, fmt.Errorf("create OAuth user: %w", err)
	}

	userID, _ := uuid.FromBytes(dbUser.UserID.Bytes[:])
	user := &User{
		ID:          userID,
		Email:       &info.Email,
		DisplayName: dbUser.DisplayName,
		UserType:    "registered",
		IsGuest:     false,
	}

	tokens, err := authSvc.generateTokenPair(*user)
	if err != nil {
		return nil, nil, fmt.Errorf("generate tokens: %w", err)
	}

	s.logger.Info().Str("user_id", userID.String()).Str("provider", provider).Msg("OAuth user created")
	return user, tokens, nil
}
