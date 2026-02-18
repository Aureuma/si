package main

import "testing"

func TestCodexBrowserMCPURLDefaults(t *testing.T) {
	t.Setenv("SI_BROWSER_MCP_DISABLED", "")
	t.Setenv("SI_BROWSER_MCP_URL_INTERNAL", "")
	t.Setenv("SI_BROWSER_MCP_URL", "")
	t.Setenv("SI_BROWSER_CONTAINER", "")
	t.Setenv("SI_BROWSER_MCP_PORT", "")
	got := codexBrowserMCPURL()
	want := "http://si-playwright-mcp-headed:8931/mcp"
	if got != want {
		t.Fatalf("codexBrowserMCPURL()=%q want=%q", got, want)
	}
}

func TestCodexBrowserMCPURLUsesExplicitInternalURL(t *testing.T) {
	t.Setenv("SI_BROWSER_MCP_DISABLED", "")
	t.Setenv("SI_BROWSER_MCP_URL_INTERNAL", "http://browser-internal:7777/mcp")
	got := codexBrowserMCPURL()
	want := "http://browser-internal:7777/mcp"
	if got != want {
		t.Fatalf("codexBrowserMCPURL()=%q want=%q", got, want)
	}
}

func TestCodexBrowserMCPURLUsesExternalURLOverride(t *testing.T) {
	t.Setenv("SI_BROWSER_MCP_DISABLED", "")
	t.Setenv("SI_BROWSER_MCP_URL_INTERNAL", "")
	t.Setenv("SI_BROWSER_MCP_URL", "http://browser-external:9999/mcp")
	got := codexBrowserMCPURL()
	want := "http://browser-external:9999/mcp"
	if got != want {
		t.Fatalf("codexBrowserMCPURL()=%q want=%q", got, want)
	}
}

func TestCodexBrowserMCPURLUsesContainerAndPortOverride(t *testing.T) {
	t.Setenv("SI_BROWSER_MCP_DISABLED", "")
	t.Setenv("SI_BROWSER_MCP_URL_INTERNAL", "")
	t.Setenv("SI_BROWSER_MCP_URL", "")
	t.Setenv("SI_BROWSER_CONTAINER", "custom-browser")
	t.Setenv("SI_BROWSER_MCP_PORT", "9998")
	got := codexBrowserMCPURL()
	want := "http://custom-browser:9998/mcp"
	if got != want {
		t.Fatalf("codexBrowserMCPURL()=%q want=%q", got, want)
	}
}

func TestCodexBrowserMCPURLReturnsEmptyWhenPortInvalid(t *testing.T) {
	t.Setenv("SI_BROWSER_MCP_DISABLED", "")
	t.Setenv("SI_BROWSER_MCP_URL_INTERNAL", "")
	t.Setenv("SI_BROWSER_MCP_URL", "")
	t.Setenv("SI_BROWSER_CONTAINER", "custom-browser")
	t.Setenv("SI_BROWSER_MCP_PORT", "0")
	if got := codexBrowserMCPURL(); got != "" {
		t.Fatalf("expected empty browser MCP URL when port invalid, got %q", got)
	}
}

func TestCodexBrowserMCPURLDisabled(t *testing.T) {
	t.Setenv("SI_BROWSER_MCP_DISABLED", "1")
	if got := codexBrowserMCPURL(); got != "" {
		t.Fatalf("expected empty browser MCP URL when disabled, got %q", got)
	}
}

func TestEnvIsTrue(t *testing.T) {
	t.Setenv("SI_TEST_BOOL", "true")
	if !envIsTrue("SI_TEST_BOOL") {
		t.Fatalf("expected true for SI_TEST_BOOL=true")
	}
	t.Setenv("SI_TEST_BOOL", "0")
	if envIsTrue("SI_TEST_BOOL") {
		t.Fatalf("expected false for SI_TEST_BOOL=0")
	}
}
