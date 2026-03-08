// Package openapi provides an embedded OpenAPI 3.1 specification for the
// Operator OS API and an HTTP handler that serves the spec as JSON/YAML.
package openapi

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

//go:embed spec.json
var specJSON []byte

// Spec returns the raw OpenAPI 3.1 specification as JSON bytes.
func Spec() []byte {
	cp := make([]byte, len(specJSON))
	copy(cp, specJSON)
	return cp
}

// SpecMap returns the OpenAPI specification parsed into a generic map.
func SpecMap() (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal(specJSON, &m); err != nil {
		return nil, fmt.Errorf("openapi: unmarshal spec: %w", err)
	}
	return m, nil
}

// Handler returns an http.Handler that serves the OpenAPI specification.
// The response format defaults to JSON. Pass Accept: application/yaml or
// ?format=yaml to receive YAML (not implemented — returns JSON with a note).
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(specJSON)
	})
}

// RegisterRoutes registers the OpenAPI documentation endpoints on the given mux.
//
//	GET /api/v1/docs/openapi.json  — full spec
//	GET /api/v1/docs              — redirect to spec
func RegisterRoutes(mux *http.ServeMux) {
	h := Handler()
	mux.Handle("GET /api/v1/docs/openapi.json", h)
	mux.HandleFunc("GET /api/v1/docs", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/api/v1/docs/openapi.json", http.StatusTemporaryRedirect)
	})
}

// Version returns the API version from the spec's info.version field.
func Version() string {
	m, err := SpecMap()
	if err != nil {
		return "unknown"
	}
	info, ok := m["info"].(map[string]any)
	if !ok {
		return "unknown"
	}
	v, _ := info["version"].(string)
	return v
}

// Paths returns a sorted list of all API paths defined in the spec.
func Paths() ([]string, error) {
	m, err := SpecMap()
	if err != nil {
		return nil, err
	}
	paths, ok := m["paths"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("openapi: no paths found")
	}
	result := make([]string, 0, len(paths))
	for p := range paths {
		result = append(result, p)
	}
	// Sort for deterministic output.
	sortStrings(result)
	return result, nil
}

// EndpointCount returns the total number of operation (method+path) entries.
func EndpointCount() (int, error) {
	m, err := SpecMap()
	if err != nil {
		return 0, err
	}
	paths, ok := m["paths"].(map[string]any)
	if !ok {
		return 0, fmt.Errorf("openapi: no paths found")
	}
	count := 0
	methods := []string{"get", "post", "put", "delete", "patch", "head", "options"}
	for _, ops := range paths {
		opsMap, ok := ops.(map[string]any)
		if !ok {
			continue
		}
		for _, method := range methods {
			if _, exists := opsMap[method]; exists {
				count++
			}
		}
	}
	return count, nil
}

// Tags returns all unique tags used across operations.
func Tags() ([]string, error) {
	m, err := SpecMap()
	if err != nil {
		return nil, err
	}
	paths, ok := m["paths"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("openapi: no paths found")
	}
	tagSet := map[string]bool{}
	methods := []string{"get", "post", "put", "delete", "patch"}
	for _, ops := range paths {
		opsMap, ok := ops.(map[string]any)
		if !ok {
			continue
		}
		for _, method := range methods {
			op, exists := opsMap[method]
			if !exists {
				continue
			}
			opMap, ok := op.(map[string]any)
			if !ok {
				continue
			}
			tags, ok := opMap["tags"].([]any)
			if !ok {
				continue
			}
			for _, t := range tags {
				if s, ok := t.(string); ok {
					tagSet[s] = true
				}
			}
		}
	}
	result := make([]string, 0, len(tagSet))
	for t := range tagSet {
		result = append(result, t)
	}
	sortStrings(result)
	return result, nil
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && strings.Compare(s[j-1], s[j]) > 0; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
