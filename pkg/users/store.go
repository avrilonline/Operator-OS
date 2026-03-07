// Package users provides user account management for Operator OS.
//
// It defines a UserStore interface for pluggable backends and includes
// an SQLite implementation, HTTP API handlers for registration, and
// password hashing utilities.
package users

import (
	"time"
)

// User represents a registered user account.
type User struct {
	ID            string    `json:"id"`
	Email         string    `json:"email"`
	PasswordHash  string    `json:"-"` // never serialized
	DisplayName   string    `json:"display_name,omitempty"`
	Role          string    `json:"role"`
	Status        string    `json:"status"`
	EmailVerified bool      `json:"email_verified"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// User status constants.
const (
	StatusPendingVerification = "pending_verification"
	StatusActive              = "active"
	StatusSuspended           = "suspended"
	StatusDeleted             = "deleted"
)

// User role constants.
const (
	RoleUser  = "user"
	RoleAdmin = "admin"
)

// UserStore abstracts user persistence.
// Implementations must be safe for concurrent use.
type UserStore interface {
	// Create inserts a new user. Returns ErrEmailExists if the email is taken.
	Create(user *User) error
	// GetByID returns the user with the given ID, or ErrNotFound.
	GetByID(id string) (*User, error)
	// GetByEmail returns the user with the given email, or ErrNotFound.
	GetByEmail(email string) (*User, error)
	// Update saves changes to an existing user. Returns ErrNotFound if missing.
	Update(user *User) error
	// Delete removes a user by ID. Returns ErrNotFound if missing.
	Delete(id string) error
	// List returns all users, ordered by created_at descending.
	List() ([]*User, error)
	// Count returns the total number of users.
	Count() (int64, error)
	// Close releases any resources held by the store.
	Close() error
}
