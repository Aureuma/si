package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"si/tools/si/internal/vault"
)

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
	safePath := filepath.Join(workspace, "safe", "lingospeak")
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
	if target.Repo != "lingospeak" {
		t.Fatalf("target.Repo=%q want=lingospeak", target.Repo)
	}
	if target.Env != "dev" {
		t.Fatalf("target.Env=%q want=dev", target.Env)
	}
}

func TestResolveSIVaultTargetRepoFlagOverridesEnvFileInference(t *testing.T) {
	workspace := t.TempDir()
	safePath := filepath.Join(workspace, "safe", "lingospeak")
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
