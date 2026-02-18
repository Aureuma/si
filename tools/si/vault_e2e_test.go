package main

import (
	"bytes"
	"encoding/base64"
	"os"
	"os/exec"
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
	plainStdout, plainStderr, err := runSICommand(t, env, "vault", "decrypt", "--file", envFile)
	if err != nil {
		t.Fatalf("vault decrypt (default stdout) failed: %v\nstdout=%s\nstderr=%s", err, plainStdout, plainStderr)
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
	stdout3, stderr3, err := runSICommand(t, env, "vault", "decrypt", "--file", envFile, "--in-place", "--yes")
	if err != nil {
		t.Fatalf("vault decrypt --in-place --yes failed: %v\nstdout=%s\nstderr=%s", err, stdout3, stderr3)
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
	stdout, stderr, err := runSICommand(t, env, "vault", "decrypt", "--file", envFile)
	if err != nil {
		t.Fatalf("vault decrypt (default stdout) failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
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

func TestVaultE2E_DecryptSelectiveKeys(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	tempState := t.TempDir()
	targetRoot := t.TempDir()
	envFile := filepath.Join(targetRoot, ".env.selective")
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

	if _, _, err := runSICommand(t, env, "vault", "init", "--file", envFile); err != nil {
		t.Fatalf("vault init failed: %v", err)
	}
	if _, _, err := runSICommand(t, env, "vault", "set", "KEY1", "v1", "--file", envFile); err != nil {
		t.Fatalf("vault set KEY1 failed: %v", err)
	}
	if _, _, err := runSICommand(t, env, "vault", "set", "KEY2", "v2", "--file", envFile); err != nil {
		t.Fatalf("vault set KEY2 failed: %v", err)
	}

	// Sanity: both keys on disk should be encrypted.
	onDiskBytes, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	onDisk := string(onDiskBytes)
	if !strings.Contains(onDisk, "KEY1="+vault.EncryptedValuePrefixV1) && !strings.Contains(onDisk, "KEY1="+vault.EncryptedValuePrefixV2) {
		t.Fatalf("expected KEY1 to be encrypted, got:\n%s", onDisk)
	}
	if !strings.Contains(onDisk, "KEY2="+vault.EncryptedValuePrefixV1) && !strings.Contains(onDisk, "KEY2="+vault.EncryptedValuePrefixV2) {
		t.Fatalf("expected KEY2 to be encrypted, got:\n%s", onDisk)
	}

	// Decrypt only KEY1 to stdout; KEY2 should remain encrypted in output, and disk should remain encrypted.
	stdout, _, err := runSICommand(t, env, "vault", "decrypt", "--file", envFile, "KEY1")
	if err != nil {
		t.Fatalf("vault decrypt (default stdout) KEY1 failed: %v\nstdout=%s", err, stdout)
	}
	outDoc := vault.ParseDotenv([]byte(stdout))
	v1Raw, ok := outDoc.Lookup("KEY1")
	if !ok {
		t.Fatalf("expected KEY1 in stdout")
	}
	v1, err := vault.NormalizeDotenvValue(v1Raw)
	if err != nil {
		t.Fatalf("NormalizeDotenvValue(KEY1): %v", err)
	}
	if v1 != "v1" {
		t.Fatalf("expected KEY1 plaintext %q, got %q", "v1", v1)
	}
	v2Raw, ok := outDoc.Lookup("KEY2")
	if !ok {
		t.Fatalf("expected KEY2 in stdout")
	}
	v2, err := vault.NormalizeDotenvValue(v2Raw)
	if err != nil {
		t.Fatalf("NormalizeDotenvValue(KEY2): %v", err)
	}
	if !vault.IsEncryptedValueV1(v2) {
		t.Fatalf("expected KEY2 to remain encrypted in stdout, got %q", v2)
	}

	onDiskBytes2, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read env file after stdout decrypt: %v", err)
	}
	onDisk2 := string(onDiskBytes2)
	if strings.Contains(onDisk2, "KEY1=v1") || strings.Contains(onDisk2, "KEY2=v2") {
		t.Fatalf("expected disk file to remain encrypted after --stdout decrypt, got:\n%s", onDisk2)
	}

	// In-place decrypt only KEY2.
	if _, _, err := runSICommand(t, env, "vault", "decrypt", "--file", envFile, "--in-place", "--yes", "KEY2"); err != nil {
		t.Fatalf("vault decrypt --in-place --yes KEY2 failed: %v", err)
	}
	onDiskBytes3, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read env file after in-place decrypt: %v", err)
	}
	onDisk3 := string(onDiskBytes3)
	doc3 := vault.ParseDotenv(onDiskBytes3)
	k2Raw, ok := doc3.Lookup("KEY2")
	if !ok {
		t.Fatalf("expected KEY2 on disk")
	}
	k2, err := vault.NormalizeDotenvValue(k2Raw)
	if err != nil {
		t.Fatalf("NormalizeDotenvValue(KEY2): %v", err)
	}
	if k2 != "v2" {
		t.Fatalf("expected KEY2 plaintext %q, got %q\nfile:\n%s", "v2", k2, onDisk3)
	}
	k1Raw, ok := doc3.Lookup("KEY1")
	if !ok {
		t.Fatalf("expected KEY1 on disk")
	}
	k1, err := vault.NormalizeDotenvValue(k1Raw)
	if err != nil {
		t.Fatalf("NormalizeDotenvValue(KEY1): %v", err)
	}
	if !vault.IsEncryptedValueV1(k1) {
		t.Fatalf("expected KEY1 to remain encrypted on disk, got %q\nfile:\n%s", k1, onDisk3)
	}
}

func TestVaultE2E_DecryptDefaultStdoutInPlaceRequiresFlag(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	tempState := t.TempDir()
	targetRoot := t.TempDir()
	envFile := filepath.Join(targetRoot, ".env.default-stdout")
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

	if _, _, err := runSICommand(t, env, "vault", "init", "--file", envFile); err != nil {
		t.Fatalf("vault init failed: %v", err)
	}
	if _, _, err := runSICommand(t, env, "vault", "set", "K", "v", "--file", envFile); err != nil {
		t.Fatalf("vault set failed: %v", err)
	}

	encBefore, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}

	// --yes alone should NOT decrypt on disk anymore (stdout is default).
	stdout, stderr, err := runSICommand(t, env, "vault", "decrypt", "--file", envFile, "--yes", "K")
	if err != nil {
		t.Fatalf("vault decrypt (default stdout) failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	outDoc := vault.ParseDotenv([]byte(stdout))
	kRaw, ok := outDoc.Lookup("K")
	if !ok {
		t.Fatalf("expected K in stdout")
	}
	k, err := vault.NormalizeDotenvValue(kRaw)
	if err != nil {
		t.Fatalf("NormalizeDotenvValue(K): %v", err)
	}
	if k != "v" {
		t.Fatalf("expected K plaintext %q, got %q", "v", k)
	}

	encAfter, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read env file after stdout decrypt: %v", err)
	}
	if !bytes.Equal(encBefore, encAfter) {
		t.Fatalf("expected disk file to be unchanged in default stdout mode")
	}

	// In-place without --yes should fail in this non-interactive test harness.
	_, stderr2, err := runSICommand(t, env, "vault", "decrypt", "--file", envFile, "--in-place", "K")
	if err == nil {
		t.Fatalf("expected vault decrypt --in-place without --yes to fail in non-interactive mode")
	}
	if !strings.Contains(stderr2, "non-interactive") {
		t.Fatalf("expected non-interactive error, got stderr=%q", stderr2)
	}

	// In-place with --yes should succeed and mutate disk.
	if _, _, err := runSICommand(t, env, "vault", "decrypt", "--file", envFile, "--in-place", "--yes", "K"); err != nil {
		t.Fatalf("vault decrypt --in-place --yes failed: %v", err)
	}
	plainBytes, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read env file after in-place decrypt: %v", err)
	}
	plainDoc := vault.ParseDotenv(plainBytes)
	pRaw, ok := plainDoc.Lookup("K")
	if !ok {
		t.Fatalf("expected K on disk")
	}
	p, err := vault.NormalizeDotenvValue(pRaw)
	if err != nil {
		t.Fatalf("NormalizeDotenvValue(K) on disk: %v", err)
	}
	if p != "v" {
		t.Fatalf("expected K plaintext on disk %q, got %q", "v", p)
	}
}

func TestVaultE2E_InitDoesNotSwitchDefaultWithoutSetDefault(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	tempState := t.TempDir()
	targetRoot := t.TempDir()
	envProd := filepath.Join(targetRoot, ".env.prod")
	envDev := filepath.Join(targetRoot, ".env.dev")
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

	if _, _, err := runSICommand(t, env, "vault", "init", "--file", envProd); err != nil {
		t.Fatalf("vault init prod failed: %v", err)
	}
	if _, _, err := runSICommand(t, env, "vault", "init", "--file", envDev); err != nil {
		t.Fatalf("vault init dev failed: %v", err)
	}
	if _, _, err := runSICommand(t, env, "vault", "set", "AWS_ACCESS_KEY_ID", "dev-access-key", "--file", envDev); err != nil {
		t.Fatalf("vault set dev key failed: %v", err)
	}

	// Default should still point to the first initialized file unless --set-default/use is used.
	stdout, stderr, err := runSICommand(t, env, "vault", "list")
	if err != nil {
		t.Fatalf("vault list failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, filepath.Clean(envProd)) {
		t.Fatalf("expected default env file to remain prod, got:\n%s", stdout)
	}
	if strings.Contains(stdout, "AWS_ACCESS_KEY_ID") {
		t.Fatalf("did not expect dev key in default prod vault list output:\n%s", stdout)
	}
}

func TestVaultE2E_VaultUseSwitchesDefaultFile(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	tempState := t.TempDir()
	targetRoot := t.TempDir()
	envProd := filepath.Join(targetRoot, ".env.prod")
	envDev := filepath.Join(targetRoot, ".env.dev")
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

	if _, _, err := runSICommand(t, env, "vault", "init", "--file", envProd); err != nil {
		t.Fatalf("vault init prod failed: %v", err)
	}
	if _, _, err := runSICommand(t, env, "vault", "init", "--file", envDev); err != nil {
		t.Fatalf("vault init dev failed: %v", err)
	}
	if _, _, err := runSICommand(t, env, "vault", "set", "AWS_ACCESS_KEY_ID", "dev-access-key", "--file", envDev); err != nil {
		t.Fatalf("vault set dev key failed: %v", err)
	}

	if _, _, err := runSICommand(t, env, "vault", "use", "--file", envDev); err != nil {
		t.Fatalf("vault use failed: %v", err)
	}

	stdout, stderr, err := runSICommand(t, env, "vault", "list")
	if err != nil {
		t.Fatalf("vault list failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, filepath.Clean(envDev)) {
		t.Fatalf("expected default env file to switch to dev, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "AWS_ACCESS_KEY_ID") {
		t.Fatalf("expected dev key in default vault list output, got:\n%s", stdout)
	}
}

func TestVaultE2E_StrictTargetScopeBlocksCrossRepoImplicitDefault(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available for strict cross-repo scope test")
	}

	tempState := t.TempDir()
	otherRepo := t.TempDir()
	envFile := filepath.Join(otherRepo, ".env.dev")
	keyFile := filepath.Join(tempState, "vault", "keys", "age.key")
	trustFile := filepath.Join(tempState, "vault", "trust.json")

	if out, err := exec.Command("git", "-C", otherRepo, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init otherRepo failed: %v: %s", err, string(out))
	}

	env := map[string]string{
		"HOME":                         tempState,
		"GOFLAGS":                      "-modcacherw",
		"GOMODCACHE":                   filepath.Join(tempState, "go-mod-cache"),
		"GOCACHE":                      filepath.Join(tempState, "go-build-cache"),
		"SI_VAULT_KEY_BACKEND":         "file",
		"SI_VAULT_KEY_FILE":            keyFile,
		"SI_VAULT_TRUST_STORE":         trustFile,
		"SI_VAULT_STRICT_TARGET_SCOPE": "1",
	}

	if _, _, err := runSICommand(t, env, "vault", "init", "--file", envFile, "--set-default"); err != nil {
		t.Fatalf("vault init failed: %v", err)
	}
	_, stderr, err := runSICommand(t, env, "vault", "list")
	if err == nil {
		t.Fatalf("expected strict target scope to fail on implicit cross-repo default")
	}
	if !strings.Contains(stderr, "SI_VAULT_ALLOW_CROSS_REPO=1") {
		t.Fatalf("expected strict target scope guidance in stderr, got: %q", stderr)
	}
}
