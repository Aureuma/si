package vault

import (
	"testing"

	"filippo.io/age"
)

func TestDecryptDotenvValuesPreservesLayoutAndComments(t *testing.T) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("GenerateX25519Identity: %v", err)
	}
	recipient := id.Recipient().String()

	in := ParseDotenv([]byte("" +
		"# si-vault:v1\n" +
		"# si-vault:recipient " + recipient + "\n" +
		"\n" +
		"export A = plaintext   # keep\n" +
		"B=two words # keep\n" +
		"C=needs#noquote\n"))

	_, err = EncryptDotenvValues(&in, nil, false)
	if err != nil {
		t.Fatalf("EncryptDotenvValues: %v", err)
	}

	out := in
	res, err := DecryptDotenvValues(&out, id)
	if err != nil {
		t.Fatalf("DecryptDotenvValues: %v", err)
	}
	if !res.Changed {
		t.Fatalf("expected changed")
	}

	// Ensure we didn't destroy layout/comments on the assignment lines.
	if got, _ := out.Lookup("A"); got == "" {
		t.Fatalf("expected A present")
	}

	// Values should round-trip through NormalizeDotenvValue and inline comment parsing.
	aRaw, _ := out.Lookup("A")
	a, err := NormalizeDotenvValue(aRaw)
	if err != nil || a != "plaintext" {
		t.Fatalf("A normalize got %q err %v", aRaw, err)
	}
	bRaw, _ := out.Lookup("B")
	b, err := NormalizeDotenvValue(bRaw)
	if err != nil || b != "two words" {
		t.Fatalf("B normalize got %q err %v", bRaw, err)
	}
	cRaw, _ := out.Lookup("C")
	c, err := NormalizeDotenvValue(cRaw)
	if err != nil || c != "needs#noquote" {
		t.Fatalf("C normalize got %q err %v", cRaw, err)
	}
}

func TestRenderDotenvValuePlainQuotesWhenNeeded(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"abc", "abc"},
		{"foo#bar", "foo#bar"},
		{"foo #bar", "\"foo #bar\""},
		{"#leading", "\"#leading\""},
		{" haslead", "\" haslead\""},
		{"trail ", "\"trail \""},
	}
	for _, tc := range cases {
		if got := RenderDotenvValuePlain(tc.in); got != tc.want {
			t.Fatalf("RenderDotenvValuePlain(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}
