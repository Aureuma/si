package main

import (
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
	server, store := newSunTestServer(t, "acme", "token-cloudflare-vault")
	defer server.Close()

	identity, err := vault.GenerateIdentity()
	if err != nil {
		t.Fatalf("GenerateIdentity: %v", err)
	}
	accountCipher, err := vault.EncryptStringV1("acc_vault_1", []string{identity.Recipient().String()})
	if err != nil {
		t.Fatalf("Encrypt account id: %v", err)
	}
	tokenCipher, err := vault.EncryptStringV1("tok_vault_1", []string{identity.Recipient().String()})
	if err != nil {
		t.Fatalf("Encrypt token: %v", err)
	}

	settings := Settings{}
	applySettingsDefaults(&settings)
	settings.Sun.BaseURL = server.URL
	settings.Sun.Token = "token-cloudflare-vault"
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
	for _, kv := range []struct {
		key   string
		value string
	}{
		{key: "VIVA_CLOUDFLARE_ACCOUNT_ID", value: accountCipher},
		{key: "VIVA_CLOUDFLARE_R2_USER_API_TOKEN", value: tokenCipher},
	} {
		objectKey := store.key(kind, kv.key)
		store.payloads[objectKey] = []byte(strings.TrimSpace(kv.value) + "\n")
		store.revs[objectKey] = 1
		store.metadata[objectKey] = map[string]any{"deleted": false}
		store.created[objectKey] = "2026-01-01T00:00:00Z"
		store.updated[objectKey] = "2026-01-02T00:00:00Z"
	}
	store.mu.Unlock()

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
