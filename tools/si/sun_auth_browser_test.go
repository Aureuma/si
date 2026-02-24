package main

import (
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"strings"
	"testing"
	"time"
)

func TestRandomHex(t *testing.T) {
	got, err := randomHex(24)
	if err != nil {
		t.Fatalf("randomHex: %v", err)
	}
	if len(got) != 48 {
		t.Fatalf("randomHex length=%d want=48", len(got))
	}
}

func TestRunSunBrowserAuthFlow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callbackRaw := strings.TrimSpace(r.URL.Query().Get("cb"))
		state := strings.TrimSpace(r.URL.Query().Get("state"))
		callbackURL, err := neturl.Parse(callbackRaw)
		if err != nil {
			t.Fatalf("parse callback: %v", err)
		}
		query := callbackURL.Query()
		query.Set("state", state)
		query.Set("token", "token-123")
		query.Set("url", "https://sun.aureuma.ai")
		query.Set("account", "sun")
		query.Set("auto_sync", "true")
		callbackURL.RawQuery = query.Encode()
		http.Redirect(w, r, callbackURL.String(), http.StatusFound)
	}))
	defer server.Close()

	oldOpen := sunBrowserAuthOpenURLFn
	t.Cleanup(func() {
		sunBrowserAuthOpenURLFn = oldOpen
	})
	sunBrowserAuthOpenURLFn = func(raw string) {
		_, _ = http.Get(raw)
	}

	got, err := runSunBrowserAuthFlow(server.URL, 10*time.Second, true)
	if err != nil {
		t.Fatalf("runSunBrowserAuthFlow: %v", err)
	}
	if got.Token != "token-123" {
		t.Fatalf("token=%q", got.Token)
	}
	if got.BaseURL != "https://sun.aureuma.ai" {
		t.Fatalf("baseURL=%q", got.BaseURL)
	}
	if got.Account != "sun" {
		t.Fatalf("account=%q", got.Account)
	}
	if !got.AutoSync {
		t.Fatalf("autoSync=%v", got.AutoSync)
	}
}

func TestRunSunBrowserAuthFlowStateMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callbackRaw := strings.TrimSpace(r.URL.Query().Get("cb"))
		callbackURL, err := neturl.Parse(callbackRaw)
		if err != nil {
			t.Fatalf("parse callback: %v", err)
		}
		query := callbackURL.Query()
		query.Set("state", "wrong-state")
		query.Set("token", "token-123")
		callbackURL.RawQuery = query.Encode()
		http.Redirect(w, r, callbackURL.String(), http.StatusFound)
	}))
	defer server.Close()

	oldOpen := sunBrowserAuthOpenURLFn
	t.Cleanup(func() {
		sunBrowserAuthOpenURLFn = oldOpen
	})
	sunBrowserAuthOpenURLFn = func(raw string) {
		_, _ = http.Get(raw)
	}

	_, err := runSunBrowserAuthFlow(server.URL, 5*time.Second, true)
	if err == nil {
		t.Fatalf("expected state mismatch error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "state mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}
