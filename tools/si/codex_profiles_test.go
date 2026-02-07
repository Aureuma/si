package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIsValidCodexAuthFileRejectsExpiredAccessToken(t *testing.T) {
	path := writeAuthFixture(t, profileAuthTokens{
		AccessToken: testJWTWithExp(t, time.Now().UTC().Add(-1*time.Hour), false),
		IDToken:     testJWTWithExp(t, time.Now().UTC().Add(1*time.Hour), false),
	})
	if isValidCodexAuthFile(path, time.Now()) {
		t.Fatalf("expected expired access token to be invalid")
	}
}

func TestIsValidCodexAuthFileRejectsExpiredIDToken(t *testing.T) {
	path := writeAuthFixture(t, profileAuthTokens{
		AccessToken: testJWTWithExp(t, time.Now().UTC().Add(1*time.Hour), false),
		IDToken:     testJWTWithExp(t, time.Now().UTC().Add(-1*time.Hour), false),
	})
	if isValidCodexAuthFile(path, time.Now()) {
		t.Fatalf("expected expired id token to be invalid")
	}
}

func TestIsValidCodexAuthFileAcceptsPaddedJWTPayload(t *testing.T) {
	path := writeAuthFixture(t, profileAuthTokens{
		AccessToken: testJWTWithExp(t, time.Now().UTC().Add(1*time.Hour), true),
		IDToken:     testJWTWithExp(t, time.Now().UTC().Add(1*time.Hour), true),
	})
	if !isValidCodexAuthFile(path, time.Now()) {
		t.Fatalf("expected padded jwt payload to be accepted")
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
