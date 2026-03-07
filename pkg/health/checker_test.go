package health

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// --- Checker tests ---

func TestNewChecker(t *testing.T) {
	c := NewChecker()
	require.NotNil(t, c)
	assert.Equal(t, 0, c.ComponentCount())
	assert.True(t, c.Uptime() >= 0)
}

func TestRegister(t *testing.T) {
	c := NewChecker()

	err := c.Register(ComponentConfig{
		Name:      "test",
		Type:      TypeInternal,
		CheckFunc: func(ctx context.Context) CheckResult { return CheckResult{Status: StatusHealthy} },
	})
	require.NoError(t, err)
	assert.Equal(t, 1, c.ComponentCount())
}

func TestRegisterEmptyName(t *testing.T) {
	c := NewChecker()
	err := c.Register(ComponentConfig{
		CheckFunc: func(ctx context.Context) CheckResult { return CheckResult{Status: StatusHealthy} },
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "name is required")
}

func TestRegisterNilFunc(t *testing.T) {
	c := NewChecker()
	err := c.Register(ComponentConfig{Name: "test"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "check function is required")
}

func TestRegisterDuplicate(t *testing.T) {
	c := NewChecker()
	cfg := ComponentConfig{
		Name:      "test",
		CheckFunc: func(ctx context.Context) CheckResult { return CheckResult{Status: StatusHealthy} },
	}
	require.NoError(t, c.Register(cfg))
	err := c.Register(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}

func TestRegisterDefaults(t *testing.T) {
	c := NewChecker()
	err := c.Register(ComponentConfig{
		Name:      "test",
		CheckFunc: func(ctx context.Context) CheckResult { return CheckResult{Status: StatusHealthy} },
	})
	require.NoError(t, err)

	c.mu.RLock()
	cs := c.components["test"]
	c.mu.RUnlock()

	assert.Equal(t, DefaultTimeout, cs.config.Timeout)
	assert.Equal(t, DefaultInterval, cs.config.Interval)
}

func TestDeregister(t *testing.T) {
	c := NewChecker()
	c.Register(ComponentConfig{
		Name:      "test",
		CheckFunc: func(ctx context.Context) CheckResult { return CheckResult{Status: StatusHealthy} },
	})
	assert.Equal(t, 1, c.ComponentCount())

	c.Deregister("test")
	assert.Equal(t, 0, c.ComponentCount())

	// Deregister non-existent is a no-op.
	c.Deregister("nonexistent")
}

func TestCheckComponent(t *testing.T) {
	c := NewChecker()
	c.Register(ComponentConfig{
		Name: "db",
		Type: TypeDatabase,
		CheckFunc: func(ctx context.Context) CheckResult {
			return CheckResult{Status: StatusHealthy, Message: "ok"}
		},
	})

	result, err := c.CheckComponent("db")
	require.NoError(t, err)
	assert.Equal(t, StatusHealthy, result.Status)
	assert.Equal(t, "ok", result.Message)
	assert.True(t, result.Duration > 0)
	assert.False(t, result.Timestamp.IsZero())
}

func TestCheckComponentNotFound(t *testing.T) {
	c := NewChecker()
	_, err := c.CheckComponent("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestCheckComponentTimeout(t *testing.T) {
	c := NewChecker()
	c.Register(ComponentConfig{
		Name:    "slow",
		Timeout: 50 * time.Millisecond,
		CheckFunc: func(ctx context.Context) CheckResult {
			select {
			case <-ctx.Done():
				return CheckResult{Status: StatusUnhealthy, Message: "context canceled"}
			case <-time.After(5 * time.Second):
				return CheckResult{Status: StatusHealthy}
			}
		},
	})

	result, err := c.CheckComponent("slow")
	require.NoError(t, err)
	assert.Equal(t, StatusUnhealthy, result.Status)
	assert.Contains(t, result.Message, "timed out")
}

func TestCheckAll(t *testing.T) {
	c := NewChecker()
	c.Register(ComponentConfig{
		Name:      "a",
		CheckFunc: func(ctx context.Context) CheckResult { return CheckResult{Status: StatusHealthy, Message: "a ok"} },
	})
	c.Register(ComponentConfig{
		Name:      "b",
		CheckFunc: func(ctx context.Context) CheckResult { return CheckResult{Status: StatusDegraded, Message: "b slow"} },
	})
	c.Register(ComponentConfig{
		Name:      "c",
		CheckFunc: func(ctx context.Context) CheckResult { return CheckResult{Status: StatusUnhealthy, Message: "c down"} },
	})

	results := c.CheckAll()
	assert.Len(t, results, 3)
	assert.Equal(t, StatusHealthy, results["a"].Status)
	assert.Equal(t, StatusDegraded, results["b"].Status)
	assert.Equal(t, StatusUnhealthy, results["c"].Status)
}

func TestCheckAllConcurrency(t *testing.T) {
	c := NewChecker()
	var running atomic.Int32

	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("check_%d", i)
		c.Register(ComponentConfig{
			Name: name,
			CheckFunc: func(ctx context.Context) CheckResult {
				running.Add(1)
				time.Sleep(20 * time.Millisecond)
				running.Add(-1)
				return CheckResult{Status: StatusHealthy}
			},
		})
	}

	results := c.CheckAll()
	assert.Len(t, results, 5)
}

func TestCheckCached(t *testing.T) {
	c := NewChecker()
	var callCount atomic.Int32

	c.Register(ComponentConfig{
		Name:     "cached",
		Interval: 1 * time.Hour, // Long interval = should cache.
		CheckFunc: func(ctx context.Context) CheckResult {
			callCount.Add(1)
			return CheckResult{Status: StatusHealthy, Message: "ok"}
		},
	})

	// First call should run the check.
	results := c.CheckCached()
	assert.Equal(t, int32(1), callCount.Load())
	assert.Equal(t, StatusHealthy, results["cached"].Status)

	// Second call should use cache.
	results = c.CheckCached()
	assert.Equal(t, int32(1), callCount.Load())
	assert.Equal(t, StatusHealthy, results["cached"].Status)
}

func TestCheckCachedStale(t *testing.T) {
	c := NewChecker()
	var callCount atomic.Int32

	c.Register(ComponentConfig{
		Name:     "stale",
		Interval: 1 * time.Millisecond, // Very short interval.
		CheckFunc: func(ctx context.Context) CheckResult {
			callCount.Add(1)
			return CheckResult{Status: StatusHealthy}
		},
	})

	c.CheckCached()
	time.Sleep(5 * time.Millisecond)
	c.CheckCached()
	assert.GreaterOrEqual(t, callCount.Load(), int32(2))
}

func TestIsReady_NoCritical(t *testing.T) {
	c := NewChecker()
	c.Register(ComponentConfig{
		Name:      "optional",
		Critical:  false,
		CheckFunc: func(ctx context.Context) CheckResult { return CheckResult{Status: StatusUnhealthy} },
	})
	assert.True(t, c.IsReady(), "non-critical failure should not affect readiness")
}

func TestIsReady_CriticalHealthy(t *testing.T) {
	c := NewChecker()
	c.Register(ComponentConfig{
		Name:      "db",
		Critical:  true,
		CheckFunc: func(ctx context.Context) CheckResult { return CheckResult{Status: StatusHealthy} },
	})
	assert.True(t, c.IsReady())
}

func TestIsReady_CriticalDegraded(t *testing.T) {
	c := NewChecker()
	c.Register(ComponentConfig{
		Name:      "db",
		Critical:  true,
		CheckFunc: func(ctx context.Context) CheckResult { return CheckResult{Status: StatusDegraded} },
	})
	assert.True(t, c.IsReady(), "degraded critical component should still be ready")
}

func TestIsReady_CriticalUnhealthy(t *testing.T) {
	c := NewChecker()
	c.Register(ComponentConfig{
		Name:      "db",
		Critical:  true,
		CheckFunc: func(ctx context.Context) CheckResult { return CheckResult{Status: StatusUnhealthy} },
	})
	assert.False(t, c.IsReady())
}

func TestIsReady_Empty(t *testing.T) {
	c := NewChecker()
	assert.True(t, c.IsReady(), "no components means ready")
}

func TestIsHealthy(t *testing.T) {
	c := NewChecker()
	c.Register(ComponentConfig{
		Name:      "a",
		CheckFunc: func(ctx context.Context) CheckResult { return CheckResult{Status: StatusHealthy} },
	})
	assert.True(t, c.IsHealthy())
}

func TestIsHealthyFalse(t *testing.T) {
	c := NewChecker()
	c.Register(ComponentConfig{
		Name:      "a",
		CheckFunc: func(ctx context.Context) CheckResult { return CheckResult{Status: StatusDegraded} },
	})
	assert.False(t, c.IsHealthy())
}

func TestComponentNames(t *testing.T) {
	c := NewChecker()
	c.Register(ComponentConfig{
		Name:      "b",
		CheckFunc: func(ctx context.Context) CheckResult { return CheckResult{Status: StatusHealthy} },
	})
	c.Register(ComponentConfig{
		Name:      "a",
		CheckFunc: func(ctx context.Context) CheckResult { return CheckResult{Status: StatusHealthy} },
	})

	names := c.ComponentNames()
	sort.Strings(names)
	assert.Equal(t, []string{"a", "b"}, names)
}

// --- OverallStatus tests ---

func TestOverallStatus(t *testing.T) {
	tests := []struct {
		name     string
		results  map[string]CheckResult
		expected ComponentStatus
	}{
		{"empty", map[string]CheckResult{}, StatusHealthy},
		{"all healthy", map[string]CheckResult{
			"a": {Status: StatusHealthy},
			"b": {Status: StatusHealthy},
		}, StatusHealthy},
		{"one degraded", map[string]CheckResult{
			"a": {Status: StatusHealthy},
			"b": {Status: StatusDegraded},
		}, StatusDegraded},
		{"one unhealthy", map[string]CheckResult{
			"a": {Status: StatusHealthy},
			"b": {Status: StatusUnhealthy},
		}, StatusUnhealthy},
		{"unknown is unhealthy", map[string]CheckResult{
			"a": {Status: StatusUnknown},
		}, StatusUnhealthy},
		{"mixed degraded and unhealthy", map[string]CheckResult{
			"a": {Status: StatusDegraded},
			"b": {Status: StatusUnhealthy},
		}, StatusUnhealthy},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, OverallStatus(tt.results))
		})
	}
}

