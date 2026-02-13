package main

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
	"si/tools/si/internal/vault"
)

func TestVaultE2E_InitSupportsArbitraryDotenvPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	tempState := t.TempDir()
	targetRoot := t.TempDir()
	envFile := filepath.Join(targetRoot, "plain-vault", ".env.app")
	keyFile := filepath.Join(tempState, "vault", "keys", "age.key")
	trustFile := filepath.Join(tempState, "vault", "trust.json")
	auditLog := filepath.Join(tempState, "vault", "audit.log")

	env := map[string]string{
		"HOME":                 tempState,
		"GOFLAGS":              "-modcacherw",
		"GOMODCACHE":           filepath.Join(tempState, "go-mod-cache"),
		"GOCACHE":              filepath.Join(tempState, "go-build-cache"),
		"SI_VAULT_KEY_BACKEND": "file",
		"SI_VAULT_KEY_FILE":    keyFile,
		"SI_VAULT_TRUST_STORE": trustFile,
		"SI_VAULT_AUDIT_LOG":   auditLog,
	}
	stdout, stderr, err := runSICommand(t, env,
		"vault", "init",
		"--file", envFile,
	)
	if err != nil {
		t.Fatalf("vault init failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	raw, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("expected env file to be created: %v", err)
	}
	content := string(raw)
	if !strings.Contains(content, vault.VaultHeaderVersionLine) {
		t.Fatalf("expected vault header, got:\n%s", content)
	}
	if !strings.Contains(content, "# si-vault:recipient age1") {
		t.Fatalf("expected recipient header, got:\n%s", content)
	}
	if !strings.Contains(stdout, filepath.Clean(envFile)) {
		t.Fatalf("expected init output to mention env file path, got:\n%s", stdout)
	}

	stdout, stderr, err = runSICommand(t, env, "vault", "trust", "status", "--file", envFile)
	if err != nil {
		t.Fatalf("vault trust status failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "trust:      ok") {
		t.Fatalf("expected trust status to be ok, got:\n%s", stdout)
	}
}

func TestVaultE2E_EncryptDecryptReencryptFlows(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	tempState := t.TempDir()
	targetRoot := t.TempDir()
	envFile := filepath.Join(targetRoot, ".env.test")
	keyFile := filepath.Join(tempState, "vault", "keys", "age.key")
	trustFile := filepath.Join(tempState, "vault", "trust.json")
	auditLog := filepath.Join(tempState, "vault", "audit.log")

	env := map[string]string{
		"HOME":                 tempState,
		"GOFLAGS":              "-modcacherw",
		"GOMODCACHE":           filepath.Join(tempState, "go-mod-cache"),
		"GOCACHE":              filepath.Join(tempState, "go-build-cache"),
		"SI_VAULT_KEY_BACKEND": "file",
		"SI_VAULT_KEY_FILE":    keyFile,
		"SI_VAULT_TRUST_STORE": trustFile,
		"SI_VAULT_AUDIT_LOG":   auditLog,
	}

	// Bootstrap the vault header + trust entry.
	if _, _, err := runSICommand(t, env, "vault", "init", "--file", envFile); err != nil {
		t.Fatalf("vault init failed: %v", err)
	}

	// Add plaintext keys with tricky values.
	want := map[string]string{
		"PLAIN_SIMPLE": "abc",
		"PLAIN_HASH":   "a # b",
		"PLAIN_LEAD":   "#starts-with-hash",
		"PLAIN_WS":     "  padded\t",
		"PLAIN_EQ":     "a=b",
		"PLAIN_EMPTY":  "",
	}
	doc, err := vault.ReadDotenvFile(envFile)
	if err != nil {
		t.Fatalf("ReadDotenvFile: %v", err)
	}
	doc.Lines = append(doc.Lines,
		vault.RawLine{Text: "", NL: doc.DefaultNL},
		vault.RawLine{Text: "PLAIN_SIMPLE=" + vault.RenderDotenvValuePlain(want["PLAIN_SIMPLE"]), NL: doc.DefaultNL},
		vault.RawLine{Text: "PLAIN_HASH=" + vault.RenderDotenvValuePlain(want["PLAIN_HASH"]) + "  # keep", NL: doc.DefaultNL},
		vault.RawLine{Text: "PLAIN_LEAD=" + vault.RenderDotenvValuePlain(want["PLAIN_LEAD"]), NL: doc.DefaultNL},
		vault.RawLine{Text: "PLAIN_WS=" + vault.RenderDotenvValuePlain(want["PLAIN_WS"]), NL: doc.DefaultNL},
		vault.RawLine{Text: "PLAIN_EQ=" + vault.RenderDotenvValuePlain(want["PLAIN_EQ"]), NL: doc.DefaultNL},
		vault.RawLine{Text: "PLAIN_EMPTY=" + vault.RenderDotenvValuePlain(want["PLAIN_EMPTY"]), NL: doc.DefaultNL},
	)
	if err := vault.WriteDotenvFileAtomic(envFile, doc.Bytes()); err != nil {
		t.Fatalf("WriteDotenvFileAtomic: %v", err)
	}

	// Encrypt in place.
	stdout, stderr, err := runSICommand(t, env, "vault", "encrypt", "--file", envFile)
	if err != nil {
		t.Fatalf("vault encrypt failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	encBytes, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	encContent := string(encBytes)
	if !strings.Contains(encContent, vault.EncryptedValuePrefixV1) && !strings.Contains(encContent, vault.EncryptedValuePrefixV2) {
		t.Fatalf("expected encrypted values in file, got:\n%s", encContent)
	}
	if strings.Contains(encContent, "PLAIN_SIMPLE="+want["PLAIN_SIMPLE"]) {
		t.Fatalf("expected plaintext to be encrypted, got:\n%s", encContent)
	}

	// Decrypt to stdout, without modifying disk.
	plainStdout, plainStderr, err := runSICommand(t, env, "vault", "decrypt", "--file", envFile, "--stdout")
	if err != nil {
		t.Fatalf("vault decrypt --stdout failed: %v\nstdout=%s\nstderr=%s", err, plainStdout, plainStderr)
	}
	outDoc := vault.ParseDotenv([]byte(plainStdout))
	gotRes, err := vault.DecryptEnv(outDoc, nil)
	if err != nil {
		t.Fatalf("DecryptEnv on stdout: %v", err)
	}
	for k, v := range want {
		if gotRes.Values[k] != v {
			t.Fatalf("expected %s=%q, got %q", k, v, gotRes.Values[k])
		}
	}

	// Re-encrypt already-encrypted values (should change ciphertext).
	stdout2, stderr2, err := runSICommand(t, env, "vault", "encrypt", "--file", envFile, "--reencrypt")
	if err != nil {
		t.Fatalf("vault encrypt --reencrypt failed: %v\nstdout=%s\nstderr=%s", err, stdout2, stderr2)
	}
	encBytes2, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read env file after reencrypt: %v", err)
	}
	if bytes.Equal(encBytes, encBytes2) {
		t.Fatalf("expected reencrypt to change ciphertext (file bytes identical)")
	}

	// In-place decrypt to disk (guarded by --yes).
	stdout3, stderr3, err := runSICommand(t, env, "vault", "decrypt", "--file", envFile, "--yes")
	if err != nil {
		t.Fatalf("vault decrypt --yes failed: %v\nstdout=%s\nstderr=%s", err, stdout3, stderr3)
	}
	diskBytes, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read env file after in-place decrypt: %v", err)
	}
	disk := string(diskBytes)
	if strings.Contains(disk, vault.EncryptedValuePrefixV1) || strings.Contains(disk, vault.EncryptedValuePrefixV2) || strings.Contains(disk, vault.EncryptedValuePrefixV2Legacy) {
		t.Fatalf("expected no encrypted values on disk after decrypt, got:\n%s", disk)
	}
}

func TestVaultE2E_DecryptBackCompatCiphertextEncodings(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	tempState := t.TempDir()
	targetRoot := t.TempDir()
	envFile := filepath.Join(targetRoot, ".env.compat")
	keyFile := filepath.Join(tempState, "vault", "keys", "age.key")
	trustFile := filepath.Join(tempState, "vault", "trust.json")

	env := map[string]string{
		"HOME":                 tempState,
		"GOFLAGS":              "-modcacherw",
		"GOMODCACHE":           filepath.Join(tempState, "go-mod-cache"),
		"GOCACHE":              filepath.Join(tempState, "go-build-cache"),
		"SI_VAULT_KEY_BACKEND": "file",
		"SI_VAULT_KEY_FILE":    keyFile,
		"SI_VAULT_TRUST_STORE": trustFile,
	}

	// Create header + key + trust.
	if _, _, err := runSICommand(t, env, "vault", "init", "--file", envFile); err != nil {
		t.Fatalf("vault init failed: %v", err)
	}

	// Load identity and craft "old style" ciphertext (StdEncoding + padding).
	info, err := vault.LoadIdentity(vault.KeyConfig{Backend: "file", KeyFile: keyFile})
	if err != nil {
		t.Fatalf("LoadIdentity: %v", err)
	}
	recipient := info.Identity.Recipient()

	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, recipient)
	if err != nil {
		t.Fatalf("age.Encrypt: %v", err)
	}
	if _, err := w.Write([]byte("legacy")); err != nil {
		_ = w.Close()
		t.Fatalf("write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	raw := buf.Bytes()
	cipher := vault.EncryptedValuePrefixV1 + base64.StdEncoding.EncodeToString(raw)

	doc, err := vault.ReadDotenvFile(envFile)
	if err != nil {
		t.Fatalf("ReadDotenvFile: %v", err)
	}
	doc.Lines = append(doc.Lines,
		vault.RawLine{Text: "", NL: doc.DefaultNL},
		vault.RawLine{Text: "OLD_CIPHER=" + cipher, NL: doc.DefaultNL},
	)
	if err := vault.WriteDotenvFileAtomic(envFile, doc.Bytes()); err != nil {
		t.Fatalf("WriteDotenvFileAtomic: %v", err)
	}

	// Decrypt should succeed (even though the ciphertext isn't the current encoding).
	stdout, stderr, err := runSICommand(t, env, "vault", "decrypt", "--file", envFile, "--stdout")
	if err != nil {
		t.Fatalf("vault decrypt --stdout failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	outDoc := vault.ParseDotenv([]byte(stdout))
	gotRes, err := vault.DecryptEnv(outDoc, nil)
	if err != nil {
		t.Fatalf("DecryptEnv: %v", err)
	}
	if gotRes.Values["OLD_CIPHER"] != "legacy" {
		t.Fatalf("expected decrypted plaintext %q, got %q", "legacy", gotRes.Values["OLD_CIPHER"])
	}
}
