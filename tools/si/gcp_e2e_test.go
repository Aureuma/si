package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGCPE2E_AuthStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/projects/proj-123/services/serviceusage.googleapis.com" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token-123" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":  "projects/proj-123/services/serviceusage.googleapis.com",
			"state": "ENABLED",
		})
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"GCP_PROJECT_ID":            "proj-123",
		"GOOGLE_OAUTH_ACCESS_TOKEN": "token-123",
	}, "gcp", "auth", "status", "--base-url", server.URL, "--project", "proj-123", "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, `"status": "ready"`) {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestGCPE2E_ServiceEnable(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/projects/proj-123/services/generativelanguage.googleapis.com:enable" {
			http.NotFound(w, r)
			return
		}
		if got := r.Method; got != http.MethodPost {
			t.Fatalf("unexpected method: %s", got)
		}
		raw, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(raw), "{}") {
			t.Fatalf("unexpected body: %s", string(raw))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name": "operations/serviceusage.proj-123.1",
		})
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"GCP_PROJECT_ID":            "proj-123",
		"GOOGLE_OAUTH_ACCESS_TOKEN": "token-123",
	}, "gcp", "service", "enable", "--name", "generativelanguage.googleapis.com", "--base-url", server.URL, "--project", "proj-123", "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, `"status_code": 200`) {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestGCPE2E_ServiceList(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/projects/proj-123/services" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("pageSize"); got != "5" {
			t.Fatalf("unexpected pageSize: %q", got)
		}
		if got := r.URL.Query().Get("filter"); got != "state:ENABLED" {
			t.Fatalf("unexpected filter: %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"services": []map[string]any{{
				"name":  "projects/proj-123/services/generativelanguage.googleapis.com",
				"state": "ENABLED",
			}},
		})
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"GCP_PROJECT_ID":            "proj-123",
		"GOOGLE_OAUTH_ACCESS_TOKEN": "token-123",
	}, "gcp", "service", "list", "--limit", "5", "--filter", "state:ENABLED", "--base-url", server.URL, "--project", "proj-123", "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, `"status_code": 200`) {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestGCPE2E_DoctorPublic(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/services" {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{}, "gcp", "doctor", "--public", "--base-url", server.URL, "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, `"ok": true`) {
		t.Fatalf("unexpected output: %s", stdout)
	}
}
