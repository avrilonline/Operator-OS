package users

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// SQLiteUserStore implements UserStore backed by a SQLite database.
type SQLiteUserStore struct {
	db *sql.DB
	mu sync.RWMutex
}

// NewSQLiteUserStore opens (or creates) a SQLite database at dbPath and
// initialises the users schema.
func NewSQLiteUserStore(dbPath string) (*SQLiteUserStore, error) {
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(4)

	if err := initUsersSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("init users schema: %w", err)
	}

	return &SQLiteUserStore{db: db}, nil
}

// NewSQLiteUserStoreFromDB wraps an existing *sql.DB connection.
// The caller is responsible for schema initialization.
func NewSQLiteUserStoreFromDB(db *sql.DB) *SQLiteUserStore {
	return &SQLiteUserStore{db: db}
}

func initUsersSchema(db *sql.DB) error {
	const schema = `
CREATE TABLE IF NOT EXISTS users (
    id              TEXT PRIMARY KEY,
    email           TEXT NOT NULL UNIQUE,
    password_hash   TEXT NOT NULL,
    display_name    TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'pending_verification',
    email_verified  INTEGER NOT NULL DEFAULT 0,
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
CREATE INDEX IF NOT EXISTS idx_users_status ON users(status);
`
	_, err := db.Exec(schema)
	return err
}

// Create inserts a new user. Generates a UUID if user.ID is empty.
func (s *SQLiteUserStore) Create(user *User) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if user.ID == "" {
		user.ID = uuid.New().String()
	}

	now := time.Now()
	user.CreatedAt = now
	user.UpdatedAt = now

	if user.Status == "" {
		user.Status = StatusPendingVerification
	}

	emailVerified := 0
	if user.EmailVerified {
		emailVerified = 1
	}

	_, err := s.db.Exec(
		`INSERT INTO users (id, email, password_hash, display_name, status, email_verified, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		user.ID,
		strings.ToLower(user.Email),
		user.PasswordHash,
		user.DisplayName,
		user.Status,
		emailVerified,
		now.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrEmailExists
		}
		return fmt.Errorf("insert user: %w", err)
	}

	return nil
}

// GetByID returns the user with the given ID.
func (s *SQLiteUserStore) GetByID(id string) (*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getBy("id", id)
}

// GetByEmail returns the user with the given email (case-insensitive).
func (s *SQLiteUserStore) GetByEmail(email string) (*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getBy("email", strings.ToLower(email))
}

func (s *SQLiteUserStore) getBy(column, value string) (*User, error) {
	query := fmt.Sprintf(
		`SELECT id, email, password_hash, display_name, status, email_verified, created_at, updated_at
		 FROM users WHERE %s = ?`, column,
	)

	var u User
	var emailVerified int
	var createdStr, updatedStr string

	err := s.db.QueryRow(query, value).Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.DisplayName,
		&u.Status, &emailVerified, &createdStr, &updatedStr,
	)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query user by %s: %w", column, err)
	}

	u.EmailVerified = emailVerified == 1
	u.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdStr)
	u.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedStr)

	return &u, nil
}

// Update saves changes to an existing user.
func (s *SQLiteUserStore) Update(user *User) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	user.UpdatedAt = time.Now()

	emailVerified := 0
	if user.EmailVerified {
		emailVerified = 1
	}

	res, err := s.db.Exec(
		`UPDATE users SET email = ?, password_hash = ?, display_name = ?,
		 status = ?, email_verified = ?, updated_at = ?
		 WHERE id = ?`,
		strings.ToLower(user.Email),
		user.PasswordHash,
		user.DisplayName,
		user.Status,
		emailVerified,
		user.UpdatedAt.Format(time.RFC3339Nano),
		user.ID,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrEmailExists
		}
		return fmt.Errorf("update user: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete removes a user by ID.
func (s *SQLiteUserStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.Exec(`DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// List returns all users ordered by created_at descending.
func (s *SQLiteUserStore) List() ([]*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		`SELECT id, email, password_hash, display_name, status, email_verified, created_at, updated_at
		 FROM users ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		var u User
		var emailVerified int
		var createdStr, updatedStr string

		if err := rows.Scan(
			&u.ID, &u.Email, &u.PasswordHash, &u.DisplayName,
			&u.Status, &emailVerified, &createdStr, &updatedStr,
		); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}

		u.EmailVerified = emailVerified == 1
		u.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdStr)
		u.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedStr)

		users = append(users, &u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate users: %w", err)
	}

	if users == nil {
		users = []*User{}
	}
	return users, nil
}

// Count returns the total number of users.
func (s *SQLiteUserStore) Count() (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int64
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return count, nil
}

// Close closes the underlying database connection.
func (s *SQLiteUserStore) Close() error {
	return s.db.Close()
}

// isUniqueViolation checks if a SQLite error is a UNIQUE constraint violation.
func isUniqueViolation(err error) bool {
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
