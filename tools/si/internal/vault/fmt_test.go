package vault

import "testing"

func TestFormatVaultDotenvCanonicalizesHeaderAndSections(t *testing.T) {
	in := ParseDotenv([]byte("" +
		"#si-vault:v2\n" +
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
		"# si-vault:v2\n" +
		"# si-vault:recipient age1exampleexampleexampleexampleexampleexampleexampleexampleexample\n" +
		"\n" +
		"# [Stripe]\n" +
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

func TestFormatVaultDotenvNoChangeForCanonicalInput(t *testing.T) {
	inRaw := "" +
		"# si-vault:v2\n" +
		"# si-vault:recipient age1exampleexampleexampleexampleexampleexampleexampleexampleexample\n" +
		"\n" +
		"# [stripe]\n" +
		"STRIPE_A=1 # note\n"
	in := ParseDotenv([]byte(inRaw))
	out, changed, err := FormatVaultDotenv(in)
	if err != nil {
		t.Fatalf("FormatVaultDotenv: %v", err)
	}
	if changed {
		t.Fatalf("expected unchanged")
	}
	if got := string(out.Bytes()); got != inRaw {
		t.Fatalf("got %q want %q", got, inRaw)
	}
}

func TestFormatVaultDotenvNormalizesCommentSpacing(t *testing.T) {
	in := ParseDotenv([]byte("" +
		"#si-vault:v2\n" +
		"#si-vault:recipient age1exampleexampleexampleexampleexampleexampleexampleexampleexample\n" +
		"\n" +
		"#comment\n" +
		"A=1#notcomment\n"))
	out, _, err := FormatVaultDotenv(in)
	if err != nil {
		t.Fatalf("FormatVaultDotenv: %v", err)
	}
	want := "" +
		"# si-vault:v2\n" +
		"# si-vault:recipient age1exampleexampleexampleexampleexampleexampleexampleexampleexample\n" +
		"\n" +
		"# comment\n" +
		"A=1#notcomment\n"
	if got := string(out.Bytes()); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestFormatVaultDotenvCollapsesExtraBlankLines(t *testing.T) {
	in := ParseDotenv([]byte("" +
		"#si-vault:v2\n" +
		"#si-vault:recipient age1exampleexampleexampleexampleexampleexampleexampleexampleexample\n" +
		"\n" +
		"\n" +
		"# [stripe]\n" +
		"\n" +
		"STRIPE_A=1\n" +
		"\n" +
		"\n"))
	out, _, err := FormatVaultDotenv(in)
	if err != nil {
		t.Fatalf("FormatVaultDotenv: %v", err)
	}
	want := "" +
		"# si-vault:v2\n" +
		"# si-vault:recipient age1exampleexampleexampleexampleexampleexampleexampleexampleexample\n" +
		"\n" +
		"# [stripe]\n" +
		"\n" +
		"STRIPE_A=1\n"
	if got := string(out.Bytes()); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestFormatVaultDotenvPreservesUnknownPreambleLines(t *testing.T) {
	in := ParseDotenv([]byte("" +
		"# si-vault:v2\n" +
		"# si-vault:recipient age1exampleexampleexampleexampleexampleexampleexampleexampleexample\n" +
		"\n" +
		"SHELL_SYNTAX: not-dotenv\n" +
		"# [stripe]\n" +
		"STRIPE_A=1\n"))
	out, _, err := FormatVaultDotenv(in)
	if err != nil {
		t.Fatalf("FormatVaultDotenv: %v", err)
	}
	want := "" +
		"# si-vault:v2\n" +
		"# si-vault:recipient age1exampleexampleexampleexampleexampleexampleexampleexampleexample\n" +
		"\n" +
		"SHELL_SYNTAX: not-dotenv\n" +
		"\n" +
		"# [stripe]\n" +
		"STRIPE_A=1\n"
	if got := string(out.Bytes()); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestFormatVaultDotenvKeepsLookalikeHeaderComment(t *testing.T) {
	in := ParseDotenv([]byte("" +
		"# si-vault:v2\n" +
		"# si-vault:recipient age1exampleexampleexampleexampleexampleexampleexampleexampleexample\n" +
		"\n" +
		"# si-vault:recipient-count 2\n" +
		"A=1\n"))
	out, _, err := FormatVaultDotenv(in)
	if err != nil {
		t.Fatalf("FormatVaultDotenv: %v", err)
	}
	want := "" +
		"# si-vault:v2\n" +
		"# si-vault:recipient age1exampleexampleexampleexampleexampleexampleexampleexampleexample\n" +
		"\n" +
		"# si-vault:recipient-count 2\n" +
		"A=1\n"
	if got := string(out.Bytes()); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestFormatVaultDotenvPreservesDividerAndSectionMarkerLines(t *testing.T) {
	in := ParseDotenv([]byte("" +
		"#si-vault:v2\n" +
		"#si-vault:recipient age1exampleexampleexampleexampleexampleexampleexampleexampleexample\n" +
		"\n" +
		"#----------------------------\n" +
		"#[Stripe]\n" +
		"A = 1\n"))
	out, _, err := FormatVaultDotenv(in)
	if err != nil {
		t.Fatalf("FormatVaultDotenv: %v", err)
	}
	want := "" +
		"# si-vault:v2\n" +
		"# si-vault:recipient age1exampleexampleexampleexampleexampleexampleexampleexampleexample\n" +
		"\n" +
		"#----------------------------\n" +
		"#[Stripe]\n" +
		"A=1\n"
	if got := string(out.Bytes()); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
