package rsgateway

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadyDescriptorAtomicWriteAndOwnedCleanup(t *testing.T) {
	build := Build{
		Version:         "v7.2.103-rs.1",
		Commit:          "gateway-commit",
		BuiltAt:         "2026-07-28T00:00:00Z",
		UpstreamVersion: "v7.2.103",
		UpstreamCommit:  "cade44b9",
	}
	descriptor, errDescriptor := NewReadyDescriptor(
		&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 32123},
		build,
		[]string{"codex"},
		4242,
		time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC),
	)
	if errDescriptor != nil {
		t.Fatalf("NewReadyDescriptor() error = %v", errDescriptor)
	}

	path := filepath.Join(t.TempDir(), "private", "ready.json")
	if errWrite := WriteReadyDescriptor(path, descriptor); errWrite != nil {
		t.Fatalf("WriteReadyDescriptor() error = %v", errWrite)
	}
	info, errStat := os.Stat(path)
	if errStat != nil {
		t.Fatalf("stat ready descriptor: %v", errStat)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("descriptor mode = %o, want 600", got)
	}
	parentInfo, errParentStat := os.Stat(filepath.Dir(path))
	if errParentStat != nil {
		t.Fatalf("stat descriptor parent: %v", errParentStat)
	}
	if got := parentInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("descriptor parent mode = %o, want 700", got)
	}

	data, errRead := os.ReadFile(path)
	if errRead != nil {
		t.Fatalf("read ready descriptor: %v", errRead)
	}
	var decoded ReadyDescriptor
	if errDecode := json.Unmarshal(data, &decoded); errDecode != nil {
		t.Fatalf("decode ready descriptor: %v", errDecode)
	}
	if decoded.Port != 32123 || decoded.PID != 4242 || decoded.ProtocolVersion != ProtocolVersion {
		t.Fatalf("decoded descriptor = %#v", decoded)
	}

	if errRemoveOther := RemoveReadyDescriptor(path, 9999); errRemoveOther != nil {
		t.Fatalf("mismatched cleanup error = %v", errRemoveOther)
	}
	if _, errStillExists := os.Stat(path); errStillExists != nil {
		t.Fatalf("mismatched cleanup removed descriptor: %v", errStillExists)
	}
	if errRemoveOwner := RemoveReadyDescriptor(path, 4242); errRemoveOwner != nil {
		t.Fatalf("owned cleanup error = %v", errRemoveOwner)
	}
	if _, errRemoved := os.Stat(path); !os.IsNotExist(errRemoved) {
		t.Fatalf("owned cleanup left descriptor: %v", errRemoved)
	}
}

func TestReadyDescriptorRejectsUnsafeInputs(t *testing.T) {
	if _, errDescriptor := NewReadyDescriptor(
		&net.TCPAddr{IP: net.ParseIP("0.0.0.0"), Port: 1234},
		Build{},
		[]string{"codex"},
		1,
		time.Now(),
	); errDescriptor == nil {
		t.Fatal("NewReadyDescriptor() accepted a wildcard address")
	}
	if errWrite := WriteReadyDescriptor("relative.json", ReadyDescriptor{}); errWrite == nil {
		t.Fatal("WriteReadyDescriptor() accepted a relative path")
	}
}

func TestReadyDescriptorRejectsSymlinkTargetAndParent(t *testing.T) {
	tempDir := t.TempDir()
	target := filepath.Join(tempDir, "target.json")
	if errWriteTarget := os.WriteFile(target, []byte(`{"pid":1}`), 0o600); errWriteTarget != nil {
		t.Fatalf("write target: %v", errWriteTarget)
	}
	symlinkPath := filepath.Join(tempDir, "ready.json")
	if errSymlink := os.Symlink(target, symlinkPath); errSymlink != nil {
		t.Fatalf("create descriptor symlink: %v", errSymlink)
	}
	if errWrite := WriteReadyDescriptor(symlinkPath, ReadyDescriptor{PID: 1}); errWrite == nil {
		t.Fatal("WriteReadyDescriptor() accepted a symlink target")
	}
	if errRemove := RemoveReadyDescriptor(symlinkPath, 1); errRemove == nil {
		t.Fatal("RemoveReadyDescriptor() accepted a symlink target")
	}
	if _, errTarget := os.Stat(target); errTarget != nil {
		t.Fatalf("symlink rejection modified target: %v", errTarget)
	}

	realParent := filepath.Join(tempDir, "real-parent")
	if errMkdir := os.Mkdir(realParent, 0o700); errMkdir != nil {
		t.Fatalf("create real parent: %v", errMkdir)
	}
	symlinkParent := filepath.Join(tempDir, "linked-parent")
	if errSymlink := os.Symlink(realParent, symlinkParent); errSymlink != nil {
		t.Fatalf("create parent symlink: %v", errSymlink)
	}
	if errWrite := WriteReadyDescriptor(
		filepath.Join(symlinkParent, "ready.json"),
		ReadyDescriptor{PID: 1},
	); errWrite == nil {
		t.Fatal("WriteReadyDescriptor() accepted a symlink parent")
	}
}

func TestReadyDescriptorRejectsNonPrivateExistingParent(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "public-parent")
	if errMkdir := os.Mkdir(parent, 0o755); errMkdir != nil {
		t.Fatalf("create public parent: %v", errMkdir)
	}
	if errWrite := WriteReadyDescriptor(
		filepath.Join(parent, "ready.json"),
		ReadyDescriptor{PID: 1},
	); errWrite == nil {
		t.Fatal("WriteReadyDescriptor() accepted a group-readable parent")
	}
	info, errStat := os.Stat(parent)
	if errStat != nil {
		t.Fatalf("stat public parent: %v", errStat)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("WriteReadyDescriptor() changed parent mode to %o", got)
	}
}
