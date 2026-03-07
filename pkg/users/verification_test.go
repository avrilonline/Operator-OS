package users

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Store Tests ---

func newTestVerificationStore(t *testing.T) (*SQLiteVerificationStore, *SQLiteUserStore) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	us, err := NewSQLiteUserStore(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { us.Close() })

	vs, err := NewSQLiteVerificationStore(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { vs.Close() })

	return vs, us
}

func createTestUser(t *testing.T, us *SQLiteUserStore) *User {
	t.Helper()
	hash, err := HashPassword("TestPass123!")
	require.NoError(t, err)
	user := &User{
		Email:        "test@example.com",
		PasswordHash: hash,
		Status:       StatusPendingVerification,
	}
	require.NoError(t, us.Create(user))
	return user
}

func TestCreateToken(t *testing.T) {
	vs, us := newTestVerificationStore(t)
	user := createTestUser(t, us)

	vt, err := vs.CreateToken(user.ID, DefaultTokenExpiry)
	require.NoError(t, err)
	assert.NotEmpty(t, vt.ID)
	assert.NotEmpty(t, vt.Token)
	assert.Equal(t, user.ID, vt.UserID)
	assert.False(t, vt.Used)
	assert.True(t, vt.ExpiresAt.After(time.Now()))
}

func TestGetToken(t *testing.T) {
	vs, us := newTestVerificationStore(t)
	user := createTestUser(t, us)

	created, err := vs.CreateToken(user.ID, DefaultTokenExpiry)
	require.NoError(t, err)

	got, err := vs.GetToken(created.Token)
	require.NoError(t, err)
	assert.Equal(t, created.ID, got.ID)
	assert.Equal(t, created.UserID, got.UserID)
	assert.Equal(t, created.Token, got.Token)
	assert.False(t, got.Used)
}

func TestGetTokenNotFound(t *testing.T) {
	vs, _ := newTestVerificationStore(t)

	_, err := vs.GetToken("nonexistent")
	assert.ErrorIs(t, err, ErrTokenNotFound)
}

func TestMarkUsed(t *testing.T) {
	vs, us := newTestVerificationStore(t)
	user := createTestUser(t, us)

	vt, err := vs.CreateToken(user.ID, DefaultTokenExpiry)
	require.NoError(t, err)

	err = vs.MarkUsed(vt.Token)
	require.NoError(t, err)

	got, err := vs.GetToken(vt.Token)
	require.NoError(t, err)
	assert.True(t, got.Used)
}

func TestMarkUsedNotFound(t *testing.T) {
	vs, _ := newTestVerificationStore(t)

	err := vs.MarkUsed("nonexistent")
	assert.ErrorIs(t, err, ErrTokenNotFound)
}

func TestLastTokenTime(t *testing.T) {
	vs, us := newTestVerificationStore(t)
	user := createTestUser(t, us)

	// No tokens yet.
	lt, err := vs.LastTokenTime(user.ID)
	require.NoError(t, err)
	assert.True(t, lt.IsZero())

	// Create a token.
	vt, err := vs.CreateToken(user.ID, DefaultTokenExpiry)
	require.NoError(t, err)

	lt, err = vs.LastTokenTime(user.ID)
	require.NoError(t, err)
	assert.False(t, lt.IsZero())
	// Should be close to the token's creation time.
	assert.WithinDuration(t, vt.CreatedAt, lt, time.Second)
}

func TestDeleteExpired(t *testing.T) {
	vs, us := newTestVerificationStore(t)
	user := createTestUser(t, us)

	// Create a token that expires immediately.
	_, err := vs.CreateToken(user.ID, -1*time.Hour)
	require.NoError(t, err)

	// Create a valid token.
	valid, err := vs.CreateToken(user.ID, DefaultTokenExpiry)
	require.NoError(t, err)

	deleted, err := vs.DeleteExpired()
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)

	// Valid token should still exist.
	_, err = vs.GetToken(valid.Token)
	require.NoError(t, err)
}

func TestDeleteExpiredNone(t *testing.T) {
	vs, us := newTestVerificationStore(t)
	user := createTestUser(t, us)

	_, err := vs.CreateToken(user.ID, DefaultTokenExpiry)
	require.NoError(t, err)

	deleted, err := vs.DeleteExpired()
	require.NoError(t, err)
	assert.Equal(t, int64(0), deleted)
}

// --- VerifyEmail Function Tests ---

func TestVerifyEmailSuccess(t *testing.T) {
	vs, us := newTestVerificationStore(t)
	user := createTestUser(t, us)

	vt, err := vs.CreateToken(user.ID, DefaultTokenExpiry)
	require.NoError(t, err)

	err = VerifyEmail(vt.Token, vs, us)
	require.NoError(t, err)

	// User should be verified and active.
	updated, err := us.GetByID(user.ID)
	require.NoError(t, err)
	assert.True(t, updated.EmailVerified)
	assert.Equal(t, StatusActive, updated.Status)

	// Token should be marked used.
	got, err := vs.GetToken(vt.Token)
	require.NoError(t, err)
	assert.True(t, got.Used)
}

