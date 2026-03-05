package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	t.Setenv("FORT_REFRESH_TOKEN", "")
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

	if err := prepareFortRuntimeAuth([]string{"get"}); err != nil {
		t.Fatalf("prepareFortRuntimeAuth: %v", err)
	}
	if refreshCalls != 1 {
		t.Fatalf("expected one refresh call, got %d", refreshCalls)
	}
	if got := strings.TrimSpace(os.Getenv("FORT_TOKEN")); got != "access-2" {
		t.Fatalf("unexpected FORT_TOKEN: %q", got)
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
	t.Setenv("FORT_REFRESH_TOKEN", "refresh-1")
	t.Setenv("FORT_HOST", "http://127.0.0.1:1")

	if err := prepareFortRuntimeAuth([]string{"auth", "session", "close"}); err != nil {
		t.Fatalf("prepareFortRuntimeAuth: %v", err)
	}
	if got := strings.TrimSpace(os.Getenv("FORT_REFRESH_TOKEN")); got != "refresh-1" {
		t.Fatalf("expected refresh token to be unchanged, got %q", got)
	}
}
