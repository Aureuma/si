package githubbridge

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAppProviderTokenWithInstallationID(t *testing.T) {
	pemKey := testRSAPrivateKeyPEM(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/app/installations/123/access_tokens" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token":      "inst-token",
				"expires_at": time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339),
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	provider, err := NewAppProvider(AppProviderConfig{
		AppID:          999,
		InstallationID: 123,
		PrivateKeyPEM:  pemKey,
		BaseURL:        srv.URL,
	})
	if err != nil {
		t.Fatalf("new app provider: %v", err)
	}
	tok, err := provider.Token(context.Background(), TokenRequest{})
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	if tok.Value != "inst-token" {
		t.Fatalf("unexpected token: %q", tok.Value)
	}
}

func TestAppProviderTokenWithLookup(t *testing.T) {
	pemKey := testRSAPrivateKeyPEM(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/repo/installation":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 321})
		case "/app/installations/321/access_tokens":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token":      "lookup-token",
				"expires_at": time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	provider, err := NewAppProvider(AppProviderConfig{
		AppID:         111,
		PrivateKeyPEM: pemKey,
		BaseURL:       srv.URL,
	})
	if err != nil {
		t.Fatalf("new app provider: %v", err)
	}
	tok, err := provider.Token(context.Background(), TokenRequest{Owner: "acme", Repo: "repo"})
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	if tok.Value != "lookup-token" {
		t.Fatalf("unexpected token: %q", tok.Value)
	}
}

func TestNormalizePrivateKey(t *testing.T) {
	value := "line1\\nline2"
	norm := normalizePrivateKey(value)
	if !strings.Contains(norm, "\n") {
		t.Fatalf("expected newline conversion")
	}
}

func testRSAPrivateKeyPEM(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	raw := x509.MarshalPKCS1PrivateKey(key)
	block := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: raw}
	return string(pem.EncodeToMemory(block))
}
