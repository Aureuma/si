package youtubebridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

type staticTokenProvider struct {
	token string
}

func (p staticTokenProvider) Token(context.Context) (Token, error) {
	return Token{Value: p.token}, nil
}

func (p staticTokenProvider) Source() string {
	return "test"
}

func TestClientDoAPIKeyInjectsQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("key"); got != "api-key-1" {
			t.Fatalf("unexpected key query: %q", got)
		}
		if r.URL.Path != "/youtube/v3/search" {
			t.Fatalf("unexpected path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"id": "v1"}}})
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{AuthMode: AuthModeAPIKey, APIKey: "api-key-1", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	resp, err := client.Do(context.Background(), Request{Method: http.MethodGet, Path: "/youtube/v3/search", Params: map[string]string{"q": "music"}})
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}
	if len(resp.List) != 1 {
		t.Fatalf("unexpected items len: %d", len(resp.List))
	}
}

func TestClientDoOAuthAddsBearer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token-123" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{AuthMode: AuthModeOAuth, BaseURL: server.URL, TokenProvider: staticTokenProvider{token: "token-123"}})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	resp, err := client.Do(context.Background(), Request{Method: http.MethodGet, Path: "/youtube/v3/channels", Params: map[string]string{"part": "id"}})
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}
}

func TestClientListAllPaginates(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := calls.Add(1)
		switch idx {
		case 1:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items":         []map[string]any{{"id": "1"}},
				"nextPageToken": "p2",
			})
		case 2:
			if got := r.URL.Query().Get("pageToken"); got != "p2" {
				t.Fatalf("unexpected page token: %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"id": "2"}}})
		default:
			t.Fatalf("unexpected extra call: %d", idx)
		}
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{AuthMode: AuthModeAPIKey, APIKey: "api-key-1", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	items, err := client.ListAll(context.Background(), Request{Method: http.MethodGet, Path: "/youtube/v3/search", Params: map[string]string{"part": "id"}}, 5)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("unexpected item count: %d", len(items))
	}
}
