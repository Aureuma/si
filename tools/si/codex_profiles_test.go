package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestIsValidCodexAuthFileAcceptsExpiredAccessTokenWhenRefreshExists(t *testing.T) {
	path := writeAuthFixture(t, profileAuthTokens{
		AccessToken:  testJWTWithExp(t, time.Now().UTC().Add(-1*time.Hour), false),
		RefreshToken: "refresh-token",
	})
	if !isValidCodexAuthFile(path, time.Now()) {
		t.Fatalf("expected auth with refresh token to be valid")
	}
}

func TestIsValidCodexAuthFileAcceptsExpiredIDTokenWhenAccessExists(t *testing.T) {
	path := writeAuthFixture(t, profileAuthTokens{
		AccessToken: "access-token",
		IDToken:     testJWTWithExp(t, time.Now().UTC().Add(-1*time.Hour), false),
	})
	if !isValidCodexAuthFile(path, time.Now()) {
		t.Fatalf("expected auth with access token to be valid")
	}
}

func TestIsValidCodexAuthFileRejectsMissingAccessAndRefresh(t *testing.T) {
	path := writeAuthFixture(t, profileAuthTokens{IDToken: "id-token-only"})
	if isValidCodexAuthFile(path, time.Now()) {
		t.Fatalf("expected auth without access/refresh to be invalid")
	}
}

func TestCodexProfileAuthStatusRecoversViaContainerSync(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	profile := codexProfile{ID: "cadma", Name: "Cadma", Email: "cadma@example.com"}

	prevFn := syncProfileAuthFromContainerStatusFn
	prevSunFn := syncProfileAuthFromSunStatusFn
	syncProfileAuthFromContainerStatusFn = func(ctx context.Context, p codexProfile) (profileAuthTokens, error) {
		path, err := codexProfileAuthPath(p)
		if err != nil {
			return profileAuthTokens{}, err
		}
		data, _ := json.Marshal(profileAuthFile{Tokens: &profileAuthTokens{AccessToken: "access-token"}})
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return profileAuthTokens{}, err
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return profileAuthTokens{}, err
		}
		return profileAuthTokens{AccessToken: "access-token"}, nil
	}
	codexAuthSyncAttempts = sync.Map{}
	defer func() {
		syncProfileAuthFromContainerStatusFn = prevFn
		syncProfileAuthFromSunStatusFn = prevSunFn
		codexAuthSyncAttempts = sync.Map{}
	}()

	status := codexProfileAuthStatus(profile)
	if !status.Exists {
		t.Fatalf("expected auth status to recover from container sync")
	}
	if strings.TrimSpace(status.Path) == "" {
		t.Fatalf("expected auth path to be populated")
	}
}

func TestCodexProfileAuthStatusRecoversViaSunSync(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	profile := codexProfile{ID: "cadma", Name: "Cadma", Email: "cadma@example.com"}

	prevContainerFn := syncProfileAuthFromContainerStatusFn
	prevSunFn := syncProfileAuthFromSunStatusFn
	var containerCalls int32
	var sunCalls int32
	syncProfileAuthFromContainerStatusFn = func(ctx context.Context, p codexProfile) (profileAuthTokens, error) {
		atomic.AddInt32(&containerCalls, 1)
		return profileAuthTokens{}, os.ErrNotExist
	}
	syncProfileAuthFromSunStatusFn = func(ctx context.Context, p codexProfile) (bool, error) {
		atomic.AddInt32(&sunCalls, 1)
		path, err := codexProfileAuthPath(p)
		if err != nil {
			return false, err
		}
		data, _ := json.Marshal(profileAuthFile{Tokens: &profileAuthTokens{RefreshToken: "refresh-token"}})
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return false, err
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return false, err
		}
		return true, nil
	}
	codexAuthSyncAttempts = sync.Map{}
	defer func() {
		syncProfileAuthFromContainerStatusFn = prevContainerFn
		syncProfileAuthFromSunStatusFn = prevSunFn
		codexAuthSyncAttempts = sync.Map{}
	}()

	status := codexProfileAuthStatus(profile)
	if !status.Exists {
		t.Fatalf("expected auth status to recover from sun sync")
	}
	if got := atomic.LoadInt32(&containerCalls); got != 1 {
		t.Fatalf("expected one container sync attempt, got %d", got)
	}
	if got := atomic.LoadInt32(&sunCalls); got != 1 {
		t.Fatalf("expected one sun sync attempt, got %d", got)
	}
}

func TestCodexProfileAuthStatusAttemptsSyncOnlyOncePerProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	profile := codexProfile{ID: "einsteina", Name: "Einsteina", Email: "einsteina@example.com"}

	prevFn := syncProfileAuthFromContainerStatusFn
	prevSunFn := syncProfileAuthFromSunStatusFn
	var calls int32
	syncProfileAuthFromContainerStatusFn = func(ctx context.Context, p codexProfile) (profileAuthTokens, error) {
		atomic.AddInt32(&calls, 1)
		return profileAuthTokens{}, os.ErrNotExist
	}
	syncProfileAuthFromSunStatusFn = func(ctx context.Context, p codexProfile) (bool, error) {
		return false, os.ErrNotExist
	}
	codexAuthSyncAttempts = sync.Map{}
	defer func() {
		syncProfileAuthFromContainerStatusFn = prevFn
		syncProfileAuthFromSunStatusFn = prevSunFn
		codexAuthSyncAttempts = sync.Map{}
	}()

	_ = codexProfileAuthStatus(profile)
	_ = codexProfileAuthStatus(profile)

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected one sync attempt, got %d", got)
	}
}

