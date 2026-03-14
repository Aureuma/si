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
	profile := codexProfile{ID: "profile-gamma", Name: "Profile Gamma", Email: "profile-gamma@example.com"}

	prevFn := syncProfileAuthFromContainerStatusFn
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

func TestCodexProfileAuthStatusAttemptsSyncOnlyOncePerProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	profile := codexProfile{ID: "profile-delta", Name: "Profile Delta", Email: "profile-delta@example.com"}

	prevFn := syncProfileAuthFromContainerStatusFn
	var calls int32
	syncProfileAuthFromContainerStatusFn = func(ctx context.Context, p codexProfile) (profileAuthTokens, error) {
		atomic.AddInt32(&calls, 1)
		return profileAuthTokens{}, os.ErrNotExist
	}
	codexAuthSyncAttempts = sync.Map{}
	defer func() {
		syncProfileAuthFromContainerStatusFn = prevFn
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
	profile := codexProfile{ID: "profile-gamma", Name: "Profile Gamma", Email: "profile-gamma@example.com"}
	if err := addCodexLogoutBlockedProfiles(home, []string{profile.ID}); err != nil {
		t.Fatalf("seed blocked profiles: %v", err)
	}

	prevFn := syncProfileAuthFromContainerStatusFn
	var calls int32
	syncProfileAuthFromContainerStatusFn = func(ctx context.Context, p codexProfile) (profileAuthTokens, error) {
		atomic.AddInt32(&calls, 1)
		return profileAuthTokens{AccessToken: "access-token"}, nil
	}
	codexAuthSyncAttempts = sync.Map{}
	defer func() {
		syncProfileAuthFromContainerStatusFn = prevFn
		codexAuthSyncAttempts = sync.Map{}
	}()

	status := codexProfileAuthStatus(profile)
	if status.Exists {
		t.Fatalf("expected blocked profile to stay missing")
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("expected no sync attempts for blocked profile, got %d", got)
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

func TestCodexProfilesReturnsLocalProfiles(t *testing.T) {
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
		t.Fatalf("expected local profiles, got %#v", got)
	}
	if got[0].ID != "local-a" || got[1].ID != "local-b" {
		t.Fatalf("expected sorted local profile ids [local-a local-b], got %#v", got)
	}
}
