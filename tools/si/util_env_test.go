package main

import (
	"net"
	"path/filepath"
	"strings"
	"testing"
)

func TestHostUserEnvIncludesSSHAuthSockWhenSocketExists(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "ssh-agent.sock")
	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	t.Setenv("SSH_AUTH_SOCK", sockPath)

	env := hostUserEnv()
	if !containsEnvExact(env, "SSH_AUTH_SOCK="+sockPath) {
		t.Fatalf("expected SSH_AUTH_SOCK in host env, got %v", env)
	}
}

func TestHostUserEnvSkipsInvalidSSHAuthSock(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", filepath.Join(t.TempDir(), "missing.sock"))
	env := hostUserEnv()
	for _, item := range env {
		if strings.HasPrefix(strings.TrimSpace(item), "SSH_AUTH_SOCK=") {
			t.Fatalf("expected invalid SSH_AUTH_SOCK to be omitted, got %v", env)
		}
	}
}

func containsEnvExact(env []string, want string) bool {
	for _, item := range env {
		if strings.TrimSpace(item) == want {
			return true
		}
	}
	return false
}