// --- Built-in check function tests ---

func TestSQLiteCheck(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	check := SQLiteCheck(db)
	result := check(context.Background())
	assert.Equal(t, StatusHealthy, result.Status)
	assert.NotNil(t, result.Details)
	assert.Contains(t, result.Details, "journal_mode")
}

func TestSQLiteCheckNilDB(t *testing.T) {
	check := SQLiteCheck(nil)
	result := check(context.Background())
	assert.Equal(t, StatusUnhealthy, result.Status)
	assert.Contains(t, result.Message, "nil")
}

func TestPostgreSQLCheckNilDB(t *testing.T) {
	check := PostgreSQLCheck(nil)
	result := check(context.Background())
	assert.Equal(t, StatusUnhealthy, result.Status)
	assert.Contains(t, result.Message, "nil")
}

type fakePinger struct {
	err error
}

func (f *fakePinger) Ping() error { return f.err }

func TestPingerCheck(t *testing.T) {
	check := PingerCheck("redis", &fakePinger{})
	result := check(context.Background())
	assert.Equal(t, StatusHealthy, result.Status)
}

func TestPingerCheckFail(t *testing.T) {
	check := PingerCheck("redis", &fakePinger{err: fmt.Errorf("connection refused")})
	result := check(context.Background())
	assert.Equal(t, StatusUnhealthy, result.Status)
	assert.Contains(t, result.Message, "connection refused")
}

