package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestResolveFortBootstrapConfigReadsHostFromSettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SI_SETTINGS_HOME", home)
	t.Setenv("HOME", home)
	settings := loadSettingsOrDefault()
	settings.Fort.Host = "https://fort.example.test"
	settings.Fort.ContainerHost = "https://fort.internal.example.test"
	if err := saveSettings(settings); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	t.Setenv("FORT_HOST", "")
	t.Setenv("SI_FORT_HOST", "")
	t.Setenv("SI_FORT_CONTAINER_HOST", "")
	tokenFile := filepath.Join(home, ".si", "fort", "bootstrap", "admin.token")
	if err := os.MkdirAll(filepath.Dir(tokenFile), 0o700); err != nil {
		t.Fatalf("mkdir token dir: %v", err)
	}
	if err := os.WriteFile(tokenFile, []byte(makeTestJWT(time.Now().Add(30*time.Minute))), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	t.Setenv("FORT_BOOTSTRAP_TOKEN_FILE", tokenFile)
	t.Setenv("SI_FORT_ALLOW_INSECURE_HOST", "")
	t.Setenv("SI_FORT_DISCOVER_DOCKER", "")

	cfg, err := resolveFortBootstrapConfig(context.Background(), nil, "")
	if err != nil {
		t.Fatalf("resolveFortBootstrapConfig: %v", err)
	}
	if cfg.HostURL != "https://fort.example.test" {
		t.Fatalf("unexpected host url: %q", cfg.HostURL)
	}
	if cfg.ContainerHostURL != "https://fort.internal.example.test" {
		t.Fatalf("unexpected container host url: %q", cfg.ContainerHostURL)
	}
}

func TestResolveFortBootstrapConfigReadsTokenFromFile(t *testing.T) {
	tmp := t.TempDir()
	tokenFile := filepath.Join(tmp, "admin.token")
	if err := os.WriteFile(tokenFile, []byte(makeTestJWT(time.Now().Add(30*time.Minute))), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	t.Setenv("FORT_BOOTSTRAP_TOKEN_FILE", tokenFile)
	t.Setenv("FORT_HOST", "https://fort.example.test")
	t.Setenv("SI_FORT_CONTAINER_HOST", "https://fort.example.test")

	cfg, err := resolveFortBootstrapConfig(context.Background(), nil, "")
	if err != nil {
		t.Fatalf("resolveFortBootstrapConfig: %v", err)
	}
	if cfg.BearerToken == "" {
		t.Fatalf("unexpected bearer token: %q", cfg.BearerToken)
	}
}

func TestResolveFortBootstrapConfigReadsTokenFromLegacyTokenFileEnv(t *testing.T) {
	tmp := t.TempDir()
	tokenFile := filepath.Join(tmp, "admin.token")
	if err := os.WriteFile(tokenFile, []byte(makeTestJWT(time.Now().Add(30*time.Minute))), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	t.Setenv("FORT_BOOTSTRAP_TOKEN_FILE", "")
	t.Setenv("FORT_TOKEN_FILE", tokenFile)
	t.Setenv("FORT_HOST", "https://fort.example.test")
	t.Setenv("SI_FORT_CONTAINER_HOST", "https://fort.example.test")

	cfg, err := resolveFortBootstrapConfig(context.Background(), nil, "")
	if err != nil {
		t.Fatalf("resolveFortBootstrapConfig: %v", err)
	}
	if cfg.BearerToken == "" {
		t.Fatalf("unexpected bearer token: %q", cfg.BearerToken)
	}
}

func TestFortResolveBootstrapBearerTokenRefreshesExpiredToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	tokenFile := filepath.Join(home, ".si", "fort", "bootstrap", "admin.token")
	refreshFile := filepath.Join(home, ".si", "fort", "bootstrap", "admin.refresh.token")
	if err := os.MkdirAll(filepath.Dir(tokenFile), 0o700); err != nil {
		t.Fatalf("mkdir bootstrap dir: %v", err)
	}
	if err := os.WriteFile(tokenFile, []byte(makeTestJWT(time.Now().Add(-2*time.Minute))), 0o600); err != nil {
		t.Fatalf("write expired token: %v", err)
	}
	if err := os.WriteFile(refreshFile, []byte("refresh-token-1"), 0o600); err != nil {
		t.Fatalf("write refresh token: %v", err)
	}
	t.Setenv("FORT_BOOTSTRAP_TOKEN_FILE", tokenFile)
	t.Setenv("FORT_BOOTSTRAP_REFRESH_TOKEN_FILE", refreshFile)

	var gotRefresh string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/auth/session/refresh" {
			http.NotFound(w, r)
			return
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode refresh request: %v", err)
		}
		gotRefresh = strings.TrimSpace(fmt.Sprint(req["refresh_token"]))
		_, _ = w.Write([]byte(`{"access_token":"new-access-token","refresh_token":"refresh-token-2","access_expires_at":"2030-01-01T00:00:00Z"}`))
	}))
	defer srv.Close()

	got, err := fortResolveBootstrapBearerToken(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("fortResolveBootstrapBearerToken: %v", err)
	}
	if got != "new-access-token" {
		t.Fatalf("unexpected access token: %q", got)
	}
	if gotRefresh != "refresh-token-1" {
		t.Fatalf("unexpected refresh token used: %q", gotRefresh)
	}
	tokenBytes, err := os.ReadFile(tokenFile)
	if err != nil {
		t.Fatalf("read token file: %v", err)
	}
	if strings.TrimSpace(string(tokenBytes)) != "new-access-token" {
		t.Fatalf("unexpected token file value: %q", string(tokenBytes))
	}
	refreshBytes, err := os.ReadFile(refreshFile)
	if err != nil {
		t.Fatalf("read refresh file: %v", err)
	}
	if strings.TrimSpace(string(refreshBytes)) != "refresh-token-2" {
		t.Fatalf("unexpected refresh file value: %q", string(refreshBytes))
	}
}

