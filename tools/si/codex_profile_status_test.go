package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFetchUsagePayloadWithClientParsesUsageAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Provided authentication token is expired. Please try signing in again.","code":"token_expired"},"status":401}`))
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	_, err := fetchUsagePayloadWithClient(context.Background(), client, srv.URL, profileAuthTokens{AccessToken: "x"})
	if err == nil {
		t.Fatalf("expected error")
	}
	var apiErr *usageAPIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected usageAPIError, got %T (%v)", err, err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unexpected status code %d", apiErr.StatusCode)
	}
	if apiErr.Code != "token_expired" {
		t.Fatalf("unexpected code %q", apiErr.Code)
	}
	if !strings.Contains(strings.ToLower(apiErr.Message), "expired") {
		t.Fatalf("unexpected message %q", apiErr.Message)
	}
	if !isExpiredAuthError(apiErr) {
		t.Fatalf("expected token-expired error to be recognized")
	}
}

func TestIsExpiredAuthErrorFalseForNonExpiredCode(t *testing.T) {
	err := &usageAPIError{
		StatusCode: http.StatusUnauthorized,
		Code:       "invalid_token",
		Message:    "invalid token",
	}
	if isExpiredAuthError(err) {
		t.Fatalf("expected non-expired error code to not match")
	}
}

func TestIsRefreshTokenReusedError(t *testing.T) {
	err := &usageAPIError{
		StatusCode: http.StatusUnauthorized,
		Code:       "refresh_token_reused",
		Message:    "reused",
	}
	if !isRefreshTokenReusedError(err) {
		t.Fatalf("expected refresh_token_reused to be detected")
	}
}

func TestIsAuthFailureError(t *testing.T) {
	if !isAuthFailureError(&usageAPIError{
		StatusCode: http.StatusUnauthorized,
		Code:       "invalid_token",
		Message:    "invalid token",
	}) {
		t.Fatalf("expected unauthorized token error to be auth failure")
	}
	if isAuthFailureError(errors.New("upstream timeout")) {
		t.Fatalf("expected timeout to not be auth failure")
	}
}

func TestIsAuthFailureErrorFromRateLimitMessage(t *testing.T) {
	err := errors.New(`failed to fetch codex rate limits: GET https://chatgpt.com/backend-api/wham/usage failed: 401 Unauthorized; body={"error":{"code":"token_expired"}}`)
	if !isAuthFailureError(err) {
		t.Fatalf("expected wrapped rate-limit auth message to be treated as auth failure")
	}
}

func TestProfileOAuthClientID(t *testing.T) {
	idToken := buildTestJWT(map[string]interface{}{
		"aud": []interface{}{"app_test_client"},
	})
	clientID, err := profileOAuthClientID(profileAuthTokens{IDToken: idToken})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if clientID != "app_test_client" {
		t.Fatalf("unexpected client id %q", clientID)
	}
}

func TestRefreshProfileAuthTokensUpdatesFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req map[string]string
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if req["grant_type"] != "refresh_token" || req["client_id"] != "app_test_client" || req["refresh_token"] != "refresh_old" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"bad refresh","code":"invalid_request"}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token":  "access_new",
			"id_token":      buildTestJWT(map[string]interface{}{"aud": []interface{}{"app_test_client"}}),
			"refresh_token": "refresh_new",
		})
	}))
	defer tokenSrv.Close()
	t.Setenv("SI_CODEX_TOKEN_URL", tokenSrv.URL)

	profile := codexProfile{ID: "cadma", Name: "Cadma", Email: "cadma@example.com"}
	authPath := filepath.Join(home, ".si", "codex", "profiles", profile.ID, "auth.json")
	if err := os.MkdirAll(filepath.Dir(authPath), 0o700); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	initial := map[string]interface{}{
		"tokens": map[string]interface{}{
			"access_token":  "access_old",
			"refresh_token": "refresh_old",
			"id_token":      buildTestJWT(map[string]interface{}{"aud": []interface{}{"app_test_client"}}),
			"account_id":    "acct-1",
		},
	}
	data, _ := json.Marshal(initial)
	if err := os.WriteFile(authPath, data, 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	current := profileAuthTokens{
		AccessToken:  "access_old",
		AccountID:    "acct-1",
		IDToken:      buildTestJWT(map[string]interface{}{"aud": []interface{}{"app_test_client"}}),
		RefreshToken: "refresh_old",
	}
	updated, err := refreshProfileAuthTokens(context.Background(), &http.Client{Timeout: 2 * time.Second}, profile, current)
	if err != nil {
		t.Fatalf("unexpected refresh error: %v", err)
	}
	if updated.AccessToken != "access_new" || updated.RefreshToken != "refresh_new" {
		t.Fatalf("unexpected updated tokens: %+v", updated)
	}

	var persisted profileAuthFile
	raw, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if persisted.Tokens == nil || persisted.Tokens.AccessToken != "access_new" || persisted.Tokens.RefreshToken != "refresh_new" {
		t.Fatalf("auth file not updated: %+v", persisted)
	}
	if strings.TrimSpace(persisted.LastRefresh) == "" {
		t.Fatalf("expected last_refresh to be populated")
	}
}

