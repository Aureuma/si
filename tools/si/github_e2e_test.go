package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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

func TestGitHubE2E_RawWithOAuthToken(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer oauth-token-123" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"login": "octocat", "id": 1})
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"GITHUB_TEST_OAUTH_ACCESS_TOKEN": "oauth-token-123",
	}, "github", "raw", "--account", "test", "--auth-mode", "oauth", "--base-url", server.URL, "--method", "GET", "--path", "/user", "--json")
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

func TestGitHubE2E_DoctorPublic(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/zen" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("keep it logically awesome"))
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{},
		"github", "doctor",
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

func TestGitHubE2E_BranchCreateFromDefaultBranch(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	pemKey := testAppPrivateKeyPEM(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app/installations/123/access_tokens":
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "inst-token", "expires_at": time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339)})
		case "/repos/acme/repo":
			_ = json.NewEncoder(w).Encode(map[string]any{"default_branch": "main"})
		case "/repos/acme/repo/branches/main":
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "main", "commit": map[string]any{"sha": "abc123"}})
		case "/repos/acme/repo/git/refs":
			if got := r.Header.Get("Authorization"); got != "Bearer inst-token" {
				t.Fatalf("unexpected auth header: %q", got)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode create ref body: %v", err)
			}
			if body["ref"] != "refs/heads/feature/new-api" {
				t.Fatalf("unexpected ref payload: %#v", body)
			}
			if body["sha"] != "abc123" {
				t.Fatalf("unexpected sha payload: %#v", body)
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ref": "refs/heads/feature/new-api",
				"object": map[string]any{
					"sha": "abc123",
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"GITHUB_TEST_APP_ID":              "1",
		"GITHUB_TEST_APP_PRIVATE_KEY_PEM": pemKey,
		"GITHUB_TEST_INSTALLATION_ID":     "123",
	}, "github", "branch", "create", "acme/repo", "--account", "test", "--base-url", server.URL, "--name", "feature/new-api", "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json output parse failed: %v\nstdout=%s", err, stdout)
	}
	if int(payload["status_code"].(float64)) != 201 {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestGitHubE2E_BranchProtect(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	pemKey := testAppPrivateKeyPEM(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app/installations/123/access_tokens":
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "inst-token", "expires_at": time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339)})
		case "/repos/acme/repo/branches/main/protection":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode branch protect body: %v", err)
			}
			requiredChecks, ok := body["required_status_checks"].(map[string]any)
			if !ok {
				t.Fatalf("missing required_status_checks: %#v", body)
			}
			checks, ok := requiredChecks["checks"].([]any)
			if !ok || len(checks) != 2 {
				t.Fatalf("unexpected checks payload: %#v", body)
			}
			if checks[0] != "ci" || checks[1] != "lint" {
				t.Fatalf("unexpected check values: %#v", checks)
			}
			prReviews, ok := body["required_pull_request_reviews"].(map[string]any)
			if !ok {
				t.Fatalf("missing required_pull_request_reviews: %#v", body)
			}
			if int(prReviews["required_approving_review_count"].(float64)) != 2 {
				t.Fatalf("unexpected approvals payload: %#v", prReviews)
			}
			if enforceAdmins, _ := body["enforce_admins"].(bool); !enforceAdmins {
				t.Fatalf("expected enforce_admins=true: %#v", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"url": "https://api.github.com/repos/acme/repo/branches/main/protection",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"GITHUB_TEST_APP_ID":              "1",
		"GITHUB_TEST_APP_PRIVATE_KEY_PEM": pemKey,
		"GITHUB_TEST_INSTALLATION_ID":     "123",
	}, "github", "branch", "protect", "acme/repo", "main", "--account", "test", "--base-url", server.URL, "--required-check", "ci", "--required-check", "lint", "--required-approvals", "2", "--dismiss-stale-reviews", "--require-code-owner-reviews", "--allow-force-pushes", "--require-linear-history", "--json")
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

func runSICommand(t *testing.T, env map[string]string, args ...string) (string, string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	binPath := siTestBinaryPath(t)
	cmd := exec.CommandContext(ctx, binPath, args...)
	cmd.Dir = "."
	cmd.Env = append([]string{}, os.Environ()...)
	cmd.Env = append(cmd.Env, "NO_COLOR=1")
	if _, ok := env["HOME"]; !ok {
		testHome := siTestHomeDir(t)
		cmd.Env = append(cmd.Env, "HOME="+testHome)
	}
	if _, ok := env["SI_SETTINGS_HOME"]; !ok {
		cmd.Env = append(cmd.Env, "SI_SETTINGS_HOME="+siTestHomeDir(t))
	}
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

var (
	siTestBinaryOnce sync.Once
	siTestBinary     string
	siTestBinaryErr  error
	siTestHomes      sync.Map
)

func siTestBinaryPath(t *testing.T) string {
	t.Helper()
	siTestBinaryOnce.Do(func() {
		tmpDir, err := os.MkdirTemp("", "si-test-binary-*")
		if err != nil {
			siTestBinaryErr = err
			return
		}
		siTestBinary = filepath.Join(tmpDir, "si-test")
		args := []string{"build", "-trimpath", "-buildvcs=false", "-o", siTestBinary, "."}
		cmd := exec.Command("go", args...)
		cmd.Dir = "."
		cmd.Env = append([]string{}, os.Environ()...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			siTestBinaryErr = fmt.Errorf("build si test binary: %w: %s", err, strings.TrimSpace(stderr.String()))
			return
		}
	})
	if siTestBinaryErr != nil {
		t.Fatalf("prepare si test binary: %v", siTestBinaryErr)
	}
	return siTestBinary
}

func siTestHomeDir(t *testing.T) string {
	t.Helper()
	if existing, ok := siTestHomes.Load(t.Name()); ok {
		return existing.(string)
	}
	created, err := os.MkdirTemp("", "si-test-home-*")
	if err != nil {
		t.Fatalf("prepare si test home: %v", err)
	}
	actual, loaded := siTestHomes.LoadOrStore(t.Name(), created)
	if loaded {
		_ = os.RemoveAll(created)
	}
	return actual.(string)
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
