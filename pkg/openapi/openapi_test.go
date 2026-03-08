package openapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSpec(t *testing.T) {
	data := Spec()
	require.NotEmpty(t, data)

	// Verify it's valid JSON.
	var m map[string]any
	err := json.Unmarshal(data, &m)
	require.NoError(t, err)

	// Verify key top-level fields.
	assert.Equal(t, "3.1.0", m["openapi"])
	assert.Contains(t, m, "info")
	assert.Contains(t, m, "paths")
	assert.Contains(t, m, "components")
	assert.Contains(t, m, "tags")
	assert.Contains(t, m, "servers")
}

func TestSpecReturnsACopy(t *testing.T) {
	a := Spec()
	b := Spec()
	a[0] = 0xFF
	assert.NotEqual(t, a[0], b[0], "Spec() should return independent copies")
}

func TestSpecMap(t *testing.T) {
	m, err := SpecMap()
	require.NoError(t, err)
	require.NotNil(t, m)

	// Verify info block.
	info, ok := m["info"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Operator OS API", info["title"])
	assert.Equal(t, "1.0.0", info["version"])

	// Verify contact.
	contact, ok := info["contact"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Operator OS", contact["name"])

	// Verify license.
	license, ok := info["license"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "MIT", license["name"])
}

func TestVersion(t *testing.T) {
	v := Version()
	assert.Equal(t, "1.0.0", v)
}

func TestPaths(t *testing.T) {
	paths, err := Paths()
	require.NoError(t, err)
	require.NotEmpty(t, paths)

	// Verify sorted.
	for i := 1; i < len(paths); i++ {
		assert.True(t, paths[i-1] <= paths[i], "paths should be sorted: %s > %s", paths[i-1], paths[i])
	}

	// Verify expected paths exist.
	expected := []string{
		"/api/v1/auth/register",
		"/api/v1/auth/login",
		"/api/v1/auth/refresh",
		"/api/v1/agents",
		"/api/v1/agents/{id}",
		"/api/v1/billing/plans",
		"/api/v1/billing/usage",
		"/api/v1/billing/checkout",
		"/api/v1/billing/overage",
		"/api/v1/oauth/providers",
		"/api/v1/integrations",
		"/api/v1/admin/users",
		"/api/v1/audit/events",
		"/api/v1/gdpr/export",
		"/api/v1/rate-limit/status",
		"/health/live",
		"/health/ready",
		"/health/detailed",
		"/metrics",
	}
	pathSet := map[string]bool{}
	for _, p := range paths {
		pathSet[p] = true
	}
	for _, e := range expected {
		assert.True(t, pathSet[e], "expected path %s not found", e)
	}
}

func TestEndpointCount(t *testing.T) {
	count, err := EndpointCount()
	require.NoError(t, err)
	// We have many endpoints — just verify a reasonable minimum.
	assert.GreaterOrEqual(t, count, 50, "should have at least 50 endpoints")
}

func TestTags(t *testing.T) {
	tags, err := Tags()
	require.NoError(t, err)
	require.NotEmpty(t, tags)

	// Verify sorted.
	for i := 1; i < len(tags); i++ {
		assert.True(t, tags[i-1] <= tags[i], "tags should be sorted")
	}

	// Verify expected tags.
	expected := []string{
		"Auth",
		"Agents",
		"Billing",
		"Stripe",
		"Usage",
		"Overage",
		"OAuth",
		"Integrations",
		"Integration Management",
		"Admin",
		"Audit",
		"GDPR",
		"Rate Limiting",
		"Health",
		"Docs",
	}
	tagSet := map[string]bool{}
	for _, tag := range tags {
		tagSet[tag] = true
	}
	for _, e := range expected {
		assert.True(t, tagSet[e], "expected tag %q not found", e)
	}
}

func TestHandler(t *testing.T) {
	h := Handler()
	require.NotNil(t, h)

	t.Run("GET returns JSON spec", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/docs/openapi.json", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
		assert.Contains(t, w.Header().Get("Cache-Control"), "public")

		body, err := io.ReadAll(w.Body)
		require.NoError(t, err)

		var m map[string]any
		err = json.Unmarshal(body, &m)
		require.NoError(t, err)
		assert.Equal(t, "3.1.0", m["openapi"])
	})

	t.Run("non-GET returns 405", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/docs/openapi.json", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})
}

func TestRegisterRoutes(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux)

	t.Run("openapi.json endpoint", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/docs/openapi.json", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("docs redirect", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/docs", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		assert.Equal(t, http.StatusTemporaryRedirect, w.Code)
		assert.Contains(t, w.Header().Get("Location"), "openapi.json")
	})
}

