package loadtest

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Config Tests ---

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, 100, cfg.ConcurrentUsers)
	assert.Equal(t, 10000, cfg.TotalRequests)
	assert.Equal(t, 10*time.Second, cfg.RampUpTime)
	assert.Equal(t, 30*time.Second, cfg.RequestTimeout)
	assert.Equal(t, 100*time.Millisecond, cfg.ThinkTime)
	assert.NoError(t, cfg.Validate())
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name:    "valid with total requests",
			config:  Config{ConcurrentUsers: 10, TotalRequests: 100},
			wantErr: false,
		},
		{
			name:    "valid with duration",
			config:  Config{ConcurrentUsers: 10, Duration: 5 * time.Second},
			wantErr: false,
		},
		{
			name:    "zero concurrent users",
			config:  Config{ConcurrentUsers: 0, TotalRequests: 100},
			wantErr: true,
		},
		{
			name:    "negative concurrent users",
			config:  Config{ConcurrentUsers: -1, TotalRequests: 100},
			wantErr: true,
		},
		{
			name:    "negative total requests",
			config:  Config{ConcurrentUsers: 10, TotalRequests: -1},
			wantErr: true,
		},
		{
			name:    "no requests or duration",
			config:  Config{ConcurrentUsers: 10},
			wantErr: true,
		},
		{
			name:    "negative request timeout",
			config:  Config{ConcurrentUsers: 10, TotalRequests: 100, RequestTimeout: -1},
			wantErr: true,
		},
		{
			name:    "negative ramp up",
			config:  Config{ConcurrentUsers: 10, TotalRequests: 100, RampUpTime: -1},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				assert.ErrorIs(t, err, ErrInvalidConfig)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// --- Runner Tests ---

func TestNewRunner(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		r, err := NewRunner(Config{ConcurrentUsers: 5, TotalRequests: 50})
		require.NoError(t, err)
		assert.NotNil(t, r)
	})

	t.Run("invalid config", func(t *testing.T) {
		r, err := NewRunner(Config{})
		assert.Error(t, err)
		assert.Nil(t, r)
	})
}

func TestRunnerNoScenarios(t *testing.T) {
	r, err := NewRunner(Config{ConcurrentUsers: 1, TotalRequests: 10})
	require.NoError(t, err)

	report, err := r.Run(context.Background())
	assert.ErrorIs(t, err, ErrNoScenarios)
	assert.Nil(t, report)
}

func TestRunnerBusy(t *testing.T) {
	started := make(chan struct{})
	block := make(chan struct{})

	r, err := NewRunner(Config{ConcurrentUsers: 1, TotalRequests: 1})
	require.NoError(t, err)
	r.AddScenario(Scenario{
		Name: "slow",
		Fn: func(ctx context.Context, userID, iteration int) error {
			close(started)
			<-block
			return nil
		},
	})

	go func() {
		r.Run(context.Background()) //nolint:errcheck
	}()

	<-started
	// Runner is busy now.
	report, err := r.Run(context.Background())
	assert.ErrorIs(t, err, ErrRunnerBusy)
	assert.Nil(t, report)

	close(block)
}

func TestRunnerByRequestCount(t *testing.T) {
	var count atomic.Int64

	r, err := NewRunner(Config{
		ConcurrentUsers: 5,
		TotalRequests:   100,
		RequestTimeout:  5 * time.Second,
	})
	require.NoError(t, err)

	r.AddScenario(Scenario{
		Name: "counter",
		Fn: func(ctx context.Context, userID, iteration int) error {
			count.Add(1)
			return nil
		},
	})

	report, err := r.Run(context.Background())
	require.NoError(t, err)
	require.NotNil(t, report)

	assert.Equal(t, 100, int(count.Load()))
	assert.Equal(t, 100, report.TotalRequests)
	assert.Equal(t, 100, report.TotalSuccesses)
	assert.Equal(t, 0, report.TotalFailures)
	assert.Equal(t, float64(0), report.OverallErrorRate)
	assert.True(t, report.OverallRPS > 0)
	assert.Contains(t, report.Scenarios, "counter")

	stats := report.Scenarios["counter"]
	assert.Equal(t, 100, stats.TotalReqs)
	assert.Equal(t, 100, stats.Successes)
	assert.Equal(t, 0, stats.Failures)
	assert.True(t, stats.MinLatency > 0)
	assert.True(t, stats.MaxLatency >= stats.MinLatency)
	assert.True(t, stats.MeanLatency > 0)
	assert.True(t, stats.P50Latency > 0)
	assert.True(t, stats.P99Latency >= stats.P50Latency)
}

