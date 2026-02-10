package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestImageE2E_UnsplashSearch(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search/photos" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Client-ID unsplash-key" {
			t.Fatalf("unexpected unsplash auth header: %q", got)
		}
		if got := r.URL.Query().Get("query"); got != "sunset" {
			t.Fatalf("unexpected query: %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{{"id": "u1", "alt_description": "sunset"}},
		})
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"UNSPLASH_ACCESS_KEY": "unsplash-key",
	}, "image", "unsplash", "search", "--query", "sunset", "--base-url", server.URL, "--json")
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

func TestImageE2E_PexelsSearch(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/search" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "pexels-key" {
			t.Fatalf("unexpected pexels auth header: %q", got)
		}
		if got := r.URL.Query().Get("query"); got != "mountain" {
			t.Fatalf("unexpected query: %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"photos": []map[string]any{{"id": 1001, "url": "https://example.test/p1"}},
		})
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"PEXELS_API_KEY": "pexels-key",
	}, "image", "pexels", "search", "--query", "mountain", "--base-url", server.URL, "--json")
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

func TestImageE2E_PixabaySearch(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("key"); got != "pixabay-key" {
			t.Fatalf("unexpected pixabay key query: %q", got)
		}
		if got := r.URL.Query().Get("q"); got != "forest" {
			t.Fatalf("unexpected query: %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"hits": []map[string]any{{"id": 42, "tags": "forest, trees"}},
		})
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"PIXABAY_API_KEY": "pixabay-key",
	}, "image", "pixabay", "search", "--query", "forest", "--base-url", server.URL, "--json")
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

func TestImageE2E_AuthStatusMissingKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	stdout, _, err := runSICommand(t, map[string]string{}, "image", "unsplash", "auth", "status", "--json")
	if err == nil {
		t.Fatalf("expected auth status to fail without api key")
	}
	var payload map[string]any
	if parseErr := json.Unmarshal([]byte(stdout), &payload); parseErr != nil {
		t.Fatalf("expected json output payload on auth failure: %v\nstdout=%s", parseErr, stdout)
	}
	if ok, _ := payload["ok"].(bool); ok {
		t.Fatalf("expected ok=false payload: %#v", payload)
	}
}
