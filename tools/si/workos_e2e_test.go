package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWorkOSE2E_AuthStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/organizations" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("limit"); got != "1" {
			t.Fatalf("unexpected limit query: %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer wk_test_123" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"id": "org_1", "name": "Acme"}},
		})
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"WORKOS_API_KEY": "wk_test_123",
	}, "workos", "auth", "status", "--base-url", server.URL, "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json output parse failed: %v\nstdout=%s", err, stdout)
	}
	if got := strings.TrimSpace(stringifyWorkOSAny(payload["status"])); got != "ready" {
		t.Fatalf("unexpected status payload: %#v", payload)
	}
}

func TestWorkOSE2E_OrganizationCreate(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/organizations" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer wk_test_123" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(raw), `"name":"Acme Inc"`) {
			t.Fatalf("unexpected request body: %s", string(raw))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":   "org_1",
			"name": "Acme Inc",
		})
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"WORKOS_API_KEY": "wk_test_123",
	}, "workos", "organization", "create", "--base-url", server.URL, "--param", "name=Acme Inc", "--json")
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

func TestWorkOSE2E_DoctorPublic(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/organizations" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("limit"); got != "1" {
			t.Fatalf("unexpected limit query: %q", got)
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{}, "workos", "doctor", "--public", "--base-url", server.URL, "--json")
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
