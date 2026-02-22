package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaybeHeliaAutoBackupVaultHeliaModeRequiresAuthConfigAdditional(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SI_SETTINGS_HOME", home)

	settings := defaultSettings()
	applySettingsDefaults(&settings)
	settings.Vault.SyncBackend = vaultSyncBackendHelia
	settings.Helia.BaseURL = ""
	settings.Helia.Token = ""
	if err := saveSettings(settings); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	vaultFile := filepath.Join(home, ".si", "vault", ".env")
	if err := os.MkdirAll(filepath.Dir(vaultFile), 0o700); err != nil {
		t.Fatalf("mkdir vault dir: %v", err)
	}
	if err := os.WriteFile(vaultFile, []byte(""), 0o600); err != nil {
		t.Fatalf("write vault file: %v", err)
	}

	err := maybeHeliaAutoBackupVault("test_missing_auth", vaultFile)
	if err == nil {
		t.Fatalf("expected strict helia mode to fail without helia auth config")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "helia") {
		t.Fatalf("expected helia context in error, got: %v", err)
	}
}

func TestMaybeHeliaAutoSyncProfileUploadsCredentialsAdditional(t *testing.T) {
	server, store := newHeliaTestServer(t, "acme", "token-autosync")
	defer server.Close()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SI_SETTINGS_HOME", home)

	settings := defaultSettings()
	applySettingsDefaults(&settings)
	settings.Helia.AutoSync = true
	settings.Helia.BaseURL = server.URL
	settings.Helia.Token = "token-autosync"
	if err := saveSettings(settings); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	profile := codexProfile{ID: "demo-sync", Name: "Demo Sync", Email: "demo-sync@example.com"}
	dir, err := ensureCodexProfileDir(profile)
	if err != nil {
		t.Fatalf("ensure profile dir: %v", err)
	}
	authPath := filepath.Join(dir, "auth.json")
	authJSON := `{"tokens":{"access_token":"access-demo","refresh_token":"refresh-demo"}}`
	if err := os.WriteFile(authPath, []byte(authJSON), 0o600); err != nil {
		t.Fatalf("write auth cache: %v", err)
	}

	maybeHeliaAutoSyncProfile("test_auto_sync", profile)

	payload, ok := store.get(heliaCodexProfileBundleKind, profile.ID)
	if !ok || len(payload) == 0 {
		t.Fatalf("expected cloud profile payload to be uploaded")
	}
	var bundle heliaCodexProfileBundle
	if err := json.Unmarshal(payload, &bundle); err != nil {
		t.Fatalf("decode uploaded profile bundle: %v", err)
	}
	if bundle.ID != profile.ID {
		t.Fatalf("bundle id mismatch: got %q want %q", bundle.ID, profile.ID)
	}
	if strings.TrimSpace(string(bundle.AuthJSON)) != authJSON {
		t.Fatalf("bundle auth payload mismatch")
	}
}

func TestSICommandSupportsLoginHelpAndVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("skip subprocess CLI smoke in short mode")
	}
	home := t.TempDir()
	env := map[string]string{
		"HOME":             home,
		"SI_SETTINGS_HOME": home,
	}
	stdout, stderr, err := runSICommand(t, env, "login", "--help")
	if err != nil {
		t.Fatalf("login --help failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	stdout, stderr, err = runSICommand(t, env, "version")
	if err != nil {
		t.Fatalf("version failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if strings.TrimSpace(stdout) == "" {
		t.Fatalf("expected non-empty version output")
	}
}
