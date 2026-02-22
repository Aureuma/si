package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolvePaasSyncBackendPrecedence(t *testing.T) {
	settings := defaultSettings()
	applySettingsDefaults(&settings)

	got, err := resolvePaasSyncBackend(settings)
	if err != nil {
		t.Fatalf("resolve default backend: %v", err)
	}
	if got.Mode != paasSyncBackendGit || got.Source != "default" {
		t.Fatalf("unexpected default backend resolution: %#v", got)
	}

	settings.Paas.SyncBackend = "dual"
	got, err = resolvePaasSyncBackend(settings)
	if err != nil {
		t.Fatalf("resolve settings backend: %v", err)
	}
	if got.Mode != paasSyncBackendDual || got.Source != "settings" {
		t.Fatalf("unexpected settings backend resolution: %#v", got)
	}

	t.Setenv(paasSyncBackendEnvKey, "sun")
	got, err = resolvePaasSyncBackend(settings)
	if err != nil {
		t.Fatalf("resolve env backend: %v", err)
	}
	if got.Mode != paasSyncBackendSun || got.Source != "env" {
		t.Fatalf("unexpected env backend resolution: %#v", got)
	}

	t.Setenv(paasSyncBackendEnvKey, "sun")
	got, err = resolvePaasSyncBackend(settings)
	if err != nil {
		t.Fatalf("resolve env backend sun alias: %v", err)
	}
	if got.Mode != paasSyncBackendSun || got.Source != "env" {
		t.Fatalf("unexpected env sun-alias backend resolution: %#v", got)
	}
}

func TestIsPaasCloudMutationCommand(t *testing.T) {
	tests := []struct {
		command string
		want    bool
	}{
		{command: "target add", want: true},
		{command: "target list", want: false},
		{command: "context show", want: false},
		{command: "context import", want: true},
		{command: "deploy webhook map list", want: false},
		{command: "deploy webhook map add", want: true},
		{command: "cloud push", want: false},
		{command: "taskboard move", want: true},
		{command: "taskboard show", want: false},
	}
	for _, tc := range tests {
		if got := isPaasCloudMutationCommand(tc.command); got != tc.want {
			t.Fatalf("isPaasCloudMutationCommand(%q)=%v want=%v", tc.command, got, tc.want)
		}
	}
}

