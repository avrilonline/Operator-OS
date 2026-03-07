// Package health provides production-ready health check infrastructure with
// component-level checks, dependency health tracking, and configurable timeouts.
package health

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ComponentStatus represents the health status of a single component.
type ComponentStatus string

const (
	StatusHealthy   ComponentStatus = "healthy"
	StatusDegraded  ComponentStatus = "degraded"
	StatusUnhealthy ComponentStatus = "unhealthy"
	StatusUnknown   ComponentStatus = "unknown"
)

// ComponentType classifies components for grouping and display.
type ComponentType string

const (
	TypeDatabase  ComponentType = "database"
	TypeCache     ComponentType = "cache"
	TypeMessaging ComponentType = "messaging"
	TypeExternal  ComponentType = "external"
	TypeInternal  ComponentType = "internal"
)

// CheckResult is the outcome of a single health check execution.
type CheckResult struct {
	Status     ComponentStatus `json:"status"`
	Message    string          `json:"message,omitempty"`
	Duration   time.Duration   `json:"-"`
	DurationMs float64         `json:"duration_ms"`
	Timestamp  time.Time       `json:"timestamp"`
	Details    map[string]any  `json:"details,omitempty"`
}

// CheckerFunc is the function signature for health check implementations.
// The context carries a timeout deadline.
type CheckerFunc func(ctx context.Context) CheckResult

// ComponentConfig configures a registered health check component.
type ComponentConfig struct {
	// Name is the unique component identifier (e.g., "postgresql", "redis", "nats").
	Name string

	// Type classifies the component (database, cache, messaging, etc.).
	Type ComponentType

	// CheckFunc is the function that performs the actual health check.
	CheckFunc CheckerFunc

	// Timeout is the maximum time allowed for this check (default 5s).
	Timeout time.Duration

	// Critical marks this component as required for readiness.
	// If a critical component is unhealthy, the system is not ready.
	Critical bool

	// Interval is the minimum time between automatic checks (default 30s).
	// Checks are still run on-demand regardless.
	Interval time.Duration
}

// DefaultTimeout is the default timeout for health checks.
const DefaultTimeout = 5 * time.Second

// DefaultInterval is the default minimum interval between checks.
const DefaultInterval = 30 * time.Second

// Checker manages component health checks with caching and timeouts.
type Checker struct {
	mu         sync.RWMutex
	components map[string]*componentState
	startTime  time.Time
}

type componentState struct {
	config     ComponentConfig
	lastResult *CheckResult
	lastCheck  time.Time
}

// NewChecker creates a new health checker.
func NewChecker() *Checker {
	return &Checker{
		components: make(map[string]*componentState),
		startTime:  time.Now(),
	}
}

