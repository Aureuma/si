package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"si/tools/si/internal/vault"
)

func writeTestSIVaultKeyring(t *testing.T, path string, entries map[string]siVaultKeyMaterial) {
	t.Helper()
	doc := siVaultKeyring{Entries: entries}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshal keyring: %v", err)
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write keyring: %v", err)
	}
}

func readTestSIVaultKeyring(t *testing.T, path string) siVaultKeyring {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read keyring: %v", err)
	}
	var doc siVaultKeyring
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse keyring: %v", err)
	}
	if doc.Entries == nil {
		doc.Entries = map[string]siVaultKeyMaterial{}
	}
	return doc
}

func TestEncryptDotenvDocSkipsEncryptedWithoutReencrypt(t *testing.T) {
	publicKey, privateKey, err := vault.GenerateSIVaultKeyPair()
	if err != nil {
		t.Fatalf("GenerateSIVaultKeyPair: %v", err)
	}
	existingCipher, err := vault.EncryptSIVaultValue("already", publicKey)
	if err != nil {
		t.Fatalf("EncryptSIVaultValue(existing): %v", err)
	}
	doc := vault.ParseDotenv([]byte(strings.Join([]string{
		vault.SIVaultPublicKeyName + "=" + publicKey,
		"A=plain",
		"B=" + existingCipher,
		"",
	}, "\n")))
	stats, err := encryptDotenvDoc(&doc, publicKey, []string{privateKey}, nil, nil, false)
	if err != nil {
		t.Fatalf("encryptDotenvDoc: %v", err)
	}
	if stats.Encrypted != 1 || stats.SkippedEncrypted != 1 || stats.Reencrypted != 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	entries, err := vault.Entries(doc)
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	values := map[string]string{}
	for _, entry := range entries {
		values[entry.Key] = entry.ValueRaw
	}
	if !vault.IsSIVaultEncryptedValue(values["A"]) {
		t.Fatalf("A was not encrypted: %q", values["A"])
	}
	if values["B"] != existingCipher {
		t.Fatalf("B changed without --reencrypt")
	}
}

func TestEncryptDotenvDocReencryptDecryptsPlaintextBeforeEncrypt(t *testing.T) {
	publicKey, privateKey, err := vault.GenerateSIVaultKeyPair()
	if err != nil {
		t.Fatalf("GenerateSIVaultKeyPair: %v", err)
	}
	originalCipher, err := vault.EncryptSIVaultValue("super-secret", publicKey)
	if err != nil {
		t.Fatalf("EncryptSIVaultValue(original): %v", err)
	}
	doc := vault.ParseDotenv([]byte(strings.Join([]string{
		vault.SIVaultPublicKeyName + "=" + publicKey,
		"A=" + originalCipher,
		"",
	}, "\n")))
	stats, err := encryptDotenvDoc(&doc, publicKey, []string{privateKey}, nil, nil, true)
	if err != nil {
		t.Fatalf("encryptDotenvDoc reencrypt: %v", err)
	}
	if stats.Reencrypted != 1 || stats.Encrypted != 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	entries, err := vault.Entries(doc)
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	values := map[string]string{}
	for _, entry := range entries {
		values[entry.Key] = entry.ValueRaw
	}
	if values["A"] == originalCipher {
		t.Fatalf("A ciphertext did not rotate under --reencrypt")
	}
	plain, err := vault.DecryptSIVaultValue(values["A"], []string{privateKey})
	if err != nil {
		t.Fatalf("DecryptSIVaultValue: %v", err)
	}
	if plain != "super-secret" {
		t.Fatalf("unexpected plaintext after reencrypt: %q", plain)
	}
}

func TestEnsureSIVaultPublicKeyHeaderLeavesBlankLineAfterHeader(t *testing.T) {
	publicKey, _, err := vault.GenerateSIVaultKeyPair()
	if err != nil {
		t.Fatalf("GenerateSIVaultKeyPair: %v", err)
	}
	doc := vault.ParseDotenv([]byte("FOO=bar\nBAR=baz\n"))
	changed, err := ensureSIVaultPublicKeyHeader(&doc, publicKey)
	if err != nil {
		t.Fatalf("ensureSIVaultPublicKeyHeader: %v", err)
	}
	if !changed {
		t.Fatalf("expected header insertion")
	}
	lines := strings.Split(strings.TrimRight(string(doc.Bytes()), "\n"), "\n")
	if len(lines) < 3 {
		t.Fatalf("unexpected rendered doc: %q", string(doc.Bytes()))
	}
	if lines[0] != vault.SIVaultPublicKeyName+"="+publicKey {
		t.Fatalf("unexpected header line: %q", lines[0])
	}
	if lines[1] != "" {
		t.Fatalf("expected blank line after header, got: %q", lines[1])
	}
	if lines[2] != "FOO=bar" {
		t.Fatalf("unexpected first dotenv assignment line: %q", lines[2])
	}
}

