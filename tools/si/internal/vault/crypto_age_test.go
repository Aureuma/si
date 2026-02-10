package vault

import "testing"

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
	plain, err := DecryptStringV1(cipher, id)
	if err != nil {
		t.Fatalf("DecryptStringV1: %v", err)
	}
	if plain != "hello" {
		t.Fatalf("got %q want %q", plain, "hello")
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