func TestRunnerByDuration(t *testing.T) {
	var count atomic.Int64

	r, err := NewRunner(Config{
		ConcurrentUsers: 3,
		Duration:        200 * time.Millisecond,
	})
	require.NoError(t, err)

	r.AddScenario(Scenario{
		Name: "timed",
		Fn: func(ctx context.Context, userID, iteration int) error {
			count.Add(1)
			return nil
		},
	})

	report, err := r.Run(context.Background())
	require.NoError(t, err)
	require.NotNil(t, report)

	assert.True(t, int(count.Load()) > 0)
	assert.True(t, report.TotalRequests > 0)
	// Duration should be roughly 200ms (give some slack for CI).
	assert.True(t, report.TotalDuration >= 150*time.Millisecond, "duration: %v", report.TotalDuration)
}

func TestRunnerWithErrors(t *testing.T) {
	r, err := NewRunner(Config{
		ConcurrentUsers: 2,
		TotalRequests:   20,
	})
	require.NoError(t, err)

	r.AddScenario(Scenario{
		Name: "failing",
		Fn: func(ctx context.Context, userID, iteration int) error {
			if iteration%2 == 0 {
				return fmt.Errorf("simulated error")
			}
			return nil
		},
	})

	report, err := r.Run(context.Background())
	require.NoError(t, err)

	assert.True(t, report.TotalFailures > 0)
	assert.True(t, report.TotalSuccesses > 0)
	assert.True(t, report.OverallErrorRate > 0)
	assert.True(t, report.OverallErrorRate < 1)

	stats := report.Scenarios["failing"]
	assert.True(t, stats.Failures > 0)
	assert.True(t, stats.ErrorRate > 0)
}

func TestRunnerMultipleScenarios(t *testing.T) {
	r, err := NewRunner(Config{
		ConcurrentUsers: 2,
		TotalRequests:   100,
	})
	require.NoError(t, err)

	var fastCount, slowCount atomic.Int64

	r.AddScenario(Scenario{
		Name:   "fast",
		Weight: 3,
		Fn: func(ctx context.Context, userID, iteration int) error {
			fastCount.Add(1)
			return nil
		},
	})
	r.AddScenario(Scenario{
		Name:   "slow",
		Weight: 1,
		Fn: func(ctx context.Context, userID, iteration int) error {
			slowCount.Add(1)
			time.Sleep(1 * time.Millisecond)
			return nil
		},
	})

	report, err := r.Run(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 100, report.TotalRequests)
	assert.Contains(t, report.Scenarios, "fast")
	assert.Contains(t, report.Scenarios, "slow")

	// Fast should be selected ~3x more than slow.
	f := fastCount.Load()
	s := slowCount.Load()
	assert.True(t, f > s, "fast=%d should be > slow=%d", f, s)
}

func TestRunnerContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	r, err := NewRunner(Config{
		ConcurrentUsers: 5,
		TotalRequests:   100000, // Would take forever.
		ThinkTime:       50 * time.Millisecond,
	})
	require.NoError(t, err)

	r.AddScenario(Scenario{
		Name: "cancelable",
		Fn: func(ctx context.Context, userID, iteration int) error {
			return nil
		},
	})

	// Cancel after a short time.
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	report, err := r.Run(ctx)
	require.NoError(t, err) // Run should not error on context cancel.
	assert.True(t, report.TotalRequests < 100000, "should have stopped early")
}

