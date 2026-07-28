package rsgateway

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestNormalizeEnabledProviders(t *testing.T) {
	providers, errNormalize := NormalizeEnabledProviders([]string{" CODEX ", "codex"})
	if errNormalize != nil {
		t.Fatalf("NormalizeEnabledProviders() error = %v", errNormalize)
	}
	if len(providers) != 1 || providers[0] != "codex" {
		t.Fatalf("providers = %#v, want [codex]", providers)
	}
}

func TestNormalizeEnabledProvidersRejectsUnsupportedProvider(t *testing.T) {
	if _, errNormalize := NormalizeEnabledProviders([]string{"claude"}); errNormalize == nil {
		t.Fatal("NormalizeEnabledProviders() accepted unsupported provider")
	}
}

func TestHardenConfigRejectsNonLoopbackHost(t *testing.T) {
	cfg := &config.Config{
		Host: "0.0.0.0",
		SDKConfig: config.SDKConfig{
			APIKeys: []string{"secret"},
		},
	}
	if errHarden := HardenConfig(cfg, []string{"codex"}); errHarden == nil {
		t.Fatal("HardenConfig() accepted a wildcard host")
	}
}

func TestHardenConfigEnforcesGatewayPolicy(t *testing.T) {
	cfg := &config.Config{
		Host:                   "",
		Port:                   0,
		RequestRetry:           4,
		MaxRetryCredentials:    9,
		MaxRetryInterval:       10,
		LoggingToFile:          true,
		UsageStatisticsEnabled: true,
		SaveCooldownStatus:     true,
		GeminiKey:              []config.GeminiKey{{APIKey: "gemini"}},
		ClaudeKey:              []config.ClaudeKey{{APIKey: "claude"}},
		XAIKey:                 []config.XAIKey{{APIKey: "xai"}},
		SDKConfig: config.SDKConfig{
			APIKeys:    []string{" secret ", "secret"},
			RequestLog: true,
			Streaming: config.StreamingConfig{
				BootstrapRetries: 3,
			},
		},
	}
	cfg.Plugins.Enabled = true
	cfg.Pprof.Enable = true
	cfg.RemoteManagement.SecretKey = "management"

	if errHarden := HardenConfig(cfg, []string{"codex"}); errHarden != nil {
		t.Fatalf("HardenConfig() error = %v", errHarden)
	}

	if cfg.Host != "127.0.0.1" || cfg.Port != 0 {
		t.Fatalf("listener config = %s:%d, want 127.0.0.1:0", cfg.Host, cfg.Port)
	}
	if len(cfg.APIKeys) != 1 || cfg.APIKeys[0] != "secret" {
		t.Fatalf("client keys = %#v, want one normalized key", cfg.APIKeys)
	}
	if cfg.RequestRetry != 0 || cfg.MaxRetryCredentials != 1 || cfg.MaxRetryInterval != 0 {
		t.Fatalf("retry config = %d/%d/%d", cfg.RequestRetry, cfg.MaxRetryCredentials, cfg.MaxRetryInterval)
	}
	if cfg.Streaming.BootstrapRetries != 0 {
		t.Fatalf("bootstrap retries = %d, want 0", cfg.Streaming.BootstrapRetries)
	}
	if cfg.LoggingToFile || cfg.RequestLog || cfg.UsageStatisticsEnabled || cfg.SaveCooldownStatus {
		t.Fatal("logging, request logging, usage, or cooldown persistence remained enabled")
	}
	if cfg.Plugins.Enabled || cfg.Pprof.Enable || cfg.RemoteManagement.SecretKey != "" {
		t.Fatal("plugin, pprof, or management control surface remained enabled")
	}
	if len(cfg.GeminiKey) != 0 || len(cfg.ClaudeKey) != 0 || len(cfg.XAIKey) != 0 {
		t.Fatal("credentials outside the provider allowlist remained configured")
	}
}

func TestHardenConfigRequiresExactlyOneBearerKey(t *testing.T) {
	cfg := &config.Config{Host: "127.0.0.1"}
	if errHarden := HardenConfig(cfg, []string{"codex"}); errHarden == nil {
		t.Fatal("HardenConfig() accepted a missing client bearer key")
	}
}