func TestFortResolveBootstrapBearerTokenFailsWhenExpiredAndRefreshUnauthorized(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	tokenFile := filepath.Join(home, ".si", "fort", "bootstrap", "admin.token")
	refreshFile := filepath.Join(home, ".si", "fort", "bootstrap", "admin.refresh.token")
	if err := os.MkdirAll(filepath.Dir(tokenFile), 0o700); err != nil {
		t.Fatalf("mkdir bootstrap dir: %v", err)
	}
	expiredToken := makeTestJWT(time.Now().Add(-3 * time.Minute))
	if err := os.WriteFile(tokenFile, []byte(expiredToken), 0o600); err != nil {
		t.Fatalf("write expired token: %v", err)
	}
	if err := os.WriteFile(refreshFile, []byte("refresh-token-1"), 0o600); err != nil {
		t.Fatalf("write refresh token: %v", err)
	}
	t.Setenv("FORT_BOOTSTRAP_TOKEN_FILE", tokenFile)
	t.Setenv("FORT_BOOTSTRAP_REFRESH_TOKEN_FILE", refreshFile)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/auth/session/refresh" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer srv.Close()

	got, err := fortResolveBootstrapBearerToken(context.Background(), srv.URL)
	if err == nil {
		t.Fatalf("expected refresh failure when bootstrap token is expired")
	}
	if got != "" {
		t.Fatalf("expected no token result on refresh failure, got %q", got)
	}
	if !strings.Contains(err.Error(), "expired/near expiry") {
		t.Fatalf("expected expiry-related error, got %v", err)
	}
	tokenBytes, readErr := os.ReadFile(tokenFile)
	if readErr != nil {
		t.Fatalf("read token file: %v", readErr)
	}
	if strings.TrimSpace(string(tokenBytes)) != strings.TrimSpace(expiredToken) {
		t.Fatalf("expected token file to remain unchanged after failed refresh")
	}
}

func TestFortResolveBootstrapBearerTokenKeepsValidTokenWhenRefreshUnauthorized(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	tokenFile := filepath.Join(home, ".si", "fort", "bootstrap", "admin.token")
	refreshFile := filepath.Join(home, ".si", "fort", "bootstrap", "admin.refresh.token")
	if err := os.MkdirAll(filepath.Dir(tokenFile), 0o700); err != nil {
		t.Fatalf("mkdir bootstrap dir: %v", err)
	}
	validToken := makeTestJWT(time.Now().Add(30 * time.Minute))
	if err := os.WriteFile(tokenFile, []byte(validToken), 0o600); err != nil {
		t.Fatalf("write valid token: %v", err)
	}
	if err := os.WriteFile(refreshFile, []byte("refresh-token-1"), 0o600); err != nil {
		t.Fatalf("write refresh token: %v", err)
	}
	t.Setenv("FORT_BOOTSTRAP_TOKEN_FILE", tokenFile)
	t.Setenv("FORT_BOOTSTRAP_REFRESH_TOKEN_FILE", refreshFile)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/auth/session/refresh" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer srv.Close()

	got, err := fortResolveBootstrapBearerToken(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("expected valid bootstrap token to be kept on refresh failure, got %v", err)
	}
	if strings.TrimSpace(got) != strings.TrimSpace(validToken) {
		t.Fatalf("expected existing valid token, got %q", got)
	}
}