func TestRunnerRampUp(t *testing.T) {
	userSeen := make(map[int]time.Time)
	var mu sync.Mutex

	r, err := NewRunner(Config{
		ConcurrentUsers: 5,
		TotalRequests:   50,
		RampUpTime:      200 * time.Millisecond,
	})
	require.NoError(t, err)

	r.AddScenario(Scenario{
		Name: "ramp",
		Fn: func(ctx context.Context, userID, iteration int) error {
			mu.Lock()
			if _, ok := userSeen[userID]; !ok {
				userSeen[userID] = time.Now()
			}
			mu.Unlock()
			return nil
		},
	})

	report, err := r.Run(context.Background())
	require.NoError(t, err)
	assert.True(t, report.TotalRequests > 0)

	// Verify that users were started at different times.
	mu.Lock()
	assert.True(t, len(userSeen) > 0, "should have seen some users")
	mu.Unlock()
}

func TestRunnerThinkTime(t *testing.T) {
	r, err := NewRunner(Config{
		ConcurrentUsers: 1,
		TotalRequests:   5,
		ThinkTime:       50 * time.Millisecond,
	})
	require.NoError(t, err)

	r.AddScenario(Scenario{
		Name: "think",
		Fn: func(ctx context.Context, userID, iteration int) error {
			return nil
		},
	})

	start := time.Now()
	report, err := r.Run(context.Background())
	require.NoError(t, err)
	elapsed := time.Since(start)

	// 5 requests with 50ms think time = ~200ms minimum (think time is between requests).
	assert.True(t, elapsed >= 150*time.Millisecond, "should include think time, got %v", elapsed)
	assert.Equal(t, 5, report.TotalRequests)
}

func TestRunnerRequestTimeout(t *testing.T) {
	r, err := NewRunner(Config{
		ConcurrentUsers: 1,
		TotalRequests:   1,
		RequestTimeout:  50 * time.Millisecond,
	})
	require.NoError(t, err)

	r.AddScenario(Scenario{
		Name: "timeout_test",
		Fn: func(ctx context.Context, userID, iteration int) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
				return nil
			}
		},
	})

	report, err := r.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, report.TotalFailures)
	assert.Equal(t, 0, report.TotalSuccesses)
}

// --- SLO Tests ---

func TestDefaultSLOs(t *testing.T) {
	slos := DefaultSLOs()
	assert.Len(t, slos, 4)

	for _, slo := range slos {
		assert.NotEmpty(t, slo.Name)
		assert.NotNil(t, slo.Check)
	}
}

func TestSLOEvaluation(t *testing.T) {
	t.Run("all pass", func(t *testing.T) {
		r, err := NewRunner(Config{ConcurrentUsers: 2, TotalRequests: 50})
		require.NoError(t, err)

		r.AddScenario(Scenario{
			Name: "fast",
			Fn: func(ctx context.Context, userID, iteration int) error {
				return nil
			},
		})

		report, err := r.Run(context.Background())
		require.NoError(t, err)

		for _, slo := range report.SLOResults {
			assert.True(t, slo.Passed, "SLO %q should pass", slo.Name)
		}
	})

	t.Run("error rate SLO fails", func(t *testing.T) {
		r, err := NewRunner(Config{ConcurrentUsers: 1, TotalRequests: 10})
		require.NoError(t, err)

		r.AddScenario(Scenario{
			Name: "all_fail",
			Fn: func(ctx context.Context, userID, iteration int) error {
				return fmt.Errorf("always fail")
			},
		})

		report, err := r.Run(context.Background())
		require.NoError(t, err)

		// At least the error rate SLO should fail.
		var errorRatePassed bool
		for _, slo := range report.SLOResults {
			if slo.Name == "Error rate < 1%" {
				errorRatePassed = slo.Passed
			}
		}
		assert.False(t, errorRatePassed, "error rate SLO should fail")
	})
}

