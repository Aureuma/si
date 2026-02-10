package vault

import "testing"

func TestValidateKeyNameAcceptsCommonEnvKeyShapes(t *testing.T) {
	ok := []string{
		"STRIPE_API_KEY",
		"github.token",
		"app-key",
		"svc:key",
		"A1_B2.C3-D4",
	}
	for _, key := range ok {
		if err := ValidateKeyName(key); err != nil {
			t.Fatalf("%q: %v", key, err)
		}
	}
}

func TestValidateKeyNameRejectsUnsafeCharacters(t *testing.T) {
	cases := []string{
		"",
		"BAD KEY",
		"BAD=KEY",
		"BAD\nKEY",
		"BAD\rKEY",
		"\x1fCTRL",
	}
	for _, key := range cases {
		if err := ValidateKeyName(key); err == nil {
			t.Fatalf("expected error for %q", key)
		}
	}
}

func TestValidateKeyNameRejectsTooLong(t *testing.T) {
	key := make([]byte, 513)
	for i := range key {
		key[i] = 'A'
	}
	if err := ValidateKeyName(string(key)); err == nil {
		t.Fatalf("expected error for oversized key")
	}
}
