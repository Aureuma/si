package main

import "testing"

func TestGoogleAccountEnvPrefix(t *testing.T) {
	if got := googleAccountEnvPrefix("core-main", GoogleAccountEntry{}); got != "GOOGLE_CORE_MAIN_" {
		t.Fatalf("unexpected prefix: %q", got)
	}
	if got := googleAccountEnvPrefix("core", GoogleAccountEntry{VaultPrefix: "google_ops"}); got != "GOOGLE_OPS_" {
		t.Fatalf("unexpected vault prefix: %q", got)
	}
}

func TestResolveGooglePlacesAPIKeyByEnvironment(t *testing.T) {
	t.Setenv("GOOGLE_CORE_STAGING_PLACES_API_KEY", "stage-key")
	value, source := resolveGooglePlacesAPIKey("core", GoogleAccountEntry{}, "staging", "")
	if value != "stage-key" {
		t.Fatalf("unexpected key: %q", value)
	}
	if source != "env:GOOGLE_CORE_STAGING_PLACES_API_KEY" {
		t.Fatalf("unexpected source: %q", source)
	}
}

func TestParseGoogleEnvironment(t *testing.T) {
	if _, err := parseGoogleEnvironment("test"); err == nil {
		t.Fatalf("expected error for test environment")
	}
	if env, err := parseGoogleEnvironment("prod"); err != nil || env != "prod" {
		t.Fatalf("unexpected parse result: env=%q err=%v", env, err)
	}
}
