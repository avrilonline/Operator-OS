package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestRegistry creates a fresh registry and registers all collectors.
// This avoids polluting the global registry across tests.
func newTestRegistry() *prometheus.Registry {
	reg := prometheus.NewRegistry()
	for _, c := range allCollectors() {
		reg.MustRegister(c)
	}
	return reg
}

func resetCounters() {
	// Reset counters/gauges for test isolation where needed.
	SessionsActive.Set(0)
	SessionsMessagesTotal.Add(0)
}

// ---------------------------------------------------------------------
// Init
// ---------------------------------------------------------------------

func TestInit_Idempotent(t *testing.T) {
	// Calling Init multiple times should not panic.
	Init("test-v1")
	Init("test-v2") // second call is a no-op
}

// ---------------------------------------------------------------------
// Handler serves /metrics
// ---------------------------------------------------------------------

func TestHandler_ServesMetrics(t *testing.T) {
	Init("test")

	// Trigger at least one observation for vec-type metrics so Prometheus exposes them.
	RecordLLMRequest("test-provider", "test-model", "ok", 0.001, 1, 1)
	RecordLLMError("test-provider", "test-model", "test")
	RecordToolExecution("test-tool", "ok", 0.001)
	RecordBusMessage("inbound")
	BusQueueDepth.WithLabelValues("inbound").Set(0)

	handler := Handler()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	body, _ := io.ReadAll(rr.Body)
	bodyStr := string(body)

	// Verify key metric families appear
	assert.Contains(t, bodyStr, "operator_llm_request_duration_seconds")
	assert.Contains(t, bodyStr, "operator_llm_tokens_total")
	assert.Contains(t, bodyStr, "operator_llm_errors_total")
	assert.Contains(t, bodyStr, "operator_sessions_active")
	assert.Contains(t, bodyStr, "operator_sessions_messages_total")
	assert.Contains(t, bodyStr, "operator_bus_messages_total")
	assert.Contains(t, bodyStr, "operator_bus_queue_depth")
	assert.Contains(t, bodyStr, "operator_tool_execution_duration_seconds")
	assert.Contains(t, bodyStr, "operator_tool_executions_total")
	assert.Contains(t, bodyStr, "operator_uptime_seconds")
	assert.Contains(t, bodyStr, "operator_info")
}

// ---------------------------------------------------------------------
// RecordLLMRequest
// ---------------------------------------------------------------------

func TestRecordLLMRequest(t *testing.T) {
	Init("test")

	RecordLLMRequest("openai", "gpt-4", "ok", 1.5, 100, 50)

	// Verify histogram and counters were updated (no panic, correct labels).
	// We check via the /metrics endpoint.
	body := scrapeMetrics(t)
	assert.Contains(t, body, `operator_llm_request_duration_seconds_bucket{`)
	assert.Contains(t, body, `provider="openai"`)
	assert.Contains(t, body, `model="gpt-4"`)
	assert.Contains(t, body, `direction="input"`)
	assert.Contains(t, body, `direction="output"`)
}

func TestRecordLLMRequest_ZeroTokens(t *testing.T) {
	Init("test")
	// Should not panic even with zero tokens.
	RecordLLMRequest("anthropic", "claude-3", "error", 0.5, 0, 0)
}

// ---------------------------------------------------------------------
// RecordLLMError
// ---------------------------------------------------------------------

func TestRecordLLMError(t *testing.T) {
	Init("test")
	RecordLLMError("openai", "gpt-4", "rate_limit")
	body := scrapeMetrics(t)
	assert.Contains(t, body, `operator_llm_errors_total{error_type="rate_limit"`)
}

// ---------------------------------------------------------------------
// RecordToolExecution
// ---------------------------------------------------------------------

func TestRecordToolExecution(t *testing.T) {
	Init("test")
	RecordToolExecution("shell_exec", "ok", 2.3)
	RecordToolExecution("shell_exec", "error", 0.1)

	body := scrapeMetrics(t)
	assert.Contains(t, body, `operator_tool_executions_total{`)
	assert.Contains(t, body, `tool_name="shell_exec"`)
	assert.Contains(t, body, `operator_tool_execution_duration_seconds_bucket{`)
}

// ---------------------------------------------------------------------
// RecordBusMessage
// ---------------------------------------------------------------------

func TestRecordBusMessage(t *testing.T) {
	Init("test")
	RecordBusMessage("inbound")
	RecordBusMessage("outbound")
	RecordBusMessage("inbound")

	body := scrapeMetrics(t)
	assert.Contains(t, body, `operator_bus_messages_total{direction="inbound"}`)
	assert.Contains(t, body, `operator_bus_messages_total{direction="outbound"}`)
}

// ---------------------------------------------------------------------
// Session gauges
// ---------------------------------------------------------------------

func TestSessionMetrics(t *testing.T) {
	Init("test")
	resetCounters()

	SessionsActive.Set(5)
	SessionsMessagesTotal.Add(42)

	body := scrapeMetrics(t)
	assert.Contains(t, body, "operator_sessions_active 5")
}

// ---------------------------------------------------------------------
// UptimeSeconds
// ---------------------------------------------------------------------

func TestUptimeSeconds(t *testing.T) {
	Init("test")
	UptimeSeconds.Set(123.456)
	body := scrapeMetrics(t)
	assert.Contains(t, body, "operator_uptime_seconds 123.456")
}

// ---------------------------------------------------------------------
// Info gauge
// ---------------------------------------------------------------------

func TestInfoGauge(t *testing.T) {
	Init("test")
	body := scrapeMetrics(t)
	// Info should have version and go_version labels
	assert.Contains(t, body, `operator_info{`)
	assert.Contains(t, body, `go_version="go`)
}

// ---------------------------------------------------------------------
// BusQueueDepth
// ---------------------------------------------------------------------

func TestBusQueueDepth(t *testing.T) {
	Init("test")
	BusQueueDepth.WithLabelValues("inbound").Set(10)
	BusQueueDepth.WithLabelValues("outbound").Set(3)

	body := scrapeMetrics(t)
	assert.Contains(t, body, `operator_bus_queue_depth{direction="inbound"} 10`)
	assert.Contains(t, body, `operator_bus_queue_depth{direction="outbound"} 3`)
}

// ---------------------------------------------------------------------
// allCollectors returns expected count
// ---------------------------------------------------------------------

func TestAllCollectors_Count(t *testing.T) {
	collectors := allCollectors()
	require.Len(t, collectors, 11, "expected 11 metric collectors")
}

// ---------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------

func scrapeMetrics(t *testing.T) string {
	t.Helper()
	handler := Handler()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	body, err := io.ReadAll(rr.Body)
	require.NoError(t, err)
	return strings.TrimSpace(string(body))
}