func TestPingerCheckNil(t *testing.T) {
	check := PingerCheck("redis", nil)
	result := check(context.Background())
	assert.Equal(t, StatusUnhealthy, result.Status)
}

type fakeNATS struct {
	connected bool
}

func (f *fakeNATS) IsConnected() bool { return f.connected }

func TestNATSCheck(t *testing.T) {
	check := NATSCheck(&fakeNATS{connected: true})
	result := check(context.Background())
	assert.Equal(t, StatusHealthy, result.Status)
}

func TestNATSCheckDisconnected(t *testing.T) {
	check := NATSCheck(&fakeNATS{connected: false})
	result := check(context.Background())
	assert.Equal(t, StatusUnhealthy, result.Status)
}

func TestNATSCheckNil(t *testing.T) {
	check := NATSCheck(nil)
	result := check(context.Background())
	assert.Equal(t, StatusUnhealthy, result.Status)
}

func TestCustomCheck(t *testing.T) {
	check := CustomCheck(func() (bool, string) { return true, "all good" })
	result := check(context.Background())
	assert.Equal(t, StatusHealthy, result.Status)
	assert.Equal(t, "all good", result.Message)
}

func TestCustomCheckFail(t *testing.T) {
	check := CustomCheck(func() (bool, string) { return false, "broken" })
	result := check(context.Background())
	assert.Equal(t, StatusUnhealthy, result.Status)
}

