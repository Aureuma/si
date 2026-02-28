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

func TestCloudflareE2E_DoctorPublic(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ips" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"ipv4_cidrs": []string{"1.1.1.0/24"}}})
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{},
		"cloudflare", "doctor",
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

func TestCloudflareE2E_PagesDomainCreate(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/accounts/acct_123/pages/projects/sample-blog/domains" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token-123" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"result": map[string]any{
				"name": "blog.example.com",
			},
		})
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"CLOUDFLARE_API_TOKEN":  "token-123",
		"CLOUDFLARE_ACCOUNT_ID": "acct_123",
	}, "cloudflare", "pages", "domain", "create", "--project", "sample-blog", "--domain", "blog.example.com", "--base-url", server.URL, "--account-id", "acct_123", "--json")
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

func TestCloudflareE2E_StatusAlias(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user/tokens/verify" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token-123" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"result": map[string]any{
				"status": "active",
			},
		})
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{},
		"cloudflare", "status",
		"--base-url", server.URL,
		"--api-token", "token-123",
		"--json",
	)
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json output parse failed: %v\nstdout=%s", err, stdout)
	}
	if status, _ := payload["status"].(string); status != "ready" {
		t.Fatalf("unexpected status payload: %#v", payload)
	}
}

func TestCloudflareE2E_TokenVerify(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user/tokens/verify" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token-123" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"result": map[string]any{
				"id":     "token-1",
				"status": "active",
			},
		})
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{},
		"cloudflare", "token", "verify",
		"--base-url", server.URL,
		"--api-token", "token-123",
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
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestCloudflareE2E_EmailRuleList(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/zones/zone-123/email/routing/rules" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token-123" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"result": []map[string]any{{
				"id":   "rule-1",
				"name": "route-to-inbox",
			}},
		})
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{},
		"cloudflare", "email", "rule", "list",
		"--base-url", server.URL,
		"--api-token", "token-123",
		"--zone-id", "zone-123",
		"--json",
	)
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json output parse failed: %v\nstdout=%s", err, stdout)
	}
	if int(payload["count"].(float64)) != 1 {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestCloudflareE2E_APIAlias(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/zones" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token-xyz" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"result": []map[string]any{{
				"id": "zone-1",
			}},
		})
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{},
		"cloudflare", "api",
		"--base-url", server.URL,
		"--api-token", "token-xyz",
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
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestCloudflareE2E_StatusWithClientV4BasePath(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/client/v4/user/tokens/verify" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token-123" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"result":  map[string]any{"status": "active"},
		})
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{},
		"cloudflare", "status",
		"--base-url", server.URL+"/client/v4",
		"--api-token", "token-123",
		"--json",
	)
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json output parse failed: %v\nstdout=%s", err, stdout)
	}
	if status, _ := payload["status"].(string); status != "ready" {
		t.Fatalf("unexpected status payload: %#v", payload)
	}
}

func TestCloudflareE2E_Smoke(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token-123" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		path := r.URL.Path
		switch path {
		case "/client/v4/user/tokens/verify", "/client/v4/accounts", "/client/v4/zones", "/client/v4/accounts/acct_123/pages/projects":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": []map[string]any{}})
			return
		default:
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": false,
				"errors": []map[string]any{{
					"code":    10000,
					"message": "Authentication error",
				}},
			})
			return
		}
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{},
		"cloudflare", "smoke",
		"--base-url", server.URL+"/client/v4",
		"--api-token", "token-123",
		"--account-id", "acct_123",
		"--no-fail",
		"--json",
	)
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json output parse failed: %v\nstdout=%s", err, stdout)
	}
	ok, _ := payload["ok"].(bool)
	if ok {
		t.Fatalf("expected smoke to report failures for restricted scopes: %#v", payload)
	}
	summary, _ := payload["summary"].(map[string]any)
	if int(summary["pass"].(float64)) < 3 {
		t.Fatalf("expected smoke pass count >= 3, got payload: %#v", payload)
	}
	if int(summary["fail"].(float64)) < 1 {
		t.Fatalf("expected smoke fail count >= 1, got payload: %#v", payload)
	}
}
