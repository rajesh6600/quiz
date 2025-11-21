package auth

import (
	"errors"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidPassword  = errors.New("invalid password")
	ErrPasswordTooShort = errors.New("password must be at least 8 characters")
)

const (
	minPasswordLength = 8
	bcryptCost        = 12
)

// HashPassword creates a bcrypt hash of the password.
func HashPassword(password string) (string, error) {
	if len(password) < minPasswordLength {
		return "", ErrPasswordTooShort
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", err
	}

	return string(hash), nil
}

// VerifyPassword checks if the provided password matches the hash.
func VerifyPassword(hashedPassword, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
}
