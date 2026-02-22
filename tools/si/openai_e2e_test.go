package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenAIE2E_AuthStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		if got := r.Method; got != http.MethodGet {
			t.Fatalf("unexpected method: %s", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test-123" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-5"}]}`))
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"OPENAI_API_KEY": "sk-test-123",
	}, "openai", "auth", "status", "--base-url", server.URL, "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, `"status": "ready"`) {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestOpenAIE2E_ProjectList(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/organization/projects" {
			http.NotFound(w, r)
			return
		}
		if got := r.Method; got != http.MethodGet {
			t.Fatalf("unexpected method: %s", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-admin-123" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		if got := r.URL.Query().Get("limit"); got != "5" {
			t.Fatalf("unexpected limit query param: %q", got)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"proj_123","name":"Acme"}]}`))
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"OPENAI_API_KEY":       "sk-test-123",
		"OPENAI_ADMIN_API_KEY": "sk-admin-123",
	}, "openai", "project", "list", "--base-url", server.URL, "--limit", "5", "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, `"status_code": 200`) {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestOpenAIE2E_UsageCompletions(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/organization/usage/completions" {
			http.NotFound(w, r)
			return
		}
		if got := r.Method; got != http.MethodGet {
			t.Fatalf("unexpected method: %s", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-admin-123" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		if got := r.URL.Query().Get("start_time"); got != "1700000000" {
			t.Fatalf("unexpected start_time: %q", got)
		}
		if got := r.URL.Query().Get("bucket_width"); got != "1d" {
			t.Fatalf("unexpected bucket_width: %q", got)
		}
		_, _ = w.Write([]byte(`{"data":[{"object":"bucket"}]}`))
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"OPENAI_API_KEY":       "sk-test-123",
		"OPENAI_ADMIN_API_KEY": "sk-admin-123",
	}, "openai", "usage", "completions", "--base-url", server.URL, "--start-time", "1700000000", "--bucket-width", "1d", "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, `"status_code": 200`) {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestOpenAIE2E_CodexUsage(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/organization/usage/completions" {
			http.NotFound(w, r)
			return
		}
		if got := r.Method; got != http.MethodGet {
			t.Fatalf("unexpected method: %s", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-admin-123" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		if got := r.URL.Query().Get("models"); got != "gpt-5-codex" {
			t.Fatalf("unexpected models filter: %q", got)
		}
		_, _ = w.Write([]byte(`{"data":[{"object":"bucket"}]}`))
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"OPENAI_API_KEY":       "sk-test-123",
		"OPENAI_ADMIN_API_KEY": "sk-admin-123",
	}, "openai", "codex", "usage", "--base-url", server.URL, "--start-time", "1700000000", "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, `"status_code": 200`) {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestOpenAIE2E_DoctorPublic(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		if got := r.Method; got != http.MethodGet {
			t.Fatalf("unexpected method: %s", got)
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "unauthorized")
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{}, "openai", "doctor", "--public", "--base-url", server.URL, "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, `"ok": true`) {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestOpenAIE2E_AuthStatusCodexMode(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/backend-api/wham/usage" {
			http.NotFound(w, r)
			return
		}
		if got := r.Method; got != http.MethodGet {
			t.Fatalf("unexpected method: %s", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer codex-access-123" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		if got := r.Header.Get("ChatGPT-Account-Id"); got != "acct_123" {
			t.Fatalf("unexpected account header: %q", got)
		}
		_, _ = io.WriteString(w, `{
  "email":"main@example.com",
  "plan_type":"chatgpt-pro",
  "rate_limit":{
    "primary_window":{"used_percent":12,"reset_after_seconds":3600},
    "secondary_window":{"used_percent":21,"reset_after_seconds":86400}
  }
}`)
	}))
	defer server.Close()

	home := t.TempDir()
	writeCodexOpenAIE2ESettings(t, home, map[string]map[string]string{
		"main": {
			"name":  "Main",
			"email": "main@example.com",
		},
	}, "main")
	writeCodexOpenAIE2EAuth(t, home, "main", profileAuthFile{
		Tokens: &profileAuthTokens{
			AccessToken: "codex-access-123",
			AccountID:   "acct_123",
		},
	})

	stdout, stderr, err := runSICommand(t, map[string]string{
		"HOME":               home,
		"SI_SETTINGS_HOME":   home,
		"SI_CODEX_USAGE_URL": server.URL + "/backend-api/wham/usage",
	}, "openai", "auth", "status", "--auth-mode", "codex", "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, `"status": "ready"`) {
		t.Fatalf("unexpected output: %s", stdout)
	}
	if !strings.Contains(stdout, `"auth_mode": "codex"`) {
		t.Fatalf("missing auth_mode output: %s", stdout)
	}
	if !strings.Contains(stdout, `"profile_id": "main"`) {
		t.Fatalf("missing profile output: %s", stdout)
	}
}

func TestOpenAIE2E_AuthCodexStatusRequiresProfileWhenMultipleConfigured(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	home := t.TempDir()
	writeCodexOpenAIE2ESettings(t, home, map[string]map[string]string{
		"alpha": {
			"name":  "Alpha",
			"email": "alpha@example.com",
		},
		"beta": {
			"name":  "Beta",
			"email": "beta@example.com",
		},
	}, "")
	writeCodexOpenAIE2EAuth(t, home, "alpha", profileAuthFile{
		Tokens: &profileAuthTokens{AccessToken: "token-alpha"},
	})

	stdout, stderr, err := runSICommand(t, map[string]string{
		"HOME":             home,
		"SI_SETTINGS_HOME": home,
	}, "openai", "auth", "codex-status", "--json")
	if err == nil {
		t.Fatalf("expected command failure\nstdout=%s\nstderr=%s", stdout, stderr)
	}
	combined := strings.ToLower(stdout + "\n" + stderr)
	if !strings.Contains(combined, "multiple codex profiles configured") {
		t.Fatalf("unexpected error output: stdout=%s stderr=%s", stdout, stderr)
	}
}

func writeCodexOpenAIE2ESettings(t *testing.T, home string, profiles map[string]map[string]string, active string) {
	t.Helper()
	settingsDir := filepath.Join(home, ".si")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	var b strings.Builder
	b.WriteString("[codex]\n")
	if strings.TrimSpace(active) != "" {
		b.WriteString(`profile = "` + active + "\"\n")
	}
	b.WriteString("\n[codex.profiles]\n")
	if strings.TrimSpace(active) != "" {
		b.WriteString(`active = "` + active + "\"\n")
	}
	for id, entry := range profiles {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		b.WriteString("\n[codex.profiles.entries." + id + "]\n")
		if name := strings.TrimSpace(entry["name"]); name != "" {
			b.WriteString(`name = "` + name + "\"\n")
		}
		if email := strings.TrimSpace(entry["email"]); email != "" {
			b.WriteString(`email = "` + email + "\"\n")
		}
	}
	path := filepath.Join(settingsDir, "settings.toml")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
}

func writeCodexOpenAIE2EAuth(t *testing.T, home string, profileID string, auth profileAuthFile) {
	t.Helper()
	path := filepath.Join(home, ".si", "codex", "profiles", profileID, "auth.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir auth dir: %v", err)
	}
	data, err := json.Marshal(auth)
	if err != nil {
		t.Fatalf("marshal auth: %v", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatalf("write auth: %v", err)
	}
}