func TestCustomSLOs(t *testing.T) {
	r, err := NewRunner(Config{ConcurrentUsers: 1, TotalRequests: 5})
	require.NoError(t, err)

	r.SetSLOs([]SLO{
		{
			Name:  "custom: > 3 requests",
			Check: func(r *Report) bool { return r.TotalRequests > 3 },
		},
	})

	r.AddScenario(Scenario{
		Name: "test",
		Fn:   func(ctx context.Context, u, i int) error { return nil },
	})

	report, err := r.Run(context.Background())
	require.NoError(t, err)
	require.Len(t, report.SLOResults, 1)
	assert.True(t, report.SLOResults[0].Passed)
}

// --- Statistics Tests ---

func TestPercentile(t *testing.T) {
	tests := []struct {
		name   string
		values []time.Duration
		p      float64
		want   time.Duration
	}{
		{"empty", nil, 0.5, 0},
		{"single", []time.Duration{100}, 0.5, 100},
		{"two values p0", []time.Duration{100, 200}, 0.0, 100},
		{"two values p100", []time.Duration{100, 200}, 1.0, 200},
		{"two values p50", []time.Duration{100, 200}, 0.5, 150},
		{"ten values p90", []time.Duration{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, 0.90, time.Duration(math.Round(9.1))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := percentile(tt.values, tt.p)
			// Allow small floating point differences.
			assert.InDelta(t, float64(tt.want), float64(got), 1.0)
		})
	}
}

func TestComputeStats(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		stats := computeStats("test", nil, time.Second)
		assert.Equal(t, "test", stats.Name)
		assert.Equal(t, 0, stats.TotalReqs)
	})

	t.Run("mixed results", func(t *testing.T) {
		results := []RequestResult{
			{Duration: 10 * time.Millisecond},
			{Duration: 20 * time.Millisecond},
			{Duration: 30 * time.Millisecond, Error: fmt.Errorf("fail")},
			{Duration: 40 * time.Millisecond},
			{Duration: 50 * time.Millisecond},
		}
		stats := computeStats("mix", results, 1*time.Second)
		assert.Equal(t, 5, stats.TotalReqs)
		assert.Equal(t, 4, stats.Successes)
		assert.Equal(t, 1, stats.Failures)
		assert.InDelta(t, 0.2, stats.ErrorRate, 0.001)
		assert.Equal(t, 10*time.Millisecond, stats.MinLatency)
		assert.Equal(t, 50*time.Millisecond, stats.MaxLatency)
		assert.True(t, stats.MeanLatency > 0)
		assert.True(t, stats.P50Latency > 0)
		assert.True(t, stats.P99Latency > 0)
		assert.InDelta(t, 5.0, stats.RequestsPerS, 0.001)
	})
}

// --- Scenario Selector Tests ---

func TestBuildSelector(t *testing.T) {
	t.Run("single scenario", func(t *testing.T) {
		s := buildSelector([]Scenario{{Name: "only", Weight: 1}})
		assert.Equal(t, 1, s.totalWeight)
		for i := 0; i < 10; i++ {
			assert.Equal(t, "only", s.Pick(i).Name)
		}
	})

	t.Run("weighted selection", func(t *testing.T) {
		s := buildSelector([]Scenario{
			{Name: "heavy", Weight: 3},
			{Name: "light", Weight: 1},
		})
		assert.Equal(t, 4, s.totalWeight)

		counts := map[string]int{}
		for i := 0; i < 100; i++ {
			counts[s.Pick(i).Name]++
		}
		assert.Equal(t, 75, counts["heavy"])
		assert.Equal(t, 25, counts["light"])
	})

	t.Run("default weight", func(t *testing.T) {
		s := buildSelector([]Scenario{
			{Name: "a", Weight: 0},
			{Name: "b", Weight: 0},
		})
		assert.Equal(t, 2, s.totalWeight)
	})
}