func TestCodexProfileAuthStatusSkipsRecoveryWhenProfileIsBlocked(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	profile := codexProfile{ID: "cadma", Name: "Cadma", Email: "cadma@example.com"}
	if err := addCodexLogoutBlockedProfiles(home, []string{profile.ID}); err != nil {
		t.Fatalf("seed blocked profiles: %v", err)
	}

	prevFn := syncProfileAuthFromContainerStatusFn
	prevSunFn := syncProfileAuthFromSunStatusFn
	var calls int32
	var sunCalls int32
	syncProfileAuthFromContainerStatusFn = func(ctx context.Context, p codexProfile) (profileAuthTokens, error) {
		atomic.AddInt32(&calls, 1)
		return profileAuthTokens{AccessToken: "access-token"}, nil
	}
	syncProfileAuthFromSunStatusFn = func(ctx context.Context, p codexProfile) (bool, error) {
		atomic.AddInt32(&sunCalls, 1)
		return true, nil
	}
	codexAuthSyncAttempts = sync.Map{}
	defer func() {
		syncProfileAuthFromContainerStatusFn = prevFn
		syncProfileAuthFromSunStatusFn = prevSunFn
		codexAuthSyncAttempts = sync.Map{}
	}()

	status := codexProfileAuthStatus(profile)
	if status.Exists {
		t.Fatalf("expected blocked profile to stay missing")
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("expected no sync attempts for blocked profile, got %d", got)
	}
	if got := atomic.LoadInt32(&sunCalls); got != 0 {
		t.Fatalf("expected no sun sync attempts for blocked profile, got %d", got)
	}
}

func writeAuthFixture(t *testing.T, tokens profileAuthTokens) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	auth := profileAuthFile{Tokens: &tokens}
	data, err := json.Marshal(auth)
	if err != nil {
		t.Fatalf("marshal auth failed: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write auth failed: %v", err)
	}
	return path
}

func testJWTWithExp(t *testing.T, exp time.Time, padded bool) string {
	t.Helper()
	header := []byte(`{"alg":"none","typ":"JWT"}`)
	body, err := json.Marshal(map[string]int64{"exp": exp.UTC().Unix()})
	if err != nil {
		t.Fatalf("marshal claims failed: %v", err)
	}
	enc := base64.RawURLEncoding
	if padded {
		enc = base64.URLEncoding
	}
	return enc.EncodeToString(header) + "." + enc.EncodeToString(body) + "."
}

func TestCodexProfilesPrefersSunListWhenAvailable(t *testing.T) {
	server, _ := newSunTestServer(t, "acme", "token-profiles")
	defer server.Close()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SI_SETTINGS_HOME", home)

	settings := defaultSettings()
	applySettingsDefaults(&settings)
	settings.Sun.BaseURL = server.URL
	settings.Sun.Token = "token-profiles"
	settings.Codex.Profiles.Entries = map[string]CodexProfileEntry{
		"local-only": {Name: "Local Only", Email: "local@example.com"},
		"sun-a":      {Name: "Sun A", Email: "sun-a@example.com"},
	}
	if err := saveSettings(settings); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	client, err := newSunClient(server.URL, "token-profiles", 5*time.Second)
	if err != nil {
		t.Fatalf("new sun client: %v", err)
	}
	payload, err := json.Marshal(sunCodexProfileBundle{ID: "sun-a"})
	if err != nil {
		t.Fatalf("marshal sun-a payload: %v", err)
	}
	if _, err := client.putObject(context.Background(), sunCodexProfileBundleKind, "sun-a", payload, "application/json", nil, nil); err != nil {
		t.Fatalf("put sun-a profile bundle: %v", err)
	}
	payload, err = json.Marshal(sunCodexProfileBundle{ID: "sun-b"})
	if err != nil {
		t.Fatalf("marshal sun-b payload: %v", err)
	}
	if _, err := client.putObject(context.Background(), sunCodexProfileBundleKind, "sun-b", payload, "application/json", nil, nil); err != nil {
		t.Fatalf("put sun-b profile bundle: %v", err)
	}

	got := codexProfiles()
	if len(got) != 2 {
		t.Fatalf("expected 2 sun-backed profiles, got %#v", got)
	}
	if got[0].ID != "sun-a" || got[1].ID != "sun-b" {
		t.Fatalf("expected sorted sun profile ids [sun-a sun-b], got %#v", got)
	}
	if got[0].Email != "sun-a@example.com" {
		t.Fatalf("expected local metadata merge for sun-a, got %#v", got[0])
	}
}

func TestCodexProfilesFallsBackToLocalWhenSunUnavailable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SI_SETTINGS_HOME", home)

	settings := defaultSettings()
	applySettingsDefaults(&settings)
	settings.Codex.Profiles.Entries = map[string]CodexProfileEntry{
		"local-a": {Name: "Local A", Email: "a@example.com"},
		"local-b": {Name: "Local B", Email: "b@example.com"},
	}
	if err := saveSettings(settings); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	got := codexProfiles()
	if len(got) != 2 {
		t.Fatalf("expected local fallback profiles, got %#v", got)
	}
	if got[0].ID != "local-a" || got[1].ID != "local-b" {
		t.Fatalf("expected sorted local profile ids [local-a local-b], got %#v", got)
	}
}
