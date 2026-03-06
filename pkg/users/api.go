package users

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"strings"
)

// API provides HTTP handlers for user management endpoints.
type API struct {
	store UserStore
}

// NewAPI creates a new API with the given UserStore.
func NewAPI(store UserStore) *API {
	return &API{store: store}
}

// RegisterRoutes registers user management endpoints on the given ServeMux.
func (a *API) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/auth/register", a.handleRegister)
}

// RegisterRequest is the JSON body for user registration.
type RegisterRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name,omitempty"`
}

// RegisterResponse is the JSON response after successful registration.
type RegisterResponse struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	DisplayName   string `json:"display_name,omitempty"`
	Status        string `json:"status"`
	EmailVerified bool   `json:"email_verified"`
}

// ErrorResponse is a standard error JSON response.
type ErrorResponse struct {
	Error   string `json:"error"`
	Code    string `json:"code,omitempty"`
	Details string `json:"details,omitempty"`
}

func (a *API) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Invalid request body")
		return
	}

	// Validate email.
	req.Email = strings.TrimSpace(req.Email)
	if req.Email == "" {
		writeError(w, http.StatusBadRequest, "missing_email", "Email is required")
		return
	}
	if err := validateEmail(req.Email); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_email", err.Error())
		return
	}

	// Validate password.
	if err := ValidatePassword(req.Password); err != nil {
		writeError(w, http.StatusBadRequest, "weak_password", err.Error())
		return
	}

	// Hash password.
	hash, err := HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Failed to process password")
		return
	}

	user := &User{
		Email:        strings.ToLower(req.Email),
		PasswordHash: hash,
		DisplayName:  strings.TrimSpace(req.DisplayName),
		Status:       StatusPendingVerification,
	}

	if err := a.store.Create(user); err != nil {
		if errors.Is(err, ErrEmailExists) {
			writeError(w, http.StatusConflict, "email_exists", "An account with this email already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Failed to create account")
		return
	}

	resp := RegisterResponse{
		ID:            user.ID,
		Email:         user.Email,
		DisplayName:   user.DisplayName,
		Status:        user.Status,
		EmailVerified: user.EmailVerified,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

// validateEmail checks basic email format using net/mail.
func validateEmail(email string) error {
	_, err := mail.ParseAddress(email)
	if err != nil {
		return ErrInvalidEmail
	}
	return nil
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(ErrorResponse{
		Error: message,
		Code:  code,
	})
}
