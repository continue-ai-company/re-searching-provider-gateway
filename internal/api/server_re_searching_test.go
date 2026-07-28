package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	sdkaccess "github.com/router-for-me/CLIProxyAPI/v7/sdk/access"
)

func TestRouteAllowlistAndListeningHook(t *testing.T) {
	listening := make(chan net.Addr, 1)
	cfg := &config.Config{
		Host:           "127.0.0.1",
		Port:           0,
		CommercialMode: true,
	}
	server := NewServer(
		cfg,
		nil,
		sdkaccess.NewManager(),
		t.TempDir()+"/config.yaml",
		WithRouteAllowlist(
			Route{Method: http.MethodGet, Path: "/healthz"},
			Route{Method: http.MethodHead, Path: "/healthz"},
		),
		WithListeningHook(func(addr net.Addr) error {
			listening <- addr
			return nil
		}),
	)

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Start()
	}()

	var addr net.Addr
	select {
	case addr = <-listening:
	case <-time.After(5 * time.Second):
		t.Fatal("server did not publish its listening address")
	}

	tcpAddr, ok := addr.(*net.TCPAddr)
	if !ok {
		t.Fatalf("listening address type = %T, want *net.TCPAddr", addr)
	}
	if tcpAddr.Port == 0 {
		t.Fatal("listening hook published port 0")
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", tcpAddr.Port)
	healthResponse, errHealth := http.Get(baseURL + "/healthz")
	if errHealth != nil {
		t.Fatalf("GET /healthz: %v", errHealth)
	}
	if errClose := healthResponse.Body.Close(); errClose != nil {
		t.Fatalf("close health response: %v", errClose)
	}
	if healthResponse.StatusCode != http.StatusOK {
		t.Fatalf("GET /healthz status = %d, want 200", healthResponse.StatusCode)
	}

	blockedResponse, errBlocked := http.Get(baseURL + "/")
	if errBlocked != nil {
		t.Fatalf("GET /: %v", errBlocked)
	}
	if errClose := blockedResponse.Body.Close(); errClose != nil {
		t.Fatalf("close blocked response: %v", errClose)
	}
	if blockedResponse.StatusCode != http.StatusNotFound {
		t.Fatalf("GET / status = %d, want 404", blockedResponse.StatusCode)
	}

	stopCtx, cancelStop := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelStop()
	if errStop := server.Stop(stopCtx); errStop != nil {
		t.Fatalf("stop server: %v", errStop)
	}
	if errStart := <-serverErr; errStart != nil {
		t.Fatalf("server Start returned error: %v", errStart)
	}
}

func TestListeningHookFailureStopsStartup(t *testing.T) {
	hookErr := fmt.Errorf("descriptor write failed")
	cfg := &config.Config{
		Host:           "127.0.0.1",
		Port:           0,
		CommercialMode: true,
	}
	server := NewServer(
		cfg,
		nil,
		sdkaccess.NewManager(),
		t.TempDir()+"/config.yaml",
		WithListeningHook(func(net.Addr) error {
			return hookErr
		}),
	)

	errStart := server.Start()
	if errStart == nil || !strings.Contains(errStart.Error(), hookErr.Error()) {
		t.Fatalf("Start() error = %v, want wrapped %q", errStart, hookErr)
	}
}

func TestEmptyRouteAllowlistFailsClosed(t *testing.T) {
	cfg := &config.Config{CommercialMode: true}
	server := NewServer(
		cfg,
		nil,
		sdkaccess.NewManager(),
		t.TempDir()+"/config.yaml",
		WithRouteAllowlist(Route{}),
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	server.engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("GET /healthz status = %d, want 404", recorder.Code)
	}
}

func TestDefaultServerRouteSurfaceIsUnchanged(t *testing.T) {
	cfg := &config.Config{CommercialMode: true}
	server := NewServer(
		cfg,
		nil,
		sdkaccess.NewManager(),
		t.TempDir()+"/config.yaml",
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)

	server.engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200", recorder.Code)
	}
}
