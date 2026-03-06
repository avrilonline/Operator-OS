package users

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) *SQLiteUserStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test_users.db")
	store, err := NewSQLiteUserStore(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	return store
}

func TestNewSQLiteUserStore(t *testing.T) {
	store := newTestStore(t)
	assert.NotNil(t, store)
}

func TestNewSQLiteUserStore_InvalidPath(t *testing.T) {
	_, err := NewSQLiteUserStore("/nonexistent/dir/test.db")
	assert.Error(t, err)
}

func TestCreateUser(t *testing.T) {
	store := newTestStore(t)

	user := &User{
		Email:        "test@example.com",
		PasswordHash: "$2a$12$fakehash",
		DisplayName:  "Test User",
	}

	err := store.Create(user)
	require.NoError(t, err)
	assert.NotEmpty(t, user.ID, "should generate UUID")
	assert.Equal(t, StatusPendingVerification, user.Status)
	assert.False(t, user.CreatedAt.IsZero())
	assert.False(t, user.UpdatedAt.IsZero())
}

func TestCreateUser_DuplicateEmail(t *testing.T) {
	store := newTestStore(t)

	user1 := &User{Email: "dupe@example.com", PasswordHash: "hash1"}
	require.NoError(t, store.Create(user1))

	user2 := &User{Email: "dupe@example.com", PasswordHash: "hash2"}
	err := store.Create(user2)
	assert.ErrorIs(t, err, ErrEmailExists)
}

func TestCreateUser_CaseInsensitiveEmail(t *testing.T) {
	store := newTestStore(t)

	user1 := &User{Email: "Test@Example.COM", PasswordHash: "hash1"}
	require.NoError(t, store.Create(user1))

	user2 := &User{Email: "test@example.com", PasswordHash: "hash2"}
	err := store.Create(user2)
	assert.ErrorIs(t, err, ErrEmailExists)
}

func TestGetByID(t *testing.T) {
	store := newTestStore(t)

	user := &User{Email: "test@example.com", PasswordHash: "hash", DisplayName: "Test"}
	require.NoError(t, store.Create(user))

	found, err := store.GetByID(user.ID)
	require.NoError(t, err)
	assert.Equal(t, user.ID, found.ID)
	assert.Equal(t, "test@example.com", found.Email)
	assert.Equal(t, "Test", found.DisplayName)
	assert.Equal(t, StatusPendingVerification, found.Status)
}

func TestGetByID_NotFound(t *testing.T) {
	store := newTestStore(t)

	_, err := store.GetByID("nonexistent")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestGetByEmail(t *testing.T) {
	store := newTestStore(t)

	user := &User{Email: "lookup@example.com", PasswordHash: "hash"}
	require.NoError(t, store.Create(user))

	found, err := store.GetByEmail("Lookup@Example.COM")
	require.NoError(t, err)
	assert.Equal(t, user.ID, found.ID)
	assert.Equal(t, "lookup@example.com", found.Email)
}

func TestGetByEmail_NotFound(t *testing.T) {
	store := newTestStore(t)

	_, err := store.GetByEmail("nobody@example.com")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestUpdateUser(t *testing.T) {
	store := newTestStore(t)

	user := &User{Email: "update@example.com", PasswordHash: "hash"}
	require.NoError(t, store.Create(user))

	user.DisplayName = "Updated Name"
	user.Status = StatusActive
	user.EmailVerified = true

	err := store.Update(user)
	require.NoError(t, err)

	found, err := store.GetByID(user.ID)
	require.NoError(t, err)
	assert.Equal(t, "Updated Name", found.DisplayName)
	assert.Equal(t, StatusActive, found.Status)
	assert.True(t, found.EmailVerified)
}

func TestUpdateUser_NotFound(t *testing.T) {
	store := newTestStore(t)

	user := &User{ID: "nonexistent", Email: "x@y.com", PasswordHash: "h"}
	err := store.Update(user)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestUpdateUser_DuplicateEmail(t *testing.T) {
	store := newTestStore(t)

	user1 := &User{Email: "first@example.com", PasswordHash: "hash"}
	require.NoError(t, store.Create(user1))

	user2 := &User{Email: "second@example.com", PasswordHash: "hash"}
	require.NoError(t, store.Create(user2))

	user2.Email = "first@example.com"
	err := store.Update(user2)
	assert.ErrorIs(t, err, ErrEmailExists)
}

func TestDeleteUser(t *testing.T) {
	store := newTestStore(t)

	user := &User{Email: "delete@example.com", PasswordHash: "hash"}
	require.NoError(t, store.Create(user))

	err := store.Delete(user.ID)
	require.NoError(t, err)

	_, err = store.GetByID(user.ID)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestDeleteUser_NotFound(t *testing.T) {
	store := newTestStore(t)

	err := store.Delete("nonexistent")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestListUsers(t *testing.T) {
	store := newTestStore(t)

	// Empty list.
	users, err := store.List()
	require.NoError(t, err)
	assert.Empty(t, users)

	// Add users.
	for _, email := range []string{"a@x.com", "b@x.com", "c@x.com"} {
		require.NoError(t, store.Create(&User{Email: email, PasswordHash: "hash"}))
	}

	users, err = store.List()
	require.NoError(t, err)
	assert.Len(t, users, 3)
	// Newest first.
	assert.Equal(t, "c@x.com", users[0].Email)
}

func TestCountUsers(t *testing.T) {
	store := newTestStore(t)

	count, err := store.Count()
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)

	for _, email := range []string{"a@x.com", "b@x.com"} {
		require.NoError(t, store.Create(&User{Email: email, PasswordHash: "hash"}))
	}

	count, err = store.Count()
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)
}

func TestNewSQLiteUserStoreFromDB(t *testing.T) {
	store := newTestStore(t)

	// Create a second store wrapping the same DB (simulating shared connection).
	store2 := NewSQLiteUserStoreFromDB(store.db)
	assert.NotNil(t, store2)

	user := &User{Email: "shared@example.com", PasswordHash: "hash"}
	require.NoError(t, store.Create(user))

	found, err := store2.GetByID(user.ID)
	require.NoError(t, err)
	assert.Equal(t, user.Email, found.Email)
}

func TestCreateUser_CustomID(t *testing.T) {
	store := newTestStore(t)

	user := &User{
		ID:           "custom-id-123",
		Email:        "custom@example.com",
		PasswordHash: "hash",
	}

	require.NoError(t, store.Create(user))
	assert.Equal(t, "custom-id-123", user.ID)

	found, err := store.GetByID("custom-id-123")
	require.NoError(t, err)
	assert.Equal(t, "custom@example.com", found.Email)
}

func TestSQLiteUserStore_Persistence(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "persist.db")

	// Create store, add user, close.
	store1, err := NewSQLiteUserStore(dbPath)
	require.NoError(t, err)

	user := &User{Email: "persist@example.com", PasswordHash: "hash"}
	require.NoError(t, store1.Create(user))
	store1.Close()

	// Reopen and verify.
	store2, err := NewSQLiteUserStore(dbPath)
	require.NoError(t, err)
	defer store2.Close()

	found, err := store2.GetByEmail("persist@example.com")
	require.NoError(t, err)
	assert.Equal(t, user.ID, found.ID)
}

// Ensure temp dir cleanup.
func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
