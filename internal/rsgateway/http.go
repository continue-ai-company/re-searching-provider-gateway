package rsgateway

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
)

const (
	// HeaderGatewayVersion identifies the locked Re-Searching gateway build.
	HeaderGatewayVersion = "X-Re-Searching-Gateway-Version"
	// HeaderProtocolVersion identifies the local gateway contract version.
	HeaderProtocolVersion = "X-Re-Searching-Gateway-Protocol-Version"
)

// Build identifies both the Re-Searching build and its pinned upstream source.
type Build struct {
	Version         string `json:"version"`
	Commit          string `json:"commit"`
	BuiltAt         string `json:"built_at"`
	UpstreamVersion string `json:"upstream_version"`
	UpstreamCommit  string `json:"upstream_commit"`
}

type capabilitiesResponse struct {
	ProtocolVersion     int                    `json:"protocol_version"`
	Build               Build                  `json:"build"`
	EnabledProviders    []string               `json:"enabled_providers"`
	Responses           bool                   `json:"responses"`
	StrictJSONSchema    bool                   `json:"strict_json_schema"`
	ModelCatalog        bool                   `json:"model_catalog"`
	MaxParallelRequests int                    `json:"max_parallel_requests"`
	Providers           []providerCapabilities `json:"providers"`
}

type providerCapabilities struct {
	ID               string              `json:"id"`
	Responses        bool                `json:"responses"`
	StrictJSONSchema bool                `json:"strict_json_schema"`
	Models           []modelCapabilities `json:"models"`
}

type modelCapabilities struct {
	ID                 string                    `json:"id"`
	Priority           int                       `json:"priority"`
	ReasoningEfforts   []string                  `json:"reasoning_efforts"`
	DefaultServiceTier *string                   `json:"default_service_tier"`
	ServiceTiers       []serviceTierCapabilities `json:"service_tiers"`
}

type providerModelCapabilityResolver func([]*registry.ModelInfo) []modelCapabilities

