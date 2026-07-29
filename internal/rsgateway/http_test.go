package rsgateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
)

func TestBearerMiddlewareProtectsCapabilitiesAndLeavesHealthPublic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(
		MetadataHeadersMiddleware(Build{Version: "v7.2.103-rs.test"}),
		BearerMiddleware([]string{"gateway-secret"}),
	)
	engine.GET("/healthz", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	RegisterCapabilitiesRoute(engine, Build{Version: "test"}, []string{"codex"})

	health := httptest.NewRecorder()
	engine.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("GET /healthz status = %d, want 200", health.Code)
	}

	unauthorized := httptest.NewRecorder()
	engine.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/rs/v1/capabilities", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized capabilities status = %d, want 401", unauthorized.Code)
	}

	queryCredential := httptest.NewRecorder()
	engine.ServeHTTP(queryCredential, httptest.NewRequest(http.MethodGet, "/rs/v1/capabilities?key=gateway-secret", nil))
	if queryCredential.Code != http.StatusUnauthorized {
		t.Fatalf("query-auth capabilities status = %d, want 401", queryCredential.Code)
	}

	authorizedRequest := httptest.NewRequest(http.MethodGet, "/rs/v1/capabilities", nil)
	authorizedRequest.Header.Set("Authorization", "Bearer gateway-secret")
	authorized := httptest.NewRecorder()
	engine.ServeHTTP(authorized, authorizedRequest)
	if authorized.Code != http.StatusOK {
		t.Fatalf("authorized capabilities status = %d, want 200", authorized.Code)
	}
	if got := authorized.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("capabilities content type = %q", got)
	}
	if got := authorized.Header().Get(HeaderGatewayVersion); got != "v7.2.103-rs.test" {
		t.Fatalf("gateway version header = %q", got)
	}
	if got := authorized.Header().Get(HeaderProtocolVersion); got != "2" {
		t.Fatalf("protocol version header = %q", got)
	}
}

func TestBearerMiddlewareFailsClosedWithoutKeys(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(BearerMiddleware(nil))
	engine.GET("/protected", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	request := httptest.NewRequest(http.MethodGet, "/protected", nil)
	request.Header.Set("Authorization", "Bearer anything")
	recorder := httptest.NewRecorder()

	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("protected status = %d, want 401", recorder.Code)
	}
}

func TestCapabilitiesProjectsProviderModelsAndReasoningEfforts(t *testing.T) {
	const clientID = "rsgateway-http-test"
	modelRegistry := registry.GetGlobalRegistry()
	modelRegistry.RegisterClient(clientID, "codex", []*registry.ModelInfo{
		{
			ID:       "gpt-5.6-sol",
			Thinking: &registry.ThinkingSupport{Levels: []string{"medium", "xhigh", "medium"}},
		},
	})
	t.Cleanup(func() {
		modelRegistry.UnregisterClient(clientID)
	})

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(BearerMiddleware([]string{"gateway-secret"}))
	RegisterCapabilitiesRoute(engine, Build{Version: "test"}, []string{"codex"})
	request := httptest.NewRequest(http.MethodGet, "/rs/v1/capabilities", nil)
	request.Header.Set("Authorization", "Bearer gateway-secret")
	recorder := httptest.NewRecorder()

	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("capabilities status = %d, want 200", recorder.Code)
	}
	var response capabilitiesResponse
	if errDecode := json.Unmarshal(recorder.Body.Bytes(), &response); errDecode != nil {
		t.Fatalf("decode capabilities: %v", errDecode)
	}
	if len(response.Providers) != 1 || response.Providers[0].ID != "codex" {
		t.Fatalf("provider capabilities = %#v", response.Providers)
	}
	if len(response.Providers[0].Models) != 1 {
		t.Fatalf("models = %#v", response.Providers[0].Models)
	}
	model := response.Providers[0].Models[0]
	if model.ID != "gpt-5.6-sol" || model.Priority <= 0 ||
		len(model.ReasoningEfforts) != 2 ||
		model.ReasoningEfforts[0] != "medium" || model.ReasoningEfforts[1] != "xhigh" {
		t.Fatalf("model capabilities = %#v", model)
	}
	if len(model.ServiceTiers) != 1 ||
		model.ServiceTiers[0].ID != "priority" ||
		model.ServiceTiers[0].Name != "Fast" {
		t.Fatalf("model service tiers = %#v", model.ServiceTiers)
	}
}

