package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/nacl/box"
)

func TestGitHubE2E_RawWithAppAuth(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	pemKey := testAppPrivateKeyPEM(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app/installations/123/access_tokens":
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "inst-token", "expires_at": time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339)})
		case "/repos/acme/repo":
			if got := r.Header.Get("Authorization"); got != "Bearer inst-token" {
				t.Fatalf("unexpected auth header: %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 1, "name": "repo"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"GITHUB_TEST_APP_ID":              "1",
		"GITHUB_TEST_APP_PRIVATE_KEY_PEM": pemKey,
		"GITHUB_TEST_INSTALLATION_ID":     "123",
	}, "github", "raw", "--account", "test", "--base-url", server.URL, "--method", "GET", "--path", "/repos/acme/repo", "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json output parse failed: %v\nstdout=%s", err, stdout)
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("missing data map in payload: %#v", payload)
	}
	if data["name"] != "repo" {
		t.Fatalf("unexpected repo data: %#v", data)
	}
}

func TestGitHubE2E_ReleaseUploadUsesAPIUploadURL(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	pemKey := testAppPrivateKeyPEM(t)
	assetPath := filepath.Join(t.TempDir(), "asset.txt")
	if err := os.WriteFile(assetPath, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write asset: %v", err)
	}
	var uploadHit atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app/installations/123/access_tokens":
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "inst-token", "expires_at": time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339)})
		case "/repos/acme/repo/releases/tags/v1":
			uploadURL := "http://" + r.Host + "/uploads/custom/path{?name,label}"
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 77, "upload_url": uploadURL})
		case "/uploads/custom/path":
			uploadHit.Store(true)
			if got := r.Header.Get("Authorization"); got != "Bearer inst-token" {
				t.Fatalf("unexpected auth header on upload: %q", got)
			}
			if name := r.URL.Query().Get("name"); name != "asset.txt" {
				t.Fatalf("unexpected upload name: %q", name)
			}
			body, _ := io.ReadAll(r.Body)
			if string(body) != "hello" {
				t.Fatalf("unexpected upload body: %q", string(body))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 9001, "name": "asset.txt"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"GITHUB_TEST_APP_ID":              "1",
		"GITHUB_TEST_APP_PRIVATE_KEY_PEM": pemKey,
		"GITHUB_TEST_INSTALLATION_ID":     "123",
	}, "github", "release", "upload", "acme/repo", "v1", "--account", "test", "--base-url", server.URL, "--asset", assetPath, "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !uploadHit.Load() {
		t.Fatalf("expected upload endpoint to be called")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json output parse failed: %v\nstdout=%s", err, stdout)
	}
	if int(payload["status_code"].(float64)) != 200 {
		t.Fatalf("unexpected status code: %#v", payload)
	}
}

func TestGitHubE2E_SecretRepoSetEncryptsValue(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	pemKey := testAppPrivateKeyPEM(t)
	public, _, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate nacl keypair: %v", err)
	}
	key := base64.StdEncoding.EncodeToString(public[:])
	var secretPutHit atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app/installations/123/access_tokens":
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "inst-token", "expires_at": time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339)})
		case "/repos/acme/repo/actions/secrets/public-key":
			_ = json.NewEncoder(w).Encode(map[string]any{"key_id": "kid-1", "key": key})
		case "/repos/acme/repo/actions/secrets/MY_SECRET":
			secretPutHit.Store(true)
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode secret put body: %v", err)
			}
			if body["key_id"] != "kid-1" {
				t.Fatalf("unexpected key_id: %#v", body)
			}
			encrypted, _ := body["encrypted_value"].(string)
			if strings.TrimSpace(encrypted) == "" || strings.Contains(encrypted, "super-secret") {
				t.Fatalf("secret value not encrypted: %#v", body)
			}
			w.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"GITHUB_TEST_APP_ID":              "1",
		"GITHUB_TEST_APP_PRIVATE_KEY_PEM": pemKey,
		"GITHUB_TEST_INSTALLATION_ID":     "123",
	}, "github", "secret", "repo", "set", "acme/repo", "MY_SECRET", "--account", "test", "--base-url", server.URL, "--value", "super-secret", "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !secretPutHit.Load() {
		t.Fatalf("expected secret PUT to be called")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json output parse failed: %v\nstdout=%s", err, stdout)
	}
	if payload["status"] != "201 Created" {
		t.Fatalf("unexpected response payload: %#v", payload)
	}
}

func runSICommand(t *testing.T, env map[string]string, args ...string) (string, string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", append([]string{"run", "."}, args...)...)
	cmd.Dir = "."
	cmd.Env = append([]string{}, os.Environ()...)
	cmd.Env = append(cmd.Env, "NO_COLOR=1")
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	stdout, err := cmd.Output()
	stderr := ""
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr = string(exitErr.Stderr)
		}
		return string(stdout), stderr, err
	}
	return strings.TrimSpace(string(stdout)), strings.TrimSpace(stderr), nil
}

func testAppPrivateKeyPEM(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	raw := x509.MarshalPKCS1PrivateKey(key)
	block := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: raw}
	return string(pem.EncodeToMemory(block))
}
