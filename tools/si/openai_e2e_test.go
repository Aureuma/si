package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIE2E_AuthStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		if got := r.Method; got != http.MethodGet {
			t.Fatalf("unexpected method: %s", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test-123" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-5"}]}`))
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"OPENAI_API_KEY": "sk-test-123",
	}, "openai", "auth", "status", "--base-url", server.URL, "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, `"status": "ready"`) {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestOpenAIE2E_ProjectList(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/organization/projects" {
			http.NotFound(w, r)
			return
		}
		if got := r.Method; got != http.MethodGet {
			t.Fatalf("unexpected method: %s", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-admin-123" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		if got := r.URL.Query().Get("limit"); got != "5" {
			t.Fatalf("unexpected limit query param: %q", got)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"proj_123","name":"Acme"}]}`))
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"OPENAI_API_KEY":       "sk-test-123",
		"OPENAI_ADMIN_API_KEY": "sk-admin-123",
	}, "openai", "project", "list", "--base-url", server.URL, "--limit", "5", "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, `"status_code": 200`) {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestOpenAIE2E_UsageCompletions(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/organization/usage/completions" {
			http.NotFound(w, r)
			return
		}
		if got := r.Method; got != http.MethodGet {
			t.Fatalf("unexpected method: %s", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-admin-123" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		if got := r.URL.Query().Get("start_time"); got != "1700000000" {
			t.Fatalf("unexpected start_time: %q", got)
		}
		if got := r.URL.Query().Get("bucket_width"); got != "1d" {
			t.Fatalf("unexpected bucket_width: %q", got)
		}
		_, _ = w.Write([]byte(`{"data":[{"object":"bucket"}]}`))
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"OPENAI_API_KEY":       "sk-test-123",
		"OPENAI_ADMIN_API_KEY": "sk-admin-123",
	}, "openai", "usage", "completions", "--base-url", server.URL, "--start-time", "1700000000", "--bucket-width", "1d", "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, `"status_code": 200`) {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestOpenAIE2E_CodexUsage(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/organization/usage/completions" {
			http.NotFound(w, r)
			return
		}
		if got := r.Method; got != http.MethodGet {
			t.Fatalf("unexpected method: %s", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-admin-123" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		if got := r.URL.Query().Get("models"); got != "gpt-5-codex" {
			t.Fatalf("unexpected models filter: %q", got)
		}
		_, _ = w.Write([]byte(`{"data":[{"object":"bucket"}]}`))
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"OPENAI_API_KEY":       "sk-test-123",
		"OPENAI_ADMIN_API_KEY": "sk-admin-123",
	}, "openai", "codex", "usage", "--base-url", server.URL, "--start-time", "1700000000", "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, `"status_code": 200`) {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestOpenAIE2E_DoctorPublic(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		if got := r.Method; got != http.MethodGet {
			t.Fatalf("unexpected method: %s", got)
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "unauthorized")
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{}, "openai", "doctor", "--public", "--base-url", server.URL, "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, `"ok": true`) {
		t.Fatalf("unexpected output: %s", stdout)
	}
}
