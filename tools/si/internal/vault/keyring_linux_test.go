//go:build linux

package vault

import "testing"

func TestValidateSecretToolAttr(t *testing.T) {
	ok := []string{"si-vault", "age-identity", "svc.account-1"}
	for _, v := range ok {
		if _, err := validateSecretToolAttr("service", v); err != nil {
			t.Fatalf("%q: %v", v, err)
		}
	}

	bad := []string{"", "bad value", "bad\nvalue"}
	for _, v := range bad {
		if _, err := validateSecretToolAttr("service", v); err == nil {
			t.Fatalf("expected error for %q", v)
		}
	}
}
