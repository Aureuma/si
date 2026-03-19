package main

import (
	"os"
	"path/filepath"
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
	workspace := t.TempDir()
	envDir := filepath.Join(workspace, "safe")
	if err := os.MkdirAll(envDir, 0o755); err != nil {
		t.Fatalf("mkdir env dir: %v", err)
	}
	envFile := filepath.Join(envDir, ".env.dev")
	keyringPath := filepath.Join(workspace, "si-vault-keyring.json")
	t.Setenv("SI_VAULT_ENV_FILE", envFile)
	t.Setenv("SI_VAULT_KEYRING_FILE", keyringPath)

	publicKey, privateKey, err := vault.GenerateSIVaultKeyPair()
	if err != nil {
		t.Fatalf("GenerateSIVaultKeyPair: %v", err)
	}
	cipher, err := vault.EncryptSIVaultValue("token-from-vault", publicKey)
	if err != nil {
		t.Fatalf("EncryptSIVaultValue: %v", err)
	}
	if err := os.WriteFile(envFile, []byte("GH_PAT_AUREUMA_VANGUARDA="+cipher+"\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	settings := Settings{}
	applySettingsDefaults(&settings)
	settings.Vault.File = envFile
	target, err := resolveSIVaultTarget("", "", envFile)
	if err != nil {
		t.Fatalf("resolveSIVaultTarget: %v", err)
	}
	keyring := siVaultKeyring{
		Entries: map[string]siVaultKeyMaterial{
			siVaultKeyringEntryKey(target.Repo, target.Env): {
				Repo:       target.Repo,
				Env:        target.Env,
				PublicKey:  publicKey,
				PrivateKey: privateKey,
			},
		},
	}
	if err := saveSIVaultKeyring(keyring); err != nil {
		t.Fatalf("saveSIVaultKeyring: %v", err)
	}

	token, source := resolveGitHubOAuthAccessTokenFromVault(settings, GitHubAccountEntry{OAuthTokenEnv: "GH_PAT_AUREUMA_VANGUARDA"})
	if token != "token-from-vault" {
		t.Fatalf("token=%q", token)
	}
	if source != "vault:GH_PAT_AUREUMA_VANGUARDA" {
		t.Fatalf("source=%q", source)
	}
}

func TestCmdGithubContextListDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' '[{\"alias\":\"core\",\"default\":\"true\"}]'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)

	out := captureOutputForTest(t, func() {
		cmdGithubContextList([]string{"--json"})
	})

	if !strings.Contains(out, "\"alias\":\"core\"") {
		t.Fatalf("unexpected output: %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\ncontext\nlist\n--json" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestCmdGithubContextCurrentDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' '{\"account_alias\":\"core\",\"owner\":\"Aureuma\",\"auth_mode\":\"oauth\",\"base_url\":\"https://api.github.com\",\"source\":\"settings.default_account\"}'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)

	out := captureOutputForTest(t, func() {
		cmdGithubContextCurrent([]string{"--json"})
	})

	if !strings.Contains(out, "\"account_alias\":\"core\"") {
		t.Fatalf("unexpected output: %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\ncontext\ncurrent\n--json" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestCmdGithubAuthStatusDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' '{\"account_alias\":\"core\",\"owner\":\"Aureuma\",\"auth_mode\":\"oauth\",\"base_url\":\"https://api.github.com\",\"source\":\"settings.default_account,env:GITHUB_TOKEN\",\"token_preview\":\"gho_exam...\"}'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)

	out := captureOutputForTest(t, func() {
		cmdGithubAuthStatus([]string{"--json"})
	})

	if !strings.Contains(out, "\"account_alias\":\"core\"") {
		t.Fatalf("unexpected output: %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "github\nauth\nstatus\n--json" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}
