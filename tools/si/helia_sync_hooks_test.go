package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

type heliaObjectStore struct {
	mu       sync.Mutex
	payloads map[string][]byte
	revs     map[string]int64
	created  map[string]string
	updated  map[string]string
	putCalls int
}

func newHeliaObjectStore() *heliaObjectStore {
	return &heliaObjectStore{
		payloads: map[string][]byte{},
		revs:     map[string]int64{},
		created:  map[string]string{},
		updated:  map[string]string{},
	}
}

func (s *heliaObjectStore) key(kind string, name string) string {
	return kind + "/" + name
}

func (s *heliaObjectStore) get(kind string, name string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, ok := s.payloads[s.key(kind, name)]
	if !ok {
		return nil, false
	}
	out := make([]byte, len(raw))
	copy(out, raw)
	return out, true
}

func (s *heliaObjectStore) putCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.putCalls
}

func (s *heliaObjectStore) list(kind string, name string, limit int) []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 {
		limit = 200
	}
	trimmedKind := strings.TrimSpace(kind)
	trimmedName := strings.TrimSpace(name)
	items := make([]map[string]any, 0, len(s.payloads))
	for key, payload := range s.payloads {
		parts := strings.SplitN(key, "/", 2)
		if len(parts) != 2 {
			continue
		}
		itemKind := parts[0]
		itemName := parts[1]
		if trimmedKind != "" && !strings.EqualFold(itemKind, trimmedKind) {
			continue
		}
		if trimmedName != "" && !strings.EqualFold(itemName, trimmedName) {
			continue
		}
		created := s.created[key]
		updated := s.updated[key]
		if strings.TrimSpace(created) == "" {
			created = "2026-01-01T00:00:00Z"
		}
		if strings.TrimSpace(updated) == "" {
			updated = created
		}
		items = append(items, map[string]any{
			"kind":            itemKind,
			"name":            itemName,
			"latest_revision": s.revs[key],
			"checksum":        heliaPayloadSHA256Hex(payload),
			"content_type":    "application/json",
			"size_bytes":      len(payload),
			"created_at":      created,
			"updated_at":      updated,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return strings.Compare(formatAny(items[i]["name"]), formatAny(items[j]["name"])) < 0
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items
}

func newHeliaTestServer(t *testing.T, account string, token string) (*httptest.Server, *heliaObjectStore) {
	t.Helper()
	store := newHeliaObjectStore()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/whoami" && r.Method == http.MethodGet {
			if got := strings.TrimSpace(r.Header.Get("Authorization")); got != "Bearer "+token {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"account_id":   "acc-1",
				"account_slug": account,
				"token_id":     "token-1",
				"scopes":       []string{"objects:read", "objects:write"},
			})
			return
		}
		if r.URL.Path == "/v1/objects" && r.Method == http.MethodGet {
			if got := strings.TrimSpace(r.Header.Get("Authorization")); got != "Bearer "+token {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			kind := strings.TrimSpace(r.URL.Query().Get("kind"))
			name := strings.TrimSpace(r.URL.Query().Get("name"))
			limit := 200
			if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
				if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
					limit = parsed
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"items": store.list(kind, name, limit)})
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/v1/objects/") {
			http.NotFound(w, r)
			return
		}
		if got := strings.TrimSpace(r.Header.Get("Authorization")); got != "Bearer "+token {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/objects/"), "/")
		if len(parts) < 2 {
			http.NotFound(w, r)
			return
		}
		kind := parts[0]
		name := parts[1]
		switch {
		case r.Method == http.MethodPut && len(parts) == 2:
			var req struct {
				PayloadBase64    string `json:"payload_base64"`
				ExpectedRevision *int64 `json:"expected_revision"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			payload, err := base64.StdEncoding.DecodeString(strings.TrimSpace(req.PayloadBase64))
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			store.mu.Lock()
			key := store.key(kind, name)
			currentRev := store.revs[key]
			if req.ExpectedRevision != nil && *req.ExpectedRevision != currentRev {
				store.mu.Unlock()
				http.Error(w, `{"error":"revision mismatch"}`, http.StatusConflict)
				return
			}
			store.putCalls++
			if _, ok := store.payloads[key]; !ok {
				store.created[key] = "2026-01-01T00:00:00Z"
			}
			store.payloads[key] = payload
			store.revs[key]++
			rev := store.revs[key]
			store.updated[key] = "2026-01-02T00:00:00Z"
			store.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"object":   map[string]any{"latest_revision": rev},
					"revision": map[string]any{"revision": rev},
				},
			})
			return
		case r.Method == http.MethodGet && len(parts) == 3 && parts[2] == "payload":
			payload, ok := store.get(kind, name)
			if !ok {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(payload)
			return
		default:
			http.NotFound(w, r)
			return
		}
	})
	return httptest.NewServer(handler), store
}

func appendCodexProfileToSettings(t *testing.T, home string, id string, name string, email string) {
	t.Helper()
	settingsPath := filepath.Join(home, ".si", "settings.toml")
	f, err := os.OpenFile(settingsPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open settings: %v", err)
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "\n[codex.profiles]\nactive = %q\n[codex.profiles.entries.%s]\nname = %q\nemail = %q\n", id, id, name, email)
	if err != nil {
		t.Fatalf("append settings: %v", err)
	}
}

func setupHeliaAuthState(t *testing.T, serverURL string, account string, token string) (string, map[string]string) {
	t.Helper()
	home := t.TempDir()
	env := map[string]string{
		"HOME":             home,
		"SI_SETTINGS_HOME": home,
	}
	stdout, stderr, err := runSICommand(t, env, "helia", "auth", "login", "--url", serverURL, "--token", token, "--account", account, "--auto-sync")
	if err != nil {
		t.Fatalf("helia auth login failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	return home, env
}

func TestHeliaE2E_ProfilePushPullRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server, store := newHeliaTestServer(t, "acme", "token-123")
	defer server.Close()

	home, env := setupHeliaAuthState(t, server.URL, "acme", "token-123")
	authPath := filepath.Join(home, ".si", "codex", "profiles", "demo", "auth.json")
	if err := os.MkdirAll(filepath.Dir(authPath), 0o700); err != nil {
		t.Fatalf("mkdir profile dir: %v", err)
	}
	wantAuth := `{"tokens":{"access_token":"demo-access","refresh_token":"demo-refresh"}}`
	if err := os.WriteFile(authPath, []byte(wantAuth), 0o600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}
	appendCodexProfileToSettings(t, home, "demo", "Demo User", "demo@example.com")

	stdout, stderr, err := runSICommand(t, env, "helia", "profile", "push", "--profile", "demo")
	if err != nil {
		t.Fatalf("profile push failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	if err := os.Remove(authPath); err != nil {
		t.Fatalf("remove auth path: %v", err)
	}
	stdout, stderr, err = runSICommand(t, env, "helia", "profile", "pull", "--profile", "demo")
	if err != nil {
		t.Fatalf("profile pull failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	gotAuth, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatalf("read pulled auth file: %v", err)
	}
	if strings.TrimSpace(string(gotAuth)) != wantAuth {
		t.Fatalf("auth payload mismatch\ngot:  %s\nwant: %s", strings.TrimSpace(string(gotAuth)), wantAuth)
	}
	if payload, ok := store.get(heliaCodexProfileBundleKind, "demo"); !ok || len(payload) == 0 {
		t.Fatalf("expected server to persist profile bundle payload")
	}
}

func TestHeliaE2E_VaultBackupPushPullRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server, _ := newHeliaTestServer(t, "acme", "token-456")
	defer server.Close()

	home, env := setupHeliaAuthState(t, server.URL, "acme", "token-456")
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
	stdout, stderr, err = runSICommand(t, env, "vault", "set", "HELIA_SYNC_TEST", "secret-value", "--file", vaultFile)
	if err != nil {
		t.Fatalf("vault set failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	before, err := os.ReadFile(vaultFile)
	if err != nil {
		t.Fatalf("read vault before backup: %v", err)
	}

	stdout, stderr, err = runSICommand(t, env, "helia", "vault", "backup", "push", "--file", vaultFile, "--name", "roundtrip")
	if err != nil {
		t.Fatalf("vault backup push failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if err := os.Remove(vaultFile); err != nil {
		t.Fatalf("remove vault file: %v", err)
	}
	stdout, stderr, err = runSICommand(t, env, "helia", "vault", "backup", "pull", "--file", vaultFile, "--name", "roundtrip")
	if err != nil {
		t.Fatalf("vault backup pull failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	after, err := os.ReadFile(vaultFile)
	if err != nil {
		t.Fatalf("read vault after restore: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("vault backup round-trip mismatch")
	}
}

func TestMaybeHeliaAutoBackupVaultSkipsPlaintext(t *testing.T) {
	server, store := newHeliaTestServer(t, "acme", "token-789")
	defer server.Close()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SI_SETTINGS_HOME", home)

	settings := defaultSettings()
	applySettingsDefaults(&settings)
	settings.Helia.AutoSync = true
	settings.Helia.BaseURL = server.URL
	settings.Helia.Token = "token-789"
	settings.Helia.VaultBackup = "default"
	if err := saveSettings(settings); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	vaultFile := filepath.Join(home, ".si", "vault", ".env")
	if err := os.MkdirAll(filepath.Dir(vaultFile), 0o700); err != nil {
		t.Fatalf("mkdir vault dir: %v", err)
	}
	if err := os.WriteFile(vaultFile, []byte("PLAINTEXT_KEY=oops\n"), 0o600); err != nil {
		t.Fatalf("write vault file: %v", err)
	}

	if err := maybeHeliaAutoBackupVault("test_plaintext", vaultFile); err != nil {
		t.Fatalf("expected best-effort mode to skip plaintext without hard error, got: %v", err)
	}
	if got := store.putCount(); got != 0 {
		t.Fatalf("expected no backup upload for plaintext vault, got %d put calls", got)
	}
}

func TestMaybeHeliaAutoBackupVaultHeliaModeFailsOnPlaintext(t *testing.T) {
	server, store := newHeliaTestServer(t, "acme", "token-plain")
	defer server.Close()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SI_SETTINGS_HOME", home)

	settings := defaultSettings()
	applySettingsDefaults(&settings)
	settings.Helia.BaseURL = server.URL
	settings.Helia.Token = "token-plain"
	settings.Vault.SyncBackend = vaultSyncBackendHelia
	if err := saveSettings(settings); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	vaultFile := filepath.Join(home, ".si", "vault", ".env")
	if err := os.MkdirAll(filepath.Dir(vaultFile), 0o700); err != nil {
		t.Fatalf("mkdir vault dir: %v", err)
	}
	if err := os.WriteFile(vaultFile, []byte("PLAINTEXT_KEY=oops\n"), 0o600); err != nil {
		t.Fatalf("write vault file: %v", err)
	}

	err := maybeHeliaAutoBackupVault("test_plaintext_strict", vaultFile)
	if err == nil {
		t.Fatalf("expected strict helia mode to fail on plaintext vault")
	}
	if !strings.Contains(err.Error(), "plaintext keys detected") {
		t.Fatalf("expected plaintext error, got: %v", err)
	}
	if got := store.putCount(); got != 0 {
		t.Fatalf("expected no backup upload for plaintext vault in strict mode, got %d put calls", got)
	}
}

func TestMaybeHeliaAutoBackupVaultGitModeSkipsCloudBackup(t *testing.T) {
	server, store := newHeliaTestServer(t, "acme", "token-git")
	defer server.Close()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SI_SETTINGS_HOME", home)

	settings := defaultSettings()
	applySettingsDefaults(&settings)
	settings.Helia.AutoSync = true
	settings.Helia.BaseURL = server.URL
	settings.Helia.Token = "token-git"
	settings.Vault.SyncBackend = vaultSyncBackendGit
	if err := saveSettings(settings); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	vaultFile := filepath.Join(home, ".si", "vault", ".env")
	if err := os.MkdirAll(filepath.Dir(vaultFile), 0o700); err != nil {
		t.Fatalf("mkdir vault dir: %v", err)
	}
	if err := os.WriteFile(vaultFile, []byte("ANY=value\n"), 0o600); err != nil {
		t.Fatalf("write vault file: %v", err)
	}

	if err := maybeHeliaAutoBackupVault("test_git_mode", vaultFile); err != nil {
		t.Fatalf("git mode should skip cloud backup without error: %v", err)
	}
	if got := store.putCount(); got != 0 {
		t.Fatalf("expected no backup upload in git mode, got %d put calls", got)
	}
}

func TestHeliaE2E_TaskboardClaimAndLocking(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server, _ := newHeliaTestServer(t, "acme", "token-taskboard")
	defer server.Close()

	_, env := setupHeliaAuthState(t, server.URL, "acme", "token-taskboard")

	stdout, stderr, err := runSICommand(t, env, "helia", "taskboard", "add", "--name", "shared", "--title", "low priority", "--prompt", "handle low", "--priority", "P3", "--json")
	if err != nil {
		t.Fatalf("taskboard add low failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var lowTask heliaTaskboardTask
	if err := json.Unmarshal([]byte(stdout), &lowTask); err != nil {
		t.Fatalf("decode low task json: %v output=%q", err, stdout)
	}

	stdout, stderr, err = runSICommand(t, env, "helia", "taskboard", "add", "--name", "shared", "--title", "high priority", "--prompt", "handle high", "--priority", "P1", "--json")
	if err != nil {
		t.Fatalf("taskboard add high failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var highTask heliaTaskboardTask
	if err := json.Unmarshal([]byte(stdout), &highTask); err != nil {
		t.Fatalf("decode high task json: %v output=%q", err, stdout)
	}

	stdout, stderr, err = runSICommand(t, env, "helia", "taskboard", "claim", "--name", "shared", "--agent", "agent-a", "--json")
	if err != nil {
		t.Fatalf("taskboard claim #1 failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var firstClaim heliaTaskboardClaimResult
	if err := json.Unmarshal([]byte(stdout), &firstClaim); err != nil {
		t.Fatalf("decode first claim json: %v output=%q", err, stdout)
	}
	if firstClaim.Task.ID != highTask.ID {
		t.Fatalf("expected first claim to pick high priority task %q, got %q", highTask.ID, firstClaim.Task.ID)
	}

	stdout, stderr, err = runSICommand(t, env, "helia", "taskboard", "claim", "--name", "shared", "--agent", "agent-b", "--json")
	if err != nil {
		t.Fatalf("taskboard claim #2 failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var secondClaim heliaTaskboardClaimResult
	if err := json.Unmarshal([]byte(stdout), &secondClaim); err != nil {
		t.Fatalf("decode second claim json: %v output=%q", err, stdout)
	}
	if secondClaim.Task.ID != lowTask.ID {
		t.Fatalf("expected second claim to pick low task %q, got %q", lowTask.ID, secondClaim.Task.ID)
	}

	_, _, err = runSICommand(t, env, "helia", "taskboard", "release", "--name", "shared", "--id", firstClaim.Task.ID, "--agent", "agent-b")
	if err == nil {
		t.Fatalf("expected release by non-owner agent to fail")
	}

	stdout, stderr, err = runSICommand(t, env, "helia", "taskboard", "release", "--name", "shared", "--id", firstClaim.Task.ID, "--agent", "agent-a", "--json")
	if err != nil {
		t.Fatalf("taskboard release by owner failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	stdout, stderr, err = runSICommand(t, env, "helia", "taskboard", "claim", "--name", "shared", "--id", firstClaim.Task.ID, "--agent", "agent-b", "--json")
	if err != nil {
		t.Fatalf("taskboard claim released task failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var reclaimed heliaTaskboardClaimResult
	if err := json.Unmarshal([]byte(stdout), &reclaimed); err != nil {
		t.Fatalf("decode reclaimed json: %v output=%q", err, stdout)
	}
	if reclaimed.Task.ID != firstClaim.Task.ID {
		t.Fatalf("expected to reclaim task %q, got %q", firstClaim.Task.ID, reclaimed.Task.ID)
	}

	stdout, stderr, err = runSICommand(t, env, "helia", "taskboard", "done", "--name", "shared", "--id", secondClaim.Task.ID, "--agent", "agent-b", "--result", "completed", "--json")
	if err != nil {
		t.Fatalf("taskboard done failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var doneTask heliaTaskboardTask
	if err := json.Unmarshal([]byte(stdout), &doneTask); err != nil {
		t.Fatalf("decode done task json: %v output=%q", err, stdout)
	}
	if doneTask.Status != heliaTaskStatusDone {
		t.Fatalf("expected done status, got %q", doneTask.Status)
	}

	stdout, stderr, err = runSICommand(t, env, "helia", "taskboard", "show", "--name", "shared", "--json")
	if err != nil {
		t.Fatalf("taskboard show failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var board heliaTaskboard
	if err := json.Unmarshal([]byte(stdout), &board); err != nil {
		t.Fatalf("decode board json: %v output=%q", err, stdout)
	}
	if len(board.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(board.Tasks))
	}
}

func TestHeliaE2E_MachineRunServeWithACL(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server, _ := newHeliaTestServer(t, "acme", "token-machine")
	defer server.Close()

	_, env := setupHeliaAuthState(t, server.URL, "acme", "token-machine")

	stdout, stderr, err := runSICommand(t, env, "helia", "machine", "register",
		"--machine", "controller-a",
		"--operator", "op:controller@local",
		"--can-control-others",
		"--can-be-controlled=false",
		"--json",
	)
	if err != nil {
		t.Fatalf("controller register failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	stdout, stderr, err = runSICommand(t, env, "helia", "machine", "register",
		"--machine", "worker-a",
		"--operator", "op:worker@remote",
		"--allow-operators", "op:controller@local",
		"--can-be-controlled",
		"--json",
	)
	if err != nil {
		t.Fatalf("worker register failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	_, _, err = runSICommand(t, env, "helia", "machine", "run",
		"--machine", "worker-a",
		"--source-machine", "controller-a",
		"--operator", "op:rogue@local",
		"--json",
		"--", "version",
	)
	if err == nil {
		t.Fatalf("expected unauthorized operator dispatch to fail")
	}

	stdout, stderr, err = runSICommand(t, env, "helia", "machine", "run",
		"--machine", "worker-a",
		"--source-machine", "controller-a",
		"--operator", "op:controller@local",
		"--json",
		"--", "version",
	)
	if err != nil {
		t.Fatalf("remote run dispatch failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var queued heliaMachineJob
	if err := json.Unmarshal([]byte(stdout), &queued); err != nil {
		t.Fatalf("decode queued job payload: %v output=%q", err, stdout)
	}
	if strings.TrimSpace(queued.Status) != heliaMachineJobStatusQueued {
		t.Fatalf("expected queued status, got %q", queued.Status)
	}

	stdout, stderr, err = runSICommand(t, env, "helia", "machine", "serve",
		"--machine", "worker-a",
		"--operator", "op:worker@remote",
		"--once",
		"--json",
	)
	if err != nil {
		t.Fatalf("machine serve once failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var serveSummary heliaMachineServeSummary
	if err := json.Unmarshal([]byte(stdout), &serveSummary); err != nil {
		t.Fatalf("decode serve summary payload: %v output=%q", err, stdout)
	}
	if serveSummary.Processed != 1 {
		t.Fatalf("expected serve to process one job, got %d", serveSummary.Processed)
	}

	stdout, stderr, err = runSICommand(t, env, "helia", "machine", "jobs",
		"--machine", "worker-a",
		"--status", "succeeded",
		"--json",
	)
	if err != nil {
		t.Fatalf("jobs list failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var jobs []heliaMachineJob
	if err := json.Unmarshal([]byte(stdout), &jobs); err != nil {
		t.Fatalf("decode jobs payload: %v output=%q", err, stdout)
	}
	if len(jobs) == 0 {
		t.Fatalf("expected at least one succeeded job")
	}
	if strings.TrimSpace(jobs[0].Status) != heliaMachineJobStatusSucceeded {
		t.Fatalf("expected succeeded job, got %q", jobs[0].Status)
	}
}

func TestHeliaE2E_MachineRunRemoteLoginAndList(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server, _ := newHeliaTestServer(t, "acme", "token-machine-remote")
	defer server.Close()

	_, env := setupHeliaAuthState(t, server.URL, "acme", "token-machine-remote")

	stdout, stderr, err := runSICommand(t, env, "helia", "machine", "register",
		"--machine", "controller-b",
		"--operator", "op:controller@local",
		"--can-control-others",
		"--can-be-controlled=false",
		"--json",
	)
	if err != nil {
		t.Fatalf("controller register failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	stdout, stderr, err = runSICommand(t, env, "helia", "machine", "register",
		"--machine", "worker-b",
		"--operator", "op:worker@remote",
		"--allow-operators", "op:controller@local",
		"--can-be-controlled",
		"--json",
	)
	if err != nil {
		t.Fatalf("worker register failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	stdout, stderr, err = runSICommand(t, env, "helia", "machine", "run",
		"--machine", "worker-b",
		"--source-machine", "controller-b",
		"--operator", "op:controller@local",
		"--json",
		"--", "login", "--help",
	)
	if err != nil {
		t.Fatalf("remote login --help dispatch failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var loginJob heliaMachineJob
	if err := json.Unmarshal([]byte(stdout), &loginJob); err != nil {
		t.Fatalf("decode login queued job: %v output=%q", err, stdout)
	}
	if loginJob.Status != heliaMachineJobStatusQueued {
		t.Fatalf("expected queued login job status, got %q", loginJob.Status)
	}

	stdout, stderr, err = runSICommand(t, env, "helia", "machine", "serve",
		"--machine", "worker-b",
		"--operator", "op:worker@remote",
		"--once",
		"--json",
	)
	if err != nil {
		t.Fatalf("serve once (login job) failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	stdout, stderr, err = runSICommand(t, env, "helia", "machine", "run",
		"--machine", "worker-b",
		"--source-machine", "controller-b",
		"--operator", "op:controller@local",
		"--json",
		"--", "list", "--json",
	)
	if err != nil {
		t.Fatalf("remote list --json dispatch failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var listJob heliaMachineJob
	if err := json.Unmarshal([]byte(stdout), &listJob); err != nil {
		t.Fatalf("decode list queued job: %v output=%q", err, stdout)
	}
	if listJob.Status != heliaMachineJobStatusQueued {
		t.Fatalf("expected queued list job status, got %q", listJob.Status)
	}

	stdout, stderr, err = runSICommand(t, env, "helia", "machine", "serve",
		"--machine", "worker-b",
		"--operator", "op:worker@remote",
		"--once",
		"--json",
	)
	if err != nil {
		t.Fatalf("serve once (list job) failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	stdout, stderr, err = runSICommand(t, env, "helia", "machine", "jobs",
		"--machine", "worker-b",
		"--status", "succeeded",
		"--json",
	)
	if err != nil {
		t.Fatalf("jobs list failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var jobs []heliaMachineJob
	if err := json.Unmarshal([]byte(stdout), &jobs); err != nil {
		t.Fatalf("decode jobs payload: %v output=%q", err, stdout)
	}
	if len(jobs) < 2 {
		t.Fatalf("expected at least two succeeded jobs, got %d", len(jobs))
	}
	hasLoginHelp := false
	hasListJSON := false
	for _, job := range jobs {
		if len(job.Command) == 2 && job.Command[0] == "login" && job.Command[1] == "--help" {
			hasLoginHelp = true
		}
		if len(job.Command) == 2 && job.Command[0] == "list" && job.Command[1] == "--json" {
			hasListJSON = true
		}
	}
	if !hasLoginHelp {
		t.Fatalf("expected succeeded remote login --help job")
	}
	if !hasListJSON {
		t.Fatalf("expected succeeded remote list --json job")
	}
}

func TestMaybeHeliaAutoBackupVaultHeliaModeRequiresAuthConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SI_SETTINGS_HOME", home)

	settings := defaultSettings()
	applySettingsDefaults(&settings)
	settings.Vault.SyncBackend = vaultSyncBackendHelia
	settings.Helia.BaseURL = ""
	settings.Helia.Token = ""
	if err := saveSettings(settings); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	vaultFile := filepath.Join(home, ".si", "vault", ".env")
	if err := os.MkdirAll(filepath.Dir(vaultFile), 0o700); err != nil {
		t.Fatalf("mkdir vault dir: %v", err)
	}
	if err := os.WriteFile(vaultFile, []byte(""), 0o600); err != nil {
		t.Fatalf("write vault file: %v", err)
	}

	err := maybeHeliaAutoBackupVault("test_missing_auth", vaultFile)
	if err == nil {
		t.Fatalf("expected strict helia mode to fail without helia auth config")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "helia") {
		t.Fatalf("expected helia context in error, got: %v", err)
	}
}

func TestMaybeHeliaAutoSyncProfileUploadsCredentials(t *testing.T) {
	server, store := newHeliaTestServer(t, "acme", "token-autosync")
	defer server.Close()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SI_SETTINGS_HOME", home)

	settings := defaultSettings()
	applySettingsDefaults(&settings)
	settings.Helia.AutoSync = true
	settings.Helia.BaseURL = server.URL
	settings.Helia.Token = "token-autosync"
	if err := saveSettings(settings); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	profile := codexProfile{ID: "demo-sync", Name: "Demo Sync", Email: "demo-sync@example.com"}
	dir, err := ensureCodexProfileDir(profile)
	if err != nil {
		t.Fatalf("ensure profile dir: %v", err)
	}
	authPath := filepath.Join(dir, "auth.json")
	authJSON := `{"tokens":{"access_token":"access-demo","refresh_token":"refresh-demo"}}`
	if err := os.WriteFile(authPath, []byte(authJSON), 0o600); err != nil {
		t.Fatalf("write auth cache: %v", err)
	}

	maybeHeliaAutoSyncProfile("test_auto_sync", profile)

	payload, ok := store.get(heliaCodexProfileBundleKind, profile.ID)
	if !ok || len(payload) == 0 {
		t.Fatalf("expected cloud profile payload to be uploaded")
	}
	var bundle heliaCodexProfileBundle
	if err := json.Unmarshal(payload, &bundle); err != nil {
		t.Fatalf("decode uploaded profile bundle: %v", err)
	}
	if bundle.ID != profile.ID {
		t.Fatalf("bundle id mismatch: got %q want %q", bundle.ID, profile.ID)
	}
	if strings.TrimSpace(string(bundle.AuthJSON)) != authJSON {
		t.Fatalf("bundle auth payload mismatch")
	}
}
