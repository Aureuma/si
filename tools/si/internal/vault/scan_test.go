package vault

import (
	"testing"
)

func TestScanDotenvEncryptionErrorsOnInvalidCiphertext(t *testing.T) {
	doc := ParseDotenv([]byte(`# si-vault:v1
# si-vault:recipient age1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq

BAD=encrypted:si:v1:not_base64!!!
`))
	if _, err := ScanDotenvEncryption(doc); err == nil {
		t.Fatalf("expected invalid ciphertext error")
	}
}

func TestValidateEncryptedValueV1RejectsNonAgePayload(t *testing.T) {
	// Base64 for "hello" is a valid payload encoding but not an age stream.
	if err := ValidateEncryptedValueV1("encrypted:si:v1:aGVsbG8"); err == nil {
		t.Fatalf("expected non-age payload to be rejected")
	}
}

func TestScanDotenvEncryptionClassifiesValues(t *testing.T) {
	id, err := GenerateIdentity()
	if err != nil {
		t.Fatalf("GenerateIdentity: %v", err)
	}
	recipient := id.Recipient().String()
	cipher, err := EncryptStringV1("secret", []string{recipient})
	if err != nil {
		t.Fatalf("EncryptStringV1: %v", err)
	}
	doc := ParseDotenv([]byte("" +
		"EMPTY=\n" +
		"PLAIN=abc\n" +
		"ENC=" + cipher + "\n"))
	scan, err := ScanDotenvEncryption(doc)
	if err != nil {
		t.Fatalf("ScanDotenvEncryption: %v", err)
	}
	if len(scan.EmptyKeys) != 1 || scan.EmptyKeys[0] != "EMPTY" {
		t.Fatalf("empty=%v", scan.EmptyKeys)
	}
	if len(scan.PlaintextKeys) != 1 || scan.PlaintextKeys[0] != "PLAIN" {
		t.Fatalf("plain=%v", scan.PlaintextKeys)
	}
	if len(scan.EncryptedKeys) != 1 || scan.EncryptedKeys[0] != "ENC" {
		t.Fatalf("enc=%v", scan.EncryptedKeys)
	}
}