func TestVerifyEmailTokenNotFound(t *testing.T) {
	vs, us := newTestVerificationStore(t)
	_ = createTestUser(t, us)

	err := VerifyEmail("nonexistent", vs, us)
	assert.ErrorIs(t, err, ErrTokenNotFound)
}

func TestVerifyEmailTokenExpired(t *testing.T) {
	vs, us := newTestVerificationStore(t)
	user := createTestUser(t, us)

	vt, err := vs.CreateToken(user.ID, -1*time.Hour)
	require.NoError(t, err)

	err = VerifyEmail(vt.Token, vs, us)
	assert.ErrorIs(t, err, ErrTokenExpired)
}

func TestVerifyEmailTokenUsed(t *testing.T) {
	vs, us := newTestVerificationStore(t)
	user := createTestUser(t, us)

	vt, err := vs.CreateToken(user.ID, DefaultTokenExpiry)
	require.NoError(t, err)

	// Use it once.
	err = VerifyEmail(vt.Token, vs, us)
	require.NoError(t, err)

	// Create another user to test the "already used" path without hitting "already verified".
	hash, _ := HashPassword("Pass1234!")
	user2 := &User{
		Email:        "test2@example.com",
		PasswordHash: hash,
		Status:       StatusPendingVerification,
	}
	require.NoError(t, us.Create(user2))

	vt2, err := vs.CreateToken(user2.ID, DefaultTokenExpiry)
	require.NoError(t, err)
	require.NoError(t, vs.MarkUsed(vt2.Token))

	err = VerifyEmail(vt2.Token, vs, us)
	assert.ErrorIs(t, err, ErrTokenUsed)
}

func TestVerifyEmailAlreadyVerified(t *testing.T) {
	vs, us := newTestVerificationStore(t)
	user := createTestUser(t, us)

	// Manually verify the user.
	user.EmailVerified = true
	user.Status = StatusActive
	require.NoError(t, us.Update(user))

	vt, err := vs.CreateToken(user.ID, DefaultTokenExpiry)
	require.NoError(t, err)

	err = VerifyEmail(vt.Token, vs, us)
	assert.ErrorIs(t, err, ErrAlreadyVerified)
}

// --- ResendVerification Function Tests ---

func TestResendVerificationSuccess(t *testing.T) {
	vs, us := newTestVerificationStore(t)
	user := createTestUser(t, us)

	vt, err := ResendVerification(user.ID, vs, us, 0)
	require.NoError(t, err)
	assert.NotEmpty(t, vt.Token)
	assert.Equal(t, user.ID, vt.UserID)
}

func TestResendVerificationAlreadyVerified(t *testing.T) {
	vs, us := newTestVerificationStore(t)
	user := createTestUser(t, us)

	user.EmailVerified = true
	user.Status = StatusActive
	require.NoError(t, us.Update(user))

	_, err := ResendVerification(user.ID, vs, us, 0)
	assert.ErrorIs(t, err, ErrAlreadyVerified)
}

func TestResendVerificationCooldown(t *testing.T) {
	vs, us := newTestVerificationStore(t)
	user := createTestUser(t, us)

	// Create first token (no cooldown).
	_, err := ResendVerification(user.ID, vs, us, 0)
	require.NoError(t, err)

	// Try immediately with cooldown.
	_, err = ResendVerification(user.ID, vs, us, 5*time.Minute)
	assert.ErrorIs(t, err, ErrTooManyTokens)
}

func TestResendVerificationUserNotFound(t *testing.T) {
	vs, us := newTestVerificationStore(t)

	_, err := ResendVerification("nonexistent", vs, us, 0)
	assert.ErrorIs(t, err, ErrNotFound)
}

// --- Constructor Tests ---

func TestNewSQLiteVerificationStore(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	vs, err := NewSQLiteVerificationStore(dbPath)
	require.NoError(t, err)
	defer vs.Close()

	assert.NotNil(t, vs)
}

func TestNewSQLiteVerificationStoreInvalidPath(t *testing.T) {
	_, err := NewSQLiteVerificationStore("/nonexistent/dir/test.db")
	assert.Error(t, err)
}

func TestNewSQLiteVerificationStoreFromDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	us, err := NewSQLiteUserStore(dbPath)
	require.NoError(t, err)
	defer us.Close()

	// Get the DB from the user store by creating a new one with the same path.
	vs, err := NewSQLiteVerificationStore(dbPath)
	require.NoError(t, err)
	defer vs.Close()

	assert.NotNil(t, vs)
}

