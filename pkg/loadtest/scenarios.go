package loadtest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// --- HTTP Client Helpers ---

// HTTPClient wraps an *http.Client with a base URL and optional auth token.
type HTTPClient struct {
	BaseURL    string
	Client     *http.Client
	AuthToken  string
	mu         sync.RWMutex
}

// NewHTTPClient creates a client for load testing against a server.
func NewHTTPClient(baseURL string) *HTTPClient {
	return &HTTPClient{
		BaseURL: baseURL,
		Client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        1000,
				MaxIdleConnsPerHost: 1000,
				MaxConnsPerHost:     1000,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

// SetAuthToken sets the bearer token for authenticated requests.
func (c *HTTPClient) SetAuthToken(token string) {
	c.mu.Lock()
	c.AuthToken = token
	c.mu.Unlock()
}

// Do executes an HTTP request with optional auth and returns the response.
func (c *HTTPClient) Do(ctx context.Context, method, path string, body interface{}) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	c.mu.RLock()
	if c.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.AuthToken)
	}
	c.mu.RUnlock()

	return c.Client.Do(req)
}

// DoAndClose executes a request and closes the response body, returning the status code.
func (c *HTTPClient) DoAndClose(ctx context.Context, method, path string, body interface{}) (int, error) {
	resp, err := c.Do(ctx, method, path, body)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

// DoJSON executes a request and decodes the JSON response into target.
func (c *HTTPClient) DoJSON(ctx context.Context, method, path string, body, target interface{}) (int, error) {
	resp, err := c.Do(ctx, method, path, body)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if target != nil {
		if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
			return resp.StatusCode, fmt.Errorf("decode response: %w", err)
		}
	} else {
		_, _ = io.Copy(io.Discard, resp.Body)
	}
	return resp.StatusCode, nil
}

// --- Built-in Scenarios ---

// HealthCheckScenario creates a scenario that hits the health endpoint.
func HealthCheckScenario(client *HTTPClient) Scenario {
	return Scenario{
		Name:   "health_check",
		Weight: 1,
		Fn: func(ctx context.Context, userID, iteration int) error {
			status, err := client.DoAndClose(ctx, "GET", "/health", nil)
			if err != nil {
				return err
			}
			if status != http.StatusOK {
				return fmt.Errorf("health check returned %d", status)
			}
			return nil
		},
	}
}

// ReadinessCheckScenario creates a scenario that hits the readiness endpoint.
func ReadinessCheckScenario(client *HTTPClient) Scenario {
	return Scenario{
		Name:   "readiness_check",
		Weight: 1,
		Fn: func(ctx context.Context, userID, iteration int) error {
			status, err := client.DoAndClose(ctx, "GET", "/ready", nil)
			if err != nil {
				return err
			}
			if status != http.StatusOK {
				return fmt.Errorf("readiness check returned %d", status)
			}
			return nil
		},
	}
}

// ListPlansScenario creates a scenario that lists billing plans (public endpoint).
func ListPlansScenario(client *HTTPClient) Scenario {
	return Scenario{
		Name:   "list_plans",
		Weight: 2,
		Fn: func(ctx context.Context, userID, iteration int) error {
			status, err := client.DoAndClose(ctx, "GET", "/api/v1/billing/plans", nil)
			if err != nil {
				return err
			}
			if status != http.StatusOK {
				return fmt.Errorf("list plans returned %d", status)
			}
			return nil
		},
	}
}

// --- User Pool for Authenticated Scenarios ---

// UserPool manages a pool of pre-registered test users with JWT tokens.
type UserPool struct {
	mu     sync.RWMutex
	tokens []string // access tokens
	emails []string
	idx    atomic.Int64
}

// NewUserPool creates a pool of test users by registering and logging them in.
func NewUserPool(ctx context.Context, client *HTTPClient, count int) (*UserPool, error) {
	pool := &UserPool{
		tokens: make([]string, 0, count),
		emails: make([]string, 0, count),
	}

	for i := 0; i < count; i++ {
		email := fmt.Sprintf("loadtest-user-%d@test.operator.os", i)
		password := fmt.Sprintf("LoadTest!Pass%d#2026", i)

		// Register.
		regBody := map[string]string{
			"email":    email,
			"password": password,
		}
		status, err := client.DoAndClose(ctx, "POST", "/api/v1/auth/register", regBody)
		if err != nil {
			return nil, fmt.Errorf("register user %d: %w", i, err)
		}
		// 201 = new, 409 = already exists (re-run)
		if status != http.StatusCreated && status != http.StatusConflict {
			return nil, fmt.Errorf("register user %d: unexpected status %d", i, status)
		}

		// Login.
		loginBody := map[string]string{
			"email":    email,
			"password": password,
		}
		var loginResp struct {
			AccessToken string `json:"access_token"`
		}
		status, err = client.DoJSON(ctx, "POST", "/api/v1/auth/login", loginBody, &loginResp)
		if err != nil {
			return nil, fmt.Errorf("login user %d: %w", i, err)
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("login user %d: unexpected status %d", i, status)
		}

		pool.tokens = append(pool.tokens, loginResp.AccessToken)
		pool.emails = append(pool.emails, email)
	}

	return pool, nil
}

