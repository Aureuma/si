package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestFortHostURLForContainer(t *testing.T) {
	got := fortHostURLForContainer("http://127.0.0.1:8088")
	if got != "http://host.docker.internal:8088" {
		t.Fatalf("unexpected localhost rewrite: %q", got)
	}
	got = fortHostURLForContainer("http://172.19.0.9:8088")
	if got != "http://172.19.0.9:8088" {
		t.Fatalf("unexpected passthrough host: %q", got)
	}
}

func TestFortAgentIDForProfile(t *testing.T) {
	got := fortAgentIDForProfile("CADMA_01!")
	if got != "si-codex-cadma-01" {
		t.Fatalf("unexpected agent id: %q", got)
	}
}

func TestPrepareFortRuntimeAuthRefresh(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SI_CODEX_PROFILE_ID", "alpha")
	t.Setenv("FORT_TOKEN", "")
	t.Setenv("FORT_TOKEN_PATH", "")
	t.Setenv("FORT_REFRESH_TOKEN_PATH", "")

	profileFortDir := filepath.Join(home, ".si", "codex", "profiles", "alpha", fortProfileStateDirName)
	if err := os.MkdirAll(profileFortDir, 0o700); err != nil {
		t.Fatalf("mkdir profile fort dir: %v", err)
	}
	refreshPath := filepath.Join(profileFortDir, fortProfileRefreshTokenFileName)
	if err := os.WriteFile(refreshPath, []byte("refresh-1"), 0o600); err != nil {
		t.Fatalf("write refresh token: %v", err)
	}
	sessionPath := filepath.Join(profileFortDir, fortProfileSessionStateFileName)
	session := fortProfileSessionState{ProfileID: "alpha", ContainerHost: "http://fort.internal:8088"}
	raw, err := json.Marshal(session)
	if err != nil {
		t.Fatalf("marshal session: %v", err)
	}
	if err := os.WriteFile(sessionPath, raw, 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}

	refreshCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/auth/session/refresh" {
			http.NotFound(w, r)
			return
		}
		refreshCalls++
		_, _ = w.Write([]byte(`{"access_token":"access-2","refresh_token":"refresh-2","access_expires_at":"2030-01-01T00:00:00Z"}`))
	}))
	defer srv.Close()
	t.Setenv("FORT_HOST", srv.URL)

	accessToken, err := prepareFortRuntimeAuth([]string{"get"})
	if err != nil {
		t.Fatalf("prepareFortRuntimeAuth: %v", err)
	}
	if refreshCalls != 1 {
		t.Fatalf("expected one refresh call, got %d", refreshCalls)
	}
	if accessToken != "access-2" {
		t.Fatalf("unexpected access token: %q", accessToken)
	}
	tokenPath := filepath.Join(profileFortDir, fortProfileAccessTokenFileName)
	tokenBytes, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("read access token file: %v", err)
	}
	if strings.TrimSpace(string(tokenBytes)) != "access-2" {
		t.Fatalf("unexpected access token file contents: %q", string(tokenBytes))
	}
	refreshBytes, err := os.ReadFile(refreshPath)
	if err != nil {
		t.Fatalf("read refresh token file: %v", err)
	}
	if strings.TrimSpace(string(refreshBytes)) != "refresh-2" {
		t.Fatalf("unexpected refresh token file contents: %q", string(refreshBytes))
	}
}

func TestPrepareFortRuntimeAuthSkipsSessionSubcommands(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SI_CODEX_PROFILE_ID", "alpha")
	t.Setenv("FORT_TOKEN", "")
	t.Setenv("FORT_HOST", "http://127.0.0.1:1")

	accessToken, err := prepareFortRuntimeAuth([]string{"auth", "session", "close"})
	if err != nil {
		t.Fatalf("prepareFortRuntimeAuth: %v", err)
	}
	if accessToken != "" {
		t.Fatalf("expected empty access token when no token file is present, got %q", accessToken)
	}
}