func TestCompositeCheck(t *testing.T) {
	check := CompositeCheck(map[string]CheckerFunc{
		"a": func(ctx context.Context) CheckResult { return CheckResult{Status: StatusHealthy, Message: "ok"} },
		"b": func(ctx context.Context) CheckResult { return CheckResult{Status: StatusDegraded, Message: "slow"} },
	})

	result := check(context.Background())
	assert.Equal(t, StatusDegraded, result.Status)
	assert.Contains(t, result.Message, "failed")
	assert.NotNil(t, result.Details)
}

func TestCompositeCheckAllHealthy(t *testing.T) {
	check := CompositeCheck(map[string]CheckerFunc{
		"a": func(ctx context.Context) CheckResult { return CheckResult{Status: StatusHealthy} },
		"b": func(ctx context.Context) CheckResult { return CheckResult{Status: StatusHealthy} },
	})

	result := check(context.Background())
	assert.Equal(t, StatusHealthy, result.Status)
	assert.Contains(t, result.Message, "passed")
}

func TestTimeoutCheck(t *testing.T) {
	check := TimeoutCheck(50*time.Millisecond, func(ctx context.Context) CheckResult {
		select {
		case <-ctx.Done():
			return CheckResult{Status: StatusUnhealthy, Message: "canceled"}
		case <-time.After(5 * time.Second):
			return CheckResult{Status: StatusHealthy}
		}
	})

	result := check(context.Background())
	assert.Equal(t, StatusUnhealthy, result.Status)
	assert.Contains(t, result.Message, "timed out")
}

func TestTimeoutCheckFast(t *testing.T) {
	check := TimeoutCheck(5*time.Second, func(ctx context.Context) CheckResult {
		return CheckResult{Status: StatusHealthy, Message: "fast"}
	})

	result := check(context.Background())
	assert.Equal(t, StatusHealthy, result.Status)
}

// --- Handler tests ---