func TestLoadProfileAuthTokensAllowsRefreshOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	profile := codexProfile{ID: "cadma", Name: "Cadma", Email: "cadma@example.com"}
	authPath := filepath.Join(home, ".si", "codex", "profiles", profile.ID, "auth.json")
	if err := os.MkdirAll(filepath.Dir(authPath), 0o700); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	initial := map[string]interface{}{
		"tokens": map[string]interface{}{
			"refresh_token": "refresh_only",
			"id_token":      buildTestJWT(map[string]interface{}{"aud": []interface{}{"app_test_client"}}),
		},
	}
	data, _ := json.Marshal(initial)
	if err := os.WriteFile(authPath, data, 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	got, err := loadProfileAuthTokens(profile)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}
	if got.AccessToken != "" {
		t.Fatalf("expected empty access token, got %q", got.AccessToken)
	}
	if got.RefreshToken != "refresh_only" {
		t.Fatalf("unexpected refresh token %q", got.RefreshToken)
	}
}

func TestRefreshProfileAuthTokensReusedFallsBackToLatestFromDisk(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	profile := codexProfile{ID: "cadma", Name: "Cadma", Email: "cadma@example.com"}
	authPath := filepath.Join(home, ".si", "codex", "profiles", profile.ID, "auth.json")
	if err := os.MkdirAll(filepath.Dir(authPath), 0o700); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	writeAuth := func(access, refresh string) {
		payload := map[string]interface{}{
			"tokens": map[string]interface{}{
				"access_token":  access,
				"refresh_token": refresh,
				"id_token":      buildTestJWT(map[string]interface{}{"aud": []interface{}{"app_test_client"}}),
				"account_id":    "acct-1",
			},
		}
		data, _ := json.Marshal(payload)
		if err := os.WriteFile(authPath, data, 0o600); err != nil {
			t.Fatalf("write failed: %v", err)
		}
	}
	writeAuth("access_old", "refresh_old")

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]string
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if req["refresh_token"] != "refresh_old" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"unexpected refresh token","code":"invalid_request"}}`))
			return
		}
		// Simulate another process that already rotated credentials.
		writeAuth("access_rotated", "refresh_new")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"reused","code":"refresh_token_reused"}}`))
	}))
	defer tokenSrv.Close()
	t.Setenv("SI_CODEX_TOKEN_URL", tokenSrv.URL)

	current := profileAuthTokens{
		AccessToken:  "access_old",
		AccountID:    "acct-1",
		IDToken:      buildTestJWT(map[string]interface{}{"aud": []interface{}{"app_test_client"}}),
		RefreshToken: "refresh_old",
	}
	updated, err := refreshProfileAuthTokens(context.Background(), &http.Client{Timeout: 2 * time.Second}, profile, current)
	if err != nil {
		t.Fatalf("unexpected refresh error: %v", err)
	}
	if updated.AccessToken != "access_rotated" {
		t.Fatalf("expected rotated access token, got %q", updated.AccessToken)
	}
	if updated.RefreshToken != "refresh_new" {
		t.Fatalf("expected rotated refresh token, got %q", updated.RefreshToken)
	}
}

func TestFormatLimitColumnPrefersResetDisplay(t *testing.T) {
	prev := ansiEnabled
	ansiEnabled = false
	defer func() { ansiEnabled = prev }()

	got := formatLimitColumn(71, "Feb 14, in 2 days", 151)
	want := "71% (Feb 14, in 2 days)"
	if got != want {
		t.Fatalf("unexpected formatted limit: got %q want %q", got, want)
	}
}

func TestStyleLimitTextByPctColorBuckets(t *testing.T) {
	prev := ansiEnabled
	ansiEnabled = true
	defer func() { ansiEnabled = prev }()

	white := styleLimitTextByPct("100% (23:12, in 5h)", 100)
	if !strings.Contains(white, "\x1b[1;37m") {
		t.Fatalf("expected bold white for 100%%, got %q", white)
	}
	green := styleLimitTextByPct("71% (20:52, in 2h31m)", 71)
	if !strings.Contains(green, "\x1b[32m") {
		t.Fatalf("expected green for mid-range percent, got %q", green)
	}
	magenta := styleLimitTextByPct("25% (14:33, in 140h)", 25)
	if !strings.Contains(magenta, "\x1b[35m") {
		t.Fatalf("expected magenta for <=25%%, got %q", magenta)
	}
}

func TestFormatRemainingDuration(t *testing.T) {
	if got := formatRemainingDuration(0); got != "" {
		t.Fatalf("expected empty for zero minutes, got %q", got)
	}
	if got := formatRemainingDuration(59); got != "59m" {
		t.Fatalf("expected 59m, got %q", got)
	}
	if got := formatRemainingDuration(60); got != "1h" {
		t.Fatalf("expected 1h, got %q", got)
	}
	if got := formatRemainingDuration(125); got != "2h05m" {
		t.Fatalf("expected 2h05m, got %q", got)
	}
}

func TestUsageWindowRemainingUsesResetAtCountdown(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	resetAt := now.Add(95 * time.Minute).Unix()
	limitSeconds := int64(5 * 60 * 60)
	window := &usageWindow{
		UsedPercent:        50,
		LimitWindowSeconds: &limitSeconds,
		ResetAt:            &resetAt,
	}
	_, remaining, _ := usageWindowRemaining(window, now)
	if remaining != 95 {
		t.Fatalf("expected reset countdown 95m, got %d", remaining)
	}
}

func TestProfileCodexAuthVolumeFallback(t *testing.T) {
	profile := codexProfile{ID: "cadma"}
	if got := profileCodexAuthVolume(profile, nil, nil, context.Background()); got != "si-codex-cadma" {
		t.Fatalf("unexpected fallback volume name %q", got)
	}
}

func buildTestJWT(payload map[string]interface{}) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	bodyBytes, _ := json.Marshal(payload)
	body := base64.RawURLEncoding.EncodeToString(bodyBytes)
	return header + "." + body + "."
}
