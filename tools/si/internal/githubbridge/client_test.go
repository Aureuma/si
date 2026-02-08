package githubbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"si/tools/si/internal/apibridge"
)

type staticProvider struct {
	token string
}

func (p staticProvider) Mode() AuthMode { return AuthModeApp }
func (p staticProvider) Source() string { return "test" }
func (p staticProvider) Token(context.Context, TokenRequest) (Token, error) {
	return Token{Value: p.token}, nil
}

type countingProvider struct {
	calls atomic.Int64
}

func (p *countingProvider) Mode() AuthMode { return AuthModeApp }
func (p *countingProvider) Source() string { return "test" }
func (p *countingProvider) Token(ctx context.Context, req TokenRequest) (Token, error) {
	_ = ctx
	_ = req
	n := p.calls.Add(1)
	return Token{Value: fmt.Sprintf("token-%d", n)}, nil
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
	u, err := apibridge.ResolveURL("https://api.github.com", "/repos/a/b", map[string]string{"page": "2"})
	if err != nil {
		t.Fatalf("resolveURL: %v", err)
	}
	if u != "https://api.github.com/repos/a/b?page=2" {
		t.Fatalf("unexpected url: %s", u)
	}
}

func TestClientListAllPagination(t *testing.T) {
	calls := 0
	var baseURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		page := r.URL.Query().Get("page")
		if page == "" {
			page = "1"
		}
		n, _ := strconv.Atoi(page)
		if n < 2 {
			w.Header().Set("Link", `<`+baseURL+`/repos/a/b/issues?page=2>; rel="next"`)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{"id": n}})
	}))
	defer srv.Close()
	baseURL = srv.URL
	client, err := NewClient(ClientConfig{BaseURL: srv.URL, Provider: staticProvider{token: "token-123"}})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	items, err := client.ListAll(context.Background(), Request{Method: "GET", Path: "/repos/a/b/issues"}, 5)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if calls != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}
}

func TestClientDo_RetriesAndReauthsPerAttempt(t *testing.T) {
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if got, want := r.Header.Get("Authorization"), "Bearer "+fmt.Sprintf("token-%d", n); got != want {
			t.Fatalf("call=%d unexpected auth header: got %q want %q", n, got, want)
		}
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "rate limited"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer srv.Close()

	provider := &countingProvider{}
	client, err := NewClient(ClientConfig{BaseURL: srv.URL, Provider: provider, MaxRetries: 1})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	resp, err := client.Do(context.Background(), Request{Method: http.MethodGet, Path: "/rate-limited"})
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}
	if got := provider.calls.Load(); got != 2 {
		t.Fatalf("expected 2 token calls, got %d", got)
	}
}
