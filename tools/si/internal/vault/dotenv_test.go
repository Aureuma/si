package vault

import "testing"

func TestDotenvSetAppendsWithoutExtraBlankLine(t *testing.T) {
	in := []byte("A=1\nB=2\n")
	f := ParseDotenv(in)
	changed, err := f.Set("C", "3", SetOptions{})
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if !changed {
		t.Fatalf("expected change")
	}
	want := "A=1\nB=2\nC=3\n"
	if got := string(f.Bytes()); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestDotenvPreservesCRLFWhenAppending(t *testing.T) {
	in := []byte("A=1\r\nB=2\r\n")
	f := ParseDotenv(in)
	if f.DefaultNL != "\r\n" {
		t.Fatalf("DefaultNL=%q", f.DefaultNL)
	}
	_, err := f.Set("C", "3", SetOptions{})
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	want := "A=1\r\nB=2\r\nC=3\r\n"
	if got := string(f.Bytes()); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestDotenvSetPreservesInlineCommentSpacing(t *testing.T) {
	in := []byte("A=1 # note\n")
	f := ParseDotenv(in)
	_, err := f.Set("A", "2", SetOptions{})
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	want := "A=2 # note\n"
	if got := string(f.Bytes()); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestDotenvSetUpdatesLastDuplicateKeyOnly(t *testing.T) {
	in := []byte("A=1\nA=2\nB=3\n")
	f := ParseDotenv(in)
	_, err := f.Set("A", "9", SetOptions{})
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	want := "A=1\nA=9\nB=3\n"
	if got := string(f.Bytes()); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestDotenvUnsetRemovesAllOccurrences(t *testing.T) {
	in := []byte("A=1\nA=2\nB=3\n")
	f := ParseDotenv(in)
	changed, err := f.Unset("A")
	if err != nil {
		t.Fatalf("Unset: %v", err)
	}
	if !changed {
		t.Fatalf("expected change")
	}
	want := "B=3\n"
	if got := string(f.Bytes()); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestDotenvSetInSectionInsertsBeforeTrailingBlankLines(t *testing.T) {
	in := []byte("" +
		"# ------------------------------------------------------------------------------\n" +
		"# [stripe]\n" +
		"STRIPE_A=1\n" +
		"\n" +
		"\n" +
		"# ------------------------------------------------------------------------------\n" +
		"# [workos]\n" +
		"WORKOS_A=1\n")
	f := ParseDotenv(in)
	_, err := f.Set("STRIPE_B", "2", SetOptions{Section: "stripe"})
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	want := "" +
		"# ------------------------------------------------------------------------------\n" +
		"# [stripe]\n" +
		"STRIPE_A=1\n" +
		"STRIPE_B=2\n" +
		"\n" +
		"\n" +
		"# ------------------------------------------------------------------------------\n" +
		"# [workos]\n" +
		"WORKOS_A=1\n"
	if got := string(f.Bytes()); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestDotenvSetInMissingSectionAppendsScaffold(t *testing.T) {
	in := []byte("A=1\n")
	f := ParseDotenv(in)
	_, err := f.Set("STRIPE_KEY", "x", SetOptions{Section: "stripe"})
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	want := "" +
		"A=1\n" +
		"\n" +
		"# ------------------------------------------------------------------------------\n" +
		"# [stripe]\n" +
		"STRIPE_KEY=x\n"
	if got := string(f.Bytes()); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
