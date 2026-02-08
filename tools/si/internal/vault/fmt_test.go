package vault

import "testing"

func TestFormatVaultDotenvCanonicalizesHeaderAndSections(t *testing.T) {
	in := ParseDotenv([]byte("" +
		"#si-vault:v1\n" +
		"#si-vault:recipient age1exampleexampleexampleexampleexampleexampleexampleexampleexample\n" +
		"\n" +
		"# [Stripe]\n" +
		"STRIPE_A = 1   #note\n"))
	out, changed, err := FormatVaultDotenv(in)
	if err != nil {
		t.Fatalf("FormatVaultDotenv: %v", err)
	}
	if !changed {
		t.Fatalf("expected change")
	}
	want := "" +
		"# si-vault:v1\n" +
		"# si-vault:recipient age1exampleexampleexampleexampleexampleexampleexampleexampleexample\n" +
		"\n" +
		"# ------------------------------------------------------------------------------\n" +
		"# [stripe]\n" +
		"STRIPE_A=1 # note\n"
	if got := string(out.Bytes()); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestFormatVaultDotenvErrorsWithoutRecipients(t *testing.T) {
	in := ParseDotenv([]byte("A=1\n"))
	_, _, err := FormatVaultDotenv(in)
	if err == nil {
		t.Fatalf("expected error")
	}
}