// --- Spec structure validation ---

func TestSpecHasSecuritySchemes(t *testing.T) {
	m, err := SpecMap()
	require.NoError(t, err)

	components, ok := m["components"].(map[string]any)
	require.True(t, ok)

	schemes, ok := components["securitySchemes"].(map[string]any)
	require.True(t, ok)

	bearer, ok := schemes["bearerAuth"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "http", bearer["type"])
	assert.Equal(t, "bearer", bearer["scheme"])
	assert.Equal(t, "JWT", bearer["bearerFormat"])
}

func TestSpecHasSchemas(t *testing.T) {
	m, err := SpecMap()
	require.NoError(t, err)

	components, ok := m["components"].(map[string]any)
	require.True(t, ok)

	schemas, ok := components["schemas"].(map[string]any)
	require.True(t, ok)

	expected := []string{
		"Error",
		"RegisterRequest",
		"RegisterResponse",
		"LoginRequest",
		"LoginResponse",
		"RefreshRequest",
		"AgentResponse",
		"CreateAgentRequest",
		"UpdateAgentRequest",
		"Plan",
		"PlanLimits",
		"UsageSummary",
		"UsageEvent",
		"OverageStatus",
		"OAuthProvider",
		"IntegrationSummary",
		"UserIntegration",
		"ConnectRequest",
		"ConnectResponse",
		"IntegrationStatus",
		"AdminUserResponse",
		"PlatformStats",
		"AuditEvent",
		"DataSubjectRequest",
		"RetentionPolicy",
		"RateLimitStatus",
		"DetailedHealthResponse",
		"ComponentHealth",
	}
	for _, name := range expected {
		assert.Contains(t, schemas, name, "schema %q should exist", name)
	}
}

func TestSpecHasResponses(t *testing.T) {
	m, err := SpecMap()
	require.NoError(t, err)

	components := m["components"].(map[string]any)
	responses, ok := components["responses"].(map[string]any)
	require.True(t, ok)

	expected := []string{"BadRequest", "Unauthorized", "Forbidden", "NotFound", "ServiceUnavailable"}
	for _, name := range expected {
		assert.Contains(t, responses, name, "response %q should exist", name)
	}
}

func TestSpecServers(t *testing.T) {
	m, err := SpecMap()
	require.NoError(t, err)

	servers, ok := m["servers"].([]any)
	require.True(t, ok)
	assert.Len(t, servers, 2)

	// Local dev server.
	s0, ok := servers[0].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, s0["url"], "localhost")

	// Production server.
	s1, ok := servers[1].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, s1["url"], "operator.onl")
}

func TestSpecTagDefinitions(t *testing.T) {
	m, err := SpecMap()
	require.NoError(t, err)

	tags, ok := m["tags"].([]any)
	require.True(t, ok)
	assert.GreaterOrEqual(t, len(tags), 10, "should have at least 10 tag definitions")

	// Each tag should have name and description.
	for _, tag := range tags {
		tagMap, ok := tag.(map[string]any)
		require.True(t, ok)
		assert.Contains(t, tagMap, "name", "tag should have a name")
		assert.Contains(t, tagMap, "description", "tag should have a description")
	}
}

