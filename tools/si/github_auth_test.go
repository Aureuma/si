package main

import (
	"strings"
	"testing"

	"si/tools/si/internal/vault"
)

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

func TestResolveGitHubOAuthAccessTokenFromEnv(t *testing.T) {
	t.Setenv("GITHUB_CORE_OAUTH_ACCESS_TOKEN", "oauth-token")
	token, source := resolveGitHubOAuthAccessToken("core", GitHubAccountEntry{}, githubAuthOverrides{})
	if token != "oauth-token" {
		t.Fatalf("unexpected oauth token: %q", token)
	}
	if source != "env:GITHUB_CORE_OAUTH_ACCESS_TOKEN" {
		t.Fatalf("unexpected source: %q", source)
	}
}

func TestResolveGitHubOAuthAccessTokenFromGitHubPAT(t *testing.T) {
	t.Setenv("GITHUB_PAT", "pat-token")
	token, source := resolveGitHubOAuthAccessToken("core", GitHubAccountEntry{}, githubAuthOverrides{})
	if token != "pat-token" {
		t.Fatalf("unexpected oauth token: %q", token)
	}
	if source != "env:GITHUB_PAT" {
		t.Fatalf("unexpected source: %q", source)
	}
}

func TestResolveGitHubOAuthAccessTokenFromGHPAT(t *testing.T) {
	t.Setenv("GH_PAT", "gh-pat-token")
	token, source := resolveGitHubOAuthAccessToken("core", GitHubAccountEntry{}, githubAuthOverrides{})
	if token != "gh-pat-token" {
		t.Fatalf("unexpected oauth token: %q", token)
	}
	if source != "env:GH_PAT" {
		t.Fatalf("unexpected source: %q", source)
	}
}

func TestResolveGitHubAuthMode_OAuthFromOverride(t *testing.T) {
	mode, source, err := resolveGitHubAuthMode(Settings{}, "core", GitHubAccountEntry{}, githubAuthOverrides{AuthMode: "oauth"})
	if err != nil {
		t.Fatalf("resolve auth mode: %v", err)
	}
	if string(mode) != "oauth" {
		t.Fatalf("unexpected mode: %q", mode)
	}
	if source != "flag:--auth-mode" {
		t.Fatalf("unexpected source: %q", source)
	}
}

func TestResolveGitHubAuthMode_OAuthFromEnv(t *testing.T) {
	t.Setenv("GITHUB_AUTH_MODE", "oauth")
	mode, source, err := resolveGitHubAuthMode(Settings{}, "core", GitHubAccountEntry{}, githubAuthOverrides{})
	if err != nil {
		t.Fatalf("resolve auth mode: %v", err)
	}
	if string(mode) != "oauth" {
		t.Fatalf("unexpected mode: %q", mode)
	}
	if source != "env:GITHUB_AUTH_MODE" {
		t.Fatalf("unexpected source: %q", source)
	}
}

func TestResolveGitHubOAuthAccessTokenFromVaultEncrypted(t *testing.T) {
	server, store := newSunTestServer(t, "acme", "token-github-vault")
	defer server.Close()

	identity, err := vault.GenerateIdentity()
	if err != nil {
		t.Fatalf("GenerateIdentity: %v", err)
	}
	cipher, err := vault.EncryptStringV1("token-from-vault", []string{identity.Recipient().String()})
	if err != nil {
		t.Fatalf("EncryptStringV1: %v", err)
	}

	settings := Settings{}
	applySettingsDefaults(&settings)
	settings.Sun.BaseURL = server.URL
	settings.Sun.Token = "token-github-vault"
	settings.Sun.Account = "acme"
	settings.Vault.File = "default"

	target, err := vaultResolveTarget(settings, "", true)
	if err != nil {
		t.Fatalf("vaultResolveTarget: %v", err)
	}
	kind := vaultSunKVKind(target)
	store.mu.Lock()
	identityKey := store.key(sunVaultIdentityKind, "default")
	store.payloads[identityKey] = []byte(strings.TrimSpace(identity.String()) + "\n")
	store.revs[identityKey] = 1
	store.metadata[identityKey] = map[string]any{}
	store.created[identityKey] = "2026-01-01T00:00:00Z"
	store.updated[identityKey] = "2026-01-02T00:00:00Z"
	valueKey := store.key(kind, "GH_PAT_AUREUMA_VANGUARDA")
	store.payloads[valueKey] = []byte(strings.TrimSpace(cipher) + "\n")
	store.revs[valueKey] = 1
	store.metadata[valueKey] = map[string]any{"deleted": false}
	store.created[valueKey] = "2026-01-01T00:00:00Z"
	store.updated[valueKey] = "2026-01-02T00:00:00Z"
	store.mu.Unlock()

	token, source := resolveGitHubOAuthAccessTokenFromVault(settings, GitHubAccountEntry{OAuthTokenEnv: "GH_PAT_AUREUMA_VANGUARDA"})
	if token != "token-from-vault" {
		t.Fatalf("token=%q", token)
	}
	if source != "vault:GH_PAT_AUREUMA_VANGUARDA" {
		t.Fatalf("source=%q", source)
	}
}
