package main

import "testing"

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
