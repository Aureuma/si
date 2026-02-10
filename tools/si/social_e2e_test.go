package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSocialE2E_FacebookProfile(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v22.0/me" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("access_token"); got != "fb-token-123" {
			t.Fatalf("unexpected access token: %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":   "page-1",
			"name": "Acme Page",
		})
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"FACEBOOK_ACCESS_TOKEN": "fb-token-123",
	}, "social", "facebook", "profile", "--base-url", server.URL, "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json output parse failed: %v\nstdout=%s", err, stdout)
	}
	if int(payload["status_code"].(float64)) != 200 {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestSocialE2E_InstagramMediaList(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v22.0/17890000000000000/media" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("access_token"); got != "ig-token-123" {
			t.Fatalf("unexpected access token: %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"id": "m1", "caption": "hello"}},
		})
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"INSTAGRAM_ACCESS_TOKEN": "ig-token-123",
		"INSTAGRAM_BUSINESS_ID":  "17890000000000000",
	}, "social", "instagram", "media", "list", "--base-url", server.URL, "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json output parse failed: %v\nstdout=%s", err, stdout)
	}
	if int(payload["status_code"].(float64)) != 200 {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestSocialE2E_XUserMe(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/2/users/me" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer x-token-123" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"id": "u1", "username": "acme"},
		})
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"X_BEARER_TOKEN": "x-token-123",
	}, "social", "x", "user", "me", "--base-url", server.URL, "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json output parse failed: %v\nstdout=%s", err, stdout)
	}
	if int(payload["status_code"].(float64)) != 200 {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestSocialE2E_LinkedInProfile(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/me" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer li-token-123" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		if got := r.Header.Get("X-Restli-Protocol-Version"); got != "2.0.0" {
			t.Fatalf("unexpected restli header: %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":                 "abc123",
			"localizedFirstName": "Ada",
		})
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"LINKEDIN_ACCESS_TOKEN": "li-token-123",
	}, "social", "linkedin", "profile", "--base-url", server.URL, "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json output parse failed: %v\nstdout=%s", err, stdout)
	}
	if int(payload["status_code"].(float64)) != 200 {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestSocialE2E_PublicNoAuthRaw(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v22.0/platform":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "provider": "facebook"})
		case "/v22.0/oauth/access_token":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "provider": "instagram"})
		case "/2/openapi.json":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "provider": "x"})
		case "/v2/me":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "provider": "linkedin"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cases := [][]string{
		{"social", "facebook", "raw", "--auth-style", "none", "--base-url", server.URL, "--path", "/platform", "--json"},
		{"social", "instagram", "raw", "--auth-style", "none", "--base-url", server.URL, "--path", "/oauth/access_token", "--json"},
		{"social", "x", "raw", "--auth-style", "none", "--base-url", server.URL, "--path", "/openapi.json", "--json"},
		{"social", "linkedin", "raw", "--auth-style", "none", "--base-url", server.URL, "--path", "/me", "--json"},
	}
	for _, args := range cases {
		stdout, stderr, err := runSICommand(t, map[string]string{}, args...)
		if err != nil {
			t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s\nargs=%v", err, stdout, stderr, args)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
			t.Fatalf("json output parse failed: %v\nstdout=%s\nargs=%v", err, stdout, args)
		}
		if int(payload["status_code"].(float64)) != 200 {
			t.Fatalf("unexpected payload: %#v args=%v", payload, args)
		}
	}
}
