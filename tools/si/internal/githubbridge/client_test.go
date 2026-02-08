package githubbridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type staticProvider struct {
	token string
}

func (p staticProvider) Mode() AuthMode { return AuthModeApp }
func (p staticProvider) Source() string { return "test" }
func (p staticProvider) Token(context.Context, TokenRequest) (Token, error) {
	return Token{Value: p.token}, nil
}

func TestClientDo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token-123" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		w.Header().Set("X-GitHub-Request-Id", "req-1")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 1, "name": "repo"})
	}))
	defer srv.Close()
	client, err := NewClient(ClientConfig{BaseURL: srv.URL, Provider: staticProvider{token: "token-123"}})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	resp, err := client.Do(context.Background(), Request{Method: "GET", Path: "/repos/acme/repo"})
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	if resp.StatusCode != 200 || resp.RequestID != "req-1" {
		t.Fatalf("unexpected response: %#v", resp)
	}
	if resp.Data["name"] != "repo" {
		t.Fatalf("unexpected data: %#v", resp.Data)
	}
}

func TestResolveURL(t *testing.T) {
	u, err := resolveURL("https://api.github.com", "/repos/a/b", map[string]string{"page": "2"})
	if err != nil {
		t.Fatalf("resolveURL: %v", err)
	}
	if u != "https://api.github.com/repos/a/b?page=2" {
		t.Fatalf("unexpected url: %s", u)
	}
}
