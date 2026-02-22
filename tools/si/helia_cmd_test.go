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

	if got := resolveVaultPath(settings, " /tmp/custom.env "); got != "/tmp/custom.env" {
		t.Fatalf("explicit path=%q", got)
	}
	if got := resolveVaultPath(settings, ""); got != "~/.si/vault/default.env" {
		t.Fatalf("settings path=%q", got)
	}
	settings.Vault.File = ""
	if got := resolveVaultPath(settings, ""); got != "~/.si/vault/.env" {
		t.Fatalf("fallback path=%q", got)
	}
}

func TestHeliaClientFromSettingsEnvPrecedence(t *testing.T) {
	t.Setenv("SI_HELIA_BASE_URL", "https://env-helia.local")
	t.Setenv("SI_HELIA_TOKEN", "env-token")
	settings := Settings{}
	settings.Helia.BaseURL = "https://settings-helia.local"
	settings.Helia.Token = "settings-token"
	settings.Helia.TimeoutSeconds = 1

	client, err := heliaClientFromSettings(settings)
	if err != nil {
		t.Fatalf("heliaClientFromSettings: %v", err)
	}
	if client.baseURL != "https://env-helia.local" {
		t.Fatalf("base url=%q", client.baseURL)
	}
	if client.token != "env-token" {
		t.Fatalf("token=%q", client.token)
	}
}