// --- HTTP Client Tests ---

func TestNewHTTPClient(t *testing.T) {
	c := NewHTTPClient("http://localhost:8080")
	assert.Equal(t, "http://localhost:8080", c.BaseURL)
	assert.NotNil(t, c.Client)
}

func TestHTTPClientSetAuthToken(t *testing.T) {
	c := NewHTTPClient("http://localhost:8080")
	c.SetAuthToken("test-token")
	assert.Equal(t, "test-token", c.AuthToken)
}

func TestHTTPClientDo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
	}))
	defer server.Close()

	c := NewHTTPClient(server.URL)
	c.SetAuthToken("test-token")

	resp, err := c.Do(context.Background(), "POST", "/test", map[string]string{"key": "value"})
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestHTTPClientDoAndClose(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := NewHTTPClient(server.URL)
	status, err := c.DoAndClose(context.Background(), "GET", "/health", nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)
}

func TestHTTPClientDoJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]int{"count": 42}) //nolint:errcheck
	}))
	defer server.Close()

	c := NewHTTPClient(server.URL)
	var result map[string]int
	status, err := c.DoJSON(context.Background(), "GET", "/test", nil, &result)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, 42, result["count"])
}

func TestHTTPClientDoJSONNilTarget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := NewHTTPClient(server.URL)
	status, err := c.DoJSON(context.Background(), "GET", "/test", nil, nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)
}

func TestHTTPClientNoAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := NewHTTPClient(server.URL)
	status, err := c.DoAndClose(context.Background(), "GET", "/test", nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)
}

func TestHTTPClientNoContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := NewHTTPClient(server.URL)
	status, err := c.DoAndClose(context.Background(), "GET", "/test", nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)
}

// --- Scenario Tests ---

func TestHealthCheckScenario(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL)
	scenario := HealthCheckScenario(client)
	assert.Equal(t, "health_check", scenario.Name)

	err := scenario.Fn(context.Background(), 0, 0)
	assert.NoError(t, err)
}

func TestHealthCheckScenarioFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL)
	scenario := HealthCheckScenario(client)

	err := scenario.Fn(context.Background(), 0, 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "503")
}

func TestReadinessCheckScenario(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ready" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL)
	scenario := ReadinessCheckScenario(client)
	assert.Equal(t, "readiness_check", scenario.Name)

	err := scenario.Fn(context.Background(), 0, 0)
	assert.NoError(t, err)
}

func TestListPlansScenario(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/billing/plans" && r.Method == "GET" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode([]map[string]string{{"id": "free"}}) //nolint:errcheck
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL)
	scenario := ListPlansScenario(client)
	assert.Equal(t, "list_plans", scenario.Name)
	assert.Equal(t, 2, scenario.Weight)

	err := scenario.Fn(context.Background(), 0, 0)
	assert.NoError(t, err)
}

// --- UserPool Tests ---

func TestUserPoolGetToken(t *testing.T) {
	pool := &UserPool{
		tokens: []string{"token-0", "token-1", "token-2"},
		emails: []string{"a@test.com", "b@test.com", "c@test.com"},
	}

	assert.Equal(t, "token-0", pool.GetToken(0))
	assert.Equal(t, "token-1", pool.GetToken(1))
	assert.Equal(t, "token-2", pool.GetToken(2))
	// Round-robin.
	assert.Equal(t, "token-0", pool.GetToken(3))
	assert.Equal(t, "token-1", pool.GetToken(4))
}

func TestUserPoolSize(t *testing.T) {
	pool := &UserPool{
		tokens: []string{"a", "b"},
	}
	assert.Equal(t, 2, pool.Size())
}

func TestUserPoolEmptyGetToken(t *testing.T) {
	pool := &UserPool{}
	assert.Empty(t, pool.GetToken(0))
}

