package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOCIE2E_RawNoAuth(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/20160918/instances" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("authorization"); got != "" {
			t.Fatalf("expected no authorization header, got %q", got)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"id":          "ocid1.instance.oc1..abc",
			"displayName": "oracular-vps",
		}})
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{}, "oci", "raw", "--auth", "none", "--base-url", server.URL, "--method", "GET", "--path", "/20160918/instances", "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, `"status_code": 200`) {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestOCIE2E_DoctorPublic(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/20160918/instances" {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{}, "oci", "doctor", "--public", "--base-url", server.URL, "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, `"ok": true`) {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestOCIE2E_CloudInit(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	stdout, stderr, err := runSICommand(t, map[string]string{}, "oci", "oracular", "cloud-init", "--ssh-port", "7129", "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("invalid json: %v\nstdout=%s", err, stdout)
	}
	encoded, _ := payload["user_data_b64"].(string)
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode user_data_b64: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, "Port 7129") {
		t.Fatalf("expected port in cloud-init, got: %s", text)
	}
	if !strings.Contains(text, "--dport 7129") {
		t.Fatalf("expected iptables ssh port rule, got: %s", text)
	}
}
