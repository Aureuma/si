package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCloudflareE2E_RawWithTokenAuth(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/zones" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token-123" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		w.Header().Set("CF-Ray", "ray-e2e")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"result": []map[string]any{{
				"id":   "zone-1",
				"name": "example.com",
			}},
		})
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{},
		"cloudflare", "raw",
		"--account", "test",
		"--base-url", server.URL,
		"--api-token", "token-123",
		"--path", "/zones",
		"--json",
	)
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json output parse failed: %v\nstdout=%s", err, stdout)
	}
	if int(payload["status_code"].(float64)) != 200 {
		t.Fatalf("unexpected status code payload: %#v", payload)
	}
}