func TestDetailedHandler(t *testing.T) {
	c := NewChecker()
	c.Register(ComponentConfig{
		Name:     "db",
		Type:     TypeDatabase,
		Critical: true,
		CheckFunc: func(ctx context.Context) CheckResult {
			return CheckResult{Status: StatusHealthy, Message: "ok"}
		},
	})
	c.Register(ComponentConfig{
		Name:     "cache",
		Type:     TypeCache,
		Critical: false,
		CheckFunc: func(ctx context.Context) CheckResult {
			return CheckResult{Status: StatusDegraded, Message: "slow"}
		},
	})

	req := httptest.NewRequest("GET", "/health/detailed", nil)
	w := httptest.NewRecorder()
	c.DetailedHandler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code) // degraded but not unhealthy.

	var resp DetailedResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, StatusDegraded, resp.Status)
	assert.Len(t, resp.Components, 2)
	assert.Equal(t, 2, resp.Summary.Total)
	assert.Equal(t, 1, resp.Summary.Healthy)
	assert.Equal(t, 1, resp.Summary.Degraded)
	assert.Equal(t, 0, resp.Summary.Unhealthy)
	assert.True(t, resp.Components["db"].Critical)
	assert.False(t, resp.Components["cache"].Critical)
}

func TestDetailedHandlerUnhealthy(t *testing.T) {
	c := NewChecker()
	c.Register(ComponentConfig{
		Name:      "broken",
		CheckFunc: func(ctx context.Context) CheckResult { return CheckResult{Status: StatusUnhealthy} },
	})

	req := httptest.NewRequest("GET", "/health/detailed", nil)
	w := httptest.NewRecorder()
	c.DetailedHandler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestDetailedHandlerEmpty(t *testing.T) {
	c := NewChecker()

	req := httptest.NewRequest("GET", "/health/detailed", nil)
	w := httptest.NewRecorder()
	c.DetailedHandler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp DetailedResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, StatusHealthy, resp.Status)
	assert.Equal(t, 0, resp.Summary.Total)
}

func TestLiveHandler(t *testing.T) {
	c := NewChecker()

	req := httptest.NewRequest("GET", "/health/live", nil)
	w := httptest.NewRecorder()
	c.LiveHandler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "alive", resp["status"])
}

func TestReadyHandler(t *testing.T) {
	c := NewChecker()
	c.Register(ComponentConfig{
		Name:      "db",
		Critical:  true,
		CheckFunc: func(ctx context.Context) CheckResult { return CheckResult{Status: StatusHealthy} },
	})

	req := httptest.NewRequest("GET", "/health/ready", nil)
	w := httptest.NewRecorder()
	c.ReadyHandler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "ready", resp["status"])
}

func TestReadyHandlerNotReady(t *testing.T) {
	c := NewChecker()
	c.Register(ComponentConfig{
		Name:      "db",
		Critical:  true,
		CheckFunc: func(ctx context.Context) CheckResult { return CheckResult{Status: StatusUnhealthy} },
	})

	req := httptest.NewRequest("GET", "/health/ready", nil)
	w := httptest.NewRecorder()
	c.ReadyHandler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "not_ready", resp["status"])
	failed := resp["failed_components"].([]any)
	assert.Contains(t, failed, "db")
}