func TestFortEnsureAgentReadPolicySetsDefaultWhenEmpty(t *testing.T) {
	var putBindings []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/agents/si-codex-alpha/policy" {
			http.NotFound(w, r)
			return
		}
		if got := strings.TrimSpace(r.Header.Get("Authorization")); got != "Bearer admin-test-token" {
			t.Fatalf("missing bearer token header: %q", got)
		}
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"bindings":[]}`))
		case http.MethodPut:
			var req struct {
				Bindings []map[string]any `json:"bindings"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode policy put body: %v", err)
			}
			putBindings = req.Bindings
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))
	defer srv.Close()

	err := fortEnsureAgentReadPolicy(context.Background(), fortBootstrapConfig{
		HostURL:     srv.URL,
		BearerToken: "admin-test-token",
	}, "si-codex-alpha")
	if err != nil {
		t.Fatalf("fortEnsureAgentReadPolicy: %v", err)
	}
	if len(putBindings) != 1 {
		t.Fatalf("expected one policy binding, got %d", len(putBindings))
	}
	binding := putBindings[0]
	if got := strings.TrimSpace(binding["repo"].(string)); got != "*" {
		t.Fatalf("unexpected binding repo: %q", got)
	}
	if got := strings.TrimSpace(binding["env"].(string)); got != "*" {
		t.Fatalf("unexpected binding env: %q", got)
	}
	rawOps, ok := binding["ops"].([]any)
	if !ok {
		t.Fatalf("expected ops array, got %#v", binding["ops"])
	}
	ops := make([]string, 0, len(rawOps))
	for _, item := range rawOps {
		ops = append(ops, strings.TrimSpace(item.(string)))
	}
	if !slices.Equal(ops, fortDefaultReadOps) {
		t.Fatalf("unexpected default ops: %#v", ops)
	}
}

func TestFortEnsureAgentReadPolicySkipsWhenPresent(t *testing.T) {
	putCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/agents/si-codex-alpha/policy" {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"bindings":[{"repo":"safe","env":"dev","ops":["get"]}]}`))
		case http.MethodPut:
			putCalled = true
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))
	defer srv.Close()

	err := fortEnsureAgentReadPolicy(context.Background(), fortBootstrapConfig{
		HostURL:     srv.URL,
		BearerToken: "admin-test-token",
	}, "si-codex-alpha")
	if err != nil {
		t.Fatalf("fortEnsureAgentReadPolicy: %v", err)
	}
	if putCalled {
		t.Fatalf("expected policy put to be skipped when bindings already exist")
	}
}

func TestLoadCodexFortBootstrapFromProfileState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	profile := codexProfile{ID: "alpha"}
	paths, err := fortProfileStatePaths(profile)
	if err != nil {
		t.Fatalf("fortProfileStatePaths: %v", err)
	}
	if err := os.WriteFile(paths.AccessTokenHostPath, []byte("access"), 0o600); err != nil {
		t.Fatalf("write access token: %v", err)
	}
	if err := os.WriteFile(paths.RefreshTokenHostPath, []byte("refresh"), 0o600); err != nil {
		t.Fatalf("write refresh token: %v", err)
	}
	state := fortProfileSessionState{
		ProfileID:     "alpha",
		AgentID:       "si-codex-alpha",
		SessionID:     "sess-1",
		Host:          "http://172.19.0.9:8088",
		ContainerHost: "http://viva-fort-app-blue:8088",
	}
	if err := saveFortProfileSessionState(paths.SessionStateHostPath, state); err != nil {
		t.Fatalf("saveFortProfileSessionState: %v", err)
	}
	boot, err := loadCodexFortBootstrapFromProfileState(profile)
	if err != nil {
		t.Fatalf("loadCodexFortBootstrapFromProfileState: %v", err)
	}
	if boot.ProfileID != "alpha" {
		t.Fatalf("unexpected profile id: %q", boot.ProfileID)
	}
	if boot.AgentID != "si-codex-alpha" {
		t.Fatalf("unexpected agent id: %q", boot.AgentID)
	}
	if boot.ContainerHostURL != "http://viva-fort-app-blue:8088" {
		t.Fatalf("unexpected container host url: %q", boot.ContainerHostURL)
	}
	if boot.AccessTokenContainerPath != "/home/si/.si/codex/profiles/alpha/fort/access.token" {
		t.Fatalf("unexpected access token container path: %q", boot.AccessTokenContainerPath)
	}
}

func TestFortDesiredFileOwnershipFromEnv(t *testing.T) {
	t.Setenv("SI_HOST_UID", "2222")
	t.Setenv("SI_HOST_GID", "3333")

	uid, gid, ok := fortDesiredFileOwnership()
	if !ok {
		t.Fatalf("expected ownership to be resolved")
	}
	if uid != 2222 || gid != 3333 {
		t.Fatalf("unexpected ownership uid=%d gid=%d", uid, gid)
	}
}

func TestFortDesiredFileOwnershipDefaultsToProcessIdentity(t *testing.T) {
	t.Setenv("SI_HOST_UID", "")
	t.Setenv("SI_HOST_GID", "")

	uid, gid, ok := fortDesiredFileOwnership()
	if !ok {
		t.Fatalf("expected ownership to be resolved")
	}
	if uid != os.Getuid() || gid != os.Getgid() {
		t.Fatalf("unexpected ownership uid=%d gid=%d want uid=%d gid=%d", uid, gid, os.Getuid(), os.Getgid())
	}
}
