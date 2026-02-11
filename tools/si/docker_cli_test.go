package main

import (
	"strings"
	"testing"
)

func TestDockerCommandWithEnvUsesAutoHostWhenUnset(t *testing.T) {
	t.Setenv("DOCKER_HOST", "")
	orig := autoDockerHostFn
	autoDockerHostFn = func() (string, bool) { return "unix:///tmp/colima.sock", true }
	defer func() { autoDockerHostFn = orig }()

	cmd := dockerCommand("ps")
	joined := strings.Join(cmd.Env, "\n")
	if !strings.Contains(joined, "DOCKER_HOST=unix:///tmp/colima.sock") {
		t.Fatalf("expected auto docker host in env, got %q", joined)
	}
}

func TestDockerCommandWithEnvHonorsExplicitHost(t *testing.T) {
	t.Setenv("DOCKER_HOST", "unix:///tmp/user.sock")
	orig := autoDockerHostFn
	autoDockerHostFn = func() (string, bool) { return "unix:///tmp/colima.sock", true }
	defer func() { autoDockerHostFn = orig }()

	cmd := dockerCommand("ps")
	joined := strings.Join(cmd.Env, "\n")
	if !strings.Contains(joined, "DOCKER_HOST=unix:///tmp/user.sock") {
		t.Fatalf("expected explicit DOCKER_HOST to be preserved, got %q", joined)
	}
	if strings.Contains(joined, "DOCKER_HOST=unix:///tmp/colima.sock") {
		t.Fatalf("expected auto docker host to not override explicit host, got %q", joined)
	}
}

func TestDockerCommandWithEnvAppendsExtraEnv(t *testing.T) {
	t.Setenv("DOCKER_HOST", "")
	orig := autoDockerHostFn
	autoDockerHostFn = func() (string, bool) { return "", false }
	defer func() { autoDockerHostFn = orig }()

	cmd := dockerCommandWithEnv([]string{"DOCKER_BUILDKIT=1"}, "build", ".")
	joined := strings.Join(cmd.Env, "\n")
	if !strings.Contains(joined, "DOCKER_BUILDKIT=1") {
		t.Fatalf("expected DOCKER_BUILDKIT env in command, got %q", joined)
	}
}
