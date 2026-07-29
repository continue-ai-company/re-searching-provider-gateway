package rsgateway

import (
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

const (
	// ProtocolVersion is the local process contract consumed by Re-Searching.
	ProtocolVersion = 2
	// MaxParallelRequests is the supported concurrent request acceptance target.
	MaxParallelRequests = 8
)

var supportedProviders = map[string]struct{}{
	"codex": {},
}

// NormalizeEnabledProviders validates and canonicalizes the provider allowlist.
func NormalizeEnabledProviders(providers []string) ([]string, error) {
	normalized := make(map[string]struct{}, len(providers))
	for _, provider := range providers {
		provider = strings.ToLower(strings.TrimSpace(provider))
		if provider == "" {
			continue
		}
		if _, ok := supportedProviders[provider]; !ok {
			return nil, fmt.Errorf("provider %q is not supported by this gateway build", provider)
		}
		normalized[provider] = struct{}{}
	}
	if len(normalized) == 0 {
		return nil, fmt.Errorf("at least one enabled provider is required")
	}
	result := make([]string, 0, len(normalized))
	for provider := range normalized {
		result = append(result, provider)
	}
	sort.Strings(result)
	return result, nil
}

// HardenConfig enforces the local-only Re-Searching runtime policy.
func HardenConfig(cfg *config.Config, enabledProviders []string) error {
	if cfg == nil {
		return fmt.Errorf("configuration is required")
	}
	providers, errProviders := NormalizeEnabledProviders(enabledProviders)
	if errProviders != nil {
		return errProviders
	}
	if errHost := validateLoopbackHost(cfg.Host); errHost != nil {
		return errHost
	}
	if cfg.Port < 0 || cfg.Port > 65535 {
		return fmt.Errorf("port %d is outside the valid range", cfg.Port)
	}
	apiKeys := normalizeAPIKeys(cfg.APIKeys)
	if len(apiKeys) != 1 {
		return fmt.Errorf("exactly one non-empty client bearer key is required")
	}

	cfg.Host = "127.0.0.1"
	cfg.APIKeys = apiKeys
	cfg.RequestRetry = 0
	cfg.MaxRetryCredentials = 1
	cfg.MaxRetryInterval = 0
	cfg.Streaming.BootstrapRetries = 0
	cfg.LoggingToFile = false
	cfg.RequestLog = false
	cfg.UsageStatisticsEnabled = false
	cfg.SaveCooldownStatus = false
	cfg.CommercialMode = true
	cfg.TLS.Enable = false
	cfg.Pprof.Enable = false
	cfg.RemoteManagement = config.RemoteManagement{
		AllowRemote:            false,
		DisableControlPanel:    true,
		DisableAutoUpdatePanel: true,
	}
	cfg.Plugins.Enabled = false
	cfg.Plugins.StoreSources = nil
	cfg.Plugins.StoreAuth = nil
	cfg.Plugins.Configs = nil
	cfg.Home.Enabled = false
	cfg.Home.NodeID = ""
	cfg.Home.Host = ""
	cfg.Home.Port = 0
	cfg.Codex.LiveMediaRelay.Enabled = false

	allowed := make(map[string]struct{}, len(providers))
	for _, provider := range providers {
		allowed[provider] = struct{}{}
	}
	if _, ok := allowed["codex"]; !ok {
		cfg.CodexKey = nil
	}
	cfg.GeminiKey = nil
	cfg.InteractionsKey = nil
	cfg.VertexCompatAPIKey = nil
	cfg.ClaudeKey = nil
	cfg.XAIKey = nil
	cfg.OpenAICompatibility = nil

	return nil
}

func validateLoopbackHost(host string) error {
	host = strings.TrimSpace(host)
	if host == "" || strings.EqualFold(host, "localhost") {
		return nil
	}
	host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("host %q is not a loopback address", host)
	}
	return nil
}

func normalizeAPIKeys(keys []string) []string {
	normalized := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, key)
	}
	return normalized
}