func TestPaasCloudUseAndStatusE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	home := t.TempDir()
	env := map[string]string{
		"HOME":             home,
		"SI_SETTINGS_HOME": home,
	}
	stdout, stderr, err := runSICommand(t, env, "paas", "cloud", "status", "--json")
	if err != nil {
		t.Fatalf("cloud status failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var status map[string]any
	if err := json.Unmarshal([]byte(stdout), &status); err != nil {
		t.Fatalf("decode cloud status payload: %v output=%q", err, stdout)
	}
	if strings.TrimSpace(status["mode"].(string)) != paasSyncBackendGit {
		t.Fatalf("expected default git mode, got %#v", status)
	}

	stdout, stderr, err = runSICommand(t, env, "paas", "cloud", "use", "--mode", "dual")
	if err != nil {
		t.Fatalf("cloud use failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	stdout, stderr, err = runSICommand(t, env, "paas", "cloud", "status", "--json")
	if err != nil {
		t.Fatalf("cloud status failed after use: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if err := json.Unmarshal([]byte(stdout), &status); err != nil {
		t.Fatalf("decode cloud status payload after use: %v output=%q", err, stdout)
	}
	if strings.TrimSpace(status["mode"].(string)) != paasSyncBackendDual {
		t.Fatalf("expected dual mode after cloud use, got %#v", status)
	}
}

func TestPaasCloudPushPullRoundTripE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server, store := newSunTestServer(t, "acme", "token-paas")
	defer server.Close()

	home, env := setupSunAuthState(t, server.URL, "acme", "token-paas")
	stateRoot := filepath.Join(home, ".si", "paas-state")
	env[paasStateRootEnvKey] = stateRoot

	stdout, stderr, err := runSICommand(t, env, "paas", "--context", "demo", "context", "init", "--json")
	if err != nil {
		t.Fatalf("context init failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	stdout, stderr, err = runSICommand(t, env, "paas", "--context", "demo", "target", "add", "--name", "edge-a", "--host", "203.0.113.7", "--user", "root", "--json")
	if err != nil {
		t.Fatalf("target add failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	stdout, stderr, err = runSICommand(t, env, "paas", "--context", "demo", "deploy", "webhook", "map", "add", "--repo", "acme/cv", "--branch", "main", "--app", "cv", "--json")
	if err != nil {
		t.Fatalf("webhook mapping add failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	stdout, stderr, err = runSICommand(t, env, "paas", "--context", "demo", "app", "addon", "enable", "--app", "cv", "--pack", "redis", "--json")
	if err != nil {
		t.Fatalf("addon enable failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	stdout, stderr, err = runSICommand(t, env, "paas", "--context", "demo", "cloud", "push", "--json")
	if err != nil {
		t.Fatalf("cloud push failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if payload, ok := store.get(sunPaasControlPlaneSnapshotKind, "demo"); !ok || len(payload) == 0 {
		t.Fatalf("expected sun object store to contain demo cloud snapshot")
	}

	if err := os.RemoveAll(filepath.Join(stateRoot, "contexts", "demo")); err != nil {
		t.Fatalf("remove demo context state: %v", err)
	}

	stdout, stderr, err = runSICommand(t, env, "paas", "--context", "demo", "cloud", "pull", "--replace", "--json")
	if err != nil {
		t.Fatalf("cloud pull failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	targets, err := loadJSONFileAs[paasTargetStore](filepath.Join(stateRoot, "contexts", "demo", "targets.json"))
	if err != nil {
		t.Fatalf("load restored targets: %v", err)
	}
	if len(targets.Targets) != 1 || strings.TrimSpace(targets.Targets[0].Name) != "edge-a" {
		t.Fatalf("unexpected restored targets: %#v", targets.Targets)
	}

	mappings, err := loadJSONFileAs[paasWebhookMappingStore](filepath.Join(stateRoot, "contexts", "demo", "webhooks", "mappings.json"))
	if err != nil {
		t.Fatalf("load restored webhooks: %v", err)
	}
	if len(mappings.Mappings) != 1 || strings.TrimSpace(mappings.Mappings[0].Repo) != "acme/cv" {
		t.Fatalf("unexpected restored webhook mappings: %#v", mappings.Mappings)
	}

	addons, err := loadJSONFileAs[paasAddonStore](filepath.Join(stateRoot, "contexts", "demo", "addons.json"))
	if err != nil {
		t.Fatalf("load restored addons: %v", err)
	}
	if len(addons.Apps) == 0 {
		t.Fatalf("expected restored addon records, got %#v", addons)
	}
}

func TestPaasCloudStrictAutoSyncPreservesJSONOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server, _ := newSunTestServer(t, "acme", "token-json")
	defer server.Close()

	home, env := setupSunAuthState(t, server.URL, "acme", "token-json")
	env[paasStateRootEnvKey] = filepath.Join(home, ".si", "paas-state")
	env[paasSyncBackendEnvKey] = paasSyncBackendSun

	stdout, stderr, err := runSICommand(t, env, "paas", "--context", "jsonctx", "context", "init", "--json")
	if err != nil {
		t.Fatalf("context init failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var envelope paasTestEnvelope
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("expected pure JSON output under strict auto-sync; decode failed: %v output=%q", err, stdout)
	}
	if !envelope.OK || envelope.Command != "context init" {
		t.Fatalf("unexpected envelope under strict auto-sync: %#v", envelope)
	}
}

func loadJSONFileAs[T any](path string) (T, error) {
	var out T
	raw, err := os.ReadFile(path)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	return out, nil
}
