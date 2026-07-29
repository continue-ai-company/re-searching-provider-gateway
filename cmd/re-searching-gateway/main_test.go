package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateConfigurationRejectsSymlink(t *testing.T) {
	tempDir := t.TempDir()
	target := filepath.Join(tempDir, "config.yaml")
	if errWrite := os.WriteFile(target, []byte("api-keys: [test]"), 0o600); errWrite != nil {
		t.Fatalf("write config target: %v", errWrite)
	}
	link := filepath.Join(tempDir, "config-link.yaml")
	if errSymlink := os.Symlink(target, link); errSymlink != nil {
		t.Fatalf("create config symlink: %v", errSymlink)
	}

	if errValidate := validatePrivateFile(link); errValidate == nil {
		t.Fatal("validatePrivateFile() accepted a symlink")
	}
}

func TestPrivateAuthenticationDirectoryRejectsSymlink(t *testing.T) {
	tempDir := t.TempDir()
	target := filepath.Join(tempDir, "auth-real")
	if errMkdir := os.Mkdir(target, 0o700); errMkdir != nil {
		t.Fatalf("create auth target: %v", errMkdir)
	}
	link := filepath.Join(tempDir, "auth-link")
	if errSymlink := os.Symlink(target, link); errSymlink != nil {
		t.Fatalf("create auth symlink: %v", errSymlink)
	}

	if _, errResolve := resolvePrivateAuthDir(link); errResolve == nil {
		t.Fatal("resolvePrivateAuthDir() accepted a symlink")
	}
}

func TestPrivatePathsRequireOwnerOnlyPermissions(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")
	if errWrite := os.WriteFile(configPath, []byte("api-keys: [test]"), 0o644); errWrite != nil {
		t.Fatalf("write config: %v", errWrite)
	}
	if errValidate := validatePrivateFile(configPath); errValidate == nil {
		t.Fatal("validatePrivateFile() accepted group-readable permissions")
	}

	authDir := filepath.Join(tempDir, "auth")
	if errMkdir := os.Mkdir(authDir, 0o755); errMkdir != nil {
		t.Fatalf("create auth directory: %v", errMkdir)
	}
	if _, errResolve := resolvePrivateAuthDir(authDir); errResolve == nil {
		t.Fatal("resolvePrivateAuthDir() accepted group-readable permissions")
	}
}
