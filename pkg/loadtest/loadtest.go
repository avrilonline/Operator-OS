// Package loadtest provides a Go-native load testing framework for Operator OS.
// It simulates concurrent users exercising the API to validate performance
// targets: 1K concurrent users, 10K total requests, with latency SLOs.
package loadtest

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// --- Errors ---

var (
	// ErrNoScenarios is returned when a Runner has no scenarios configured.
	ErrNoScenarios = fmt.Errorf("loadtest: no scenarios configured")

	// ErrInvalidConfig is returned for invalid runner configuration.
	ErrInvalidConfig = fmt.Errorf("loadtest: invalid configuration")

	// ErrRunnerBusy is returned when a runner is already executing.
	ErrRunnerBusy = fmt.Errorf("loadtest: runner is already executing")
)

// --- Configuration ---

// Config defines load test execution parameters.
type Config struct {
	// ConcurrentUsers is the number of simulated concurrent users.
	ConcurrentUsers int `json:"concurrent_users"`

	// TotalRequests is the total number of requests to execute.
	// If 0, runs for Duration instead.
	TotalRequests int `json:"total_requests"`

	// Duration is how long to run the test (if TotalRequests is 0).
	Duration time.Duration `json:"duration"`

	// RampUpTime is the time to gradually bring all users online.
	// Users are started at even intervals during ramp-up.
	RampUpTime time.Duration `json:"ramp_up_time"`

	// RequestTimeout is the max time per individual request.
	RequestTimeout time.Duration `json:"request_timeout"`

	// ThinkTime is the pause between requests per user (simulates real behavior).
	ThinkTime time.Duration `json:"think_time"`
}

// DefaultConfig returns a sensible default configuration.
func DefaultConfig() Config {
	return Config{
		ConcurrentUsers: 100,
		TotalRequests:   10000,
		RampUpTime:      10 * time.Second,
		RequestTimeout:  30 * time.Second,
		ThinkTime:       100 * time.Millisecond,
	}
}

// Validate checks the configuration for errors.
func (c Config) Validate() error {
	if c.ConcurrentUsers <= 0 {
		return fmt.Errorf("%w: concurrent_users must be > 0", ErrInvalidConfig)
	}
	if c.TotalRequests < 0 {
		return fmt.Errorf("%w: total_requests must be >= 0", ErrInvalidConfig)
	}
	if c.TotalRequests == 0 && c.Duration <= 0 {
		return fmt.Errorf("%w: either total_requests or duration must be set", ErrInvalidConfig)
	}
	if c.RequestTimeout < 0 {
		return fmt.Errorf("%w: request_timeout must be >= 0", ErrInvalidConfig)
	}
	if c.RampUpTime < 0 {
		return fmt.Errorf("%w: ramp_up_time must be >= 0", ErrInvalidConfig)
	}
	return nil
}

// --- Scenario ---

// ScenarioFunc is a function that executes a single test action.
// It receives the context, the virtual user ID, and the iteration number.
// It should return nil on success or an error on failure.
type ScenarioFunc func(ctx context.Context, userID int, iteration int) error

// Scenario defines a named test scenario with a weight for selection probability.
type Scenario struct {
	// Name identifies this scenario in results.
	Name string

	// Weight determines how often this scenario is selected relative to others.
	// Higher weight = more frequent selection.
	Weight int

	// Fn is the function that executes the scenario.
	Fn ScenarioFunc
}

// --- Result Tracking ---

// RequestResult captures the outcome of a single request.
type RequestResult struct {
	Scenario  string        `json:"scenario"`
	Duration  time.Duration `json:"duration_ns"`
	Error     error         `json:"-"`
	ErrorMsg  string        `json:"error,omitempty"`
	Timestamp time.Time     `json:"timestamp"`
	UserID    int           `json:"user_id"`
	Iteration int           `json:"iteration"`
}

