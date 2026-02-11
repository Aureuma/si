package googleplaybridge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestClientDoAddsBearer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token-123" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		if r.URL.Path != "/androidpublisher/v3/applications/com.acme.app/edits" {
			t.Fatalf("unexpected path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "edit-1"})
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{TokenProvider: staticTokenProvider{token: "token-123"}, BaseURL: server.URL})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	resp, err := client.Do(context.Background(), Request{Method: http.MethodPost, Path: "/androidpublisher/v3/applications/com.acme.app/edits", JSONBody: map[string]any{}})
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}
	if resp.Data["id"] != "edit-1" {
		t.Fatalf("unexpected payload: %#v", resp.Data)
	}
}

func TestClientDoUploadUsesUploadBaseURLAndMediaFile(t *testing.T) {
	base := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("base server should not be called")
	}))
	defer base.Close()

	var gotBody []byte
	upload := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/upload/androidpublisher/v3/applications/com.acme.app/edits/edit-1/bundles" {
			t.Fatalf("unexpected upload path: %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("uploadType"); got != "media" {
			t.Fatalf("unexpected uploadType: %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/octet-stream" {
			t.Fatalf("unexpected content-type: %q", got)
		}
		gotBody, _ = io.ReadAll(r.Body)
		_ = r.Body.Close()
		_ = json.NewEncoder(w).Encode(map[string]any{"versionCode": "123"})
	}))
	defer upload.Close()

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "app-release.aab")
	if err := os.WriteFile(filePath, []byte("bundle-bytes"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	client, err := NewClient(ClientConfig{
		TokenProvider:    staticTokenProvider{token: "token-123"},
		BaseURL:          base.URL,
		UploadBaseURL:    upload.URL,
		CustomAppBaseURL: upload.URL,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	resp, err := client.Do(context.Background(), Request{
		Method:      http.MethodPost,
		Path:        "/upload/androidpublisher/v3/applications/com.acme.app/edits/edit-1/bundles",
		Params:      map[string]string{"uploadType": "media"},
		MediaPath:   filePath,
		UseUpload:   true,
		ContentType: "application/octet-stream",
	})
	if err != nil {
		t.Fatalf("upload request: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}
	if string(gotBody) != "bundle-bytes" {
		t.Fatalf("unexpected upload body: %q", string(gotBody))
	}
}

func TestClientDoCustomAppBase(t *testing.T) {
	base := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("base server should not be called")
	}))
	defer base.Close()

	custom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/playcustomapp/v1/accounts/123/customApps" {
			t.Fatalf("unexpected custom app path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"packageName": "com.acme.app"})
	}))
	defer custom.Close()

	client, err := NewClient(ClientConfig{
		TokenProvider:    staticTokenProvider{token: "token-123"},
		BaseURL:          base.URL,
		CustomAppBaseURL: custom.URL,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	resp, err := client.Do(context.Background(), Request{
		Method:           http.MethodPost,
		Path:             "/playcustomapp/v1/accounts/123/customApps",
		UseCustomAppBase: true,
		JSONBody:         map[string]any{"title": "Acme"},
	})
	if err != nil {
		t.Fatalf("custom app request: %v", err)
	}
	if resp.Data["packageName"] != "com.acme.app" {
		t.Fatalf("unexpected payload: %#v", resp.Data)
	}
}

func TestClientListAllPaginates(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := calls.Add(1)
		switch idx {
		case 1:
			_ = json.NewEncoder(w).Encode(map[string]any{"listings": []map[string]any{{"language": "en-US"}}, "nextPageToken": "p2"})
		case 2:
			if got := r.URL.Query().Get("nextPageToken"); got != "p2" {
				t.Fatalf("unexpected page token: %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"listings": []map[string]any{{"language": "de-DE"}}})
		default:
			t.Fatalf("unexpected extra call: %d", idx)
		}
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{TokenProvider: staticTokenProvider{token: "token-123"}, BaseURL: server.URL})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	items, err := client.ListAll(context.Background(), Request{Method: http.MethodGet, Path: "/androidpublisher/v3/applications/com.acme.app/edits/edit-1/listings"}, 5, "nextPageToken")
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("unexpected items len: %d", len(items))
	}
}
