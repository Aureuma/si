package docker

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSocketExists(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "not-a-socket")
	if err := os.WriteFile(filePath, []byte("data"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if socketExists(filePath) {
		t.Fatalf("expected regular file to not be detected as socket")
	}

	socketPath := filepath.Join(dir, "test.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	if !socketExists(socketPath) {
		t.Fatalf("expected unix socket to be detected")
	}
}

func TestDetectColimaHostNonDarwin(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("non-darwin behavior")
	}
	if host, ok := detectColimaHost(); ok || host != "" {
		t.Fatalf("expected no host on %s, got %q (ok=%v)", runtime.GOOS, host, ok)
	}
}

func TestDetectColimaHostDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only behavior")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)

	socketPath := filepath.Join(home, ".colima", "default", "docker.sock")
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	host, ok := detectColimaHost()
	if !ok {
		t.Fatalf("expected colima host to be detected")
	}
	if host != "unix://"+socketPath {
		t.Fatalf("expected host %q, got %q", "unix://"+socketPath, host)
	}
}
