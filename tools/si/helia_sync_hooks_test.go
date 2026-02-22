package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type heliaObjectStore struct {
	mu       sync.Mutex
	payloads map[string][]byte
	revs     map[string]int64
	putCalls int
}

func newHeliaObjectStore() *heliaObjectStore {
	return &heliaObjectStore{
		payloads: map[string][]byte{},
		revs:     map[string]int64{},
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
				PayloadBase64 string `json:"payload_base64"`
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
			store.putCalls++
			store.payloads[key] = payload
			store.revs[key]++
			rev := store.revs[key]
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

	maybeHeliaAutoBackupVault("test_plaintext", vaultFile)
	if got := store.putCount(); got != 0 {
		t.Fatalf("expected no backup upload for plaintext vault, got %d put calls", got)
	}
}