// Register adds a component health check.
// Returns an error if the name is empty or already registered.
func (c *Checker) Register(cfg ComponentConfig) error {
	if cfg.Name == "" {
		return fmt.Errorf("component name is required")
	}
	if cfg.CheckFunc == nil {
		return fmt.Errorf("check function is required for component %q", cfg.Name)
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.Interval == 0 {
		cfg.Interval = DefaultInterval
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.components[cfg.Name]; exists {
		return fmt.Errorf("component %q already registered", cfg.Name)
	}

	c.components[cfg.Name] = &componentState{
		config: cfg,
	}
	return nil
}

// Deregister removes a component health check.
func (c *Checker) Deregister(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.components, name)
}

// CheckComponent runs a single component's health check with its configured timeout.
func (c *Checker) CheckComponent(name string) (CheckResult, error) {
	c.mu.RLock()
	cs, exists := c.components[name]
	c.mu.RUnlock()

	if !exists {
		return CheckResult{}, fmt.Errorf("component %q not found", name)
	}

	result := c.runCheck(cs)
	return result, nil
}

// CheckAll runs all registered health checks concurrently and returns results.
func (c *Checker) CheckAll() map[string]CheckResult {
	c.mu.RLock()
	components := make(map[string]*componentState, len(c.components))
	for k, v := range c.components {
		components[k] = v
	}
	c.mu.RUnlock()

	results := make(map[string]CheckResult, len(components))
	var mu sync.Mutex
	var wg sync.WaitGroup

	for name, cs := range components {
		wg.Add(1)
		go func(n string, s *componentState) {
			defer wg.Done()
			result := c.runCheck(s)
			mu.Lock()
			results[n] = result
			mu.Unlock()
		}(name, cs)
	}

	wg.Wait()
	return results
}

// CheckCached returns cached results where available, only running checks
// that haven't been checked within their configured interval.
func (c *Checker) CheckCached() map[string]CheckResult {
	c.mu.RLock()
	components := make(map[string]*componentState, len(c.components))
	for k, v := range c.components {
		components[k] = v
	}
	c.mu.RUnlock()

	now := time.Now()
	results := make(map[string]CheckResult, len(components))
	var stale []*componentState
	var staleNames []string

	for name, cs := range components {
		c.mu.RLock()
		if cs.lastResult != nil && now.Sub(cs.lastCheck) < cs.config.Interval {
			results[name] = *cs.lastResult
			c.mu.RUnlock()
			continue
		}
		c.mu.RUnlock()
		stale = append(stale, cs)
		staleNames = append(staleNames, name)
	}

	// Run stale checks concurrently.
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i, cs := range stale {
		wg.Add(1)
		go func(n string, s *componentState) {
			defer wg.Done()
			result := c.runCheck(s)
			mu.Lock()
			results[n] = result
			mu.Unlock()
		}(staleNames[i], cs)
	}
	wg.Wait()

	return results
}

// IsReady returns true if all critical components are healthy or degraded.
func (c *Checker) IsReady() bool {
	results := c.CheckCached()

	c.mu.RLock()
	defer c.mu.RUnlock()

	for name, cs := range c.components {
		if !cs.config.Critical {
			continue
		}
		result, ok := results[name]
		if !ok {
			return false
		}
		if result.Status == StatusUnhealthy || result.Status == StatusUnknown {
			return false
		}
	}
	return true
}

// IsHealthy returns true if ALL components are healthy.
func (c *Checker) IsHealthy() bool {
	results := c.CheckCached()

	for _, result := range results {
		if result.Status != StatusHealthy {
			return false
		}
	}
	return true
}

// ComponentNames returns the names of all registered components.
func (c *Checker) ComponentNames() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	names := make([]string, 0, len(c.components))
	for name := range c.components {
		names = append(names, name)
	}
	return names
}

// ComponentCount returns the number of registered components.
func (c *Checker) ComponentCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.components)
}

// Uptime returns the duration since the checker was created.
func (c *Checker) Uptime() time.Duration {
	return time.Since(c.startTime)
}

// runCheck executes a component's check function with timeout and caches the result.
func (c *Checker) runCheck(cs *componentState) CheckResult {
	ctx, cancel := context.WithTimeout(context.Background(), cs.config.Timeout)
	defer cancel()

	start := time.Now()
	resultCh := make(chan CheckResult, 1)

	go func() {
		resultCh <- cs.config.CheckFunc(ctx)
	}()

	var result CheckResult
	select {
	case result = <-resultCh:
		// Check completed in time.
	case <-ctx.Done():
		result = CheckResult{
			Status:  StatusUnhealthy,
			Message: fmt.Sprintf("check timed out after %s", cs.config.Timeout),
		}
	}

	result.Duration = time.Since(start)
	result.DurationMs = float64(result.Duration.Milliseconds())
	result.Timestamp = time.Now()

	// Cache the result.
	c.mu.Lock()
	cs.lastResult = &result
	cs.lastCheck = time.Now()
	c.mu.Unlock()

	return result
}

// OverallStatus computes the aggregate system status from component results.
func OverallStatus(results map[string]CheckResult) ComponentStatus {
	if len(results) == 0 {
		return StatusHealthy
	}

	hasUnhealthy := false
	hasDegraded := false

	for _, r := range results {
		switch r.Status {
		case StatusUnhealthy:
			hasUnhealthy = true
		case StatusDegraded:
			hasDegraded = true
		case StatusUnknown:
			hasUnhealthy = true
		}
	}

	if hasUnhealthy {
		return StatusUnhealthy
	}
	if hasDegraded {
		return StatusDegraded
	}
	return StatusHealthy
}
