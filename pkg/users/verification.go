package users

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Verification-related errors.
var (
	// ErrTokenNotFound is returned when a verification token does not exist.
	ErrTokenNotFound = errors.New("verification token not found")
	// ErrTokenExpired is returned when a verification token has expired.
	ErrTokenExpired = errors.New("verification token has expired")
	// ErrTokenUsed is returned when a verification token has already been used.
	ErrTokenUsed = errors.New("verification token has already been used")
	// ErrAlreadyVerified is returned when a user's email is already verified.
	ErrAlreadyVerified = errors.New("email is already verified")
	// ErrTooManyTokens is returned when rate-limiting resend requests.
	ErrTooManyTokens = errors.New("too many verification requests, try again later")
)

// VerificationToken represents an email verification token.
type VerificationToken struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Token     string    `json:"token"`
	Used      bool      `json:"used"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// DefaultTokenExpiry is how long a verification token remains valid.
const DefaultTokenExpiry = 24 * time.Hour

// DefaultResendCooldown is the minimum interval between resend requests.
const DefaultResendCooldown = 60 * time.Second

// VerificationStore handles verification token persistence.
type VerificationStore interface {
	// CreateToken generates and stores a new verification token for the user.
	CreateToken(userID string, expiry time.Duration) (*VerificationToken, error)
	// GetToken retrieves a token by its value.
	GetToken(token string) (*VerificationToken, error)
	// MarkUsed marks a token as used.
	MarkUsed(token string) error
	// LastTokenTime returns the creation time of the most recent token for a user.
	// Returns zero time if no tokens exist.
	LastTokenTime(userID string) (time.Time, error)
	// DeleteExpired removes all expired tokens.
	DeleteExpired() (int64, error)
	// Close releases resources.
	Close() error
}

// SQLiteVerificationStore implements VerificationStore backed by SQLite.
type SQLiteVerificationStore struct {
	db *sql.DB
	mu sync.RWMutex
}

// NewSQLiteVerificationStore creates a new verification store using the given DB path.
func NewSQLiteVerificationStore(dbPath string) (*SQLiteVerificationStore, error) {
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(4)

	if err := initVerificationSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("init verification schema: %w", err)
	}

	return &SQLiteVerificationStore{db: db}, nil
}

// NewSQLiteVerificationStoreFromDB wraps an existing *sql.DB connection.
func NewSQLiteVerificationStoreFromDB(db *sql.DB) (*SQLiteVerificationStore, error) {
	if err := initVerificationSchema(db); err != nil {
		return nil, fmt.Errorf("init verification schema: %w", err)
	}
	return &SQLiteVerificationStore{db: db}, nil
}

func initVerificationSchema(db *sql.DB) error {
	const schema = `
