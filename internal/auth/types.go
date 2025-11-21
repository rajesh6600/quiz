package auth

import (
	"time"

	"github.com/google/uuid"
)

// User represents an authenticated user (registered or guest).
type User struct {
	ID          uuid.UUID
	Email       *string
	DisplayName string
	UserType    string // "registered" or "guest"
	IsGuest     bool
}

// TokenPair holds access and refresh tokens.
type TokenPair struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int64
}

// RegisterRequest for email/password registration.
type RegisterRequest struct {
	Email       string
	Password    string
	DisplayName string
}

// LoginRequest for email/password authentication.
type LoginRequest struct {
	Email    string
	Password string
}

// GuestRequest for creating ephemeral guest accounts.
type GuestRequest struct {
	DeviceFingerprint string
	DisplayName       string
}

// ConvertGuestRequest upgrades a guest to registered account.
type ConvertGuestRequest struct {
	GuestID  uuid.UUID
	Email    string
	Password string
}

// OAuthProvider constants.
const (
	OAuthProviderGoogle = "google"
	OAuthProviderGithub = "github"
)

// OAuthStartRequest initiates OAuth flow.
type OAuthStartRequest struct {
	Provider string
	State    string // CSRF token
}

// OAuthCallbackRequest processes OAuth callback.
type OAuthCallbackRequest struct {
	Provider string
	Code     string
	State    string
}

// TokenConfig holds JWT signing configuration.
type TokenConfig struct {
	AccessSecret  []byte
	RefreshSecret []byte
	AccessTTL     time.Duration // default: 1 hour
	RefreshTTL    time.Duration // default: 7 days
	Issuer        string
}
