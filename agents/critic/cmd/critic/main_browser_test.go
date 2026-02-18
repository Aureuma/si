package main

import "testing"

func TestBrowserMCPURLFromEnvDefaults(t *testing.T) {
	t.Setenv("SI_BROWSER_MCP_DISABLED", "")
	t.Setenv("SI_BROWSER_MCP_URL_INTERNAL", "")
	t.Setenv("SI_BROWSER_MCP_URL", "")
	t.Setenv("SI_BROWSER_CONTAINER", "")
	t.Setenv("SI_BROWSER_MCP_PORT", "")
	got := browserMCPURLFromEnv()
	want := "http://si-playwright-mcp-headed:8931/mcp"
	if got != want {
		t.Fatalf("browserMCPURLFromEnv()=%q want=%q", got, want)
	}
}

func TestBrowserMCPURLFromEnvRespectsOverride(t *testing.T) {
	t.Setenv("SI_BROWSER_MCP_DISABLED", "")
	t.Setenv("SI_BROWSER_MCP_URL_INTERNAL", "http://browser:7777/mcp")
	if got := browserMCPURLFromEnv(); got != "http://browser:7777/mcp" {
		t.Fatalf("unexpected browser MCP URL: %q", got)
	}
}

func TestBrowserMCPURLFromEnvDisabled(t *testing.T) {
	t.Setenv("SI_BROWSER_MCP_DISABLED", "true")
	if got := browserMCPURLFromEnv(); got != "" {
		t.Fatalf("expected empty browser MCP URL when disabled, got %q", got)
	}
}
