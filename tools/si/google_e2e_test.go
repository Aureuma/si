package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGooglePlacesE2E_SearchText(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/places:searchText" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("X-Goog-Api-Key"); got != "key-123" {
			t.Fatalf("unexpected api key header: %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"places": []map[string]any{{
				"id":               "place-1",
				"displayName":      map[string]any{"text": "Cafe One"},
				"formattedAddress": "1 Main St",
			}},
		})
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"GOOGLE_TEST_PLACES_API_KEY": "key-123",
	}, "google", "places", "search-text", "--account", "test", "--base-url", server.URL, "--query", "coffee", "--field-mask", "places.id,places.displayName,places.formattedAddress", "--json")
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

func TestGooglePlacesE2E_DoctorPublic(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/$discovery/rest" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("version"); got != "v1" {
			t.Fatalf("unexpected discovery version query: %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"kind": "discovery#restDescription"})
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{},
		"google", "places", "doctor",
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
