package main

import (
	"os"
	"path/filepath"
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
	accountCipher, err := vault.EncryptStringV1("acc_vault_1", []string{identity.Recipient().String()})
	if err != nil {
		t.Fatalf("Encrypt account id: %v", err)
	}
	tokenCipher, err := vault.EncryptStringV1("tok_vault_1", []string{identity.Recipient().String()})
	if err != nil {
		t.Fatalf("Encrypt token: %v", err)
	}
	if _, err := doc.Set("VIVA_CLOUDFLARE_ACCOUNT_ID", accountCipher, vault.SetOptions{}); err != nil {
		t.Fatalf("doc.Set account: %v", err)
	}
	if _, err := doc.Set("VIVA_CLOUDFLARE_R2_USER_API_TOKEN", tokenCipher, vault.SetOptions{}); err != nil {
		t.Fatalf("doc.Set token: %v", err)
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