func TestResolveBootstrapConfigAndEnsureAgentWithExpiredBootstrapToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	tokenFile := filepath.Join(home, ".si", "fort", "bootstrap", "admin.token")
	refreshFile := filepath.Join(home, ".si", "fort", "bootstrap", "admin.refresh.token")
	if err := os.MkdirAll(filepath.Dir(tokenFile), 0o700); err != nil {
		t.Fatalf("mkdir bootstrap dir: %v", err)
	}
	if err := os.WriteFile(tokenFile, []byte(makeTestJWT(time.Now().Add(-5*time.Minute))), 0o600); err != nil {
		t.Fatalf("write expired token: %v", err)
	}
	if err := os.WriteFile(refreshFile, []byte("refresh-token-old"), 0o600); err != nil {
		t.Fatalf("write refresh token: %v", err)
	}
	t.Setenv("FORT_BOOTSTRAP_TOKEN_FILE", tokenFile)
	t.Setenv("FORT_BOOTSTRAP_REFRESH_TOKEN_FILE", refreshFile)
	t.Setenv("SI_FORT_ALLOW_INSECURE_HOST", "1")

	refreshCalls := 0
	createCalls := 0
	enableCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/auth/session/refresh":
			refreshCalls++
			var req map[string]any
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode refresh request: %v", err)
			}
			if got := strings.TrimSpace(fmt.Sprint(req["refresh_token"])); got != "refresh-token-old" {
				t.Fatalf("unexpected refresh token request: %q", got)
			}
			_, _ = w.Write([]byte(`{"access_token":"fresh-admin-token","refresh_token":"refresh-token-new","access_expires_at":"2030-01-01T00:00:00Z"}`))
			return
		case r.Method == http.MethodPost && r.URL.Path == "/v1/agents":
			createCalls++
			if got := strings.TrimSpace(r.Header.Get("Authorization")); got != "Bearer fresh-admin-token" {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		case r.Method == http.MethodPost && r.URL.Path == "/v1/agents/si-codex-ferma/enable":
			enableCalls++
			if got := strings.TrimSpace(r.Header.Get("Authorization")); got != "Bearer fresh-admin-token" {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		default:
			http.NotFound(w, r)
			return
		}
	}))
	defer srv.Close()

	t.Setenv("FORT_HOST", srv.URL)
	t.Setenv("SI_FORT_CONTAINER_HOST", srv.URL)

	cfg, err := resolveFortBootstrapConfig(context.Background(), nil, "")
	if err != nil {
		t.Fatalf("resolveFortBootstrapConfig: %v", err)
	}
	if cfg.BearerToken != "fresh-admin-token" {
		t.Fatalf("unexpected bootstrap bearer token: %q", cfg.BearerToken)
	}
	if err := fortEnsureAgent(context.Background(), cfg, "si-codex-ferma"); err != nil {
		t.Fatalf("fortEnsureAgent: %v", err)
	}
	if refreshCalls != 1 || createCalls != 1 || enableCalls != 1 {
		t.Fatalf("unexpected call counts refresh=%d create=%d enable=%d", refreshCalls, createCalls, enableCalls)
	}
	if got := strings.TrimSpace(readFileOrEmpty(t, tokenFile)); got != "fresh-admin-token" {
		t.Fatalf("unexpected token file content: %q", got)
	}
	if got := strings.TrimSpace(readFileOrEmpty(t, refreshFile)); got != "refresh-token-new" {
		t.Fatalf("unexpected refresh file content: %q", got)
	}
}

