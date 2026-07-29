package rsgateway

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// ReadyDescriptor is the private process handoff written after the listener binds.
type ReadyDescriptor struct {
	ProtocolVersion  int      `json:"protocol_version"`
	Build            Build    `json:"build"`
	EnabledProviders []string `json:"enabled_providers"`
	PID              int      `json:"pid"`
	Host             string   `json:"host"`
	Port             int      `json:"port"`
	ReadyAt          string   `json:"ready_at"`
}

// NewReadyDescriptor builds a descriptor from an already-bound TCP listener.
func NewReadyDescriptor(addr net.Addr, build Build, enabledProviders []string, pid int, now time.Time) (ReadyDescriptor, error) {
	tcpAddr, ok := addr.(*net.TCPAddr)
	if !ok || tcpAddr == nil {
		return ReadyDescriptor{}, fmt.Errorf("listener address %T is not TCP", addr)
	}
	if tcpAddr.Port <= 0 || tcpAddr.Port > 65535 {
		return ReadyDescriptor{}, fmt.Errorf("listener port %d is invalid", tcpAddr.Port)
	}
	if tcpAddr.IP == nil || !tcpAddr.IP.IsLoopback() {
		return ReadyDescriptor{}, fmt.Errorf("listener address %q is not loopback", tcpAddr.String())
	}
	if pid <= 0 {
		return ReadyDescriptor{}, fmt.Errorf("pid %d is invalid", pid)
	}
	return ReadyDescriptor{
		ProtocolVersion:  ProtocolVersion,
		Build:            build,
		EnabledProviders: append([]string(nil), enabledProviders...),
		PID:              pid,
		Host:             tcpAddr.IP.String(),
		Port:             tcpAddr.Port,
		ReadyAt:          now.UTC().Format(time.RFC3339Nano),
	}, nil
}

// WriteReadyDescriptor atomically publishes a private descriptor.
func WriteReadyDescriptor(path string, descriptor ReadyDescriptor) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("ready descriptor path must be absolute")
	}
	parent := filepath.Dir(path)
	if _, errParentBefore := os.Lstat(parent); errors.Is(errParentBefore, os.ErrNotExist) {
		if errMkdir := os.MkdirAll(parent, 0o700); errMkdir != nil {
			return fmt.Errorf("create ready descriptor directory: %w", errMkdir)
		}
	} else if errParentBefore != nil {
		return fmt.Errorf("inspect ready descriptor directory: %w", errParentBefore)
	}
	parentInfo, errParentStat := os.Lstat(parent)
	if errParentStat != nil {
		return fmt.Errorf("inspect ready descriptor directory: %w", errParentStat)
	}
	if parentInfo.Mode()&os.ModeSymlink != 0 || !parentInfo.IsDir() {
		return fmt.Errorf("ready descriptor directory must be a real directory")
	}
	if parentInfo.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("ready descriptor directory permissions must not allow group or other access")
	}
	if existingInfo, errExisting := os.Lstat(path); errExisting == nil {
		if existingInfo.Mode()&os.ModeSymlink != 0 || !existingInfo.Mode().IsRegular() {
			return fmt.Errorf("ready descriptor target must be a regular file")
		}
	} else if !errors.Is(errExisting, os.ErrNotExist) {
		return fmt.Errorf("inspect ready descriptor target: %w", errExisting)
	}

	tempFile, errCreate := os.CreateTemp(parent, "."+filepath.Base(path)+".tmp-*")
	if errCreate != nil {
		return fmt.Errorf("create ready descriptor temp file: %w", errCreate)
	}
	tempPath := tempFile.Name()
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if errChmod := tempFile.Chmod(0o600); errChmod != nil {
		_ = tempFile.Close()
		return fmt.Errorf("secure ready descriptor temp file: %w", errChmod)
	}
	encoder := json.NewEncoder(tempFile)
	encoder.SetEscapeHTML(false)
	if errEncode := encoder.Encode(descriptor); errEncode != nil {
		_ = tempFile.Close()
		return fmt.Errorf("encode ready descriptor: %w", errEncode)
	}
	if errSync := tempFile.Sync(); errSync != nil {
		_ = tempFile.Close()
		return fmt.Errorf("sync ready descriptor temp file: %w", errSync)
	}
	if errClose := tempFile.Close(); errClose != nil {
		return fmt.Errorf("close ready descriptor temp file: %w", errClose)
	}
	if errRename := os.Rename(tempPath, path); errRename != nil {
		return fmt.Errorf("publish ready descriptor: %w", errRename)
	}
	cleanupTemp = false
	if errChmod := os.Chmod(path, 0o600); errChmod != nil {
		return fmt.Errorf("secure ready descriptor: %w", errChmod)
	}
	if errSyncDir := syncDirectory(parent); errSyncDir != nil {
		return errSyncDir
	}
	return nil
}

// RemoveReadyDescriptor removes only a descriptor owned by pid.
func RemoveReadyDescriptor(path string, pid int) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("ready descriptor cleanup path must be absolute")
	}
	info, errStat := os.Lstat(path)
	if errors.Is(errStat, os.ErrNotExist) {
		return nil
	}
	if errStat != nil {
		return fmt.Errorf("inspect ready descriptor before cleanup: %w", errStat)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("ready descriptor cleanup target must be a regular file")
	}
	data, errRead := os.ReadFile(path)
	if errors.Is(errRead, os.ErrNotExist) {
		return nil
	}
	if errRead != nil {
		return fmt.Errorf("read ready descriptor before cleanup: %w", errRead)
	}
	var descriptor ReadyDescriptor
	if errDecode := json.Unmarshal(data, &descriptor); errDecode != nil {
		return fmt.Errorf("decode ready descriptor before cleanup: %w", errDecode)
	}
	if descriptor.PID != pid {
		return nil
	}
	if errRemove := os.Remove(path); errRemove != nil && !errors.Is(errRemove, os.ErrNotExist) {
		return fmt.Errorf("remove ready descriptor for pid %s: %w", strconv.Itoa(pid), errRemove)
	}
	return nil
}

func syncDirectory(path string) error {
	directory, errOpen := os.Open(path)
	if errOpen != nil {
		return fmt.Errorf("open ready descriptor directory: %w", errOpen)
	}
	defer func() {
		_ = directory.Close()
	}()
	if errSync := directory.Sync(); errSync != nil {
		return fmt.Errorf("sync ready descriptor directory: %w", errSync)
	}
	return nil
}