func TestSpecAuthEndpointsArePublic(t *testing.T) {
	m, err := SpecMap()
	require.NoError(t, err)

	paths := m["paths"].(map[string]any)

	publicPaths := []struct {
		path   string
		method string
	}{
		{"/api/v1/auth/register", "post"},
		{"/api/v1/auth/login", "post"},
		{"/api/v1/auth/refresh", "post"},
		{"/api/v1/auth/verify-email", "post"},
		{"/api/v1/auth/resend-verification", "post"},
		{"/api/v1/billing/plans", "get"},
		{"/api/v1/oauth/providers", "get"},
		{"/api/v1/oauth/callback", "get"},
		{"/api/v1/integrations", "get"},
		{"/health/live", "get"},
		{"/health/ready", "get"},
	}

	for _, pp := range publicPaths {
		pathObj, ok := paths[pp.path].(map[string]any)
		require.True(t, ok, "path %s should exist", pp.path)

		op, ok := pathObj[pp.method].(map[string]any)
		require.True(t, ok, "method %s on %s should exist", pp.method, pp.path)

		sec, exists := op["security"]
		if exists {
			secArr, ok := sec.([]any)
			if ok {
				assert.Empty(t, secArr, "%s %s should have empty security (public)", pp.method, pp.path)
			}
		}
	}
}

func TestSpecProtectedEndpointsHaveAuth(t *testing.T) {
	m, err := SpecMap()
	require.NoError(t, err)

	paths := m["paths"].(map[string]any)

	protectedPaths := []struct {
		path   string
		method string
	}{
		{"/api/v1/agents", "get"},
		{"/api/v1/agents", "post"},
		{"/api/v1/billing/usage", "get"},
		{"/api/v1/billing/checkout", "post"},
		{"/api/v1/admin/users", "get"},
		{"/api/v1/gdpr/export", "post"},
	}

	for _, pp := range protectedPaths {
		pathObj, ok := paths[pp.path].(map[string]any)
		require.True(t, ok, "path %s should exist", pp.path)

		op, ok := pathObj[pp.method].(map[string]any)
		require.True(t, ok, "method %s on %s should exist", pp.method, pp.path)

		// Protected endpoints either:
		// 1. Don't have a "security" key (inheriting global security), or
		// 2. Have a non-empty "security" array.
		sec, exists := op["security"]
		if exists {
			secArr, ok := sec.([]any)
			if ok {
				assert.NotEmpty(t, secArr, "%s %s should be protected", pp.method, pp.path)
			}
		}
		// If security key doesn't exist, the global security applies (bearer auth).
	}
}

func TestSpecOperationIDs(t *testing.T) {
	m, err := SpecMap()
	require.NoError(t, err)

	paths := m["paths"].(map[string]any)
	methods := []string{"get", "post", "put", "delete", "patch"}

	opIDs := map[string]string{}
	for path, ops := range paths {
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
			opID, ok := opMap["operationId"].(string)
			if !ok {
				t.Errorf("%s %s: missing operationId", method, path)
				continue
			}
			assert.NotEmpty(t, opID, "%s %s should have an operationId", method, path)

			// Check uniqueness.
			if existing, dup := opIDs[opID]; dup {
				t.Errorf("duplicate operationId %q: %s and %s %s", opID, existing, method, path)
			}
			opIDs[opID] = method + " " + path
		}
	}
	assert.GreaterOrEqual(t, len(opIDs), 50, "should have at least 50 unique operation IDs")
}

func TestSpecAllOperationsHaveTags(t *testing.T) {
	m, err := SpecMap()
	require.NoError(t, err)

	paths := m["paths"].(map[string]any)
	methods := []string{"get", "post", "put", "delete", "patch"}

	for path, ops := range paths {
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
			assert.True(t, ok && len(tags) > 0, "%s %s should have at least one tag", method, path)
		}
	}
}

func TestSpecAllOperationsHaveSummary(t *testing.T) {
	m, err := SpecMap()
	require.NoError(t, err)

	paths := m["paths"].(map[string]any)
	methods := []string{"get", "post", "put", "delete", "patch"}

	for path, ops := range paths {
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
			summary, ok := opMap["summary"].(string)
			assert.True(t, ok && summary != "", "%s %s should have a summary", method, path)
		}
	}
}

