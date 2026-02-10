package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestPublishE2E_CatalogList(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	catalogHTML := `<!doctype html><html><body><table><tbody>
<tr class="platform-row" data-platform-name="Product Hunt" data-platform-slug="product-hunt" data-score="high" data-pricing="free" data-account="required" data-url="https://www.producthunt.com">
<td></td><td><div class="text-[13px] leading-5">Launch your product</div></td>
</tr>
<tr class="platform-row" data-platform-name="MicroLaunch" data-platform-slug="micro-launch" data-score="medium" data-pricing="free + paid" data-account="required" data-url="https://microlaunch.net">
<td></td><td><div class="text-[13px] leading-5">Makers launch place</div></td>
</tr>
<tr class="platform-row" data-platform-name="Paid Only Dir" data-platform-slug="paid-only" data-score="low" data-pricing="paid" data-account="required" data-url="https://paid.example.com">
<td></td><td><div class="text-[13px] leading-5">Paid only</div></td>
</tr>
</tbody></table></body></html>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(catalogHTML))
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{}, "publish", "catalog", "list", "--source-url", server.URL, "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var payload []map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("invalid json: %v\nstdout=%s", err, stdout)
	}
	if len(payload) != 2 {
		t.Fatalf("expected 2 free-at-least entries, got %d: %#v", len(payload), payload)
	}
}

func TestPublishE2E_DevToArticleCreate(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/articles" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("api-key"); got != "devto-test-token" {
			t.Fatalf("expected api-key header, got %q", got)
		}
		raw, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(raw), `"title":"Launch post"`) {
			t.Fatalf("unexpected body: %s", string(raw))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":123,"title":"Launch post","url":"https://dev.to/x/123"}`))
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{}, "publish", "devto", "article",
		"--base-url", server.URL,
		"--api-key", "devto-test-token",
		"--title", "Launch post",
		"--body", "# hello",
		"--json",
	)
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, `"status_code": 201`) {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestPublishE2E_RedditSubmit(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/submit" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer reddit-test-token" {
			t.Fatalf("unexpected authorization: %q", got)
		}
		raw, _ := io.ReadAll(r.Body)
		values, err := url.ParseQuery(string(raw))
		if err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if values.Get("sr") != "golang" || values.Get("title") != "Launch on Reddit" || values.Get("kind") != "self" {
			t.Fatalf("unexpected form: %s", values.Encode())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"json":{"errors":[],"data":{"id":"t3_abc"}}}`))
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{}, "publish", "reddit", "submit",
		"--base-url", server.URL,
		"--token", "reddit-test-token",
		"--subreddit", "golang",
		"--title", "Launch on Reddit",
		"--text", "hello world",
		"--json",
	)
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, `"status_code": 200`) {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestPublishE2E_ProductHuntPosts(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer ph-test-token" {
			t.Fatalf("unexpected authorization: %q", got)
		}
		raw, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(raw), `"first":2`) {
			t.Fatalf("unexpected body: %s", string(raw))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"posts":{"edges":[{"node":{"id":"1","name":"Tool","votesCount":42}}]}}}`))
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{}, "publish", "producthunt", "posts",
		"--base-url", server.URL,
		"--token", "ph-test-token",
		"--first", "2",
		"--json",
	)
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, `"votesCount": 42`) {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestPublishE2E_HackerNewsTop(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/topstories.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[111,222,333]`))
		case "/v0/item/111.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":111,"title":"A","url":"https://a.example"}`))
		case "/v0/item/222.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":222,"title":"B","url":"https://b.example"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{}, "publish", "hackernews", "top",
		"--base-url", server.URL,
		"--limit", "2",
		"--json",
	)
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("invalid json: %v\nstdout=%s", err, stdout)
	}
	if got, _ := payload["count"].(float64); got != 2 {
		t.Fatalf("expected count=2, got %#v", payload["count"])
	}
}
