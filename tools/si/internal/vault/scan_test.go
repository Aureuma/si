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
