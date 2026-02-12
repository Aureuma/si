package vault

import (
	"bytes"
	"encoding/base64"
	"io"
	"strings"
	"testing"

	"filippo.io/age"
)

func TestAgeEncryptDecryptRoundTrip(t *testing.T) {
	id, err := GenerateIdentity()
	if err != nil {
		t.Fatalf("GenerateIdentity: %v", err)
	}
	recipient := id.Recipient().String()

	cipher, err := EncryptStringV1("hello", []string{recipient})
	if err != nil {
		t.Fatalf("EncryptStringV1: %v", err)
	}
	if !IsEncryptedValueV1(cipher) {
		t.Fatalf("expected encrypted prefix, got %q", cipher)
	}
	if !strings.HasPrefix(cipher, EncryptedValuePrefixV2) {
		t.Fatalf("expected v2 prefix, got %q", cipher)
	}
	plain, err := DecryptStringV1(cipher, id)
	if err != nil {
		t.Fatalf("DecryptStringV1: %v", err)
	}
	if plain != "hello" {
		t.Fatalf("got %q want %q", plain, "hello")
	}
}

func TestAgeEncryptV2StripsCommonAgePrefix(t *testing.T) {
	id, err := GenerateIdentity()
	if err != nil {
		t.Fatalf("GenerateIdentity: %v", err)
	}
	recipient := id.Recipient().String()

	cipher, err := EncryptStringV1("hello", []string{recipient})
	if err != nil {
		t.Fatalf("EncryptStringV1: %v", err)
	}
	if !strings.HasPrefix(cipher, EncryptedValuePrefixV2) {
		t.Fatalf("expected v2 prefix, got %q", cipher)
	}
	payload := strings.TrimPrefix(cipher, EncryptedValuePrefixV2)
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("DecodeString: %v", err)
	}
	if bytes.HasPrefix(raw, []byte(ageMagicLine)) {
		t.Fatalf("v2 payload should not include age magic line")
	}
	if bytes.HasPrefix(raw, []byte(ageStanzaX25519Prefix)) {
		t.Fatalf("v2 payload should not include x25519 stanza prefix")
	}
	legacyLen := len(EncryptedValuePrefixV1 + base64.RawURLEncoding.EncodeToString(append([]byte(ageMagicLine+ageStanzaX25519Prefix), raw...)))
	if len(cipher) >= legacyLen {
		t.Fatalf("expected v2 ciphertext shorter than v1-like encoding: v2=%d v1=%d", len(cipher), legacyLen)
	}
}

func TestAgeEncryptV2AvoidsLargeSharedPrefixAcrossCiphertexts(t *testing.T) {
	id, err := GenerateIdentity()
	if err != nil {
		t.Fatalf("GenerateIdentity: %v", err)
	}
	recipient := id.Recipient().String()
	c1, err := EncryptStringV1("same", []string{recipient})
	if err != nil {
		t.Fatalf("EncryptStringV1 #1: %v", err)
	}
	c2, err := EncryptStringV1("same", []string{recipient})
	if err != nil {
		t.Fatalf("EncryptStringV1 #2: %v", err)
	}
	common := 0
	limit := len(c1)
	if len(c2) < limit {
		limit = len(c2)
	}
	for i := 0; i < limit; i++ {
		if c1[i] != c2[i] {
			break
		}
		common++
	}
	if common > len(EncryptedValuePrefixV2)+8 {
		t.Fatalf("unexpected large shared prefix (%d chars): %q vs %q", common, c1, c2)
	}
}

func TestDecryptStringV1SupportsLegacyV1Ciphertexts(t *testing.T) {
	id, err := GenerateIdentity()
	if err != nil {
		t.Fatalf("GenerateIdentity: %v", err)
	}
	recipient := id.Recipient()
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, recipient)
	if err != nil {
		t.Fatalf("age.Encrypt: %v", err)
	}
	if _, err := io.WriteString(w, "legacy-value"); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	legacy := EncryptedValuePrefixV1 + base64.RawURLEncoding.EncodeToString(buf.Bytes())
	if !IsEncryptedValueV1(legacy) {
		t.Fatalf("expected legacy ciphertext to be recognized")
	}
	plain, err := DecryptStringV1(legacy, id)
	if err != nil {
		t.Fatalf("DecryptStringV1: %v", err)
	}
	if plain != "legacy-value" {
		t.Fatalf("plain=%q", plain)
	}
}

func TestDecryptStringV1SupportsLegacyV2Es2Ciphertexts(t *testing.T) {
	id, err := GenerateIdentity()
	if err != nil {
		t.Fatalf("GenerateIdentity: %v", err)
	}
	recipient := id.Recipient().String()
	cipher, err := EncryptStringV1("legacy-v2", []string{recipient})
	if err != nil {
		t.Fatalf("EncryptStringV1: %v", err)
	}
	if !strings.HasPrefix(cipher, EncryptedValuePrefixV2) {
		t.Fatalf("expected v2 prefix, got %q", cipher)
	}
	legacy := strings.Replace(cipher, EncryptedValuePrefixV2, EncryptedValuePrefixV2Legacy, 1)
	if !IsEncryptedValueV1(legacy) {
		t.Fatalf("expected legacy v2 ciphertext to be recognized")
	}
	plain, err := DecryptStringV1(legacy, id)
	if err != nil {
		t.Fatalf("DecryptStringV1: %v", err)
	}
	if plain != "legacy-v2" {
		t.Fatalf("plain=%q", plain)
	}
}

func TestRecipientsFingerprintStableAndOrderIndependent(t *testing.T) {
	a := "age1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	b := "age1bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	fp1 := RecipientsFingerprint([]string{a, b})
	fp2 := RecipientsFingerprint([]string{" " + b + " ", a, a})
	if fp1 != fp2 {
		t.Fatalf("fingerprints differ: %s vs %s", fp1, fp2)
	}
}

func TestParseRecipientsFromDotenvIgnoresLookalikes(t *testing.T) {
	doc := ParseDotenv([]byte("" +
		"# si-vault:recipient age1valid\n" +
		"# si-vault:recipient-count 2\n" +
		"si-vault:recipient age1notcomment\n" +
		"#si-vault:recipient\tage1tab\n"))
	got := ParseRecipientsFromDotenv(doc)
	if len(got) != 2 {
		t.Fatalf("recipients=%v", got)
	}
	if got[0] != "age1valid" || got[1] != "age1tab" {
		t.Fatalf("recipients=%v", got)
	}
}
