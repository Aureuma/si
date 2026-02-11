package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestResolveGooglePlayRuntimeContextFromEnv(t *testing.T) {
	serviceJSON := testGooglePlayServiceAccountJSON(t, "https://oauth2.googleapis.com/token")
	t.Setenv("GOOGLE_TEST_PLAY_SERVICE_ACCOUNT_JSON", serviceJSON)
	runtime, err := resolveGooglePlayRuntimeContext(googlePlayRuntimeContextInput{AccountFlag: "test", EnvFlag: "prod"})
	if err != nil {
		t.Fatalf("resolve runtime: %v", err)
	}
	if runtime.AccountAlias != "test" {
		t.Fatalf("unexpected account alias: %q", runtime.AccountAlias)
	}
	if !strings.Contains(runtime.ServiceAccountEmail, "@") {
		t.Fatalf("unexpected service account email: %q", runtime.ServiceAccountEmail)
	}
	if runtime.TokenSource != "env:GOOGLE_TEST_PLAY_SERVICE_ACCOUNT_JSON" {
		t.Fatalf("unexpected token source: %q", runtime.TokenSource)
	}
}

func TestGooglePlayServiceAccountTokenProviderRefreshAndCache(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/token" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		values, err := url.ParseQuery(string(body))
		if err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if got := values.Get("grant_type"); got != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
			t.Fatalf("unexpected grant_type: %q", got)
		}
		if strings.TrimSpace(values.Get("assertion")) == "" {
			t.Fatalf("missing assertion")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "ya29.test-token", "expires_in": 3600, "token_type": "Bearer"})
	}))
	defer server.Close()

	runtime := googlePlayRuntimeContext{
		ServiceAccountJSON: testGooglePlayServiceAccountJSON(t, server.URL+"/token"),
		TokenSource:        "test",
	}
	provider, err := buildGooglePlayTokenProvider(runtime)
	if err != nil {
		t.Fatalf("build token provider: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	first, err := provider.Token(ctx)
	if err != nil {
		t.Fatalf("first token: %v", err)
	}
	second, err := provider.Token(ctx)
	if err != nil {
		t.Fatalf("second token: %v", err)
	}
	if first.Value != "ya29.test-token" || second.Value != "ya29.test-token" {
		t.Fatalf("unexpected tokens: first=%q second=%q", first.Value, second.Value)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected one token exchange call, got %d", calls.Load())
	}
}

func testGooglePlayServiceAccountJSON(t *testing.T, tokenURI string) string {
	t.Helper()
	payload := map[string]string{
		"type":         "service_account",
		"project_id":   "acme-project",
		"private_key":  testAppPrivateKeyPEM(t),
		"client_email": "si-test@acme-project.iam.gserviceaccount.com",
		"token_uri":    tokenURI,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal service account json: %v", err)
	}
	return string(raw)
}