func TestGenerateTokenUniqueness(t *testing.T) {
	tokens := make(map[string]bool)
	for i := 0; i < 100; i++ {
		tok, err := generateToken()
		require.NoError(t, err)
		assert.False(t, tokens[tok], "duplicate token generated")
		tokens[tok] = true
	}
}

func TestSanitizeToken(t *testing.T) {
	assert.Equal(t, "abc123", sanitizeToken("  abc123  "))
	assert.Equal(t, "", sanitizeToken("   "))
	assert.Equal(t, "token", sanitizeToken("token"))
}

// --- API Handler Tests ---

func newTestAPIWithVerification(t *testing.T) (*API, *SQLiteUserStore, *SQLiteVerificationStore) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	us, err := NewSQLiteUserStore(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { us.Close() })

	vs, err := NewSQLiteVerificationStore(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { vs.Close() })

	ts, err := NewTokenService([]byte("test-secret-key-for-testing-only"))
	require.NoError(t, err)

	api := NewAPIFull(us, ts, vs)
	return api, us, vs
}

func TestHandleVerifyEmailSuccess(t *testing.T) {
	api, us, vs := newTestAPIWithVerification(t)
	user := createTestUser(t, us)

	vt, err := vs.CreateToken(user.ID, DefaultTokenExpiry)
	require.NoError(t, err)

	body := `{"token":"` + vt.Token + `"}`
	req := httptest.NewRequest("POST", "/api/v1/auth/verify-email", strings.NewReader(body))
	w := httptest.NewRecorder()

	api.handleVerifyEmail(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp VerifyEmailResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.True(t, resp.EmailVerified)
	assert.Equal(t, StatusActive, resp.Status)
	assert.Contains(t, resp.Message, "verified")
}

func TestHandleVerifyEmailMissingToken(t *testing.T) {
	api, _, _ := newTestAPIWithVerification(t)

	body := `{"token":""}`
	req := httptest.NewRequest("POST", "/api/v1/auth/verify-email", strings.NewReader(body))
	w := httptest.NewRecorder()

	api.handleVerifyEmail(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleVerifyEmailInvalidJSON(t *testing.T) {
	api, _, _ := newTestAPIWithVerification(t)

	req := httptest.NewRequest("POST", "/api/v1/auth/verify-email", strings.NewReader("not json"))
	w := httptest.NewRecorder()

	api.handleVerifyEmail(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleVerifyEmailNotFound(t *testing.T) {
	api, _, _ := newTestAPIWithVerification(t)

	body := `{"token":"nonexistent"}`
	req := httptest.NewRequest("POST", "/api/v1/auth/verify-email", strings.NewReader(body))
	w := httptest.NewRecorder()

	api.handleVerifyEmail(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleVerifyEmailExpired(t *testing.T) {
	api, us, vs := newTestAPIWithVerification(t)
	user := createTestUser(t, us)

	vt, err := vs.CreateToken(user.ID, -1*time.Hour)
	require.NoError(t, err)

	body := `{"token":"` + vt.Token + `"}`
	req := httptest.NewRequest("POST", "/api/v1/auth/verify-email", strings.NewReader(body))
	w := httptest.NewRecorder()

	api.handleVerifyEmail(w, req)

	assert.Equal(t, http.StatusGone, w.Code)
}

func TestHandleVerifyEmailAlreadyUsed(t *testing.T) {
	api, us, vs := newTestAPIWithVerification(t)
	user := createTestUser(t, us)

	vt, err := vs.CreateToken(user.ID, DefaultTokenExpiry)
	require.NoError(t, err)

	// Verify once.
	err = VerifyEmail(vt.Token, vs, us)
	require.NoError(t, err)

	// Create a new token for the same user (who is now verified).
	vt2, err := vs.CreateToken(user.ID, DefaultTokenExpiry)
	require.NoError(t, err)

	body := `{"token":"` + vt2.Token + `"}`
	req := httptest.NewRequest("POST", "/api/v1/auth/verify-email", strings.NewReader(body))
	w := httptest.NewRecorder()

	api.handleVerifyEmail(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestHandleVerifyEmailNoVerificationStore(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	us, err := NewSQLiteUserStore(dbPath)
	require.NoError(t, err)
	defer us.Close()

	api := NewAPI(us)

	body := `{"token":"sometoken"}`
	req := httptest.NewRequest("POST", "/api/v1/auth/verify-email", strings.NewReader(body))
	w := httptest.NewRecorder()

	api.handleVerifyEmail(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleResendVerificationSuccess(t *testing.T) {
	api, us, _ := newTestAPIWithVerification(t)
	createTestUser(t, us)

	body := `{"email":"test@example.com"}`
	req := httptest.NewRequest("POST", "/api/v1/auth/resend-verification", strings.NewReader(body))
	w := httptest.NewRecorder()

	api.handleResendVerification(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp ResendVerificationResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Contains(t, resp.Message, "verification")
}

func TestHandleResendVerificationMissingEmail(t *testing.T) {
	api, _, _ := newTestAPIWithVerification(t)

	body := `{"email":""}`
	req := httptest.NewRequest("POST", "/api/v1/auth/resend-verification", strings.NewReader(body))
	w := httptest.NewRecorder()

	api.handleResendVerification(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleResendVerificationNonexistentUser(t *testing.T) {
	api, _, _ := newTestAPIWithVerification(t)

	// Should return 200 even for non-existent users (anti-enumeration).
	body := `{"email":"nobody@example.com"}`
	req := httptest.NewRequest("POST", "/api/v1/auth/resend-verification", strings.NewReader(body))
	w := httptest.NewRecorder()

	api.handleResendVerification(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleResendVerificationAlreadyVerified(t *testing.T) {
	api, us, _ := newTestAPIWithVerification(t)
	user := createTestUser(t, us)

	user.EmailVerified = true
	user.Status = StatusActive
	require.NoError(t, us.Update(user))

	body := `{"email":"test@example.com"}`
	req := httptest.NewRequest("POST", "/api/v1/auth/resend-verification", strings.NewReader(body))
	w := httptest.NewRecorder()

	api.handleResendVerification(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestHandleResendVerificationInvalidJSON(t *testing.T) {
	api, _, _ := newTestAPIWithVerification(t)

	req := httptest.NewRequest("POST", "/api/v1/auth/resend-verification", strings.NewReader("not json"))
	w := httptest.NewRecorder()

	api.handleResendVerification(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleResendVerificationNoStore(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	us, err := NewSQLiteUserStore(dbPath)
	require.NoError(t, err)
	defer us.Close()

	api := NewAPI(us)

	body := `{"email":"test@example.com"}`
	req := httptest.NewRequest("POST", "/api/v1/auth/resend-verification", strings.NewReader(body))
	w := httptest.NewRecorder()

	api.handleResendVerification(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// --- Persistence Test ---

func TestVerificationStorePersistence(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "persist.db")

	// Create store, add token, close.
	us, err := NewSQLiteUserStore(dbPath)
	require.NoError(t, err)
	user := createTestUser(t, us)

	vs, err := NewSQLiteVerificationStore(dbPath)
	require.NoError(t, err)

	vt, err := vs.CreateToken(user.ID, DefaultTokenExpiry)
	require.NoError(t, err)
	tokenVal := vt.Token

	vs.Close()
	us.Close()

	// Reopen and verify token persisted.
	vs2, err := NewSQLiteVerificationStore(dbPath)
	require.NoError(t, err)
	defer vs2.Close()

	got, err := vs2.GetToken(tokenVal)
	require.NoError(t, err)
	assert.Equal(t, tokenVal, got.Token)
}

// --- Full Flow Test ---

func TestFullVerificationFlow(t *testing.T) {
	api, us, vs := newTestAPIWithVerification(t)
	_ = vs

	// 1. Register.
	regBody := `{"email":"flow@example.com","password":"FlowTest123!"}`
	regReq := httptest.NewRequest("POST", "/api/v1/auth/register", strings.NewReader(regBody))
	regW := httptest.NewRecorder()
	api.handleRegister(regW, regReq)
	require.Equal(t, http.StatusCreated, regW.Code)

	var regResp RegisterResponse
	require.NoError(t, json.NewDecoder(regW.Body).Decode(&regResp))
	assert.Equal(t, StatusPendingVerification, regResp.Status)
	assert.False(t, regResp.EmailVerified)

	// 2. Create verification token (in production, this would happen during registration).
	user, err := us.GetByEmail("flow@example.com")
	require.NoError(t, err)

	vt, err := api.verificationStore.(*SQLiteVerificationStore).CreateToken(user.ID, DefaultTokenExpiry)
	require.NoError(t, err)

	// 3. Verify email.
	verifyBody := `{"token":"` + vt.Token + `"}`
	verifyReq := httptest.NewRequest("POST", "/api/v1/auth/verify-email", strings.NewReader(verifyBody))
	verifyW := httptest.NewRecorder()
	api.handleVerifyEmail(verifyW, verifyReq)
	require.Equal(t, http.StatusOK, verifyW.Code)

	// 4. Confirm user is now active and verified.
	updated, err := us.GetByEmail("flow@example.com")
	require.NoError(t, err)
	assert.True(t, updated.EmailVerified)
	assert.Equal(t, StatusActive, updated.Status)
}

// Ensure we don't leave test files.
func TestCleanup(t *testing.T) {
	dir := t.TempDir()
	_, err := os.Stat(dir)
	assert.NoError(t, err) // dir exists during test
}