func TestSpecPathParameters(t *testing.T) {
	m, err := SpecMap()
	require.NoError(t, err)

	paths := m["paths"].(map[string]any)

	// Paths with {id} should have an id parameter.
	pathsWithID := []string{
		"/api/v1/agents/{id}",
		"/api/v1/billing/plans/{id}",
		"/api/v1/admin/users/{id}",
		"/api/v1/gdpr/requests/{id}",
	}

	for _, p := range pathsWithID {
		pathObj, ok := paths[p].(map[string]any)
		require.True(t, ok, "path %s should exist", p)

		// Check any operation for parameters.
		for _, method := range []string{"get", "post", "put", "delete"} {
			op, exists := pathObj[method]
			if !exists {
				continue
			}
			opMap, ok := op.(map[string]any)
			if !ok {
				continue
			}
			params, ok := opMap["parameters"].([]any)
			if !ok {
				t.Errorf("%s %s: should have parameters", method, p)
				continue
			}
			foundID := false
			for _, param := range params {
				pm, ok := param.(map[string]any)
				if !ok {
					continue
				}
				if pm["name"] == "id" && pm["in"] == "path" {
					foundID = true
					assert.Equal(t, true, pm["required"], "%s %s: id param should be required", method, p)
				}
			}
			assert.True(t, foundID, "%s %s: should have an 'id' path parameter", method, p)
			break // Only need to check one method.
		}
	}
}

func TestSpecSchemaRefsAreValid(t *testing.T) {
	m, err := SpecMap()
	require.NoError(t, err)

	components := m["components"].(map[string]any)
	schemas := components["schemas"].(map[string]any)

	// Collect all $ref values from paths.
	refs := collectRefs(m["paths"])

	for _, ref := range refs {
		// Parse schema name from $ref like "#/components/schemas/Error"
		if len(ref) < 23 {
			continue
		}
		prefix := "#/components/schemas/"
		if ref[:len(prefix)] == prefix {
			name := ref[len(prefix):]
			assert.Contains(t, schemas, name, "$ref %s points to nonexistent schema", ref)
		}
	}
}

func collectRefs(v any) []string {
	var refs []string
	switch val := v.(type) {
	case map[string]any:
		if ref, ok := val["$ref"].(string); ok {
			refs = append(refs, ref)
		}
		for _, child := range val {
			refs = append(refs, collectRefs(child)...)
		}
	case []any:
		for _, child := range val {
			refs = append(refs, collectRefs(child)...)
		}
	}
	return refs
}

func TestSpecRequestBodies(t *testing.T) {
	m, err := SpecMap()
	require.NoError(t, err)

	paths := m["paths"].(map[string]any)

	// POST endpoints that should have request bodies.
	postsWithBody := []string{
		"/api/v1/auth/register",
		"/api/v1/auth/login",
		"/api/v1/auth/refresh",
		"/api/v1/billing/checkout",
		"/api/v1/billing/change-plan",
		"/api/v1/gdpr/erase",
	}

	for _, p := range postsWithBody {
		pathObj := paths[p].(map[string]any)
		op := pathObj["post"].(map[string]any)
		assert.Contains(t, op, "requestBody", "POST %s should have a requestBody", p)
	}
}

func TestSpecGETEndpointsHaveNoRequestBody(t *testing.T) {
	m, err := SpecMap()
	require.NoError(t, err)

	paths := m["paths"].(map[string]any)

	for path, ops := range paths {
		opsMap, ok := ops.(map[string]any)
		if !ok {
			continue
		}
		getOp, exists := opsMap["get"]
		if !exists {
			continue
		}
		opMap, ok := getOp.(map[string]any)
		if !ok {
			continue
		}
		assert.NotContains(t, opMap, "requestBody", "GET %s should not have requestBody", path)
	}
}

func TestSpecResponseCodes(t *testing.T) {
	m, err := SpecMap()
	require.NoError(t, err)

	paths := m["paths"].(map[string]any)
	methods := []string{"get", "post", "put", "delete", "patch"}

	for path, ops := range paths {
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
			responses, ok := opMap["responses"].(map[string]any)
			assert.True(t, ok && len(responses) > 0, "%s %s should have at least one response", method, path)
		}
	}
}

