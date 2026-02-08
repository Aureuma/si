package main

import "testing"

func TestSlugUpper(t *testing.T) {
	if got := slugUpper("core-main 1"); got != "CORE_MAIN_1" {
		t.Fatalf("unexpected slug: %q", got)
	}
}

func TestGithubAccountEnvPrefix(t *testing.T) {
	if got := githubAccountEnvPrefix("core", GitHubAccountEntry{}); got != "GITHUB_CORE_" {
		t.Fatalf("unexpected prefix: %q", got)
	}
	if got := githubAccountEnvPrefix("core", GitHubAccountEntry{VaultPrefix: "my_prefix"}); got != "MY_PREFIX_" {
		t.Fatalf("unexpected vault prefix: %q", got)
	}
}

func TestResolveGitHubAppIDFromEnv(t *testing.T) {
	t.Setenv("GITHUB_CORE_APP_ID", "123")
	id, source := resolveGitHubAppID("core", GitHubAccountEntry{}, githubAuthOverrides{})
	if id != 123 {
		t.Fatalf("expected app id 123, got %d", id)
	}
	if source != "env:GITHUB_CORE_APP_ID" {
		t.Fatalf("unexpected source: %q", source)
	}
}

func TestResolveGitHubAppKeyFromEnv(t *testing.T) {
	t.Setenv("GITHUB_CORE_APP_PRIVATE_KEY_PEM", "pem-value")
	key, source := resolveGitHubAppKey("core", GitHubAccountEntry{}, githubAuthOverrides{})
	if key != "pem-value" {
		t.Fatalf("unexpected key: %q", key)
	}
	if source != "env:GITHUB_CORE_APP_PRIVATE_KEY_PEM" {
		t.Fatalf("unexpected source: %q", source)
	}
}

func TestResolveGitHubInstallationIDFromEnv(t *testing.T) {
	t.Setenv("GITHUB_CORE_INSTALLATION_ID", "456")
	id, source := resolveGitHubInstallationID("core", GitHubAccountEntry{}, githubAuthOverrides{})
	if id != 456 {
		t.Fatalf("expected installation id 456, got %d", id)
	}
	if source != "env:GITHUB_CORE_INSTALLATION_ID" {
		t.Fatalf("unexpected source: %q", source)
	}
}
