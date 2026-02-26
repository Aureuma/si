package docker

import (
	"strings"
	"testing"
)

func TestBuildDyadEnvForwardsBrowserMCPOverrides(t *testing.T) {
	t.Setenv("SI_BROWSER_MCP_URL_INTERNAL", "http://si-browser-mcp:8931/mcp")
	t.Setenv("SI_BROWSER_CONTAINER", "si-browser-mcp")
	t.Setenv("SI_BROWSER_MCP_PORT", "8931")

	env := buildDyadEnv(DyadOptions{
		Dyad:       "browser-env",
		Role:       "generic",
		CodexModel: "gpt-5.2-codex",
	}, "actor", "medium")

	for _, expected := range []string{
		"SI_BROWSER_MCP_URL_INTERNAL=http://si-browser-mcp:8931/mcp",
		"SI_BROWSER_CONTAINER=si-browser-mcp",
		"SI_BROWSER_MCP_PORT=8931",
	} {
		if !containsEnv(env, expected) {
			t.Fatalf("buildDyadEnv missing %q in env: %v", expected, env)
		}
	}
}

func TestBuildDyadEnvForwardsSSHAuthSock(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "/tmp/agent.sock")

	env := buildDyadEnv(DyadOptions{
		Dyad:       "ssh-env",
		Role:       "generic",
		CodexModel: "gpt-5.2-codex",
	}, "actor", "medium")
	if !containsEnv(env, "SSH_AUTH_SOCK=/tmp/agent.sock") {
		t.Fatalf("buildDyadEnv missing SSH_AUTH_SOCK forwarding in env: %v", env)
	}
}

func containsEnv(env []string, want string) bool {
	for _, item := range env {
		if strings.TrimSpace(item) == want {
			return true
		}
	}
	return false
}