func TestSortStrings(t *testing.T) {
	input := []string{"c", "a", "b", "d"}
	sortStrings(input)
	assert.Equal(t, []string{"a", "b", "c", "d"}, input)

	// Empty.
	sortStrings(nil)
	sortStrings([]string{})

	// Single element.
	single := []string{"x"}
	sortStrings(single)
	assert.Equal(t, []string{"x"}, single)
}

func TestSpecHealthEndpoints(t *testing.T) {
	m, err := SpecMap()
	require.NoError(t, err)

	paths := m["paths"].(map[string]any)

	healthPaths := []string{"/health/live", "/health/ready", "/health/detailed", "/health/component/{name}", "/metrics"}
	for _, p := range healthPaths {
		_, ok := paths[p]
		assert.True(t, ok, "health path %s should exist", p)
	}
}

func TestSpecGDPREndpoints(t *testing.T) {
	m, err := SpecMap()
	require.NoError(t, err)

	paths := m["paths"].(map[string]any)

	gdprPaths := []string{
		"/api/v1/gdpr/export",
		"/api/v1/gdpr/erase",
		"/api/v1/gdpr/requests",
		"/api/v1/gdpr/requests/{id}",
		"/api/v1/gdpr/retention",
	}
	for _, p := range gdprPaths {
		_, ok := paths[p]
		assert.True(t, ok, "GDPR path %s should exist", p)
	}
}

func TestSpecManagementEndpoints(t *testing.T) {
	m, err := SpecMap()
	require.NoError(t, err)

	paths := m["paths"].(map[string]any)

	mgmtPaths := []string{
		"/api/v1/manage/integrations/connect",
		"/api/v1/manage/integrations/disconnect",
		"/api/v1/manage/integrations/status",
		"/api/v1/manage/integrations/{id}/status",
		"/api/v1/manage/integrations/{id}/enable",
		"/api/v1/manage/integrations/{id}/disable",
		"/api/v1/manage/integrations/{id}/reconnect",
		"/api/v1/manage/integrations/{id}/config",
	}
	for _, p := range mgmtPaths {
		_, ok := paths[p]
		assert.True(t, ok, "management path %s should exist", p)
	}
}

func TestSpecAdminSelfProtection(t *testing.T) {
	m, err := SpecMap()
	require.NoError(t, err)

	paths := m["paths"].(map[string]any)

	// Verify admin endpoints that have self-protection document 409.
	selfProtected := []struct {
		path   string
		method string
	}{
		{"/api/v1/admin/users/{id}", "delete"},
		{"/api/v1/admin/users/{id}/suspend", "post"},
		{"/api/v1/admin/users/{id}/role", "post"},
	}

	for _, sp := range selfProtected {
		pathObj := paths[sp.path].(map[string]any)
		op := pathObj[sp.method].(map[string]any)
		responses := op["responses"].(map[string]any)
		_, has409 := responses["409"]
		assert.True(t, has409, "%s %s should document 409 for self-protection", sp.method, sp.path)
	}
}

func TestSpecStripeWebhookNoAuth(t *testing.T) {
	m, err := SpecMap()
	require.NoError(t, err)

	paths := m["paths"].(map[string]any)
	webhookPath := paths["/api/v1/billing/webhook"].(map[string]any)
	webhookOp := webhookPath["post"].(map[string]any)

	// Webhook should be public (signature-verified, not JWT-verified).
	sec, ok := webhookOp["security"].([]any)
	assert.True(t, ok && len(sec) == 0, "webhook should have empty security (uses Stripe signature)")

	// Should mention Stripe-Signature header.
	params, ok := webhookOp["parameters"].([]any)
	require.True(t, ok)
	foundSig := false
	for _, p := range params {
		pm := p.(map[string]any)
		if pm["name"] == "Stripe-Signature" {
			foundSig = true
		}
	}
	assert.True(t, foundSig, "webhook should document Stripe-Signature header")
}

func TestSpecDocsEndpoint(t *testing.T) {
	m, err := SpecMap()
	require.NoError(t, err)

	paths := m["paths"].(map[string]any)
	_, ok := paths["/api/v1/docs/openapi.json"]
	assert.True(t, ok, "docs endpoint should exist")
}
