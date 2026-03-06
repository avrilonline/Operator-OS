package users

import (
	"golang.org/x/crypto/bcrypt"
)

const (
	// bcryptCost is the work factor for password hashing.
	// 12 is a good balance between security and performance.
	bcryptCost = 12
)

// HashPassword returns a bcrypt hash of the plaintext password.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// CheckPassword compares a plaintext password against a bcrypt hash.
// Returns nil on match, bcrypt.ErrMismatchedHashAndPassword on mismatch.
func CheckPassword(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

// ValidatePassword checks that a password meets minimum requirements.
func ValidatePassword(password string) error {
	if len(password) < 8 {
		return ErrWeakPassword
	}
	return nil
}