// GetToken returns a token for the given user ID (round-robin through pool).
func (p *UserPool) GetToken(userID int) string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if len(p.tokens) == 0 {
		return ""
	}
	return p.tokens[userID%len(p.tokens)]
}

// Size returns the number of users in the pool.
func (p *UserPool) Size() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.tokens)
}

// --- Authenticated Scenarios ---

// AuthenticatedClientForUser returns an HTTP client with the user's token set.
func AuthenticatedClientForUser(baseClient *HTTPClient, pool *UserPool, userID int) *HTTPClient {
	return &HTTPClient{
		BaseURL:   baseClient.BaseURL,
		Client:    baseClient.Client,
		AuthToken: pool.GetToken(userID),
	}
}

// ListAgentsScenario creates a scenario that lists the user's agents.
func ListAgentsScenario(client *HTTPClient, pool *UserPool) Scenario {
	return Scenario{
		Name:   "list_agents",
		Weight: 3,
		Fn: func(ctx context.Context, userID, iteration int) error {
			ac := AuthenticatedClientForUser(client, pool, userID)
			status, err := ac.DoAndClose(ctx, "GET", "/api/v1/agents", nil)
			if err != nil {
				return err
			}
			if status != http.StatusOK {
				return fmt.Errorf("list agents returned %d", status)
			}
			return nil
		},
	}
}

// CreateAndDeleteAgentScenario creates an agent and immediately deletes it.
func CreateAndDeleteAgentScenario(client *HTTPClient, pool *UserPool) Scenario {
	var counter atomic.Int64
	return Scenario{
		Name:   "create_delete_agent",
		Weight: 2,
		Fn: func(ctx context.Context, userID, iteration int) error {
			ac := AuthenticatedClientForUser(client, pool, userID)
			n := counter.Add(1)
			body := map[string]interface{}{
				"name":        fmt.Sprintf("loadtest-agent-%d-%d", userID, n),
				"description": "Load test agent",
			}
			var createResp struct {
				ID string `json:"id"`
			}
			status, err := ac.DoJSON(ctx, "POST", "/api/v1/agents", body, &createResp)
			if err != nil {
				return err
			}
			if status != http.StatusCreated {
				return fmt.Errorf("create agent returned %d", status)
			}

			// Delete it.
			status, err = ac.DoAndClose(ctx, "DELETE", "/api/v1/agents/"+createResp.ID, nil)
			if err != nil {
				return err
			}
			if status != http.StatusOK && status != http.StatusNoContent {
				return fmt.Errorf("delete agent returned %d", status)
			}
			return nil
		},
	}
}

// GetUsageSummaryScenario creates a scenario that fetches usage data.
func GetUsageSummaryScenario(client *HTTPClient, pool *UserPool) Scenario {
	return Scenario{
		Name:   "get_usage_summary",
		Weight: 2,
		Fn: func(ctx context.Context, userID, iteration int) error {
			ac := AuthenticatedClientForUser(client, pool, userID)
			status, err := ac.DoAndClose(ctx, "GET", "/api/v1/billing/usage", nil)
			if err != nil {
				return err
			}
			// 200 OK or 503 (no store) are both acceptable in load test.
			if status != http.StatusOK && status != http.StatusServiceUnavailable {
				return fmt.Errorf("get usage returned %d", status)
			}
			return nil
		},
	}
}

// RateLimitStatusScenario creates a scenario that checks rate limit status.
func RateLimitStatusScenario(client *HTTPClient, pool *UserPool) Scenario {
	return Scenario{
		Name:   "rate_limit_status",
		Weight: 1,
		Fn: func(ctx context.Context, userID, iteration int) error {
			ac := AuthenticatedClientForUser(client, pool, userID)
			status, err := ac.DoAndClose(ctx, "GET", "/api/v1/rate-limit/status", nil)
			if err != nil {
				return err
			}
			if status != http.StatusOK && status != http.StatusUnauthorized {
				return fmt.Errorf("rate limit status returned %d", status)
			}
			return nil
		},
	}
}

// TokenRefreshScenario creates a scenario that refreshes JWT tokens.
func TokenRefreshScenario(client *HTTPClient, pool *UserPool) Scenario {
	return Scenario{
		Name:   "token_refresh",
		Weight: 1,
		Fn: func(ctx context.Context, userID, iteration int) error {
			ac := AuthenticatedClientForUser(client, pool, userID)
			// This will likely fail since we only have access tokens in the pool,
			// but it tests the endpoint under load.
			body := map[string]string{
				"refresh_token": "invalid-token-for-load-test",
			}
			_, err := ac.DoAndClose(ctx, "POST", "/api/v1/auth/refresh", body)
			// We just want to ensure the endpoint doesn't crash.
			return err
		},
	}
}

// MixedWorkloadScenarios returns a standard mix of scenarios for production load testing.
// It includes public endpoints, authenticated CRUD, and read-heavy patterns.
func MixedWorkloadScenarios(client *HTTPClient, pool *UserPool) []Scenario {
	return []Scenario{
		HealthCheckScenario(client),
		ReadinessCheckScenario(client),
		ListPlansScenario(client),
		ListAgentsScenario(client, pool),
		CreateAndDeleteAgentScenario(client, pool),
		GetUsageSummaryScenario(client, pool),
		RateLimitStatusScenario(client, pool),
	}
}
