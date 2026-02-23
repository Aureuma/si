package docker

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestDockerSocketPathUsesDefaultForColimaDockerHost(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DOCKER_HOST", "unix://"+filepath.Join(home, ".colima", "default", "docker.sock"))

	got, ok := dockerSocketPath()
	if !ok {
		t.Fatalf("expected docker socket path to resolve")
	}
	if got != defaultDockerSocketPath {
		t.Fatalf("expected colima docker host to resolve to %q, got %q", defaultDockerSocketPath, got)
	}
}

func TestDockerSocketPathUsesExplicitUnixHostWhenNotColima(t *testing.T) {
	socketPath := filepath.Join("/tmp", fmt.Sprintf("si-docker-socket-%d.sock", os.Getpid()))
	_ = os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	t.Cleanup(func() {
		_ = listener.Close()
		_ = os.Remove(socketPath)
	})

	t.Setenv("DOCKER_HOST", "unix://"+socketPath)

	got, ok := dockerSocketPath()
	if !ok {
		t.Fatalf("expected docker socket path to resolve")
	}
	if got != socketPath {
		t.Fatalf("expected explicit docker host path %q, got %q", socketPath, got)
	}
}

func TestIsColimaUnixSocketPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	colimaPath := filepath.Join(home, ".colima", "work", "docker.sock")
	if !isColimaUnixSocketPath(colimaPath) {
		t.Fatalf("expected colima socket path to be detected")
	}

	other := filepath.Join(home, ".docker", "docker.sock")
	if isColimaUnixSocketPath(other) {
		t.Fatalf("expected non-colima socket path to be ignored")
	}

	t.Setenv("COLIMA_HOME", filepath.Join(home, "custom-colima"))
	custom := filepath.Join(home, "custom-colima", "prod", "docker.sock")
	if !isColimaUnixSocketPath(custom) {
		t.Fatalf("expected custom COLIMA_HOME socket path to be detected")
	}
}
