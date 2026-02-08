package googleplacesbridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"si/tools/si/internal/apibridge"
)

func TestClientDo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Goog-Api-Key"); got != "key-123" {
			t.Fatalf("unexpected api key header: %q", got)
		}
		if got := r.Header.Get("X-Goog-FieldMask"); got != "suggestions.placePrediction.placeId" {
			t.Fatalf("unexpected field mask: %q", got)
		}
		w.Header().Set("X-Request-Id", "req-1")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"suggestions": []map[string]any{{
				"placePrediction": map[string]any{
					"placeId": "abc",
				},
			}},
		})
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{BaseURL: srv.URL, APIKey: "key-123"})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	resp, err := client.Do(context.Background(), Request{Method: http.MethodPost, Path: "/v1/places:autocomplete", JSONBody: map[string]any{"input": "coffee"}, FieldMask: "suggestions.placePrediction.placeId"})
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	if resp.StatusCode != 200 || resp.RequestID != "req-1" {
		t.Fatalf("unexpected response: %#v", resp)
	}
	if len(resp.List) != 1 {
		t.Fatalf("unexpected list length: %d", len(resp.List))
	}
}

func TestResolveURL(t *testing.T) {
	u, err := apibridge.ResolveURL("https://places.googleapis.com", "/v1/places:searchText", map[string]string{"pageToken": "abc"})
	if err != nil {
		t.Fatalf("resolveURL: %v", err)
	}
	if u != "https://places.googleapis.com/v1/places:searchText?pageToken=abc" {
		t.Fatalf("unexpected url: %q", u)
	}
}
