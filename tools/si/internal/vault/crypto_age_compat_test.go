package vault

import (
	"bytes"
	"encoding/base64"
	"testing"

	"filippo.io/age"
)

func mustEncryptAgeRaw(t *testing.T, plaintext string, recipients ...age.Recipient) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, recipients...)
	if err != nil {
		t.Fatalf("age.Encrypt: %v", err)
	}
	if _, err := w.Write([]byte(plaintext)); err != nil {
		_ = w.Close()
		t.Fatalf("write plaintext: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return buf.Bytes()
}

func TestDecryptStringV1_BackCompatBase64Encodings(t *testing.T) {
	id, err := GenerateIdentity()
	if err != nil {
		t.Fatalf("GenerateIdentity: %v", err)
	}
	recipient := id.Recipient()
	raw := mustEncryptAgeRaw(t, "hello", recipient)

	compactPrefix := []byte(ageMagicLine + ageStanzaX25519Prefix)
	if !bytes.HasPrefix(raw, compactPrefix) {
		t.Fatalf("expected age ciphertext to start with compact prefix")
	}
	compactPayload := raw[len(compactPrefix):]

	cases := []struct {
		name   string
		cipher string
	}{
		{"v1_rawurl", EncryptedValuePrefixV1 + base64.RawURLEncoding.EncodeToString(raw)},
		{"v1_url", EncryptedValuePrefixV1 + base64.URLEncoding.EncodeToString(raw)},
		{"v1_rawstd", EncryptedValuePrefixV1 + base64.RawStdEncoding.EncodeToString(raw)},
		{"v1_std", EncryptedValuePrefixV1 + base64.StdEncoding.EncodeToString(raw)},

		{"v2_rawurl", EncryptedValuePrefixV2 + base64.RawURLEncoding.EncodeToString(compactPayload)},
		{"v2_url", EncryptedValuePrefixV2 + base64.URLEncoding.EncodeToString(compactPayload)},
		{"v2_rawstd", EncryptedValuePrefixV2 + base64.RawStdEncoding.EncodeToString(compactPayload)},
		{"v2_std", EncryptedValuePrefixV2 + base64.StdEncoding.EncodeToString(compactPayload)},

		{"es2_rawurl", EncryptedValuePrefixV2Legacy + base64.RawURLEncoding.EncodeToString(compactPayload)},
		{"es2_url", EncryptedValuePrefixV2Legacy + base64.URLEncoding.EncodeToString(compactPayload)},
		{"es2_rawstd", EncryptedValuePrefixV2Legacy + base64.RawStdEncoding.EncodeToString(compactPayload)},
		{"es2_std", EncryptedValuePrefixV2Legacy + base64.StdEncoding.EncodeToString(compactPayload)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateEncryptedValueV1(tc.cipher); err != nil {
				t.Fatalf("ValidateEncryptedValueV1: %v", err)
			}
			got, err := DecryptStringV1(tc.cipher, id)
			if err != nil {
				t.Fatalf("DecryptStringV1: %v", err)
			}
			if got != "hello" {
				t.Fatalf("expected plaintext %q, got %q", "hello", got)
			}
		})
	}
}

