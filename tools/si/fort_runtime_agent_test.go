package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEnsureFortRuntimeAgentLockedStartsOncePerProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	profile := codexProfile{ID: "alpha"}
	paths, err := fortProfileStatePaths(profile)
	if err != nil {
		t.Fatalf("fortProfileStatePaths: %v", err)
	}

	startCalls := 0
	prevStart := fortRuntimeAgentStartProcess
	prevAlive := fortRuntimeAgentProcessAlive
	t.Cleanup(func() {
		fortRuntimeAgentStartProcess = prevStart
		fortRuntimeAgentProcessAlive = prevAlive
	})
	fortRuntimeAgentStartProcess = func(profile codexProfile, paths fortProfilePaths) (fortProfileRuntimeAgentState, error) {
		startCalls++
		now := time.Now().UTC().Format(time.RFC3339)
		return fortProfileRuntimeAgentState{ProfileID: profile.ID, PID: 4242, StartedAt: now, UpdatedAt: now}, nil
	}
	fortRuntimeAgentProcessAlive = func(state fortProfileRuntimeAgentState, profile codexProfile) bool {
		return state.PID == 4242
	}

	if err := ensureFortRuntimeAgentLocked(profile, paths); err != nil {
		t.Fatalf("ensureFortRuntimeAgentLocked: %v", err)
	}
	if err := ensureFortRuntimeAgentLocked(profile, paths); err != nil {
		t.Fatalf("ensureFortRuntimeAgentLocked second call: %v", err)
	}
	if startCalls != 1 {
		t.Fatalf("expected a single agent start, got %d", startCalls)
	}
}

func TestFortRuntimeAgentStepRefreshesAndPersistsState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SI_FORT_ALLOW_INSECURE_HOST", "1")
	profile := codexProfile{ID: "alpha"}
	paths, err := fortProfileStatePaths(profile)
	if err != nil {
		t.Fatalf("fortProfileStatePaths: %v", err)
	}
	if err := writeSecretFile(paths.RefreshTokenHostPath, "refresh-1"); err != nil {
		t.Fatalf("write refresh token: %v", err)
	}

	refreshCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/auth/session/refresh" {
			http.NotFound(w, r)
			return
		}
		refreshCalls++
		_, _ = w.Write([]byte(`{"access_token":"` + makeTestJWT(time.Now().Add(10*time.Minute)) + `","refresh_token":"refresh-2","access_expires_at":"2030-01-01T00:00:00Z"}`))
	}))
	defer srv.Close()

	state := fortProfileSessionState{
		ProfileID:        profile.ID,
		AgentID:          fortAgentIDForProfile(profile.ID),
		SessionID:        "rfs_existing",
		Host:             srv.URL,
		ContainerHost:    srv.URL,
		AccessTokenPath:  paths.AccessTokenHostPath,
		RefreshTokenPath: paths.RefreshTokenHostPath,
	}
	if err := saveFortProfileSessionState(paths.SessionStateHostPath, state); err != nil {
		t.Fatalf("saveFortProfileSessionState: %v", err)
	}

	delay, err := fortRuntimeAgentStep(context.Background(), profile, paths)
	if err != nil {
		t.Fatalf("fortRuntimeAgentStep: %v", err)
	}
	if refreshCalls != 1 {
		t.Fatalf("expected one refresh call, got %d", refreshCalls)
	}
	if delay <= 0 {
		t.Fatalf("expected positive delay, got %s", delay)
	}
	updatedState, err := loadFortProfileSessionState(paths.SessionStateHostPath)
	if err != nil {
		t.Fatalf("loadFortProfileSessionState: %v", err)
	}
	if updatedState.AccessExpiresAt != "2030-01-01T00:00:00Z" {
		t.Fatalf("unexpected access expiry: %q", updatedState.AccessExpiresAt)
	}
	if got := strings.TrimSpace(readFileOrEmpty(t, paths.RefreshTokenHostPath)); got != "refresh-2" {
		t.Fatalf("unexpected refresh token contents: %q", got)
	}
}