func TestEnsureCodexProfileFortSessionPrefersExistingProfileRefresh(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SI_FORT_ALLOW_INSECURE_HOST", "1")

	t.Setenv("FORT_BOOTSTRAP_TOKEN_FILE", filepath.Join(home, ".si", "fort", "bootstrap", "missing-admin.token"))
	t.Setenv("FORT_BOOTSTRAP_REFRESH_TOKEN_FILE", filepath.Join(home, ".si", "fort", "bootstrap", "missing-admin.refresh.token"))

	profile := codexProfile{ID: "berylla"}
	paths, err := fortProfileStatePaths(profile)
	if err != nil {
		t.Fatalf("fortProfileStatePaths: %v", err)
	}
	if err := writeSecretFile(paths.AccessTokenHostPath, "old-profile-access"); err != nil {
		t.Fatalf("write profile access token: %v", err)
	}
	if err := writeSecretFile(paths.RefreshTokenHostPath, "profile-refresh-1"); err != nil {
		t.Fatalf("write profile refresh token: %v", err)
	}

	refreshCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/auth/session/refresh" {
			http.NotFound(w, r)
			return
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode refresh request: %v", err)
		}
		refreshCalls++
		switch got := strings.TrimSpace(fmt.Sprint(req["refresh_token"])); got {
		case "profile-refresh-1":
			_, _ = w.Write([]byte(`{"access_token":"profile-access-2","refresh_token":"profile-refresh-2","access_expires_at":"2030-01-01T00:00:00Z"}`))
		default:
			t.Fatalf("unexpected refresh token: %q", got)
		}
	}))
	defer srv.Close()
	t.Setenv("FORT_HOST", srv.URL)
	t.Setenv("SI_FORT_CONTAINER_HOST", srv.URL)

	state := fortProfileSessionState{
		ProfileID:        profile.ID,
		AgentID:          fortAgentIDForProfile(profile.ID),
		SessionID:        "rfs_existing",
		Host:             srv.URL,
		ContainerHost:    srv.URL,
		AccessTokenPath:  paths.AccessTokenHostPath,
		RefreshTokenPath: paths.RefreshTokenHostPath,
		RefreshExpiresAt: "2030-01-02T00:00:00Z",
		UpdatedAt:        time.Now().UTC().Format(time.RFC3339),
	}
	if err := saveFortProfileSessionState(paths.SessionStateHostPath, state); err != nil {
		t.Fatalf("saveFortProfileSessionState: %v", err)
	}

	boot, err := ensureCodexProfileFortSession(context.Background(), nil, profile, "")
	if err != nil {
		t.Fatalf("ensureCodexProfileFortSession: %v", err)
	}
	if refreshCalls != 1 {
		t.Fatalf("expected only profile refresh, got %d calls", refreshCalls)
	}
	if boot.ProfileID != profile.ID {
		t.Fatalf("unexpected profile id: %q", boot.ProfileID)
	}
	if boot.AgentID != fortAgentIDForProfile(profile.ID) {
		t.Fatalf("unexpected agent id: %q", boot.AgentID)
	}
	if got := strings.TrimSpace(readFileOrEmpty(t, paths.AccessTokenHostPath)); got != "profile-access-2" {
		t.Fatalf("unexpected profile access token: %q", got)
	}
	if got := strings.TrimSpace(readFileOrEmpty(t, paths.RefreshTokenHostPath)); got != "profile-refresh-2" {
		t.Fatalf("unexpected profile refresh token: %q", got)
	}
	updatedState, err := loadFortProfileSessionState(paths.SessionStateHostPath)
	if err != nil {
		t.Fatalf("loadFortProfileSessionState: %v", err)
	}
	if updatedState.AccessExpiresAt != "2030-01-01T00:00:00Z" {
		t.Fatalf("unexpected access expiry: %q", updatedState.AccessExpiresAt)
	}
	if updatedState.Host != srv.URL || updatedState.ContainerHost != srv.URL {
		t.Fatalf("unexpected session state hosts: host=%q container=%q", updatedState.Host, updatedState.ContainerHost)
	}
}

func TestFortTokenNeedsRefreshWithFutureExp(t *testing.T) {
	token := makeTestJWT(time.Now().Add(10 * time.Minute))
	needs, reason := fortTokenNeedsRefresh(token)
	if needs {
		t.Fatalf("expected non-expiring token to be accepted, reason=%q", reason)
	}
}

func makeTestJWT(expiry time.Time) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, expiry.UTC().Unix())))
	return header + "." + payload + ".x"
}

func readFileOrEmpty(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file %s: %v", path, err)
	}
	return string(raw)
}

func TestResolveFortBootstrapConfigRejectsWeakTokenFilePermissions(t *testing.T) {
	tmp := t.TempDir()
	tokenFile := filepath.Join(tmp, "admin.token")
	if err := os.WriteFile(tokenFile, []byte("file-token"), 0o644); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	t.Setenv("FORT_BOOTSTRAP_TOKEN_FILE", tokenFile)
	t.Setenv("FORT_HOST", "https://fort.example.test")
	t.Setenv("SI_FORT_CONTAINER_HOST", "https://fort.example.test")

	if _, err := resolveFortBootstrapConfig(context.Background(), nil, ""); err == nil {
		t.Fatalf("expected weak token file permissions to be rejected")
	}
}

