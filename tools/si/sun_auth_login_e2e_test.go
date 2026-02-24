package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSunAuthLoginGoogleFlowE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}

	const (
		loginToken = "token-google-flow"
		account    = "acme"
	)

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/start":
			callbackRaw := strings.TrimSpace(r.URL.Query().Get("cb"))
			state := strings.TrimSpace(r.URL.Query().Get("state"))
			callbackURL, err := neturl.Parse(callbackRaw)
			if err != nil {
				http.Error(w, "invalid callback", http.StatusBadRequest)
				return
			}
			query := callbackURL.Query()
			query.Set("state", state)
			query.Set("token", loginToken)
			query.Set("url", server.URL)
			query.Set("account", account)
			query.Set("auto_sync", "true")
			callbackURL.RawQuery = query.Encode()
			http.Redirect(w, r, callbackURL.String(), http.StatusFound)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/auth/whoami":
			if got := strings.TrimSpace(r.Header.Get("Authorization")); got != "Bearer "+loginToken {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"account_id":   "acc-1",
				"account_slug": account,
				"token_id":     "tok-1",
				"scopes":       []string{"objects:read", "objects:write"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	launcherDir := t.TempDir()
	launcherPath := filepath.Join(launcherDir, "xdg-open")
	launcher := `#!/usr/bin/env sh
set -eu
python3 - "$1" <<'PY'
import sys
import urllib.request
urllib.request.urlopen(sys.argv[1]).read()
PY
`
	if err := os.WriteFile(launcherPath, []byte(launcher), 0o755); err != nil {
		t.Fatalf("write fake xdg-open: %v", err)
	}

	env := map[string]string{
		"SI_SUN_LOGIN_OPEN_CMD": launcherPath,
	}
	stdout, stderr, err := runSICommand(t, env,
		"sun", "auth", "login",
		"--google",
		"--login-url", server.URL+"/start",
		"--open-browser",
		"--timeout-seconds", "20",
	)
	if err != nil {
		t.Fatalf("sun auth login google failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	stdout, stderr, err = runSICommand(t, env, "sun", "auth", "status", "--json", "--timeout-seconds", "20")
	if err != nil {
		t.Fatalf("sun auth status failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("parse status json: %v\nstdout=%s", err, stdout)
	}
	if got := strings.TrimSpace(sunAnyToString(payload["base_url"])); got != server.URL {
		t.Fatalf("base_url=%q want=%q", got, server.URL)
	}
	who, ok := payload["whoami"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected whoami payload: %#v", payload["whoami"])
	}
	if got := strings.TrimSpace(sunAnyToString(who["account_slug"])); got != account {
		t.Fatalf("account=%q want=%q", got, account)
	}
}

func sunAnyToString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	default:
		return ""
	}
}
