package main

import (
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

func TestEnsureSIVaultPublicKeyHeaderOnlyHeaderLine(t *testing.T) {
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
	if strings.HasPrefix(lines[1], "#") {
		t.Fatalf("unexpected preamble/comment after header: %q", lines[1])
	}
}