func TestResolveCodexModelCapabilitiesFiltersAndSorts(t *testing.T) {
	models := []*registry.ModelInfo{
		{
			ID:       "model-second",
			Thinking: &registry.ThinkingSupport{Levels: []string{" medium ", "xhigh", "medium"}},
		},
		{
			ID:       "model-first-b",
			Thinking: &registry.ThinkingSupport{Levels: []string{"high"}},
		},
		{
			ID:       "model-first-a",
			Thinking: &registry.ThinkingSupport{Levels: []string{"low"}},
		},
		{
			ID:       "model-without-reasoning",
			Thinking: &registry.ThinkingSupport{},
		},
		{
			ID:       "model-missing-from-catalog",
			Thinking: &registry.ThinkingSupport{Levels: []string{"medium"}},
		},
		{
			ID:       "model-zero-priority",
			Thinking: &registry.ThinkingSupport{Levels: []string{"medium"}},
		},
	}
	catalog := []byte(`{
		"models": [
			{
				"slug": "model-second",
				"priority": 2,
				"default_service_tier": "priority",
				"service_tiers": [
					{"id": "priority", "name": "Fast", "description": "1.5x speed"}
				]
			},
			{"slug": "model-first-b", "priority": 1},
			{"slug": "model-first-a", "priority": 1},
			{"slug": "model-without-reasoning", "priority": 3},
			{"slug": "model-zero-priority", "priority": 0}
		]
	}`)

	got := resolveCodexModelCapabilitiesFromCatalog(models, catalog)

	if len(got) != 3 {
		t.Fatalf("resolved model count = %d, want 3: %#v", len(got), got)
	}
	if got[0].ID != "model-first-a" || got[0].Priority != 1 {
		t.Fatalf("first model = %#v", got[0])
	}
	if got[1].ID != "model-first-b" || got[1].Priority != 1 {
		t.Fatalf("second model = %#v", got[1])
	}
	if got[2].ID != "model-second" || got[2].Priority != 2 {
		t.Fatalf("third model = %#v", got[2])
	}
	if len(got[2].ReasoningEfforts) != 2 ||
		got[2].ReasoningEfforts[0] != "medium" ||
		got[2].ReasoningEfforts[1] != "xhigh" {
		t.Fatalf("normalized efforts = %#v", got[2].ReasoningEfforts)
	}
	if got[2].DefaultServiceTier == nil || *got[2].DefaultServiceTier != "priority" {
		t.Fatalf("default service tier = %#v", got[2].DefaultServiceTier)
	}
	if len(got[2].ServiceTiers) != 1 ||
		got[2].ServiceTiers[0].Description != "1.5x speed" {
		t.Fatalf("service tiers = %#v", got[2].ServiceTiers)
	}
}

func TestResolveCodexModelCapabilitiesFailsClosedForInvalidOrAmbiguousCatalog(t *testing.T) {
	models := []*registry.ModelInfo{
		{
			ID:       "duplicate",
			Thinking: &registry.ThinkingSupport{Levels: []string{"medium"}},
		},
	}

	if got := resolveCodexModelCapabilitiesFromCatalog(models, []byte(`not-json`)); len(got) != 0 {
		t.Fatalf("invalid catalog resolved models = %#v", got)
	}
	duplicateCatalog := []byte(`{
		"models": [
			{"slug": "duplicate", "priority": 1},
			{"slug": "duplicate", "priority": 2}
		]
	}`)
	if got := resolveCodexModelCapabilitiesFromCatalog(models, duplicateCatalog); len(got) != 0 {
		t.Fatalf("ambiguous catalog resolved models = %#v", got)
	}
}