CREATE TABLE IF NOT EXISTS verification_tokens (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL,
    token       TEXT NOT NULL UNIQUE,
    used        INTEGER NOT NULL DEFAULT 0,
    expires_at  TEXT NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE INDEX IF NOT EXISTS idx_verification_tokens_user ON verification_tokens(user_id);
CREATE INDEX IF NOT EXISTS idx_verification_tokens_token ON verification_tokens(token);
`
	_, err := db.Exec(schema)
	return err
}

// generateToken creates a cryptographically random hex token.
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// CreateToken generates and stores a new verification token for the user.
func (s *SQLiteVerificationStore) CreateToken(userID string, expiry time.Duration) (*VerificationToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	token, err := generateToken()
	if err != nil {
		return nil, err
	}

	id, err := generateToken()
	if err != nil {
		return nil, err
	}
	// Use first 16 chars for ID to keep it shorter.
	id = id[:16]

	now := time.Now()
	expiresAt := now.Add(expiry)

	vt := &VerificationToken{
		ID:        id,
		UserID:    userID,
		Token:     token,
		Used:      false,
		ExpiresAt: expiresAt,
		CreatedAt: now,
	}

	_, err = s.db.Exec(
		`INSERT INTO verification_tokens (id, user_id, token, used, expires_at, created_at)
		 VALUES (?, ?, ?, 0, ?, ?)`,
		vt.ID,
		vt.UserID,
		vt.Token,
		expiresAt.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, fmt.Errorf("insert verification token: %w", err)
	}

	return vt, nil
}

// GetToken retrieves a verification token by its value.
func (s *SQLiteVerificationStore) GetToken(token string) (*VerificationToken, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var vt VerificationToken
	var used int
	var expiresStr, createdStr string

	err := s.db.QueryRow(
		`SELECT id, user_id, token, used, expires_at, created_at
		 FROM verification_tokens WHERE token = ?`,
		token,
	).Scan(&vt.ID, &vt.UserID, &vt.Token, &used, &expiresStr, &createdStr)
	if err == sql.ErrNoRows {
		return nil, ErrTokenNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query verification token: %w", err)
	}

	vt.Used = used == 1
	vt.ExpiresAt, _ = time.Parse(time.RFC3339Nano, expiresStr)
	vt.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdStr)

	return &vt, nil
}

// MarkUsed marks a token as used.
func (s *SQLiteVerificationStore) MarkUsed(token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.Exec(`UPDATE verification_tokens SET used = 1 WHERE token = ?`, token)
	if err != nil {
		return fmt.Errorf("mark token used: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrTokenNotFound
	}
	return nil
}

// LastTokenTime returns the creation time of the most recent token for a user.
func (s *SQLiteVerificationStore) LastTokenTime(userID string) (time.Time, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var createdStr sql.NullString
	err := s.db.QueryRow(
		`SELECT created_at FROM verification_tokens
		 WHERE user_id = ? ORDER BY created_at DESC LIMIT 1`,
		userID,
	).Scan(&createdStr)
	if err == sql.ErrNoRows || !createdStr.Valid {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("query last token time: %w", err)
	}

	t, _ := time.Parse(time.RFC3339Nano, createdStr.String)
	return t, nil
}

// DeleteExpired removes all expired tokens and returns the count deleted.
func (s *SQLiteVerificationStore) DeleteExpired() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Format(time.RFC3339Nano)
	res, err := s.db.Exec(`DELETE FROM verification_tokens WHERE expires_at < ?`, now)
	if err != nil {
		return 0, fmt.Errorf("delete expired tokens: %w", err)
	}
	return res.RowsAffected()
}

// Close closes the underlying database connection.
func (s *SQLiteVerificationStore) Close() error {
	return s.db.Close()
}

// VerifyEmail validates a token and marks the user's email as verified.
// It checks token existence, expiry, and usage. On success, updates the user's
// email_verified flag and status (pending_verification → active).
func VerifyEmail(token string, vs VerificationStore, us UserStore) error {
	vt, err := vs.GetToken(token)
	if err != nil {
		return err
	}

	if vt.Used {
		return ErrTokenUsed
	}

	if time.Now().After(vt.ExpiresAt) {
		return ErrTokenExpired
	}

	// Look up the user.
	user, err := us.GetByID(vt.UserID)
	if err != nil {
		return fmt.Errorf("get user for verification: %w", err)
	}

	if user.EmailVerified {
		return ErrAlreadyVerified
	}

	// Mark token as used.
	if err := vs.MarkUsed(token); err != nil {
		return fmt.Errorf("mark token used: %w", err)
	}

	// Update user.
	user.EmailVerified = true
	if user.Status == StatusPendingVerification {
		user.Status = StatusActive
	}

	if err := us.Update(user); err != nil {
		return fmt.Errorf("update user verification: %w", err)
	}

	return nil
}

// ResendVerification creates a new verification token, enforcing a cooldown.
func ResendVerification(userID string, vs VerificationStore, us UserStore, cooldown time.Duration) (*VerificationToken, error) {
	// Check user exists and isn't already verified.
	user, err := us.GetByID(userID)
	if err != nil {
		return nil, err
	}

	if user.EmailVerified {
		return nil, ErrAlreadyVerified
	}

	// Enforce cooldown.
	lastTime, err := vs.LastTokenTime(userID)
	if err != nil {
		return nil, fmt.Errorf("check last token time: %w", err)
	}
	if !lastTime.IsZero() && time.Since(lastTime) < cooldown {
		return nil, ErrTooManyTokens
	}

	// Create new token.
	vt, err := vs.CreateToken(userID, DefaultTokenExpiry)
	if err != nil {
		return nil, fmt.Errorf("create verification token: %w", err)
	}

	return vt, nil
}

// sanitizeToken trims whitespace from token input.
func sanitizeToken(token string) string {
	return strings.TrimSpace(token)
}