func TestComponentHandler(t *testing.T) {
	c := NewChecker()
	c.Register(ComponentConfig{
		Name:      "redis",
		CheckFunc: func(ctx context.Context) CheckResult { return CheckResult{Status: StatusHealthy, Message: "ok"} },
	})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/component/{name}", c.ComponentHandler())

	req := httptest.NewRequest("GET", "/health/component/redis", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestComponentHandlerNotFound(t *testing.T) {
	c := NewChecker()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/component/{name}", c.ComponentHandler())

	req := httptest.NewRequest("GET", "/health/component/nonexistent", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestComponentHandlerUnhealthy(t *testing.T) {
	c := NewChecker()
	c.Register(ComponentConfig{
		Name:      "broken",
		CheckFunc: func(ctx context.Context) CheckResult { return CheckResult{Status: StatusUnhealthy} },
	})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/component/{name}", c.ComponentHandler())

	req := httptest.NewRequest("GET", "/health/component/broken", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestRegisterHandlers(t *testing.T) {
	c := NewChecker()
	mux := http.NewServeMux()
	c.RegisterHandlers(mux)

	// Verify all endpoints are registered by making requests.
	endpoints := []string{"/health/live", "/health/ready", "/health/detailed"}
	for _, ep := range endpoints {
		req := httptest.NewRequest("GET", ep, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		assert.NotEqual(t, http.StatusNotFound, w.Code, "endpoint %s should be registered", ep)
	}
}

// --- Concurrency safety test ---

func TestConcurrentAccess(t *testing.T) {
	c := NewChecker()
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("comp_%d", i)
		c.Register(ComponentConfig{
			Name:      name,
			CheckFunc: func(ctx context.Context) CheckResult { return CheckResult{Status: StatusHealthy} },
		})
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.CheckAll()
			c.CheckCached()
			c.IsReady()
			c.IsHealthy()
			c.ComponentNames()
			c.ComponentCount()
		}()
	}
	wg.Wait()
}

// --- CheckResult JSON test ---

func TestCheckResultJSON(t *testing.T) {
	result := CheckResult{
		Status:     StatusHealthy,
		Message:    "ok",
		Duration:   150 * time.Millisecond,
		DurationMs: 150,
		Timestamp:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Details:    map[string]any{"version": "15.4"},
	}

	data, err := json.Marshal(result)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, "healthy", decoded["status"])
	assert.Equal(t, float64(150), decoded["duration_ms"])
}

// --- Constants test ---

func TestStatusConstants(t *testing.T) {
	assert.Equal(t, ComponentStatus("healthy"), StatusHealthy)
	assert.Equal(t, ComponentStatus("degraded"), StatusDegraded)
	assert.Equal(t, ComponentStatus("unhealthy"), StatusUnhealthy)
	assert.Equal(t, ComponentStatus("unknown"), StatusUnknown)
}

func TestTypeConstants(t *testing.T) {
	assert.Equal(t, ComponentType("database"), TypeDatabase)
	assert.Equal(t, ComponentType("cache"), TypeCache)
	assert.Equal(t, ComponentType("messaging"), TypeMessaging)
	assert.Equal(t, ComponentType("external"), TypeExternal)
	assert.Equal(t, ComponentType("internal"), TypeInternal)
}

func TestDefaultConstants(t *testing.T) {
	assert.Equal(t, 5*time.Second, DefaultTimeout)
	assert.Equal(t, 30*time.Second, DefaultInterval)
}

// --- Integration with existing Server ---

func TestCheckerWithExistingServer(t *testing.T) {
	// Verify the Checker works independently of the existing Server struct.
	c := NewChecker()
	c.Register(ComponentConfig{
		Name:      "sqlite",
		Type:      TypeDatabase,
		Critical:  true,
		CheckFunc: func(ctx context.Context) CheckResult { return CheckResult{Status: StatusHealthy, Message: "WAL mode"} },
	})
	c.Register(ComponentConfig{
		Name:      "redis",
		Type:      TypeCache,
		Critical:  false,
		CheckFunc: func(ctx context.Context) CheckResult { return CheckResult{Status: StatusHealthy} },
	})

	assert.True(t, c.IsReady())
	assert.True(t, c.IsHealthy())

	results := c.CheckAll()
	assert.Len(t, results, 2)
	assert.Equal(t, StatusHealthy, OverallStatus(results))
}

// --- Cache invalidation on re-check ---

func TestCacheUpdatesOnRecheck(t *testing.T) {
	c := NewChecker()
	var healthy atomic.Bool
	healthy.Store(true)

	c.Register(ComponentConfig{
		Name:     "flaky",
		Interval: 1 * time.Millisecond,
		CheckFunc: func(ctx context.Context) CheckResult {
			if healthy.Load() {
				return CheckResult{Status: StatusHealthy}
			}
			return CheckResult{Status: StatusUnhealthy}
		},
	})

	// First check: healthy.
	results := c.CheckCached()
	assert.Equal(t, StatusHealthy, results["flaky"].Status)

	// Flip to unhealthy and wait for cache to expire.
	healthy.Store(false)
	time.Sleep(5 * time.Millisecond)

	results = c.CheckCached()
	assert.Equal(t, StatusUnhealthy, results["flaky"].Status)
}