func TestFortRuntimeAgentStepDelegatesRefreshTransitionToRustCLIWhenConfigured(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SI_FORT_ALLOW_INSECURE_HOST", "1")
	profile := codexProfile{ID: "alpha"}
	paths, err := fortProfileStatePaths(profile)
	if err != nil {
		t.Fatalf("fortProfileStatePaths: %v", err)
	}
	if err := writeSecretFile(paths.RefreshTokenHostPath, "refresh-1"); err != nil {
		t.Fatalf("write refresh token: %v", err)
	}

	refreshCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/auth/session/refresh" {
			http.NotFound(w, r)
			return
		}
		refreshCalls++
		_, _ = w.Write([]byte(`{"access_token":"` + makeTestJWT(time.Now().Add(10*time.Minute)) + `","refresh_token":"refresh-2","access_expires_at":"2030-01-01T00:00:00Z"}`))
	}))
	defer srv.Close()

	state := fortProfileSessionState{
		ProfileID:        profile.ID,
		AgentID:          fortAgentIDForProfile(profile.ID),
		SessionID:        "rfs_existing",
		Host:             srv.URL,
		ContainerHost:    srv.URL,
		AccessTokenPath:  paths.AccessTokenHostPath,
		RefreshTokenPath: paths.RefreshTokenHostPath,
		RefreshExpiresAt: "2030-02-01T00:00:00Z",
	}
	if err := saveFortProfileSessionState(paths.SessionStateHostPath, state); err != nil {
		t.Fatalf("saveFortProfileSessionState: %v", err)
	}

	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >>" + shellSingleQuote(argsPath) + "\nif [ \"$1\" = \"fort\" ] && [ \"$2\" = \"session-state\" ] && [ \"$3\" = \"show\" ]; then\n  printf '%s\\n' '{\"profile_id\":\"alpha\",\"agent_id\":\"si-codex-alpha\",\"session_id\":\"rfs_existing\",\"host\":\"" + srv.URL + "\",\"container_host\":\"" + srv.URL + "\",\"access_token_path\":\"" + strings.ReplaceAll(paths.AccessTokenHostPath, "\\", "\\\\") + "\",\"refresh_token_path\":\"" + strings.ReplaceAll(paths.RefreshTokenHostPath, "\\", "\\\\") + "\",\"refresh_expires_at\":\"2030-02-01T00:00:00Z\"}'\n  exit 0\nfi\nif [ \"$1\" = \"fort\" ] && [ \"$2\" = \"session-state\" ] && [ \"$3\" = \"classify\" ]; then\n  printf '%s\\n' '\"Resumable\"'\n  exit 0\nfi\nif [ \"$1\" = \"fort\" ] && [ \"$2\" = \"session-state\" ] && [ \"$3\" = \"bootstrap-view\" ]; then\n  printf '%s\\n' '{\"profile_id\":\"alpha\",\"agent_id\":\"si-codex-alpha\",\"session_id\":\"rfs_existing\",\"host_url\":\"" + srv.URL + "\",\"container_host_url\":\"" + srv.URL + "\",\"access_token_path\":\"" + strings.ReplaceAll(paths.AccessTokenHostPath, "\\", "\\\\") + "\",\"refresh_token_path\":\"" + strings.ReplaceAll(paths.RefreshTokenHostPath, "\\", "\\\\") + "\",\"access_token_container_path\":\"" + strings.ReplaceAll(paths.AccessTokenContainerPath, "\\", "\\\\") + "\",\"refresh_token_container_path\":\"" + strings.ReplaceAll(paths.RefreshTokenContainerPath, "\\", "\\\\") + "\"}'\n  exit 0\nfi\nif [ \"$1\" = \"fort\" ] && [ \"$2\" = \"session-state\" ] && [ \"$3\" = \"refresh-outcome\" ]; then\n  printf '%s\\n' '{\"state\":{\"profile_id\":\"alpha\",\"agent_id\":\"si-codex-alpha\",\"session_id\":\"rfs_existing\",\"host\":\"" + srv.URL + "\",\"container_host\":\"" + srv.URL + "\",\"access_token_path\":\"" + strings.ReplaceAll(paths.AccessTokenHostPath, "\\", "\\\\") + "\",\"refresh_token_path\":\"" + strings.ReplaceAll(paths.RefreshTokenHostPath, "\\", "\\\\") + "\",\"access_expires_at\":\"2030-01-01T00:00:00Z\",\"refresh_expires_at\":\"2030-02-01T00:00:00Z\"},\"classification\":{\"state\":\"resumable\"}}'\n  exit 0\nfi\nif [ \"$1\" = \"fort\" ] && [ \"$2\" = \"session-state\" ] && [ \"$3\" = \"write\" ]; then\n  exit 0\nfi\nexit 1\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siRustCLILegacyToggleEnv, "")

	if _, err := fortRuntimeAgentStep(context.Background(), profile, paths); err != nil {
		t.Fatalf("fortRuntimeAgentStep: %v", err)
	}
	if refreshCalls != 1 {
		t.Fatalf("expected one refresh call, got %d", refreshCalls)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if !strings.Contains(string(argsData), "fort\nsession-state\nrefresh-outcome\n") {
		t.Fatalf("expected refresh-outcome delegation, got %q", string(argsData))
	}
	if !strings.Contains(string(argsData), "fort\nsession-state\nwrite\n") {
		t.Fatalf("expected delegated state write, got %q", string(argsData))
	}
}

func TestFortRuntimeAgentStepUsesRustBootstrapViewForHostResolution(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SI_FORT_ALLOW_INSECURE_HOST", "1")
	profile := codexProfile{ID: "alpha"}
	paths, err := fortProfileStatePaths(profile)
	if err != nil {
		t.Fatalf("fortProfileStatePaths: %v", err)
	}
	if err := writeSecretFile(paths.RefreshTokenHostPath, "refresh-1"); err != nil {
		t.Fatalf("write refresh token: %v", err)
	}

	refreshCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/auth/session/refresh" {
			http.NotFound(w, r)
			return
		}
		refreshCalls++
		_, _ = w.Write([]byte(`{"access_token":"` + makeTestJWT(time.Now().Add(10*time.Minute)) + `","refresh_token":"refresh-2","access_expires_at":"2030-01-01T00:00:00Z"}`))
	}))
	defer srv.Close()

	state := fortProfileSessionState{
		ProfileID:        profile.ID,
		AgentID:          fortAgentIDForProfile(profile.ID),
		SessionID:        "rfs_existing",
		Host:             "",
		ContainerHost:    "",
		AccessTokenPath:  paths.AccessTokenHostPath,
		RefreshTokenPath: paths.RefreshTokenHostPath,
		RefreshExpiresAt: "2030-02-01T00:00:00Z",
	}
	if err := saveFortProfileSessionState(paths.SessionStateHostPath, state); err != nil {
		t.Fatalf("saveFortProfileSessionState: %v", err)
	}

	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >>" + shellSingleQuote(argsPath) + "\nif [ \"$1\" = \"fort\" ] && [ \"$2\" = \"session-state\" ] && [ \"$3\" = \"show\" ]; then\n  printf '%s\\n' '{\"profile_id\":\"alpha\",\"agent_id\":\"si-codex-alpha\",\"session_id\":\"rfs_existing\",\"host\":\"\",\"container_host\":\"\",\"access_token_path\":\"" + strings.ReplaceAll(paths.AccessTokenHostPath, "\\", "\\\\") + "\",\"refresh_token_path\":\"" + strings.ReplaceAll(paths.RefreshTokenHostPath, "\\", "\\\\") + "\",\"refresh_expires_at\":\"2030-02-01T00:00:00Z\"}'\n  exit 0\nfi\nif [ \"$1\" = \"fort\" ] && [ \"$2\" = \"session-state\" ] && [ \"$3\" = \"classify\" ]; then\n  printf '%s\\n' '\"Resumable\"'\n  exit 0\nfi\nif [ \"$1\" = \"fort\" ] && [ \"$2\" = \"session-state\" ] && [ \"$3\" = \"bootstrap-view\" ]; then\n  printf '%s\\n' '{\"profile_id\":\"alpha\",\"agent_id\":\"si-codex-alpha\",\"session_id\":\"rfs_existing\",\"host_url\":\"" + srv.URL + "\",\"container_host_url\":\"" + srv.URL + "\",\"access_token_path\":\"" + strings.ReplaceAll(paths.AccessTokenHostPath, "\\", "\\\\") + "\",\"refresh_token_path\":\"" + strings.ReplaceAll(paths.RefreshTokenHostPath, "\\", "\\\\") + "\",\"access_token_container_path\":\"" + strings.ReplaceAll(paths.AccessTokenContainerPath, "\\", "\\\\") + "\",\"refresh_token_container_path\":\"" + strings.ReplaceAll(paths.RefreshTokenContainerPath, "\\", "\\\\") + "\"}'\n  exit 0\nfi\nif [ \"$1\" = \"fort\" ] && [ \"$2\" = \"session-state\" ] && [ \"$3\" = \"refresh-outcome\" ]; then\n  printf '%s\\n' '{\"state\":{\"profile_id\":\"alpha\",\"agent_id\":\"si-codex-alpha\",\"session_id\":\"rfs_existing\",\"host\":\"" + srv.URL + "\",\"container_host\":\"" + srv.URL + "\",\"access_token_path\":\"" + strings.ReplaceAll(paths.AccessTokenHostPath, "\\", "\\\\") + "\",\"refresh_token_path\":\"" + strings.ReplaceAll(paths.RefreshTokenHostPath, "\\", "\\\\") + "\",\"access_expires_at\":\"2030-01-01T00:00:00Z\",\"refresh_expires_at\":\"2030-02-01T00:00:00Z\"},\"classification\":{\"state\":\"resumable\"}}'\n  exit 0\nfi\nif [ \"$1\" = \"fort\" ] && [ \"$2\" = \"session-state\" ] && [ \"$3\" = \"write\" ]; then\n  exit 0\nfi\nexit 1\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siRustCLILegacyToggleEnv, "")

	if _, err := fortRuntimeAgentStep(context.Background(), profile, paths); err != nil {
		t.Fatalf("fortRuntimeAgentStep: %v", err)
	}
	if refreshCalls != 1 {
		t.Fatalf("expected one refresh call, got %d", refreshCalls)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if !strings.Contains(string(argsData), "fort\nsession-state\nbootstrap-view\n") {
		t.Fatalf("expected bootstrap-view delegation, got %q", string(argsData))
	}
}

func TestFortRuntimeAgentStepPersistsRustRevocationOnUnauthorized(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SI_FORT_ALLOW_INSECURE_HOST", "1")
	profile := codexProfile{ID: "alpha"}
	paths, err := fortProfileStatePaths(profile)
	if err != nil {
		t.Fatalf("fortProfileStatePaths: %v", err)
	}
	if err := writeSecretFile(paths.RefreshTokenHostPath, "refresh-1"); err != nil {
		t.Fatalf("write refresh token: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/auth/session/refresh" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer srv.Close()

	state := fortProfileSessionState{
		ProfileID:        profile.ID,
		AgentID:          fortAgentIDForProfile(profile.ID),
		SessionID:        "rfs_existing",
		Host:             srv.URL,
		ContainerHost:    srv.URL,
		AccessTokenPath:  paths.AccessTokenHostPath,
		RefreshTokenPath: paths.RefreshTokenHostPath,
		RefreshExpiresAt: "2030-02-01T00:00:00Z",
	}
	if err := saveFortProfileSessionState(paths.SessionStateHostPath, state); err != nil {
		t.Fatalf("saveFortProfileSessionState: %v", err)
	}

	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\n' \"$@\" >>" + shellSingleQuote(argsPath) + `
if [ "$1" = "fort" ] && [ "$2" = "session-state" ] && [ "$3" = "show" ]; then
  printf '%s\n' '{"profile_id":"alpha","agent_id":"si-codex-alpha","session_id":"rfs_existing","host":"` + srv.URL + `","container_host":"` + srv.URL + `","access_token_path":"` + strings.ReplaceAll(paths.AccessTokenHostPath, "\\", "\\\\") + `","refresh_token_path":"` + strings.ReplaceAll(paths.RefreshTokenHostPath, "\\", "\\\\") + `","refresh_expires_at":"2030-02-01T00:00:00Z"}'
  exit 0
fi
if [ "$1" = "fort" ] && [ "$2" = "session-state" ] && [ "$3" = "classify" ]; then
  printf '%s\n' '"Resumable"'
  exit 0
fi
if [ "$1" = "fort" ] && [ "$2" = "session-state" ] && [ "$3" = "bootstrap-view" ]; then
  printf '%s\n' '{"profile_id":"alpha","agent_id":"si-codex-alpha","session_id":"rfs_existing","host_url":"` + srv.URL + `","container_host_url":"` + srv.URL + `","access_token_path":"` + strings.ReplaceAll(paths.AccessTokenHostPath, "\\", "\\\\") + `","refresh_token_path":"` + strings.ReplaceAll(paths.RefreshTokenHostPath, "\\", "\\\\") + `","access_token_container_path":"` + strings.ReplaceAll(paths.AccessTokenContainerPath, "\\", "\\\\") + `","refresh_token_container_path":"` + strings.ReplaceAll(paths.RefreshTokenContainerPath, "\\", "\\\\") + `"}'
  exit 0
fi
if [ "$1" = "fort" ] && [ "$2" = "session-state" ] && [ "$3" = "refresh-outcome" ]; then
  printf '%s\n' '{"state":{"profile_id":"alpha","agent_id":"si-codex-alpha","session_id":"","host":"` + srv.URL + `","container_host":"` + srv.URL + `","access_token_path":"` + strings.ReplaceAll(paths.AccessTokenHostPath, "\\", "\\\\") + `","refresh_token_path":"` + strings.ReplaceAll(paths.RefreshTokenHostPath, "\\", "\\\\") + `","refresh_expires_at":"2030-02-01T00:00:00Z"},"classification":{"state":"revoked","reason":"RefreshUnauthorized"}}'
  exit 0
fi
if [ "$1" = "fort" ] && [ "$2" = "session-state" ] && [ "$3" = "write" ]; then
  exit 0
fi
exit 1
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siRustCLILegacyToggleEnv, "")

	if _, err := fortRuntimeAgentStep(context.Background(), profile, paths); err == nil {
		t.Fatalf("expected unauthorized refresh to fail")
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if !strings.Contains(string(argsData), "--outcome\nunauthorized") {
		t.Fatalf("expected unauthorized refresh-outcome delegation, got %q", string(argsData))
	}
	if !strings.Contains(string(argsData), "fort\nsession-state\nwrite\n") {
		t.Fatalf("expected delegated state write after unauthorized refresh, got %q", string(argsData))
	}
}

func TestCloseCodexProfileFortSessionRemovesArtifacts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SI_FORT_ALLOW_INSECURE_HOST", "1")
	profile := codexProfile{ID: "alpha"}
	paths, err := fortProfileStatePaths(profile)
	if err != nil {
		t.Fatalf("fortProfileStatePaths: %v", err)
	}
	if err := writeSecretFile(paths.AccessTokenHostPath, makeTestJWT(time.Now().Add(10*time.Minute))); err != nil {
		t.Fatalf("write access token: %v", err)
	}
	if err := writeSecretFile(paths.RefreshTokenHostPath, "refresh-1"); err != nil {
		t.Fatalf("write refresh token: %v", err)
	}
	if err := os.WriteFile(paths.RuntimeAgentLogHostPath, []byte("log"), 0o600); err != nil {
		t.Fatalf("write runtime log: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if err := saveFortRuntimeAgentState(paths.RuntimeAgentStateHostPath, fortProfileRuntimeAgentState{ProfileID: profile.ID, PID: 999999, StartedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("saveFortRuntimeAgentState: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/auth/session/close" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	state := fortProfileSessionState{
		ProfileID:        profile.ID,
		AgentID:          fortAgentIDForProfile(profile.ID),
		SessionID:        "rfs_existing",
		Host:             srv.URL,
		ContainerHost:    srv.URL,
		AccessTokenPath:  paths.AccessTokenHostPath,
		RefreshTokenPath: paths.RefreshTokenHostPath,
	}
	if err := saveFortProfileSessionState(paths.SessionStateHostPath, state); err != nil {
		t.Fatalf("saveFortProfileSessionState: %v", err)
	}

	if err := closeCodexProfileFortSession(profile); err != nil {
		t.Fatalf("closeCodexProfileFortSession: %v", err)
	}
	for _, path := range []string{
		paths.AccessTokenHostPath,
		paths.RefreshTokenHostPath,
		paths.SessionStateHostPath,
		paths.RuntimeAgentStateHostPath,
		paths.RuntimeAgentLogHostPath,
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed, err=%v", path, err)
		}
	}
}

func TestCloseCodexProfileFortSessionDelegatesTeardownToRustCLIWhenConfigured(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SI_FORT_ALLOW_INSECURE_HOST", "1")
	profile := codexProfile{ID: "alpha"}
	paths, err := fortProfileStatePaths(profile)
	if err != nil {
		t.Fatalf("fortProfileStatePaths: %v", err)
	}
	if err := writeSecretFile(paths.AccessTokenHostPath, makeTestJWT(time.Now().Add(10*time.Minute))); err != nil {
		t.Fatalf("write access token: %v", err)
	}
	if err := writeSecretFile(paths.RefreshTokenHostPath, "refresh-1"); err != nil {
		t.Fatalf("write refresh token: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if err := saveFortRuntimeAgentState(paths.RuntimeAgentStateHostPath, fortProfileRuntimeAgentState{
		ProfileID: profile.ID,
		PID:       999999,
		StartedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("saveFortRuntimeAgentState: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/auth/session/close" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	state := fortProfileSessionState{
		ProfileID:        profile.ID,
		AgentID:          fortAgentIDForProfile(profile.ID),
		SessionID:        "rfs_existing",
		Host:             srv.URL,
		ContainerHost:    srv.URL,
		AccessTokenPath:  paths.AccessTokenHostPath,
		RefreshTokenPath: paths.RefreshTokenHostPath,
		AccessExpiresAt:  "1970-01-01T00:01:30Z",
		RefreshExpiresAt: "1970-01-01T00:06:40Z",
	}
	if err := saveFortProfileSessionState(paths.SessionStateHostPath, state); err != nil {
		t.Fatalf("saveFortProfileSessionState: %v", err)
	}

	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >>" + shellSingleQuote(argsPath) + "\nif [ \"$1\" = \"fort\" ] && [ \"$2\" = \"session-state\" ] && [ \"$3\" = \"show\" ]; then\n  printf '%s\\n' '{\"profile_id\":\"alpha\",\"agent_id\":\"si-codex-alpha\",\"session_id\":\"rfs_existing\",\"host\":\"" + srv.URL + "\",\"container_host\":\"" + srv.URL + "\",\"access_token_path\":\"" + strings.ReplaceAll(paths.AccessTokenHostPath, "\\", "\\\\") + "\",\"refresh_token_path\":\"" + strings.ReplaceAll(paths.RefreshTokenHostPath, "\\", "\\\\") + "\",\"access_expires_at\":\"1970-01-01T00:01:30Z\",\"refresh_expires_at\":\"1970-01-01T00:06:40Z\"}'\n  exit 0\nfi\nif [ \"$1\" = \"fort\" ] && [ \"$2\" = \"session-state\" ] && [ \"$3\" = \"teardown\" ]; then\n  printf '%s\\n' '{\"state\":\"closed\"}'\n  exit 0\nfi\nif [ \"$1\" = \"fort\" ] && [ \"$2\" = \"session-state\" ] && [ \"$3\" = \"bootstrap-view\" ]; then\n  printf '%s\\n' '{\"profile_id\":\"alpha\",\"agent_id\":\"si-codex-alpha\",\"session_id\":\"rfs_existing\",\"host_url\":\"" + srv.URL + "\",\"container_host_url\":\"" + srv.URL + "\",\"access_token_path\":\"" + strings.ReplaceAll(paths.AccessTokenHostPath, "\\", "\\\\") + "\",\"refresh_token_path\":\"" + strings.ReplaceAll(paths.RefreshTokenHostPath, "\\", "\\\\") + "\",\"access_token_container_path\":\"" + strings.ReplaceAll(paths.AccessTokenContainerPath, "\\", "\\\\") + "\",\"refresh_token_container_path\":\"" + strings.ReplaceAll(paths.RefreshTokenContainerPath, "\\", "\\\\") + "\"}'\n  exit 0\nfi\nexit 1\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siRustCLILegacyToggleEnv, "")

	if err := closeCodexProfileFortSession(profile); err != nil {
		t.Fatalf("closeCodexProfileFortSession: %v", err)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if !strings.Contains(string(argsData), "fort\nsession-state\nteardown\n") {
		t.Fatalf("expected teardown delegation, got %q", string(argsData))
	}
}

func TestCloseCodexProfileFortSessionUsesRustBootstrapViewForRemoteClose(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SI_FORT_ALLOW_INSECURE_HOST", "1")
	profile := codexProfile{ID: "alpha"}
	paths, err := fortProfileStatePaths(profile)
	if err != nil {
		t.Fatalf("fortProfileStatePaths: %v", err)
	}
	if err := writeSecretFile(paths.RefreshTokenHostPath, "refresh-1"); err != nil {
		t.Fatalf("write refresh token: %v", err)
	}

	closeCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/auth/session/close" {
			http.NotFound(w, r)
			return
		}
		closeCalls++
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	state := fortProfileSessionState{
		ProfileID:        profile.ID,
		AgentID:          fortAgentIDForProfile(profile.ID),
		SessionID:        "rfs_existing",
		Host:             "",
		ContainerHost:    "",
		AccessTokenPath:  paths.AccessTokenHostPath,
		RefreshTokenPath: paths.RefreshTokenHostPath,
	}
	if err := saveFortProfileSessionState(paths.SessionStateHostPath, state); err != nil {
		t.Fatalf("saveFortProfileSessionState: %v", err)
	}

	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >>" + shellSingleQuote(argsPath) + "\nif [ \"$1\" = \"fort\" ] && [ \"$2\" = \"session-state\" ] && [ \"$3\" = \"show\" ]; then\n  printf '%s\\n' '{\"profile_id\":\"alpha\",\"agent_id\":\"si-codex-alpha\",\"session_id\":\"rfs_existing\",\"host\":\"\",\"container_host\":\"\",\"access_token_path\":\"" + strings.ReplaceAll(paths.AccessTokenHostPath, "\\", "\\\\") + "\",\"refresh_token_path\":\"" + strings.ReplaceAll(paths.RefreshTokenHostPath, "\\", "\\\\") + "\"}'\n  exit 0\nfi\nif [ \"$1\" = \"fort\" ] && [ \"$2\" = \"session-state\" ] && [ \"$3\" = \"teardown\" ]; then\n  printf '%s\\n' '{\"state\":\"closed\"}'\n  exit 0\nfi\nif [ \"$1\" = \"fort\" ] && [ \"$2\" = \"session-state\" ] && [ \"$3\" = \"bootstrap-view\" ]; then\n  printf '%s\\n' '{\"profile_id\":\"alpha\",\"agent_id\":\"si-codex-alpha\",\"session_id\":\"rfs_existing\",\"host_url\":\"" + srv.URL + "\",\"container_host_url\":\"" + srv.URL + "\",\"access_token_path\":\"" + strings.ReplaceAll(paths.AccessTokenHostPath, "\\", "\\\\") + "\",\"refresh_token_path\":\"" + strings.ReplaceAll(paths.RefreshTokenHostPath, "\\", "\\\\") + "\",\"access_token_container_path\":\"" + strings.ReplaceAll(paths.AccessTokenContainerPath, "\\", "\\\\") + "\",\"refresh_token_container_path\":\"" + strings.ReplaceAll(paths.RefreshTokenContainerPath, "\\", "\\\\") + "\"}'\n  exit 0\nfi\nexit 1\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siRustCLILegacyToggleEnv, "")

	if err := closeCodexProfileFortSession(profile); err != nil {
		t.Fatalf("closeCodexProfileFortSession: %v", err)
	}
	if closeCalls != 1 {
		t.Fatalf("expected one remote close call, got %d", closeCalls)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if !strings.Contains(string(argsData), "fort\nsession-state\nbootstrap-view\n") {
		t.Fatalf("expected bootstrap-view delegation, got %q", string(argsData))
	}
}

func TestLoadFortRuntimeAgentStateDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"profile_id\":\"alpha\",\"pid\":4242,\"command_path\":\"/tmp/si\",\"started_at\":\"2030-01-01T00:00:00Z\",\"updated_at\":\"2030-01-01T00:00:01Z\"}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siRustCLILegacyToggleEnv, "")

	state, err := loadFortRuntimeAgentState("/tmp/runtime-agent.json")
	if err != nil {
		t.Fatalf("loadFortRuntimeAgentState: %v", err)
	}
	if state.ProfileID != "alpha" || state.PID != 4242 {
		t.Fatalf("unexpected runtime agent state: %+v", state)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "fort\nruntime-agent-state\nshow\n--path\n/tmp/runtime-agent.json\n--format\njson" {
		t.Fatalf("unexpected rust cli args: %q", string(argsData))
	}
}

func TestSaveFortRuntimeAgentStateDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siRustCLILegacyToggleEnv, "")

	err := saveFortRuntimeAgentState("/tmp/runtime-agent.json", fortProfileRuntimeAgentState{
		ProfileID: "alpha",
		PID:       4242,
	})
	if err != nil {
		t.Fatalf("saveFortRuntimeAgentState: %v", err)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if !strings.Contains(string(argsData), "fort\nruntime-agent-state\nwrite\n--path\n/tmp/runtime-agent.json\n--state-json\n") {
		t.Fatalf("unexpected rust cli args: %q", string(argsData))
	}
}

func TestClearFortRuntimeAgentStateFileDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siRustCLILegacyToggleEnv, "")

	if err := clearFortRuntimeAgentStateFile("/tmp/runtime-agent.json"); err != nil {
		t.Fatalf("clearFortRuntimeAgentStateFile: %v", err)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "fort\nruntime-agent-state\nclear\n--path\n/tmp/runtime-agent.json" {
		t.Fatalf("unexpected rust cli args: %q", string(argsData))
	}
}

func TestClearFortSessionStateFileDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siRustCLILegacyToggleEnv, "")

	if err := clearFortSessionStateFile("/tmp/session.json"); err != nil {
		t.Fatalf("clearFortSessionStateFile: %v", err)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "fort\nsession-state\nclear\n--path\n/tmp/session.json" {
		t.Fatalf("unexpected rust cli args: %q", string(argsData))
	}
}

func TestFortRuntimeAgentStepHonorsRustRevokedClassification(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	profile := codexProfile{ID: "alpha"}
	paths, err := fortProfileStatePaths(profile)
	if err != nil {
		t.Fatalf("fortProfileStatePaths: %v", err)
	}
	if err := writeSecretFile(paths.RefreshTokenHostPath, "refresh-1"); err != nil {
		t.Fatalf("write refresh token: %v", err)
	}
	state := fortProfileSessionState{
		ProfileID:        profile.ID,
		AgentID:          fortAgentIDForProfile(profile.ID),
		SessionID:        "rfs_existing",
		Host:             "https://fort.example.test",
		ContainerHost:    "http://fort.internal:8088",
		AccessTokenPath:  paths.AccessTokenHostPath,
		RefreshTokenPath: paths.RefreshTokenHostPath,
	}
	if err := saveFortProfileSessionState(paths.SessionStateHostPath, state); err != nil {
		t.Fatalf("saveFortProfileSessionState: %v", err)
	}

	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >>" + shellSingleQuote(argsPath) + "\nif [ \"$1\" = \"fort\" ] && [ \"$2\" = \"session-state\" ] && [ \"$3\" = \"show\" ]; then\n  printf '%s\\n' '{\"profile_id\":\"alpha\",\"agent_id\":\"si-codex-alpha\",\"session_id\":\"rfs_existing\",\"host\":\"https://fort.example.test\",\"container_host\":\"http://fort.internal:8088\",\"access_token_path\":\"" + strings.ReplaceAll(paths.AccessTokenHostPath, "\\", "\\\\") + "\",\"refresh_token_path\":\"" + strings.ReplaceAll(paths.RefreshTokenHostPath, "\\", "\\\\") + "\"}'\n  exit 0\nfi\nif [ \"$1\" = \"fort\" ] && [ \"$2\" = \"session-state\" ] && [ \"$3\" = \"classify\" ]; then\n  printf '%s\\n' '{\"Revoked\":{\"snapshot\":{\"profile_id\":\"alpha\"},\"reason\":\"RefreshUnauthorized\"}}'\n  exit 0\nfi\nexit 1\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siRustCLILegacyToggleEnv, "")

	refreshCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshCalls++
		http.NotFound(w, r)
	}))
	defer srv.Close()
	t.Setenv("FORT_HOST", srv.URL)

	if _, err := fortRuntimeAgentStep(context.Background(), profile, paths); err == nil {
		t.Fatalf("expected revoked rust classification to stop runtime agent step")
	}
	if refreshCalls != 0 {
		t.Fatalf("expected no refresh attempt, got %d calls", refreshCalls)
	}
}
