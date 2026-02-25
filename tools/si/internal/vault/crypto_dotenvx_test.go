package vault

import (
	"encoding/base64"
	"os"
	"testing"

	ecies "github.com/ecies/go/v2"
)

func TestSIVaultEmptyPlaintextRoundTrip(t *testing.T) {
	publicKey, privateKey, err := GenerateSIVaultKeyPair()
	if err != nil {
		t.Fatalf("GenerateSIVaultKeyPair: %v", err)
	}
	cipher, err := EncryptSIVaultValue("", publicKey)
	if err != nil {
		t.Fatalf("EncryptSIVaultValue: %v", err)
	}
	if cipher != SIVaultEncryptedPrefix {
		t.Fatalf("expected canonical empty ciphertext marker, got %q", cipher)
	}
	plain, err := DecryptSIVaultValue(cipher, []string{privateKey})
	if err != nil {
		t.Fatalf("DecryptSIVaultValue: %v", err)
	}
	if plain != "" {
		t.Fatalf("expected empty plaintext, got %q", plain)
	}
}

func TestDecryptSIVaultValueAcceptsEmptyLegacyPlaceholders(t *testing.T) {
	cases := []string{
		dotenvxEncryptedPrefix,
		SIVaultEncryptedPrefix,
		"  " + SIVaultEncryptedPrefix + "  ",
	}
	for _, tc := range cases {
		got, err := DecryptSIVaultValue(tc, []string{"deadbeef"})
		if err != nil {
			t.Fatalf("DecryptSIVaultValue(%q): %v", tc, err)
		}
		if got != "" {
			t.Fatalf("DecryptSIVaultValue(%q)=%q want empty", tc, got)
		}
	}
}

func TestDecryptSIVaultValueRejectsMalformedPayload(t *testing.T) {
	_, err := DecryptSIVaultValue(SIVaultEncryptedPrefix+"@@@@", []string{"deadbeef"})
	if err == nil {
		t.Fatalf("expected malformed payload error")
	}
}

func TestDecryptSIVaultValueAcceptsLegacyEciesEmptyCiphertext(t *testing.T) {
	privateKey, err := ecies.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	cipher, err := ecies.Encrypt(privateKey.PublicKey, []byte(""))
	if err != nil {
		t.Fatalf("Encrypt empty: %v", err)
	}
	encoded := base64.StdEncoding.EncodeToString(cipher)
	plain, err := DecryptSIVaultValue(SIVaultEncryptedPrefix+encoded, []string{privateKey.Hex()})
	if err != nil {
		t.Fatalf("DecryptSIVaultValue(legacy empty): %v", err)
	}
	if plain != "" {
		t.Fatalf("expected empty plaintext, got %q", plain)
	}
}

func TestDecryptSIVaultValueDecryptsLegacyAgeCiphertextViaEnvIdentity(t *testing.T) {
	identity, err := GenerateIdentity()
	if err != nil {
		t.Fatalf("GenerateIdentity: %v", err)
	}
	cipher, err := EncryptStringV1("legacy-secret", []string{identity.Recipient().String()})
	if err != nil {
		t.Fatalf("EncryptStringV1: %v", err)
	}
	t.Setenv("SI_VAULT_IDENTITY", identity.String())
	plain, err := DecryptSIVaultValue(cipher, nil)
	if err != nil {
		t.Fatalf("DecryptSIVaultValue(legacy age): %v", err)
	}
	if plain != "legacy-secret" {
		t.Fatalf("plain=%q want legacy-secret", plain)
	}
}

func TestDecryptSIVaultValueLegacyAgeWithoutIdentityFails(t *testing.T) {
	identity, err := GenerateIdentity()
	if err != nil {
		t.Fatalf("GenerateIdentity: %v", err)
	}
	cipher, err := EncryptStringV1("legacy-secret", []string{identity.Recipient().String()})
	if err != nil {
		t.Fatalf("EncryptStringV1: %v", err)
	}
	_ = os.Unsetenv("SI_VAULT_IDENTITY")
	if _, err := DecryptSIVaultValue(cipher, nil); err == nil {
		t.Fatalf("expected missing identity error")
	}
}