func TestEnsureSIVaultPublicKeyHeaderCollapsesExtraBlankLines(t *testing.T) {
	publicKey, _, err := vault.GenerateSIVaultKeyPair()
	if err != nil {
		t.Fatalf("GenerateSIVaultKeyPair: %v", err)
	}
	doc := vault.ParseDotenv([]byte(strings.Join([]string{
		vault.SIVaultPublicKeyName + "=old",
		"",
		"",
		"",
		"FOO=bar",
		"",
	}, "\n")))
	_, err = ensureSIVaultPublicKeyHeader(&doc, publicKey)
	if err != nil {
		t.Fatalf("ensureSIVaultPublicKeyHeader: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(doc.Bytes()), "\n"), "\n")
	if len(lines) < 3 {
		t.Fatalf("unexpected rendered doc: %q", string(doc.Bytes()))
	}
	if lines[0] != vault.SIVaultPublicKeyName+"="+publicKey {
		t.Fatalf("unexpected header line: %q", lines[0])
	}
	if lines[1] != "" {
		t.Fatalf("expected exactly one blank line after header, got: %q", lines[1])
	}
	if lines[2] == "" {
		t.Fatalf("expected header-adjacent blank lines to be collapsed")
	}
}

func TestInferSIVaultEnvFromEnvFile(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: ".env.dev", want: "dev"},
		{in: ".env.prod", want: "prod"},
		{in: ".env.staging.local", want: "staging"},
		{in: "/tmp/project/.env.qa", want: "qa"},
		{in: ".env", want: ""},
		{in: "env", want: ""},
		{in: ".env.", want: ""},
	}
	for _, tc := range tests {
		if got := inferSIVaultEnvFromEnvFile(tc.in); got != tc.want {
			t.Fatalf("inferSIVaultEnvFromEnvFile(%q)=%q want=%q", tc.in, got, tc.want)
		}
	}
}

func TestResolveSIVaultTargetInfersEnvFromEnvFile(t *testing.T) {
	repoDir := t.TempDir()
	t.Setenv("SI_VAULT_ENV", "")
	t.Setenv("SI_VAULT_ENV_FILE", "")

	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevWD) })
	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}

	target, err := resolveSIVaultTarget("", "", ".env.prod")
	if err != nil {
		t.Fatalf("resolveSIVaultTarget: %v", err)
	}
	if target.Env != "prod" {
		t.Fatalf("target.Env=%q want=prod", target.Env)
	}
	if target.EnvFile != filepath.Join(repoDir, ".env.prod") {
		t.Fatalf("target.EnvFile=%q", target.EnvFile)
	}

	override, err := resolveSIVaultTarget("", "dev", ".env.prod")
	if err != nil {
		t.Fatalf("resolveSIVaultTarget with explicit env: %v", err)
	}
	if override.Env != "dev" {
		t.Fatalf("override env=%q want=dev", override.Env)
	}
}

func TestResolveSIVaultTargetInfersRepoFromEnvFileParent(t *testing.T) {
	workspace := t.TempDir()
	safePath := filepath.Join(workspace, "safe", "sampleapp")
	if err := os.MkdirAll(safePath, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	envFile := filepath.Join(safePath, ".env.dev")

	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevWD) })
	if err := os.Chdir(workspace); err != nil {
		t.Fatalf("Chdir: %v", err)
	}

	target, err := resolveSIVaultTarget("", "", envFile)
	if err != nil {
		t.Fatalf("resolveSIVaultTarget: %v", err)
	}
	if target.Repo != "sampleapp" {
		t.Fatalf("target.Repo=%q want=sampleapp", target.Repo)
	}
	if target.Env != "dev" {
		t.Fatalf("target.Env=%q want=dev", target.Env)
	}
}

