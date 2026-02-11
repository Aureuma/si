package appstorebridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func TestClientDoAddsBearer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token-123" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		if r.URL.Path != "/v1/apps" {
			t.Fatalf("unexpected path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"id": "1", "type": "apps"}}})
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{TokenProvider: staticTokenProvider{token: "token-123"}, BaseURL: server.URL})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	resp, err := client.Do(context.Background(), Request{Method: http.MethodGet, Path: "/v1/apps"})
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}
	if len(resp.List) != 1 {
		t.Fatalf("unexpected list payload: %#v", resp.List)
	}
}

func TestClientDoErrorNormalization(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errors":[{"status":"403","code":"FORBIDDEN","title":"Forbidden","detail":"not allowed"}]}`))
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{TokenProvider: staticTokenProvider{token: "token-123"}, BaseURL: server.URL})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	_, err = client.Do(context.Background(), Request{Method: http.MethodGet, Path: "/v1/apps"})
	if err == nil {
		t.Fatalf("expected error")
	}
	apiErr, ok := err.(*APIErrorDetails)
	if !ok {
		t.Fatalf("unexpected error type: %T", err)
	}
	if apiErr.StatusCode != 403 {
		t.Fatalf("unexpected status code: %d", apiErr.StatusCode)
	}
	if apiErr.Code != "FORBIDDEN" {
		t.Fatalf("unexpected code: %q", apiErr.Code)
	}
}
