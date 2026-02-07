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

func buildTestJWT(payload map[string]interface{}) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	bodyBytes, _ := json.Marshal(payload)
	body := base64.RawURLEncoding.EncodeToString(bodyBytes)
	return header + "." + body + "."
}
