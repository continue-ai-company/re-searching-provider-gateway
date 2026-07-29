package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/api"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/buildinfo"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/logging"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/rsgateway"
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/translator"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/util"
	apihandlers "github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers"
	sdkAuth "github.com/router-for-me/CLIProxyAPI/v7/sdk/auth"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy"
	log "github.com/sirupsen/logrus"
)

const (
	upstreamVersion = "v7.2.103"
	upstreamCommit  = "cade44b9cdee6b9328ea2648fd119129fdf11e2d"
)

var (
	Version   = "v7.2.103-rs.dev"
	Commit    = "none"
	BuildDate = "unknown"
)

func init() {
	logging.SetupBaseLogger()
	buildinfo.Version = Version
	buildinfo.Commit = Commit
	buildinfo.BuildDate = BuildDate
}

func main() {
	if errRun := run(os.Args[1:]); errRun != nil {
		log.WithError(errRun).Error("Re-Searching Provider Gateway stopped")
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("re-searching-provider-gateway", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	configPathFlag := flags.String("config", "", "Path to the private gateway YAML configuration")
	readyPathFlag := flags.String("ready-descriptor", "", "Absolute path for the private ready descriptor")
	providersFlag := flags.String("enabled-providers", "codex", "Comma-separated provider allowlist")
	showVersion := flags.Bool("version", false, "Print build metadata and exit")
	if errParse := flags.Parse(args); errParse != nil {
		return errParse
	}
	if *showVersion {
		fmt.Printf(
			"Re-Searching Provider Gateway %s (%s), upstream %s (%s), built %s\n",
			Version,
			Commit,
			upstreamVersion,
			upstreamCommit,
			BuildDate,
		)
		return nil
	}
	if strings.TrimSpace(*configPathFlag) == "" {
		return fmt.Errorf("--config is required")
	}
	if strings.TrimSpace(*readyPathFlag) == "" {
		return fmt.Errorf("--ready-descriptor is required")
	}

	configPath, errConfigAbs := filepath.Abs(*configPathFlag)
	if errConfigAbs != nil {
		return fmt.Errorf("resolve configuration path: %w", errConfigAbs)
	}
	readyPath := filepath.Clean(*readyPathFlag)
	if !filepath.IsAbs(readyPath) {
		return fmt.Errorf("--ready-descriptor must be absolute")
	}
	if errPrivate := validatePrivateFile(configPath); errPrivate != nil {
		return errPrivate
	}
	rawConfig, errReadConfig := os.ReadFile(configPath)
	if errReadConfig != nil {
		return fmt.Errorf("read configuration: %w", errReadConfig)
	}
	cfg, errParseConfig := config.ParseConfigBytes(rawConfig)
	if errParseConfig != nil {
		return fmt.Errorf("parse configuration: %w", errParseConfig)
	}
	enabledProviders, errProviders := rsgateway.NormalizeEnabledProviders(strings.Split(*providersFlag, ","))
	if errProviders != nil {
		return errProviders
	}
	if errHarden := rsgateway.HardenConfig(cfg, enabledProviders); errHarden != nil {
		return fmt.Errorf("apply gateway policy: %w", errHarden)
	}
	authDir, errAuthDir := resolvePrivateAuthDir(cfg.AuthDir)
	if errAuthDir != nil {
		return errAuthDir
	}
	cfg.AuthDir = authDir

	fixedBearer := cfg.APIKeys[0]
	fixedPort := cfg.Port
	configPolicy := func(candidate *config.Config) error {
		if errHarden := rsgateway.HardenConfig(candidate, enabledProviders); errHarden != nil {
			return errHarden
		}
		candidateAuthDir, errResolve := resolvePrivateAuthDir(candidate.AuthDir)
		if errResolve != nil {
			return fmt.Errorf("authentication directory rejected")
		}
		if candidateAuthDir != authDir {
			return fmt.Errorf("authentication directory is immutable")
		}
		if candidate.Port != fixedPort {
			return fmt.Errorf("listener port is immutable")
		}
		if len(candidate.APIKeys) != 1 || candidate.APIKeys[0] != fixedBearer {
			return fmt.Errorf("client bearer key is immutable")
		}
		candidate.AuthDir = authDir
		return nil
	}

	build := rsgateway.Build{
		Version:         Version,
		Commit:          Commit,
		BuiltAt:         BuildDate,
		UpstreamVersion: upstreamVersion,
		UpstreamCommit:  upstreamCommit,
	}
	pid := os.Getpid()
	serverOptions := []api.ServerOption{
		api.WithRouteAllowlist(
			api.Route{Method: http.MethodGet, Path: "/healthz"},
			api.Route{Method: http.MethodHead, Path: "/healthz"},
			api.Route{Method: http.MethodGet, Path: "/rs/v1/capabilities"},
			api.Route{Method: http.MethodGet, Path: "/v1/models"},
			api.Route{Method: http.MethodPost, Path: "/v1/responses"},
		),
		api.WithMiddleware(
			rsgateway.MetadataHeadersMiddleware(build),
			rsgateway.BearerMiddleware(cfg.APIKeys),
		),
		api.WithRouterConfigurator(func(engine *gin.Engine, _ *apihandlers.BaseAPIHandler, _ *config.Config) {
			rsgateway.RegisterCapabilitiesRoute(engine, build, enabledProviders)
		}),
		api.WithListeningHook(func(addr net.Addr) error {
			descriptor, errDescriptor := rsgateway.NewReadyDescriptor(
				addr,
				build,
				enabledProviders,
				pid,
				time.Now(),
			)
			if errDescriptor != nil {
				return errDescriptor
			}
			return rsgateway.WriteReadyDescriptor(readyPath, descriptor)
		}),
	}

	sdkAuth.RegisterTokenStore(sdkAuth.NewFileTokenStore())
	service, errBuild := cliproxy.NewBuilder().
		WithConfig(cfg).
		WithConfigPath(configPath).
		WithProviderAllowlist(enabledProviders...).
		WithConfigPolicy(configPolicy).
		WithRestrictedRuntime().
		WithServerOptions(serverOptions...).
		Build()
	if errBuild != nil {
		return fmt.Errorf("build gateway service: %w", errBuild)
	}

	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	errRun := service.Run(ctx)
	errCleanup := rsgateway.RemoveReadyDescriptor(readyPath, pid)
	if errRun != nil && !errors.Is(errRun, context.Canceled) {
		return errRun
	}
	return errCleanup
}

func validatePrivateFile(path string) error {
	if errParent := validatePrivateDirectory(filepath.Dir(path), "configuration parent"); errParent != nil {
		return errParent
	}
	info, errStat := os.Lstat(path)
	if errStat != nil {
		return fmt.Errorf("stat private configuration: %w", errStat)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("private configuration must not be a symbolic link")
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("private configuration is not a regular file")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("private configuration permissions must not allow group or other access")
	}
	return nil
}

func resolvePrivateAuthDir(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("private authentication directory is required")
	}
	resolved, errResolve := util.ResolveAuthDir(path)
	if errResolve != nil {
		return "", fmt.Errorf("resolve private authentication directory: %w", errResolve)
	}
	if errParent := validatePrivateDirectory(filepath.Dir(resolved), "authentication parent"); errParent != nil {
		return "", errParent
	}
	info, errStat := os.Lstat(resolved)
	if errStat != nil {
		return "", fmt.Errorf("stat private authentication directory: %w", errStat)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("private authentication directory must not be a symbolic link")
	}
	if !info.IsDir() {
		return "", fmt.Errorf("private authentication path is not a directory")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("private authentication directory permissions must not allow group or other access")
	}
	return resolved, nil
}

func validatePrivateDirectory(path string, label string) error {
	info, errStat := os.Lstat(path)
	if errStat != nil {
		return fmt.Errorf("stat private %s directory: %w", label, errStat)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("private %s must be a real directory", label)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("private %s permissions must not allow group or other access", label)
	}
	return nil
}
