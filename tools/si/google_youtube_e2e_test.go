package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGoogleYouTubeE2E_SearchListAPIKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/youtube/v3/search" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("key"); got != "key-123" {
			t.Fatalf("unexpected api key query: %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"id": map[string]any{"videoId": "v1"}, "snippet": map[string]any{"title": "Video 1"}}}})
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"GOOGLE_TEST_YOUTUBE_API_KEY": "key-123",
	}, "google", "youtube", "search", "list", "--account", "test", "--base-url", server.URL, "--query", "music", "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json output parse failed: %v\nstdout=%s", err, stdout)
	}
	list, ok := payload["list"].([]any)
	if !ok || len(list) != 1 {
		t.Fatalf("unexpected list payload: %#v", payload)
	}
}

func TestGoogleYouTubeE2E_SearchListAPIKeyAlias(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/youtube/v3/search" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("key"); got != "key-123" {
			t.Fatalf("unexpected api key query: %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"id": map[string]any{"videoId": "v1"}, "snippet": map[string]any{"title": "Video 1"}}}})
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"GOOGLE_TEST_YOUTUBE_API_KEY": "key-123",
	}, "google", "youtube-data", "search", "list", "--account", "test", "--base-url", server.URL, "--query", "music", "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json output parse failed: %v\nstdout=%s", err, stdout)
	}
	list, ok := payload["list"].([]any)
	if !ok || len(list) != 1 {
		t.Fatalf("unexpected list payload: %#v", payload)
	}
}

func TestGoogleYouTubeE2E_VideoRateOAuth(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/youtube/v3/videos/rate" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token-xyz" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		w.WriteHeader(http.StatusNoContent)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"GOOGLE_TEST_YOUTUBE_CLIENT_ID": "cid-1",
	}, "google", "youtube", "video", "rate", "--account", "test", "--mode", "oauth", "--base-url", server.URL, "--id", "v1", "--rating", "like", "--access-token", "token-xyz", "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json output parse failed: %v\nstdout=%s", err, stdout)
	}
	if int(payload["status_code"].(float64)) != 204 {
		t.Fatalf("unexpected status code payload: %#v", payload)
	}
}

func TestGoogleYouTubeE2E_DoctorPublic(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/discovery/v1/apis/youtube/v3/rest" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"kind": "discovery#restDescription"})
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{},
		"google", "youtube", "doctor",
		"--public",
		"--base-url", server.URL,
		"--json",
	)
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json output parse failed: %v\nstdout=%s", err, stdout)
	}
	if ok, _ := payload["ok"].(bool); !ok {
		t.Fatalf("expected ok payload: %#v", payload)
	}
}
