package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestSunE2E_VaultHistoryShowsRevisions(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server, _ := newSunTestServer(t, "acme", "token-vault-history")
	defer server.Close()

	home, env := setupSunAuthState(t, server.URL, "acme", "token-vault-history")
	keyFile := filepath.Join(home, ".si", "vault", "keys", "age.key")
	trustFile := filepath.Join(home, ".si", "vault", "trust.json")
	auditLog := filepath.Join(home, ".si", "vault", "audit.log")
	env["SI_VAULT_KEY_BACKEND"] = "file"
	env["SI_VAULT_KEY_FILE"] = keyFile
	env["SI_VAULT_TRUST_STORE"] = trustFile
	env["SI_VAULT_AUDIT_LOG"] = auditLog

	vaultFile := filepath.Join(home, ".si", "vault", ".env")
	stdout, stderr, err := runSICommand(t, env, "vault", "init", "--file", vaultFile, "--set-default")
	if err != nil {
		t.Fatalf("vault init failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	stdout, stderr, err = runSICommand(t, env, "vault", "set", "HISTORY_KEY", "first", "--file", vaultFile)
	if err != nil {
		t.Fatalf("vault set first failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	stdout, stderr, err = runSICommand(t, env, "vault", "set", "HISTORY_KEY", "second", "--file", vaultFile)
	if err != nil {
		t.Fatalf("vault set second failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	stdout, stderr, err = runSICommand(t, env, "vault", "unset", "HISTORY_KEY", "--file", vaultFile)
	if err != nil {
		t.Fatalf("vault unset failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	stdout, stderr, err = runSICommand(t, env, "vault", "history", "HISTORY_KEY", "--file", vaultFile, "--limit", "20", "--json")
	if err != nil {
		t.Fatalf("vault history failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("parse vault history json: %v\nstdout=%s", err, stdout)
	}
	if got := strings.TrimSpace(formatAny(payload["source"])); got != "sun-kv" {
		t.Fatalf("expected sun-kv source, got %q", got)
	}
	rawRows, ok := payload["revisions"].([]any)
	if !ok || len(rawRows) == 0 {
		t.Fatalf("expected non-empty revisions array, got %#v", payload["revisions"])
	}
	seenSet := false
	seenUnset := false
	for _, rowAny := range rawRows {
		row, _ := rowAny.(map[string]any)
		meta, _ := row["metadata"].(map[string]any)
		op := strings.ToLower(strings.TrimSpace(formatAny(meta["operation"])))
		switch op {
		case "set":
			seenSet = true
		case "unset":
			seenUnset = true
		}
	}
	if !seenSet || !seenUnset {
		t.Fatalf("expected both set and unset operations in history, got %#v", payload["revisions"])
	}
}