// ScenarioStats holds aggregated statistics for a single scenario.
type ScenarioStats struct {
	Name         string        `json:"name"`
	TotalReqs    int           `json:"total_requests"`
	Successes    int           `json:"successes"`
	Failures     int           `json:"failures"`
	MinLatency   time.Duration `json:"min_latency_ns"`
	MaxLatency   time.Duration `json:"max_latency_ns"`
	MeanLatency  time.Duration `json:"mean_latency_ns"`
	P50Latency   time.Duration `json:"p50_latency_ns"`
	P90Latency   time.Duration `json:"p90_latency_ns"`
	P95Latency   time.Duration `json:"p95_latency_ns"`
	P99Latency   time.Duration `json:"p99_latency_ns"`
	ErrorRate    float64       `json:"error_rate"`
	RequestsPerS float64       `json:"requests_per_second"`
}

// Report is the final output of a load test run.
type Report struct {
	Config           Config                   `json:"config"`
	StartTime        time.Time                `json:"start_time"`
	EndTime          time.Time                `json:"end_time"`
	TotalDuration    time.Duration            `json:"total_duration_ns"`
	TotalRequests    int                      `json:"total_requests"`
	TotalSuccesses   int                      `json:"total_successes"`
	TotalFailures    int                      `json:"total_failures"`
	OverallRPS       float64                  `json:"overall_rps"`
	OverallErrorRate float64                  `json:"overall_error_rate"`
	Scenarios        map[string]ScenarioStats `json:"scenarios"`
	SLOResults       []SLOResult              `json:"slo_results"`
}

// --- SLO Definitions ---

// SLO defines a Service Level Objective to validate against results.
type SLO struct {
	// Name describes this SLO (e.g., "P99 latency < 500ms").
	Name string

	// Check evaluates the report and returns true if the SLO is met.
	Check func(report *Report) bool
}

// SLOResult captures whether an SLO was met.
type SLOResult struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
}

// DefaultSLOs returns standard SLOs for Operator OS production readiness.
func DefaultSLOs() []SLO {
	return []SLO{
		{
			Name: "Error rate < 1%",
			Check: func(r *Report) bool {
				return r.OverallErrorRate < 0.01
			},
		},
		{
			Name: "P95 latency < 500ms (all scenarios)",
			Check: func(r *Report) bool {
				for _, s := range r.Scenarios {
					if s.P95Latency > 500*time.Millisecond {
						return false
					}
				}
				return true
			},
		},
		{
			Name: "P99 latency < 1s (all scenarios)",
			Check: func(r *Report) bool {
				for _, s := range r.Scenarios {
					if s.P99Latency > 1*time.Second {
						return false
					}
				}
				return true
			},
		},
		{
			Name: "Zero scenario with > 5% error rate",
			Check: func(r *Report) bool {
				for _, s := range r.Scenarios {
					if s.ErrorRate > 0.05 {
						return false
					}
				}
				return true
			},
		},
	}
}

// --- Runner ---

// Runner orchestrates load test execution.
type Runner struct {
	config    Config
	scenarios []Scenario
	slos      []SLO
	running   atomic.Bool

	mu      sync.Mutex
	results []RequestResult
}

// NewRunner creates a new load test runner with the given configuration.
func NewRunner(cfg Config) (*Runner, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Runner{
		config: cfg,
		slos:   DefaultSLOs(),
	}, nil
}

// AddScenario registers a test scenario.
func (r *Runner) AddScenario(s Scenario) {
	r.scenarios = append(r.scenarios, s)
}

// SetSLOs replaces the default SLOs with custom ones.
func (r *Runner) SetSLOs(slos []SLO) {
	r.slos = slos
}

// Run executes the load test and returns a report.
func (r *Runner) Run(ctx context.Context) (*Report, error) {
	if len(r.scenarios) == 0 {
		return nil, ErrNoScenarios
	}
	if !r.running.CompareAndSwap(false, true) {
		return nil, ErrRunnerBusy
	}
	defer r.running.Store(false)

	r.mu.Lock()
	r.results = nil
	r.mu.Unlock()

	// Build weighted scenario selector.
	selector := buildSelector(r.scenarios)

	startTime := time.Now()

	// Determine how to distribute work.
	if r.config.TotalRequests > 0 {
		r.runByRequestCount(ctx, selector)
	} else {
		r.runByDuration(ctx, selector)
	}

	endTime := time.Now()

	return r.buildReport(startTime, endTime), nil
}

