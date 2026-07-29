package cliproxy

import (
	"context"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/pluginhost"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestRestrictedRuntimeRegistersOnlyAllowlistedExecutor(t *testing.T) {
	oldRegisterPluginExecutors := registerPluginExecutors
	pluginCalls := 0
	registerPluginExecutors = func(*pluginhost.Host, *coreauth.Manager) {
		pluginCalls++
	}
	t.Cleanup(func() {
		registerPluginExecutors = oldRegisterPluginExecutors
	})

	service := &Service{
		cfg:                      &config.Config{},
		coreManager:              coreauth.NewManager(nil, nil, nil),
		pluginHost:               pluginhost.New(),
		providerAllowlistEnabled: true,
		providerAllowlist:        normalizeProviderAllowlist([]string{"codex"}),
		restrictedRuntime:        true,
	}

	service.ensureWebsocketGateway()
	service.registerAvailableExecutors(context.Background(), executorRegistrationOptions{
		includeBaseline: true,
		includePlugins:  true,
	})

	if service.wsGateway != nil {
		t.Fatal("restricted runtime created a websocket gateway")
	}
	if pluginCalls != 0 {
		t.Fatalf("plugin executor registration calls = %d, want 0", pluginCalls)
	}
	if executor, ok := service.coreManager.Executor("codex"); !ok || executor == nil {
		t.Fatal("Codex executor was not registered")
	}
	for _, provider := range []string{"claude", "gemini", "antigravity", "xai", "openai-compatibility"} {
		if executor, ok := service.coreManager.Executor(provider); ok || executor != nil {
			t.Fatalf("restricted runtime registered %s executor %T", provider, executor)
		}
	}
}

func TestRestrictedRuntimeSkipsModelRefreshCallback(t *testing.T) {
	registrations := 0
	service := &Service{
		restrictedRuntime: true,
		modelRefreshRegistrar: func(registry.ModelRefreshCallback) {
			registrations++
		},
	}

	service.registerModelRefreshCallback()

	if registrations != 0 {
		t.Fatalf("model refresh callback registrations = %d, want 0", registrations)
	}
}

func TestDefaultRuntimeStillRegistersModelRefreshCallback(t *testing.T) {
	registrations := 0
	service := &Service{
		modelRefreshRegistrar: func(registry.ModelRefreshCallback) {
			registrations++
		},
	}

	service.registerModelRefreshCallback()

	if registrations != 1 {
		t.Fatalf("model refresh callback registrations = %d, want 1", registrations)
	}
}

func TestRestrictedBuilderDoesNotCreatePluginHost(t *testing.T) {
	service, errBuild := NewBuilder().
		WithConfig(&config.Config{}).
		WithConfigPath(t.TempDir() + "/config.yaml").
		WithRestrictedRuntime().
		Build()
	if errBuild != nil {
		t.Fatalf("Build() error = %v", errBuild)
	}
	if service.pluginHost != nil {
		t.Fatal("restricted builder created a plugin host")
	}
}
