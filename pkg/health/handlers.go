package health

import (
	"encoding/json"
	"net/http"
	"sort"
	"time"
)

// DetailedResponse is the JSON response for the /health/detailed endpoint.
type DetailedResponse struct {
	Status     ComponentStatus           `json:"status"`
	Uptime     string                    `json:"uptime"`
	UptimeSec  float64                   `json:"uptime_seconds"`
	Timestamp  string                    `json:"timestamp"`
	Components map[string]ComponentInfo  `json:"components"`
	Summary    HealthSummary             `json:"summary"`
}

// ComponentInfo is the per-component section in a detailed health response.
type ComponentInfo struct {
	Status    ComponentStatus `json:"status"`
	Type      ComponentType   `json:"type"`
	Critical  bool            `json:"critical"`
	Message   string          `json:"message,omitempty"`
	DurationMs float64        `json:"duration_ms"`
	Details   map[string]any  `json:"details,omitempty"`
}

// HealthSummary provides aggregate counts by status.
type HealthSummary struct {
	Total     int `json:"total"`
	Healthy   int `json:"healthy"`
	Degraded  int `json:"degraded"`
	Unhealthy int `json:"unhealthy"`
}

// DetailedHandler returns an http.HandlerFunc for the /health/detailed endpoint.
// It runs all registered checks and returns a comprehensive status report.
func (c *Checker) DetailedHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		results := c.CheckAll()
		overall := OverallStatus(results)

		components := make(map[string]ComponentInfo, len(results))
		summary := HealthSummary{Total: len(results)}

		c.mu.RLock()
		for name, result := range results {
			cs := c.components[name]
			info := ComponentInfo{
				Status:     result.Status,
				Message:    result.Message,
				DurationMs: result.DurationMs,
				Details:    result.Details,
			}
			if cs != nil {
				info.Type = cs.config.Type
				info.Critical = cs.config.Critical
			}
			components[name] = info

			switch result.Status {
			case StatusHealthy:
				summary.Healthy++
			case StatusDegraded:
				summary.Degraded++
			default:
				summary.Unhealthy++
			}
		}
		c.mu.RUnlock()

		uptime := c.Uptime()
		resp := DetailedResponse{
			Status:     overall,
			Uptime:     uptime.String(),
			UptimeSec:  uptime.Seconds(),
			Timestamp:  time.Now().UTC().Format(time.RFC3339),
			Components: components,
			Summary:    summary,
		}

		if overall == StatusUnhealthy {
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusOK)
		}
		json.NewEncoder(w).Encode(resp)
	}
}

// LiveHandler returns an http.HandlerFunc for the /health/live endpoint.
// Liveness checks indicate whether the process is running and not deadlocked.
// This always returns 200 unless the server is explicitly marked as not ready.
func (c *Checker) LiveHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "alive",
			"uptime": c.Uptime().String(),
		})
	}
}

// ReadyHandler returns an http.HandlerFunc for the /health/ready endpoint.
// Readiness checks indicate whether the service can serve traffic.
// Returns 503 if any critical component is unhealthy.
func (c *Checker) ReadyHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		ready := c.IsReady()
		results := c.CheckCached()

		// Build list of failed critical components for the response.
		var failedCritical []string
		c.mu.RLock()
		for name, cs := range c.components {
			if !cs.config.Critical {
				continue
			}
			if r, ok := results[name]; ok {
				if r.Status == StatusUnhealthy || r.Status == StatusUnknown {
					failedCritical = append(failedCritical, name)
				}
			}
		}
		c.mu.RUnlock()
		sort.Strings(failedCritical)

		if !ready {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]any{
				"status":           "not_ready",
				"failed_components": failedCritical,
			})
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "ready",
		})
	}
}

// ComponentHandler returns an http.HandlerFunc for /health/component/{name}.
// It runs a single component's check and returns the result.
func (c *Checker) ComponentHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Extract component name from URL path.
		name := r.PathValue("name")
		if name == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "component name required",
			})
			return
		}

		result, err := c.CheckComponent(name)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{
				"error": err.Error(),
			})
			return
		}

		if result.Status == StatusUnhealthy || result.Status == StatusUnknown {
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusOK)
		}

		json.NewEncoder(w).Encode(result)
	}
}

// RegisterHandlers registers all health check endpoints on the given mux.
// Endpoints:
//   - GET /health/live      — Liveness probe (always 200 if process is running)
//   - GET /health/ready     — Readiness probe (503 if critical components are down)
//   - GET /health/detailed  — Full component-level health report
//   - GET /health/component/{name} — Single component health check
func (c *Checker) RegisterHandlers(mux *http.ServeMux) {
	mux.HandleFunc("GET /health/live", c.LiveHandler())
	mux.HandleFunc("GET /health/ready", c.ReadyHandler())
	mux.HandleFunc("GET /health/detailed", c.DetailedHandler())
	mux.HandleFunc("GET /health/component/{name}", c.ComponentHandler())
}