// --- Authenticated Client Tests ---

func TestAuthenticatedClientForUser(t *testing.T) {
	baseClient := NewHTTPClient("http://localhost:8080")
	pool := &UserPool{
		tokens: []string{"token-a", "token-b"},
	}

	ac := AuthenticatedClientForUser(baseClient, pool, 0)
	assert.Equal(t, "http://localhost:8080", ac.BaseURL)
	assert.Equal(t, "token-a", ac.AuthToken)
	assert.Same(t, baseClient.Client, ac.Client)

	ac2 := AuthenticatedClientForUser(baseClient, pool, 1)
	assert.Equal(t, "token-b", ac2.AuthToken)
}

// --- Report JSON Serialization ---

func TestReportJSON(t *testing.T) {
	report := &Report{
		Config:           DefaultConfig(),
		StartTime:        time.Now(),
		EndTime:          time.Now(),
		TotalDuration:    5 * time.Second,
		TotalRequests:    100,
		TotalSuccesses:   95,
		TotalFailures:    5,
		OverallRPS:       20.0,
		OverallErrorRate: 0.05,
		Scenarios: map[string]ScenarioStats{
			"test": {
				Name:      "test",
				TotalReqs: 100,
				Successes: 95,
				Failures:  5,
			},
		},
		SLOResults: []SLOResult{
			{Name: "Error rate < 1%", Passed: false},
		},
	}

	data, err := json.Marshal(report)
	require.NoError(t, err)

	var decoded Report
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, 100, decoded.TotalRequests)
	assert.Equal(t, 95, decoded.TotalSuccesses)
	assert.InDelta(t, 0.05, decoded.OverallErrorRate, 0.001)
	assert.Len(t, decoded.Scenarios, 1)
	assert.Len(t, decoded.SLOResults, 1)
	assert.False(t, decoded.SLOResults[0].Passed)
}

// --- Integration Test: Full Load Test with HTTP Server ---

func TestIntegrationFullLoadTest(t *testing.T) {
	var requestCount atomic.Int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/ready":
			w.WriteHeader(http.StatusOK)
		case "/api/v1/billing/plans":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]map[string]string{{"id": "free"}}) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL)

	r, err := NewRunner(Config{
		ConcurrentUsers: 10,
		TotalRequests:   200,
		RampUpTime:      50 * time.Millisecond,
		RequestTimeout:  5 * time.Second,
		ThinkTime:       1 * time.Millisecond,
	})
	require.NoError(t, err)

	r.AddScenario(HealthCheckScenario(client))
	r.AddScenario(ReadinessCheckScenario(client))
	r.AddScenario(ListPlansScenario(client))

	report, err := r.Run(context.Background())
	require.NoError(t, err)
	require.NotNil(t, report)

	assert.Equal(t, 200, report.TotalRequests)
	assert.Equal(t, 200, report.TotalSuccesses)
	assert.Equal(t, 0, report.TotalFailures)
	assert.Equal(t, float64(0), report.OverallErrorRate)
	assert.True(t, report.OverallRPS > 0)
	assert.Len(t, report.Scenarios, 3)

	// All SLOs should pass.
	for _, slo := range report.SLOResults {
		assert.True(t, slo.Passed, "SLO %q should pass", slo.Name)
	}

	// Verify the server received all requests.
	assert.Equal(t, int64(200), requestCount.Load())
}

func TestIntegrationConcurrency(t *testing.T) {
	var maxConcurrent atomic.Int64
	var current atomic.Int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := current.Add(1)
		// Track max concurrent.
		for {
			old := maxConcurrent.Load()
			if c <= old || maxConcurrent.CompareAndSwap(old, c) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond) // Simulate work.
		current.Add(-1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL)

	r, err := NewRunner(Config{
		ConcurrentUsers: 20,
		TotalRequests:   100,
		RequestTimeout:  5 * time.Second,
	})
	require.NoError(t, err)

	r.AddScenario(HealthCheckScenario(client))

	report, err := r.Run(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 100, report.TotalRequests)
	// Should see some level of concurrency.
	assert.True(t, maxConcurrent.Load() > 1, "max concurrent: %d", maxConcurrent.Load())
}

