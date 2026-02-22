package main

import (
	"os"
	"path/filepath"
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
	tempDir := t.TempDir()
	vaultFile := filepath.Join(tempDir, ".env")
	trustFile := filepath.Join(tempDir, "trust.json")
	keyFile := filepath.Join(tempDir, "age.key")

	identity, err := vault.GenerateIdentity()
	if err != nil {
		t.Fatalf("GenerateIdentity: %v", err)
	}
	if err := os.WriteFile(keyFile, []byte(identity.String()+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile key: %v", err)
	}

	doc := vault.ParseDotenv(nil)
	if _, err := vault.EnsureVaultHeader(&doc, []string{identity.Recipient().String()}); err != nil {
		t.Fatalf("EnsureVaultHeader: %v", err)
	}
	cipher, err := vault.EncryptStringV1("token-from-vault", []string{identity.Recipient().String()})
	if err != nil {
		t.Fatalf("EncryptStringV1: %v", err)
	}
	if _, err := doc.Set("GH_PAT_AUREUMA_VANGUARDA", cipher, vault.SetOptions{}); err != nil {
		t.Fatalf("doc.Set: %v", err)
	}
	if err := os.WriteFile(vaultFile, doc.Bytes(), 0o600); err != nil {
		t.Fatalf("WriteFile vault: %v", err)
	}

	target, err := vault.ResolveTarget(vault.ResolveOptions{File: vaultFile, DefaultFile: vaultFile})
	if err != nil {
		t.Fatalf("ResolveTarget: %v", err)
	}
	store, err := vault.LoadTrustStore(trustFile)
	if err != nil {
		t.Fatalf("LoadTrustStore: %v", err)
	}
	store.Upsert(vault.TrustEntry{
		RepoRoot:    target.RepoRoot,
		File:        target.File,
		Fingerprint: vault.RecipientsFingerprint(vault.ParseRecipientsFromDotenv(doc)),
	})
	if err := store.Save(trustFile); err != nil {
		t.Fatalf("Save trust store: %v", err)
	}

	settings := Settings{}
	applySettingsDefaults(&settings)
	settings.Vault.File = vaultFile
	settings.Vault.TrustStore = trustFile
	settings.Vault.KeyBackend = "file"
	settings.Vault.KeyFile = keyFile

	token, source := resolveGitHubOAuthAccessTokenFromVault(settings, GitHubAccountEntry{OAuthTokenEnv: "GH_PAT_AUREUMA_VANGUARDA"})
	if token != "token-from-vault" {
		t.Fatalf("token=%q", token)
	}
	if source != "vault:GH_PAT_AUREUMA_VANGUARDA" {
		t.Fatalf("source=%q", source)
	}
}
