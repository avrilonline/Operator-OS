package users

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHashPassword(t *testing.T) {
	hash, err := HashPassword("testpassword")
	require.NoError(t, err)
	assert.NotEmpty(t, hash)
	assert.NotEqual(t, "testpassword", hash)
	// bcrypt hashes start with $2a$ or $2b$.
	assert.Contains(t, hash, "$2a$")
}

func TestCheckPassword_Match(t *testing.T) {
	hash, err := HashPassword("correctpassword")
	require.NoError(t, err)
	assert.NoError(t, CheckPassword(hash, "correctpassword"))
}

func TestCheckPassword_Mismatch(t *testing.T) {
	hash, err := HashPassword("correctpassword")
	require.NoError(t, err)
	assert.Error(t, CheckPassword(hash, "wrongpassword"))
}

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{"valid 8 chars", "12345678", false},
		{"valid long", "averylongpassword", false},
		{"too short 7", "1234567", true},
		{"empty", "", true},
		{"one char", "a", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePassword(tt.password)
			if tt.wantErr {
				assert.ErrorIs(t, err, ErrWeakPassword)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
