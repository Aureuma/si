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

func TestLoadFortRuntimeAgentStateDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"profile_id\":\"alpha\",\"pid\":4242,\"command_path\":\"/tmp/si\",\"started_at\":\"2030-01-01T00:00:00Z\",\"updated_at\":\"2030-01-01T00:00:01Z\"}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

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
