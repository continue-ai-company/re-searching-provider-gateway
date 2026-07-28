package cliproxy

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestProviderAllowlistIsOptIn(t *testing.T) {
	service := &Service{}
	if !service.providerAllowed("codex") || !service.providerAllowed("claude") {
		t.Fatal("service without an allowlist must preserve upstream provider behavior")
	}
}

func TestProviderAllowlistNormalizesAndRejectsOtherProviders(t *testing.T) {
	service := &Service{
		providerAllowlistEnabled: true,
		providerAllowlist:        normalizeProviderAllowlist([]string{" CODEX ", "codex"}),
	}
	if !service.providerAllowed("Codex") {
		t.Fatal("Codex should be enabled")
	}
	if service.providerAllowed("claude") {
		t.Fatal("Claude should be rejected")
	}
	if len(service.providerAllowlist) != 1 {
		t.Fatalf("allowlist size = %d, want 1", len(service.providerAllowlist))
	}
}

func TestExplicitEmptyProviderAllowlistRejectsAllProviders(t *testing.T) {
	service, errBuild := NewBuilder().
		WithConfig(&config.Config{}).
		WithConfigPath(t.TempDir() + "/config.yaml").
		WithProviderAllowlist().
		Build()
	if errBuild != nil {
		t.Fatalf("Build() error = %v", errBuild)
	}
	if service.providerAllowed("codex") {
		t.Fatal("explicit empty allowlist must reject Codex")
	}
}
