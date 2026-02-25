package main

import (
	"strings"
	"testing"
)

func TestVaultGetAcceptsTrailingFlagsAfterKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skip subprocess CLI test in short mode")
	}

	server, _ := newSunTestServer(t, "acme", "token-vault-get")
	defer server.Close()

	_, env := setupSunAuthState(t, server.URL, "acme", "token-vault-get")
	scope := "trailing-get"

	stdout, stderr, err := runSICommand(t, env, "vault", "init", "--scope", scope)
	if err != nil {
		t.Fatalf("vault init failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	stdout, stderr, err = runSICommand(t, env, "vault", "set", "TRAILING_GET_KEY", "ok-value", "--scope", scope)
	if err != nil {
		t.Fatalf("vault set failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	stdout, stderr, err = runSICommand(t, env, "vault", "get", "TRAILING_GET_KEY", "--scope", scope, "--reveal")
	if err != nil {
		t.Fatalf("vault get failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if strings.TrimSpace(stdout) != "ok-value" {
		t.Fatalf("unexpected vault get output: %q", strings.TrimSpace(stdout))
	}
}