func TestResolveFortBootstrapConfigRejectsInsecureFortHostByDefault(t *testing.T) {
	tmp := t.TempDir()
	tokenFile := filepath.Join(tmp, "admin.token")
	if err := os.WriteFile(tokenFile, []byte(makeTestJWT(time.Now().Add(30*time.Minute))), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	t.Setenv("FORT_BOOTSTRAP_TOKEN_FILE", tokenFile)
	t.Setenv("FORT_HOST", "http://127.0.0.1:8088")
	t.Setenv("SI_FORT_CONTAINER_HOST", "")
	t.Setenv("SI_FORT_ALLOW_INSECURE_HOST", "")

	if _, err := resolveFortBootstrapConfig(context.Background(), nil, ""); err == nil {
		t.Fatalf("expected insecure host to be rejected")
	}
}

func TestResolveFortBootstrapConfigAllowsInsecureWhenExplicitlyEnabled(t *testing.T) {
	tmp := t.TempDir()
	tokenFile := filepath.Join(tmp, "admin.token")
	if err := os.WriteFile(tokenFile, []byte(makeTestJWT(time.Now().Add(30*time.Minute))), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	t.Setenv("FORT_BOOTSTRAP_TOKEN_FILE", tokenFile)
	t.Setenv("FORT_HOST", "http://127.0.0.1:8088")
	t.Setenv("SI_FORT_CONTAINER_HOST", "http://host.docker.internal:8088")
	t.Setenv("SI_FORT_ALLOW_INSECURE_HOST", "1")

	cfg, err := resolveFortBootstrapConfig(context.Background(), nil, "")
	if err != nil {
		t.Fatalf("resolveFortBootstrapConfig: %v", err)
	}
	if cfg.HostURL != "http://127.0.0.1:8088" {
		t.Fatalf("unexpected host: %q", cfg.HostURL)
	}
	if cfg.ContainerHostURL != "http://host.docker.internal:8088" {
		t.Fatalf("unexpected container host: %q", cfg.ContainerHostURL)
	}
}

func TestFortAgentIDForProfile(t *testing.T) {
	got := fortAgentIDForProfile("PROFILE_01!")
	if got != "si-codex-profile-01" {
		t.Fatalf("unexpected agent id: %q", got)
	}
}

func TestPrepareFortRuntimeAuthRefresh(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SI_CODEX_PROFILE_ID", "alpha")
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
	t.Setenv("FORT_HOST", "http://127.0.0.1:1")
	// Avoid inheriting runtime container token paths from the parent process.
	t.Setenv("FORT_TOKEN_PATH", "")
	t.Setenv("FORT_REFRESH_TOKEN_PATH", "")

	accessToken, err := prepareFortRuntimeAuth([]string{"auth", "session", "close"})
	if err != nil {
		t.Fatalf("prepareFortRuntimeAuth: %v", err)
	}
	if accessToken != "" {
		t.Fatalf("expected empty access token when no token file is present, got %q", accessToken)
	}
}

func TestFortRequireAgentPolicyBindingsRejectsEmpty(t *testing.T) {
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
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))
	defer srv.Close()

	err := fortRequireAgentPolicyBindings(context.Background(), fortBootstrapConfig{
		HostURL:     srv.URL,
		BearerToken: "admin-test-token",
	}, "si-codex-alpha")
	if err == nil {
		t.Fatalf("expected missing policy bindings error")
	}
}

func TestFortRequireAgentPolicyBindingsAcceptsNonAdminPolicy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/agents/si-codex-alpha/policy" {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"bindings":[{"repo":"safe","env":"dev","ops":["get"]}]}`))
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))
	defer srv.Close()

	err := fortRequireAgentPolicyBindings(context.Background(), fortBootstrapConfig{
		HostURL:     srv.URL,
		BearerToken: "admin-test-token",
	}, "si-codex-alpha")
	if err != nil {
		t.Fatalf("fortRequireAgentPolicyBindings: %v", err)
	}
}

func TestFortRequireAgentPolicyBindingsAcceptsAdminPolicy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/agents/si-codex-alpha/policy" {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"bindings":[{"repo":"*","env":"*","ops":["*"]}]}`))
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))
	defer srv.Close()

	err := fortRequireAgentPolicyBindings(context.Background(), fortBootstrapConfig{
		HostURL:     srv.URL,
		BearerToken: "admin-test-token",
	}, "si-codex-alpha")
	if err != nil {
		t.Fatalf("fortRequireAgentPolicyBindings: %v", err)
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
		ContainerHost: "https://fort.example.test",
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
	if boot.ContainerHostURL != "https://fort.example.test" {
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
