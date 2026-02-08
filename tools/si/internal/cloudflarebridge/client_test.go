package cloudflarebridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientDo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token-123" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		w.Header().Set("CF-Ray", "ray-1")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"result":  map[string]any{"id": "z1", "name": "example.com"},
		})
	}))
	defer srv.Close()
	client, err := NewClient(ClientConfig{BaseURL: srv.URL, APIToken: "token-123"})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	resp, err := client.Do(context.Background(), Request{Method: "GET", Path: "/zones/z1"})
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	if resp.StatusCode != 200 || resp.RequestID != "ray-1" {
		t.Fatalf("unexpected response: %#v", resp)
	}
	if resp.Data["name"] != "example.com" {
		t.Fatalf("unexpected data: %#v", resp.Data)
	}
}

func TestResolveURL(t *testing.T) {
	u, err := resolveURL("https://api.cloudflare.com/client/v4", "/zones", map[string]string{"page": "2"})
	if err != nil {
		t.Fatalf("resolveURL: %v", err)
	}
	if u != "https://api.cloudflare.com/zones?page=2" {
		t.Fatalf("unexpected url: %q", u)
	}
}
