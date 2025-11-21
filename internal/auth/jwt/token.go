package jwt

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Claims for JWT tokens.
type Claims struct {
	UserID      uuid.UUID `json:"user_id"`
	Email       string    `json:"email,omitempty"`
	DisplayName string    `json:"display_name"`
	UserType    string    `json:"user_type"`
	IsGuest     bool      `json:"is_guest"`
	jwt.RegisteredClaims
}

var (
	ErrInvalidToken = errors.New("invalid token")
	ErrExpiredToken = errors.New("token expired")
)

// TokenConfig holds JWT signing configuration.
type TokenConfig struct {
	AccessSecret  []byte
	RefreshSecret []byte
	AccessTTL     time.Duration // default: 1 hour
	RefreshTTL    time.Duration // default: 7 days
	Issuer        string
}

// Manager handles JWT token generation and validation.
type Manager struct {
	accessSecret  []byte
	refreshSecret []byte
	accessTTL     time.Duration
	refreshTTL    time.Duration
	issuer        string
}

// NewManager creates a JWT token manager.
func NewManager(cfg TokenConfig) *Manager {
	if cfg.AccessTTL == 0 {
		cfg.AccessTTL = 1 * time.Hour
	}
	if cfg.RefreshTTL == 0 {
		cfg.RefreshTTL = 7 * 24 * time.Hour
	}
	if cfg.Issuer == "" {
		cfg.Issuer = "quiz-platform"
	}

	return &Manager{
		accessSecret:  cfg.AccessSecret,
		refreshSecret: cfg.RefreshSecret,
		accessTTL:     cfg.AccessTTL,
		refreshTTL:    cfg.RefreshTTL,
		issuer:        cfg.Issuer,
	}
}

// User represents user data for token generation.
type User struct {
	ID          uuid.UUID
	Email       *string
	DisplayName string
	UserType    string
	IsGuest     bool
}

// GenerateAccessToken creates a short-lived access token.
func (m *Manager) GenerateAccessToken(user User) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:      user.ID,
		Email:       "",
		DisplayName: user.DisplayName,
		UserType:    user.UserType,
		IsGuest:     user.IsGuest,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.issuer,
			Subject:   user.ID.String(),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.accessTTL)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
		},
	}

	if user.Email != nil {
		claims.Email = *user.Email
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(m.accessSecret)
}

// GenerateRefreshToken creates a long-lived refresh token.
func (m *Manager) GenerateRefreshToken(user User) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:      user.ID,
		Email:       "",
		DisplayName: user.DisplayName,
		UserType:    user.UserType,
		IsGuest:     user.IsGuest,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.issuer,
			Subject:   user.ID.String(),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.refreshTTL)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
		},
	}

	if user.Email != nil {
		claims.Email = *user.Email
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(m.refreshSecret)
}

// ValidateAccessToken parses and validates an access token.
func (m *Manager) ValidateAccessToken(tokenString string) (*Claims, error) {
	return m.validateToken(tokenString, m.accessSecret)
}

// ValidateRefreshToken parses and validates a refresh token.
func (m *Manager) ValidateRefreshToken(tokenString string) (*Claims, error) {
	return m.validateToken(tokenString, m.refreshSecret)
}

func (m *Manager) validateToken(tokenString string, secret []byte) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return secret, nil
	})

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrExpiredToken
		}
		return nil, ErrInvalidToken
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}

	return claims, nil
}
