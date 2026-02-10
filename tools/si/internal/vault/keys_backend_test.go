package vault

import "testing"

func TestNormalizeKeyBackend(t *testing.T) {
	if got := NormalizeKeyBackend("keychain"); got != "keyring" {
		t.Fatalf("got %q want %q", got, "keyring")
	}
	if got := NormalizeKeyBackend(" KEYCHAIN "); got != "keyring" {
		t.Fatalf("got %q want %q", got, "keyring")
	}
	if got := NormalizeKeyBackend("keyring"); got != "keyring" {
		t.Fatalf("got %q want %q", got, "keyring")
	}
	if got := NormalizeKeyBackend("file"); got != "file" {
		t.Fatalf("got %q want %q", got, "file")
	}
}