// runByRequestCount distributes a fixed number of requests across users.
func (r *Runner) runByRequestCount(ctx context.Context, selector *scenarioSelector) {
	var remaining atomic.Int64
	remaining.Store(int64(r.config.TotalRequests))

	var wg sync.WaitGroup
	rampDelay := r.rampDelay()

	for i := 0; i < r.config.ConcurrentUsers; i++ {
		wg.Add(1)
		userID := i

		go func() {
			defer wg.Done()

			// Ramp-up delay.
			if rampDelay > 0 {
				select {
				case <-time.After(time.Duration(userID) * rampDelay):
				case <-ctx.Done():
					return
				}
			}

			iteration := 0
			for {
				if remaining.Add(-1) < 0 {
					return
				}
				select {
				case <-ctx.Done():
					return
				default:
				}

				scenario := selector.Pick(iteration)
				r.executeScenario(ctx, scenario, userID, iteration)
				iteration++

				if r.config.ThinkTime > 0 {
					select {
					case <-time.After(r.config.ThinkTime):
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}

	wg.Wait()
}

// runByDuration runs scenarios for a fixed duration.
func (r *Runner) runByDuration(ctx context.Context, selector *scenarioSelector) {
	ctx, cancel := context.WithTimeout(ctx, r.config.Duration)
	defer cancel()

	var wg sync.WaitGroup
	rampDelay := r.rampDelay()

	for i := 0; i < r.config.ConcurrentUsers; i++ {
		wg.Add(1)
		userID := i

		go func() {
			defer wg.Done()

			// Ramp-up delay.
			if rampDelay > 0 {
				select {
				case <-time.After(time.Duration(userID) * rampDelay):
				case <-ctx.Done():
					return
				}
			}

			iteration := 0
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				scenario := selector.Pick(iteration)
				r.executeScenario(ctx, scenario, userID, iteration)
				iteration++

				if r.config.ThinkTime > 0 {
					select {
					case <-time.After(r.config.ThinkTime):
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}

	wg.Wait()
}

// executeScenario runs a single scenario and records the result.
func (r *Runner) executeScenario(ctx context.Context, scenario *Scenario, userID, iteration int) {
	var execCtx context.Context
	var cancel context.CancelFunc
	if r.config.RequestTimeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, r.config.RequestTimeout)
	} else {
		execCtx, cancel = context.WithCancel(ctx)
	}

	start := time.Now()
	err := scenario.Fn(execCtx, userID, iteration)
	duration := time.Since(start)
	cancel()

	result := RequestResult{
		Scenario:  scenario.Name,
		Duration:  duration,
		Error:     err,
		Timestamp: start,
		UserID:    userID,
		Iteration: iteration,
	}
	if err != nil {
		result.ErrorMsg = err.Error()
	}

	r.mu.Lock()
	r.results = append(r.results, result)
	r.mu.Unlock()
}

// rampDelay calculates the delay between starting each user.
func (r *Runner) rampDelay() time.Duration {
	if r.config.RampUpTime <= 0 || r.config.ConcurrentUsers <= 1 {
		return 0
	}
	return r.config.RampUpTime / time.Duration(r.config.ConcurrentUsers-1)
}

// buildReport computes the final report from collected results.
func (r *Runner) buildReport(startTime, endTime time.Time) *Report {
	r.mu.Lock()
	results := make([]RequestResult, len(r.results))
	copy(results, r.results)
	r.mu.Unlock()

	totalDuration := endTime.Sub(startTime)

	// Group by scenario.
	grouped := make(map[string][]RequestResult)
	for _, res := range results {
		grouped[res.Scenario] = append(grouped[res.Scenario], res)
	}

	scenarioStats := make(map[string]ScenarioStats)
	totalSuccesses := 0
	totalFailures := 0

	for name, scenarioResults := range grouped {
		stats := computeStats(name, scenarioResults, totalDuration)
		scenarioStats[name] = stats
		totalSuccesses += stats.Successes
		totalFailures += stats.Failures
	}

	total := totalSuccesses + totalFailures
	var overallRPS float64
	var overallErrorRate float64
	if totalDuration.Seconds() > 0 {
		overallRPS = float64(total) / totalDuration.Seconds()
	}
	if total > 0 {
		overallErrorRate = float64(totalFailures) / float64(total)
	}

	report := &Report{
		Config:           r.config,
		StartTime:        startTime,
		EndTime:          endTime,
		TotalDuration:    totalDuration,
		TotalRequests:    total,
		TotalSuccesses:   totalSuccesses,
		TotalFailures:    totalFailures,
		OverallRPS:       overallRPS,
		OverallErrorRate: overallErrorRate,
		Scenarios:        scenarioStats,
	}

	// Evaluate SLOs.
	for _, slo := range r.slos {
		report.SLOResults = append(report.SLOResults, SLOResult{
			Name:   slo.Name,
			Passed: slo.Check(report),
		})
	}

	return report
}

// computeStats calculates aggregate statistics for a set of results.
func computeStats(name string, results []RequestResult, totalDuration time.Duration) ScenarioStats {
	if len(results) == 0 {
		return ScenarioStats{Name: name}
	}

	successes := 0
	failures := 0
	var totalLatency time.Duration
	durations := make([]time.Duration, 0, len(results))

	for _, r := range results {
		durations = append(durations, r.Duration)
		totalLatency += r.Duration
		if r.Error != nil {
			failures++
		} else {
			successes++
		}
	}

	sort.Slice(durations, func(i, j int) bool {
		return durations[i] < durations[j]
	})

	total := len(results)
	var errorRate float64
	if total > 0 {
		errorRate = float64(failures) / float64(total)
	}
	var rps float64
	if totalDuration.Seconds() > 0 {
		rps = float64(total) / totalDuration.Seconds()
	}

	return ScenarioStats{
		Name:         name,
		TotalReqs:    total,
		Successes:    successes,
		Failures:     failures,
		MinLatency:   durations[0],
		MaxLatency:   durations[len(durations)-1],
		MeanLatency:  time.Duration(int64(totalLatency) / int64(total)),
		P50Latency:   percentile(durations, 0.50),
		P90Latency:   percentile(durations, 0.90),
		P95Latency:   percentile(durations, 0.95),
		P99Latency:   percentile(durations, 0.99),
		ErrorRate:    errorRate,
		RequestsPerS: rps,
	}
}

// percentile returns the value at the given percentile (0.0–1.0) from a sorted slice.
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	idx := p * float64(len(sorted)-1)
	lower := int(math.Floor(idx))
	upper := int(math.Ceil(idx))
	if lower == upper {
		return sorted[lower]
	}
	// Linear interpolation.
	frac := idx - float64(lower)
	return time.Duration(float64(sorted[lower])*(1-frac) + float64(sorted[upper])*frac)
}

// --- Scenario Selector ---

// scenarioSelector picks scenarios based on weights.
type scenarioSelector struct {
	scenarios    []*Scenario
	totalWeight  int
	cumulWeights []int
}

func buildSelector(scenarios []Scenario) *scenarioSelector {
	s := &scenarioSelector{
		scenarios:    make([]*Scenario, len(scenarios)),
		cumulWeights: make([]int, len(scenarios)),
	}
	cumul := 0
	for i := range scenarios {
		s.scenarios[i] = &scenarios[i]
		weight := scenarios[i].Weight
		if weight <= 0 {
			weight = 1
		}
		cumul += weight
		s.cumulWeights[i] = cumul
	}
	s.totalWeight = cumul
	return s
}

// Pick selects a scenario deterministically based on the iteration.
// Uses modular arithmetic on cumulative weights for even distribution.
func (s *scenarioSelector) Pick(iteration int) *Scenario {
	if len(s.scenarios) == 1 {
		return s.scenarios[0]
	}
	target := (iteration % s.totalWeight) + 1
	for i, cw := range s.cumulWeights {
		if target <= cw {
			return s.scenarios[i]
		}
	}
	return s.scenarios[len(s.scenarios)-1]
}
