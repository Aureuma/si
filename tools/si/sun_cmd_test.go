package main

import "testing"

func TestSplitCSVScopes(t *testing.T) {
	got := splitCSVScopes("objects:read, objects:write,objects:read, ,audit:read")
	want := []string{"objects:read", "objects:write", "audit:read"}
	if len(got) != len(want) {
		t.Fatalf("len=%d want=%d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("scope[%d]=%q want=%q", i, got[i], want[i])
		}
	}
}

func TestResolveVaultPath(t *testing.T) {
	settings := Settings{}
	settings.Vault.File = "~/.si/vault/default.env"

	if got := resolveVaultPath(settings, " /tmp/custom.env "); got != "custom" {
		t.Fatalf("explicit scope=%q", got)
	}
	if got := resolveVaultPath(settings, ""); got != "default" {
		t.Fatalf("settings scope=%q", got)
	}
	settings.Vault.File = ""
	if got := resolveVaultPath(settings, ""); got != "default" {
		t.Fatalf("fallback scope=%q", got)
	}
}

func TestSunClientFromSettingsEnvPrecedence(t *testing.T) {
	t.Setenv("SI_SUN_BASE_URL", "https://env-sun.local")
	t.Setenv("SI_SUN_TOKEN", "env-token")
	settings := Settings{}
	settings.Sun.BaseURL = "https://settings-sun.local"
	settings.Sun.Token = "settings-token"
	settings.Sun.TimeoutSeconds = 1

	client, err := sunClientFromSettings(settings)
	if err != nil {
		t.Fatalf("sunClientFromSettings: %v", err)
	}
	if client.baseURL != "https://env-sun.local" {
		t.Fatalf("base url=%q", client.baseURL)
	}
	if client.token != "env-token" {
		t.Fatalf("token=%q", client.token)
	}
}