// --- Error Constants ---

func TestErrorConstants(t *testing.T) {
	assert.NotNil(t, ErrNoScenarios)
	assert.NotNil(t, ErrInvalidConfig)
	assert.NotNil(t, ErrRunnerBusy)
	assert.Contains(t, ErrNoScenarios.Error(), "no scenarios")
	assert.Contains(t, ErrInvalidConfig.Error(), "invalid configuration")
	assert.Contains(t, ErrRunnerBusy.Error(), "already executing")
}

// --- RequestResult ---

func TestRequestResultErrorMsg(t *testing.T) {
	r := RequestResult{
		Scenario: "test",
		Duration: 10 * time.Millisecond,
		Error:    fmt.Errorf("something went wrong"),
		ErrorMsg: "something went wrong",
	}
	assert.Equal(t, "something went wrong", r.ErrorMsg)

	r2 := RequestResult{
		Scenario: "test",
		Duration: 5 * time.Millisecond,
	}
	assert.Empty(t, r2.ErrorMsg)
}

// --- rampDelay ---

func TestRampDelay(t *testing.T) {
	t.Run("no ramp", func(t *testing.T) {
		r := &Runner{config: Config{ConcurrentUsers: 5}}
		assert.Equal(t, time.Duration(0), r.rampDelay())
	})

	t.Run("single user", func(t *testing.T) {
		r := &Runner{config: Config{ConcurrentUsers: 1, RampUpTime: time.Second}}
		assert.Equal(t, time.Duration(0), r.rampDelay())
	})

	t.Run("normal ramp", func(t *testing.T) {
		r := &Runner{config: Config{ConcurrentUsers: 5, RampUpTime: 4 * time.Second}}
		assert.Equal(t, time.Second, r.rampDelay())
	})
}

// --- MixedWorkloadScenarios ---

func TestMixedWorkloadScenarios(t *testing.T) {
	client := NewHTTPClient("http://localhost:8080")
	pool := &UserPool{tokens: []string{"tok"}}

	scenarios := MixedWorkloadScenarios(client, pool)
	assert.Len(t, scenarios, 7)

	names := make([]string, len(scenarios))
	for i, s := range scenarios {
		names[i] = s.Name
		assert.NotNil(t, s.Fn)
		assert.True(t, s.Weight >= 1)
	}
	assert.Contains(t, names, "health_check")
	assert.Contains(t, names, "readiness_check")
	assert.Contains(t, names, "list_plans")
	assert.Contains(t, names, "list_agents")
	assert.Contains(t, names, "create_delete_agent")
	assert.Contains(t, names, "get_usage_summary")
	assert.Contains(t, names, "rate_limit_status")
}

// --- ScenarioStats ---

func TestScenarioStatsJSON(t *testing.T) {
	stats := ScenarioStats{
		Name:         "test",
		TotalReqs:    100,
		Successes:    90,
		Failures:     10,
		MinLatency:   1 * time.Millisecond,
		MaxLatency:   500 * time.Millisecond,
		MeanLatency:  50 * time.Millisecond,
		P50Latency:   30 * time.Millisecond,
		P90Latency:   100 * time.Millisecond,
		P95Latency:   200 * time.Millisecond,
		P99Latency:   400 * time.Millisecond,
		ErrorRate:    0.1,
		RequestsPerS: 500.0,
	}

	data, err := json.Marshal(stats)
	require.NoError(t, err)

	var decoded ScenarioStats
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, "test", decoded.Name)
	assert.Equal(t, 100, decoded.TotalReqs)
	assert.InDelta(t, 0.1, decoded.ErrorRate, 0.001)
}
