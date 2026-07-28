package cliproxy

import (
	"errors"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestBuilderConfigPolicyIsOptIn(t *testing.T) {
	cfg := &config.Config{}
	service, errBuild := NewBuilder().
		WithConfig(cfg).
		WithConfigPath(t.TempDir() + "/config.yaml").
		Build()
	if errBuild != nil {
		t.Fatalf("Build() error = %v", errBuild)
	}
	if service.configPolicy != nil {
		t.Fatal("default builder unexpectedly installed a config policy")
	}
}

func TestBuilderConfigPolicyRejectsInitialConfiguration(t *testing.T) {
	wantErr := errors.New("unsafe host")
	_, errBuild := NewBuilder().
		WithConfig(&config.Config{}).
		WithConfigPath(t.TempDir() + "/config.yaml").
		WithConfigPolicy(func(*config.Config) error {
			return wantErr
		}).
		Build()
	if errBuild == nil || !errors.Is(errBuild, wantErr) {
		t.Fatalf("Build() error = %v, want wrapped %v", errBuild, wantErr)
	}
}

func TestWatcherConfigPolicyHardensBeforeCommit(t *testing.T) {
	service := &Service{
		cfg: &config.Config{Host: "127.0.0.1"},
		configPolicy: func(cfg *config.Config) error {
			cfg.Host = "127.0.0.1"
			cfg.RequestRetry = 0
			return nil
		},
	}
	update := &config.Config{Host: "0.0.0.0", RequestRetry: 5}

	service.applyWatcherConfigUpdate(update)

	if service.cfg.Host != "127.0.0.1" || service.cfg.RequestRetry != 0 {
		t.Fatalf("committed config = host %q retry %d", service.cfg.Host, service.cfg.RequestRetry)
	}
}
