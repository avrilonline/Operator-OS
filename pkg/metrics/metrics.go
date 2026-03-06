// Package metrics provides Prometheus instrumentation for Operator OS.
//
// It exposes pre-registered collectors for LLM requests, sessions, bus traffic,
// tool executions and general system info. Call Init() once at startup to
// register all collectors with the default Prometheus registry. Use Handler()
// to obtain an http.Handler for the /metrics endpoint.
package metrics

import (
	"net/http"
	"runtime"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ---------------------------------------------------------------------
// Namespace used for all metrics.
// ---------------------------------------------------------------------

const namespace = "operator"

// ---------------------------------------------------------------------
// LLM metrics
// ---------------------------------------------------------------------

var (
	// LLMRequestDuration measures LLM request latency in seconds.
	LLMRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "llm",
			Name:      "request_duration_seconds",
			Help:      "Duration of LLM provider requests in seconds.",
			Buckets:   prometheus.ExponentialBuckets(0.1, 2, 12), // 0.1s → ~204s
		},
		[]string{"provider", "model", "status"},
	)

	// LLMTokensTotal counts tokens consumed/produced by LLM calls.
	LLMTokensTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "llm",
			Name:      "tokens_total",
			Help:      "Total LLM tokens processed.",
		},
		[]string{"provider", "model", "direction"}, // direction: input | output
	)

	// LLMErrorsTotal counts LLM request errors.
	LLMErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "llm",
			Name:      "errors_total",
			Help:      "Total LLM request errors.",
		},
		[]string{"provider", "model", "error_type"},
	)
)

// ---------------------------------------------------------------------
// Session metrics
// ---------------------------------------------------------------------

var (
	// SessionsActive tracks the number of active sessions.
	SessionsActive = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "sessions",
			Name:      "active",
			Help:      "Number of active sessions.",
		},
	)

	// SessionsMessagesTotal counts messages across all sessions.
	SessionsMessagesTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "sessions",
			Name:      "messages_total",
			Help:      "Total messages added to sessions.",
		},
	)
)

// ---------------------------------------------------------------------
// Bus metrics
// ---------------------------------------------------------------------

var (
	// BusMessagesTotal counts messages flowing through the bus.
	BusMessagesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "bus",
			Name:      "messages_total",
			Help:      "Total messages published on the bus.",
		},
		[]string{"direction"}, // inbound | outbound
	)

	// BusQueueDepth reports the approximate queue depth.
	BusQueueDepth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "bus",
			Name:      "queue_depth",
			Help:      "Approximate queue depth of bus channels.",
		},
		[]string{"direction"},
	)
)

// ---------------------------------------------------------------------
// Tool metrics
// ---------------------------------------------------------------------

var (
	// ToolExecutionDuration measures tool execution latency in seconds.
	ToolExecutionDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "tool",
			Name:      "execution_duration_seconds",
			Help:      "Duration of tool executions in seconds.",
			Buckets:   prometheus.ExponentialBuckets(0.01, 2, 14), // 10ms → ~163s
		},
		[]string{"tool_name", "status"},
	)

	// ToolExecutionsTotal counts tool executions.
	ToolExecutionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "tool",
			Name:      "executions_total",
			Help:      "Total tool executions.",
		},
		[]string{"tool_name", "status"},
	)
)

// ---------------------------------------------------------------------
// System / uptime metrics
// ---------------------------------------------------------------------

var (
	// UptimeSeconds reports how long the process has been running.
	// Updated by callers via Set() or by a periodic goroutine.
	UptimeSeconds = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "uptime_seconds",
			Help:      "Seconds since the process started.",
		},
	)

	// Info is a constant-value gauge labelled with build metadata.
	Info = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "info",
			Help:      "Build and runtime information (constant 1).",
		},
		[]string{"version", "go_version"},
	)
)

// ---------------------------------------------------------------------
// Registration
// ---------------------------------------------------------------------

var initOnce sync.Once

// allCollectors returns every collector declared above.
func allCollectors() []prometheus.Collector {
	return []prometheus.Collector{
		// LLM
		LLMRequestDuration,
		LLMTokensTotal,
		LLMErrorsTotal,
		// Sessions
		SessionsActive,
		SessionsMessagesTotal,
		// Bus
		BusMessagesTotal,
		BusQueueDepth,
		// Tools
		ToolExecutionDuration,
		ToolExecutionsTotal,
		// System
		UptimeSeconds,
		Info,
	}
}

// Init registers all Operator OS metrics with the default Prometheus registry.
// It is idempotent; subsequent calls are no-ops.
// version may be empty; go_version is filled automatically.
func Init(version string) {
	initOnce.Do(func() {
		for _, c := range allCollectors() {
			prometheus.MustRegister(c)
		}
		if version == "" {
			version = "unknown"
		}
		Info.WithLabelValues(version, runtime.Version()).Set(1)
	})
}

// Handler returns an http.Handler that serves the /metrics endpoint
// using the default Prometheus gatherer.
func Handler() http.Handler {
	return promhttp.Handler()
}

// ---------------------------------------------------------------------
// Convenience helpers (keep call sites terse)
// ---------------------------------------------------------------------

// RecordLLMRequest records duration, tokens, and (optionally) an error
// for a single LLM call.
func RecordLLMRequest(provider, model, status string, durationSec float64, inputTokens, outputTokens int) {
	LLMRequestDuration.WithLabelValues(provider, model, status).Observe(durationSec)
	if inputTokens > 0 {
		LLMTokensTotal.WithLabelValues(provider, model, "input").Add(float64(inputTokens))
	}
	if outputTokens > 0 {
		LLMTokensTotal.WithLabelValues(provider, model, "output").Add(float64(outputTokens))
	}
}

// RecordLLMError increments the LLM error counter.
func RecordLLMError(provider, model, errorType string) {
	LLMErrorsTotal.WithLabelValues(provider, model, errorType).Inc()
}

// RecordToolExecution records a single tool execution.
func RecordToolExecution(toolName, status string, durationSec float64) {
	ToolExecutionDuration.WithLabelValues(toolName, status).Observe(durationSec)
	ToolExecutionsTotal.WithLabelValues(toolName, status).Inc()
}

// RecordBusMessage increments the bus message counter.
// direction should be "inbound" or "outbound".
func RecordBusMessage(direction string) {
	BusMessagesTotal.WithLabelValues(direction).Inc()
}