func TestResolveSIVaultTargetRepoFlagOverridesEnvFileInference(t *testing.T) {
	workspace := t.TempDir()
	safePath := filepath.Join(workspace, "safe", "sampleapp")
	if err := os.MkdirAll(safePath, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	envFile := filepath.Join(safePath, ".env.dev")

	target, err := resolveSIVaultTarget("override-repo", "", envFile)
	if err != nil {
		t.Fatalf("resolveSIVaultTarget: %v", err)
	}
	if target.Repo != "override-repo" {
		t.Fatalf("target.Repo=%q want=override-repo", target.Repo)
	}
}

func TestEnsureSIVaultKeyMaterialRejectsMissingCanonical(t *testing.T) {
	keyringPath := filepath.Join(t.TempDir(), "si-vault-keyring.json")
	t.Setenv("SI_VAULT_KEYRING_FILE", keyringPath)
	t.Setenv(vault.SIVaultPublicKeyName, "")
	t.Setenv(vault.SIVaultPrivateKeyName, "")

	_, err := ensureSIVaultKeyMaterial(defaultSettings(), siVaultTarget{Repo: "safe", Env: "dev"})
	if err == nil {
		t.Fatalf("expected missing canonical error")
	}
	if !strings.Contains(err.Error(), "canonical keypair is not initialized") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureSIVaultKeyMaterialSeedsMissingEntryFromCanonical(t *testing.T) {
	keyringPath := filepath.Join(t.TempDir(), "si-vault-keyring.json")
	t.Setenv("SI_VAULT_KEYRING_FILE", keyringPath)
	t.Setenv(vault.SIVaultPublicKeyName, "")
	t.Setenv(vault.SIVaultPrivateKeyName, "")

	pub, priv, err := vault.GenerateSIVaultKeyPair()
	if err != nil {
		t.Fatalf("GenerateSIVaultKeyPair: %v", err)
	}
	writeTestSIVaultKeyring(t, keyringPath, map[string]siVaultKeyMaterial{
		"safe/dev": {Repo: "safe", Env: "dev", PublicKey: pub, PrivateKey: priv},
	})

	material, err := ensureSIVaultKeyMaterial(defaultSettings(), siVaultTarget{Repo: "core", Env: "prod"})
	if err != nil {
		t.Fatalf("ensureSIVaultKeyMaterial: %v", err)
	}
	if strings.TrimSpace(material.PublicKey) != strings.TrimSpace(pub) || strings.TrimSpace(material.PrivateKey) != strings.TrimSpace(priv) {
		t.Fatalf("expected seeded entry to reuse canonical keypair")
	}
	doc := readTestSIVaultKeyring(t, keyringPath)
	seeded, ok := doc.Entries["core/prod"]
	if !ok {
		t.Fatalf("expected core/prod entry to be created")
	}
	if strings.TrimSpace(seeded.PublicKey) != strings.TrimSpace(pub) || strings.TrimSpace(seeded.PrivateKey) != strings.TrimSpace(priv) {
		t.Fatalf("expected keyring core/prod to match canonical keypair")
	}
}

func TestEnsureSIVaultKeyMaterialRejectsSprawl(t *testing.T) {
	keyringPath := filepath.Join(t.TempDir(), "si-vault-keyring.json")
	t.Setenv("SI_VAULT_KEYRING_FILE", keyringPath)
	t.Setenv(vault.SIVaultPublicKeyName, "")
	t.Setenv(vault.SIVaultPrivateKeyName, "")

	pubA, privA, err := vault.GenerateSIVaultKeyPair()
	if err != nil {
		t.Fatalf("GenerateSIVaultKeyPair A: %v", err)
	}
	pubB, privB, err := vault.GenerateSIVaultKeyPair()
	if err != nil {
		t.Fatalf("GenerateSIVaultKeyPair B: %v", err)
	}
	writeTestSIVaultKeyring(t, keyringPath, map[string]siVaultKeyMaterial{
		"safe/dev":  {Repo: "safe", Env: "dev", PublicKey: pubA, PrivateKey: privA},
		"safe/prod": {Repo: "safe", Env: "prod", PublicKey: pubB, PrivateKey: privB},
	})

	_, err = ensureSIVaultKeyMaterial(defaultSettings(), siVaultTarget{Repo: "safe", Env: "dev"})
	if err == nil {
		t.Fatalf("expected key sprawl error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "key sprawl") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureSIVaultKeyMaterialBootstrapsFromEnv(t *testing.T) {
	keyringPath := filepath.Join(t.TempDir(), "si-vault-keyring.json")
	t.Setenv("SI_VAULT_KEYRING_FILE", keyringPath)

	pub, priv, err := vault.GenerateSIVaultKeyPair()
	if err != nil {
		t.Fatalf("GenerateSIVaultKeyPair: %v", err)
	}
	t.Setenv(vault.SIVaultPublicKeyName, pub)
	t.Setenv(vault.SIVaultPrivateKeyName, priv)

	material, err := ensureSIVaultKeyMaterial(defaultSettings(), siVaultTarget{Repo: "safe", Env: "dev"})
	if err != nil {
		t.Fatalf("ensureSIVaultKeyMaterial: %v", err)
	}
	if strings.TrimSpace(material.PublicKey) != strings.TrimSpace(pub) || strings.TrimSpace(material.PrivateKey) != strings.TrimSpace(priv) {
		t.Fatalf("expected bootstrap material from env")
	}
	doc := readTestSIVaultKeyring(t, keyringPath)
	entry, ok := doc.Entries["safe/dev"]
	if !ok {
		t.Fatalf("expected safe/dev entry in keyring")
	}
	if strings.TrimSpace(entry.PublicKey) != strings.TrimSpace(pub) || strings.TrimSpace(entry.PrivateKey) != strings.TrimSpace(priv) {
		t.Fatalf("expected keyring safe/dev to match env bootstrap material")
	}
}

func TestEnsureSIVaultDecryptMaterialCompatibilityDetectsDrift(t *testing.T) {
	pubExpected, _, err := vault.GenerateSIVaultKeyPair()
	if err != nil {
		t.Fatalf("GenerateSIVaultKeyPair expected: %v", err)
	}
	_, privActive, err := vault.GenerateSIVaultKeyPair()
	if err != nil {
		t.Fatalf("GenerateSIVaultKeyPair active: %v", err)
	}
	doc := vault.ParseDotenv([]byte(strings.Join([]string{
		vault.SIVaultPublicKeyName + "=" + pubExpected,
		"SECRET_TOKEN=encrypted:si-vault:Zm9v",
		"",
	}, "\n")))
	material := siVaultKeyMaterial{
		PublicKey:  "",
		PrivateKey: privActive,
	}
	settings := defaultSettings()
	applySettingsDefaults(&settings)
	target := siVaultTarget{Repo: "viva", Env: "dev", EnvFile: "/tmp/.env.dev"}

	err = ensureSIVaultDecryptMaterialCompatibility(doc, material, target, settings)
	if err == nil {
		t.Fatalf("expected drift error")
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "vault key drift detected") {
		t.Fatalf("expected key drift error message, got %q", err.Error())
	}
	if !strings.Contains(msg, "viva/dev") {
		t.Fatalf("expected repo/env in message, got %q", err.Error())
	}
}

func TestEnsureSIVaultDecryptMaterialCompatibilityAllowsBackupKeyMatch(t *testing.T) {
	pubExpected, privExpected, err := vault.GenerateSIVaultKeyPair()
	if err != nil {
		t.Fatalf("GenerateSIVaultKeyPair expected: %v", err)
	}
	_, privActive, err := vault.GenerateSIVaultKeyPair()
	if err != nil {
		t.Fatalf("GenerateSIVaultKeyPair active: %v", err)
	}
	doc := vault.ParseDotenv([]byte(strings.Join([]string{
		vault.SIVaultPublicKeyName + "=" + pubExpected,
		"SECRET_TOKEN=encrypted:si-vault:Zm9v",
		"",
	}, "\n")))
	material := siVaultKeyMaterial{
		PrivateKey:        privActive,
		BackupPrivateKeys: []string{privExpected},
	}
	settings := defaultSettings()
	applySettingsDefaults(&settings)
	target := siVaultTarget{Repo: "viva", Env: "dev", EnvFile: "/tmp/.env.dev"}

	if err := ensureSIVaultDecryptMaterialCompatibility(doc, material, target, settings); err != nil {
		t.Fatalf("expected compatibility with backup key match, got %v", err)
	}
}

func TestAnalyzeDotenvDecryptabilityDetectsUndecryptableEntries(t *testing.T) {
	pubA, privA, err := vault.GenerateSIVaultKeyPair()
	if err != nil {
		t.Fatalf("GenerateSIVaultKeyPair A: %v", err)
	}
	pubB, _, err := vault.GenerateSIVaultKeyPair()
	if err != nil {
		t.Fatalf("GenerateSIVaultKeyPair B: %v", err)
	}
	cipherA, err := vault.EncryptSIVaultValue("alpha", pubA)
	if err != nil {
		t.Fatalf("EncryptSIVaultValue A: %v", err)
	}
	cipherB, err := vault.EncryptSIVaultValue("beta", pubB)
	if err != nil {
		t.Fatalf("EncryptSIVaultValue B: %v", err)
	}
	doc := vault.ParseDotenv([]byte(strings.Join([]string{
		vault.SIVaultPublicKeyName + "=" + pubA,
		"A=" + cipherA,
		"B=" + cipherB,
		"C=plain",
		"",
	}, "\n")))

	stats, err := analyzeDotenvDecryptability(doc, []string{privA})
	if err != nil {
		t.Fatalf("analyzeDotenvDecryptability: %v", err)
	}
	if stats.Encrypted != 2 || stats.Decryptable != 1 {
		t.Fatalf("unexpected decryptability stats: %+v", stats)
	}
	if len(stats.Undecryptable) != 1 || stats.Undecryptable[0] != "B" {
		t.Fatalf("unexpected undecryptable keys: %+v", stats.Undecryptable)
	}
}
