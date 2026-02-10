package vault

import "testing"

func TestEnsureVaultHeaderPrependsWhenMissing(t *testing.T) {
	doc := ParseDotenv([]byte("A=1\n"))
	changed, err := EnsureVaultHeader(&doc, []string{"age1abc"})
	if err != nil {
		t.Fatalf("EnsureVaultHeader: %v", err)
	}
	if !changed {
		t.Fatalf("expected change")
	}
	want := "" +
		"# si-vault:v1\n" +
		"# si-vault:recipient age1abc\n" +
		"\n" +
		"A=1\n"
	if got := string(doc.Bytes()); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestEnsureVaultHeaderAddsMissingRecipientOnly(t *testing.T) {
	doc := ParseDotenv([]byte("" +
		"# si-vault:v1\n" +
		"# si-vault:recipient age1old\n" +
		"\n" +
		"A=1\n"))
	changed, err := EnsureVaultHeader(&doc, []string{"age1old", "age1new"})
	if err != nil {
		t.Fatalf("EnsureVaultHeader: %v", err)
	}
	if !changed {
		t.Fatalf("expected change")
	}
	want := "" +
		"# si-vault:v1\n" +
		"# si-vault:recipient age1old\n" +
		"# si-vault:recipient age1new\n" +
		"\n" +
		"A=1\n"
	if got := string(doc.Bytes()); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestEnsureVaultHeaderAddsVersionWhenOnlyRecipientsPresent(t *testing.T) {
	doc := ParseDotenv([]byte("" +
		"# si-vault:recipient age1old\n" +
		"\n" +
		"A=1\n"))
	changed, err := EnsureVaultHeader(&doc, []string{"age1old"})
	if err != nil {
		t.Fatalf("EnsureVaultHeader: %v", err)
	}
	if !changed {
		t.Fatalf("expected change")
	}
	want := "" +
		"# si-vault:v1\n" +
		"# si-vault:recipient age1old\n" +
		"\n" +
		"A=1\n"
	if got := string(doc.Bytes()); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestEnsureVaultHeaderEnsuresBlankLineAfterHeader(t *testing.T) {
	doc := ParseDotenv([]byte("" +
		"# si-vault:v1\n" +
		"# si-vault:recipient age1old\n" +
		"A=1\n"))
	changed, err := EnsureVaultHeader(&doc, []string{"age1old"})
	if err != nil {
		t.Fatalf("EnsureVaultHeader: %v", err)
	}
	if !changed {
		t.Fatalf("expected change")
	}
	want := "" +
		"# si-vault:v1\n" +
		"# si-vault:recipient age1old\n" +
		"\n" +
		"A=1\n"
	if got := string(doc.Bytes()); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestRemoveRecipientOnlyRemovesTarget(t *testing.T) {
	doc := ParseDotenv([]byte("" +
		"# si-vault:v1\n" +
		"# si-vault:recipient age1a\n" +
		"# si-vault:recipient age1b\n" +
		"\n" +
		"A=1\n"))
	changed := RemoveRecipient(&doc, "age1a")
	if !changed {
		t.Fatalf("expected change")
	}
	want := "" +
		"# si-vault:v1\n" +
		"# si-vault:recipient age1b\n" +
		"\n" +
		"A=1\n"
	if got := string(doc.Bytes()); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
