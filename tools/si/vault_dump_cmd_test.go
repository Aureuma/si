package main

import (
	"strings"
	"testing"
)

func TestVaultDumpRevealOutputsDotenvLines(t *testing.T) {
	if testing.Short() {
		t.Skip("skip subprocess CLI test in short mode")
	}

	server, _ := newSunTestServer(t, "acme", "token-vault-dump-reveal")
	defer server.Close()

	_, env := setupSunAuthState(t, server.URL, "acme", "token-vault-dump-reveal")
	scope := "dump-reveal"

	if stdout, stderr, err := runSICommand(t, env, "vault", "init", "--scope", scope); err != nil {
		t.Fatalf("vault init failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if stdout, stderr, err := runSICommand(t, env, "vault", "set", "Z_KEY", "z-value", "--scope", scope); err != nil {
		t.Fatalf("vault set Z_KEY failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if stdout, stderr, err := runSICommand(t, env, "vault", "set", "A_KEY", "hello # world", "--scope", scope); err != nil {
		t.Fatalf("vault set A_KEY failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	stdout, stderr, err := runSICommand(t, env, "vault", "dump", "--scope", scope, "--reveal")
	if err != nil {
		t.Fatalf("vault dump --reveal failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	want := []string{
		`A_KEY="hello # world"`,
		`Z_KEY=z-value`,
	}
	if len(lines) != len(want) {
		t.Fatalf("unexpected line count: got=%d want=%d\nstdout=%s", len(lines), len(want), stdout)
	}
	for i := range want {
		if strings.TrimSpace(lines[i]) != want[i] {
			t.Fatalf("unexpected dump line %d: got=%q want=%q\nstdout=%s", i, strings.TrimSpace(lines[i]), want[i], stdout)
		}
	}
}

func TestVaultDumpWithoutRevealShowsKeyStatuses(t *testing.T) {
	if testing.Short() {
		t.Skip("skip subprocess CLI test in short mode")
	}

	server, _ := newSunTestServer(t, "acme", "token-vault-dump-status")
	defer server.Close()

	_, env := setupSunAuthState(t, server.URL, "acme", "token-vault-dump-status")
	scope := "dump-status"

	if stdout, stderr, err := runSICommand(t, env, "vault", "init", "--scope", scope); err != nil {
		t.Fatalf("vault init failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if stdout, stderr, err := runSICommand(t, env, "vault", "set", "ONLY_KEY", "value", "--scope", scope); err != nil {
		t.Fatalf("vault set failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	stdout, stderr, err := runSICommand(t, env, "vault", "dump", "--scope", scope)
	if err != nil {
		t.Fatalf("vault dump failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "scope: "+scope) {
		t.Fatalf("missing scope in dump output:\n%s", stdout)
	}
	if !strings.Contains(stdout, "source: sun-kv") {
		t.Fatalf("missing source in dump output:\n%s", stdout)
	}
	if !strings.Contains(stdout, "ONLY_KEY\t(encrypted; use --reveal)") {
		t.Fatalf("missing encrypted key marker in dump output:\n%s", stdout)
	}
}