type codexClientModelPriorityCatalog struct {
	Models []struct {
		Slug               string  `json:"slug"`
		Priority           int     `json:"priority"`
		DefaultServiceTier *string `json:"default_service_tier"`
		ServiceTiers       []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"service_tiers"`
	} `json:"models"`
}

type serviceTierCapabilities struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// MetadataHeadersMiddleware adds gateway provenance without changing the
// provider-compatible response body.
func MetadataHeadersMiddleware(build Build) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header(HeaderGatewayVersion, build.Version)
		c.Header(HeaderProtocolVersion, strconv.Itoa(ProtocolVersion))
		c.Next()
	}
}

// BearerMiddleware requires the configured bearer token on every endpoint
// except the deliberately non-sensitive health check.
func BearerMiddleware(keys []string) gin.HandlerFunc {
	normalizedKeys := normalizeAPIKeys(keys)
	return func(c *gin.Context) {
		if c.Request != nil && c.Request.URL != nil &&
			c.Request.URL.Path == "/healthz" &&
			(c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead) {
			c.Next()
			return
		}
		token := bearerToken(c.GetHeader("Authorization"))
		if !matchesAnyToken(token, normalizedKeys) {
			c.Header("WWW-Authenticate", "Bearer")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "unauthorized",
			})
			return
		}
		c.Next()
	}
}

// RegisterCapabilitiesRoute adds the authenticated gateway metadata endpoint.
func RegisterCapabilitiesRoute(engine *gin.Engine, build Build, enabledProviders []string) {
	if engine == nil {
		return
	}
	providers := append([]string(nil), enabledProviders...)
	engine.GET("/rs/v1/capabilities", func(c *gin.Context) {
		c.JSON(http.StatusOK, capabilitiesResponse{
			ProtocolVersion:     ProtocolVersion,
			Build:               build,
			EnabledProviders:    providers,
			Responses:           true,
			StrictJSONSchema:    true,
			ModelCatalog:        true,
			MaxParallelRequests: MaxParallelRequests,
			Providers:           currentProviderCapabilities(providers),
		})
	})
}

func currentProviderCapabilities(enabledProviders []string) []providerCapabilities {
	result := make([]providerCapabilities, 0, len(enabledProviders))
	modelRegistry := registry.GetGlobalRegistry()
	for _, provider := range enabledProviders {
		provider = strings.ToLower(strings.TrimSpace(provider))
		if provider == "" {
			continue
		}
		models := modelRegistry.GetAvailableModelsByProvider(provider)
		projectedModels := make([]modelCapabilities, 0)
		if resolver := providerModelCapabilityResolverFor(provider); resolver != nil {
			projectedModels = resolver(models)
		}
		result = append(result, providerCapabilities{
			ID:               provider,
			Responses:        true,
			StrictJSONSchema: true,
			Models:           projectedModels,
		})
	}
	return result
}

func providerModelCapabilityResolverFor(provider string) providerModelCapabilityResolver {
	switch provider {
	case "codex":
		return resolveCodexModelCapabilities
	default:
		return nil
	}
}

func resolveCodexModelCapabilities(models []*registry.ModelInfo) []modelCapabilities {
	return resolveCodexModelCapabilitiesFromCatalog(models, registry.GetCodexClientModelsJSON())
}

func resolveCodexModelCapabilitiesFromCatalog(
	models []*registry.ModelInfo,
	catalogJSON []byte,
) []modelCapabilities {
	result := make([]modelCapabilities, 0, len(models))
	var catalog codexClientModelPriorityCatalog
	if err := json.Unmarshal(catalogJSON, &catalog); err != nil {
		return result
	}

	catalogBySlug := make(map[string]modelCapabilities, len(catalog.Models))
	ambiguousSlugs := make(map[string]struct{})
	for _, catalogModel := range catalog.Models {
		slug := strings.TrimSpace(catalogModel.Slug)
		if slug == "" || catalogModel.Priority <= 0 {
			continue
		}
		if _, exists := catalogBySlug[slug]; exists {
			delete(catalogBySlug, slug)
			ambiguousSlugs[slug] = struct{}{}
			continue
		}
		if _, ambiguous := ambiguousSlugs[slug]; ambiguous {
			continue
		}
		serviceTiers := normalizeServiceTiers(catalogModel.ServiceTiers)
		defaultServiceTier := normalizeOptionalServiceTier(
			catalogModel.DefaultServiceTier,
			serviceTiers,
		)
		catalogBySlug[slug] = modelCapabilities{
			ID:                 slug,
			Priority:           catalogModel.Priority,
			DefaultServiceTier: defaultServiceTier,
			ServiceTiers:       serviceTiers,
		}
	}

	for _, model := range models {
		if model == nil {
			continue
		}
		modelID := strings.TrimSpace(model.ID)
		catalogModel, found := catalogBySlug[modelID]
		if modelID == "" || !found || catalogModel.Priority <= 0 {
			continue
		}
		efforts := make([]string, 0)
		if model.Thinking != nil {
			efforts = append(efforts, model.Thinking.Levels...)
		}
		efforts = normalizeReasoningEfforts(efforts)
		if len(efforts) == 0 {
			continue
		}
		result = append(result, modelCapabilities{
			ID:                 modelID,
			Priority:           catalogModel.Priority,
			ReasoningEfforts:   efforts,
			DefaultServiceTier: catalogModel.DefaultServiceTier,
			ServiceTiers:       catalogModel.ServiceTiers,
		})
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].Priority != result[right].Priority {
			return result[left].Priority < result[right].Priority
		}
		return result[left].ID < result[right].ID
	})
	return result
}

func normalizeServiceTiers(raw []struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}) []serviceTierCapabilities {
	result := make([]serviceTierCapabilities, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, tier := range raw {
		id := strings.TrimSpace(tier.ID)
		name := strings.TrimSpace(tier.Name)
		description := strings.TrimSpace(tier.Description)
		if id == "" || name == "" || description == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, serviceTierCapabilities{
			ID:          id,
			Name:        name,
			Description: description,
		})
	}
	return result
}

func normalizeOptionalServiceTier(
	raw *string,
	tiers []serviceTierCapabilities,
) *string {
	if raw == nil {
		return nil
	}
	value := strings.TrimSpace(*raw)
	if value == "" {
		return nil
	}
	for _, tier := range tiers {
		if tier.ID == value {
			return &value
		}
	}
	return nil
}

func normalizeReasoningEfforts(efforts []string) []string {
	result := make([]string, 0, len(efforts))
	seen := make(map[string]struct{}, len(efforts))
	for _, effort := range efforts {
		effort = strings.ToLower(strings.TrimSpace(effort))
		if effort == "" {
			continue
		}
		if _, ok := seen[effort]; ok {
			continue
		}
		seen[effort] = struct{}{}
		result = append(result, effort)
	}
	return result
}

func bearerToken(header string) string {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}

func matchesAnyToken(token string, keys []string) bool {
	if token == "" {
		return false
	}
	matched := 0
	for _, key := range keys {
		if len(token) == len(key) {
			matched |= subtle.ConstantTimeCompare([]byte(token), []byte(key))
		}
	}
	return matched == 1
}
