package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"si/tools/si/internal/vault"
)

func TestCloudflareAccountEnvPrefix(t *testing.T) {
	if got := cloudflareAccountEnvPrefix("core-main", CloudflareAccountEntry{}); got != "CLOUDFLARE_CORE_MAIN_" {
		t.Fatalf("unexpected prefix: %q", got)
	}
	if got := cloudflareAccountEnvPrefix("core", CloudflareAccountEntry{VaultPrefix: "cf_ops"}); got != "CF_OPS_" {
		t.Fatalf("unexpected vault prefix: %q", got)
	}
}

func TestResolveCloudflareAPITokenFromEnv(t *testing.T) {
	t.Setenv("CLOUDFLARE_CORE_API_TOKEN", "token-123")
	value, source := resolveCloudflareAPIToken("core", CloudflareAccountEntry{}, "")
	if value != "token-123" {
		t.Fatalf("unexpected token: %q", value)
	}
	if source != "env:CLOUDFLARE_CORE_API_TOKEN" {
		t.Fatalf("unexpected source: %q", source)
	}
}

func TestResolveCloudflareZoneIDByEnvironment(t *testing.T) {
	zone, source := resolveCloudflareZoneID("core", CloudflareAccountEntry{StagingZoneID: "zone-stg"}, "staging", "")
	if zone != "zone-stg" {
		t.Fatalf("unexpected zone: %q", zone)
	}
	if source != "settings.staging_zone_id" {
		t.Fatalf("unexpected source: %q", source)
	}
}

func TestParseCloudflareEnvironment(t *testing.T) {
	if _, err := parseCloudflareEnvironment("test"); err == nil {
		t.Fatalf("expected error for test environment")
	}
	if env, err := parseCloudflareEnvironment("prod"); err != nil || env != "prod" {
		t.Fatalf("unexpected parse result: env=%q err=%v", env, err)
	}
}

func TestCloudflareResolvePath(t *testing.T) {
	runtime := cloudflareRuntimeContext{AccountID: "acc_1", ZoneID: "zone_1", ZoneName: "example.com"}
	path, err := cloudflareResolvePath("/accounts/{account_id}/zones/{zone_id}/items/{id}", runtime, "abc")
	if err != nil {
		t.Fatalf("resolve path: %v", err)
	}
	if path != "/accounts/acc_1/zones/zone_1/items/abc" {
		t.Fatalf("unexpected path: %q", path)
	}
}

func TestResolveCloudflareCredentialsFromVaultEncrypted(t *testing.T) {
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
	accountCipher, err := vault.EncryptSIVaultValue("acc_vault_1", publicKey)
	if err != nil {
		t.Fatalf("Encrypt account id: %v", err)
	}
	tokenCipher, err := vault.EncryptSIVaultValue("tok_vault_1", publicKey)
	if err != nil {
		t.Fatalf("Encrypt token: %v", err)
	}

	body := strings.Join([]string{
		"VIVA_CLOUDFLARE_ACCOUNT_ID=" + accountCipher,
		"VIVA_CLOUDFLARE_R2_USER_API_TOKEN=" + tokenCipher,
		"",
	}, "\n")
	if err := os.WriteFile(envFile, []byte(body), 0o600); err != nil {
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

	account := CloudflareAccountEntry{
		AccountIDEnv: "VIVA_CLOUDFLARE_ACCOUNT_ID",
		APITokenEnv:  "VIVA_CLOUDFLARE_R2_USER_API_TOKEN",
	}
	accountID, accountSource := resolveCloudflareAccountIDFromVault(settings, account)
	if accountID != "acc_vault_1" {
		t.Fatalf("account id=%q", accountID)
	}
	if accountSource != "vault:VIVA_CLOUDFLARE_ACCOUNT_ID" {
		t.Fatalf("account source=%q", accountSource)
	}
	token, tokenSource := resolveCloudflareAPITokenFromVault(settings, account)
	if token != "tok_vault_1" {
		t.Fatalf("token=%q", token)
	}
	if tokenSource != "vault:VIVA_CLOUDFLARE_R2_USER_API_TOKEN" {
		t.Fatalf("token source=%q", tokenSource)
	}
}

func TestCmdCloudflareContextListDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' '{\"contexts\":[{\"alias\":\"core\"}]}'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)

	out := captureOutputForTest(t, func() {
		cmdCloudflareContextList([]string{"--json"})
	})

	if !strings.Contains(out, "\"alias\":\"core\"") {
		t.Fatalf("unexpected output: %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "cloudflare\ncontext\nlist\n--json" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestCmdCloudflareContextCurrentDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' '{\"account_alias\":\"core\",\"account_id\":\"acc_core\",\"environment\":\"prod\",\"zone_id\":\"zone_prod\",\"zone_name\":\"example.com\",\"base_url\":\"https://api.cloudflare.com/client/v4\",\"source\":\"settings.account_id,settings.prod_zone_id\"}'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)

	out := captureOutputForTest(t, func() {
		cmdCloudflareContextCurrent([]string{"--json"})
	})

	if !strings.Contains(out, "\"account_alias\":\"core\"") {
		t.Fatalf("unexpected output: %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "cloudflare\ncontext\ncurrent\n--json" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}
