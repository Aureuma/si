package vault

import (
	"os"
	"path/filepath"
	"testing"
)

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

func TestDotenvParseBytesRoundTripLF(t *testing.T) {
	in := []byte("A=1\n# note\nB=2\n")
	f := ParseDotenv(in)
	if got := string(f.Bytes()); got != string(in) {
		t.Fatalf("got %q want %q", got, string(in))
	}
}

func TestDotenvParseBytesRoundTripCRLF(t *testing.T) {
	in := []byte("A=1\r\n# note\r\nB=2\r\n")
	f := ParseDotenv(in)
	if got := string(f.Bytes()); got != string(in) {
		t.Fatalf("got %q want %q", got, string(in))
	}
}

func TestDotenvSetAppendsWhenFinalLineHasNoNewline(t *testing.T) {
	in := []byte("A=1")
	f := ParseDotenv(in)
	_, err := f.Set("B", "2", SetOptions{})
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	want := "A=1\nB=2\n"
	if got := string(f.Bytes()); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestDotenvSetPreservesExportIndentAndEqSpacing(t *testing.T) {
	in := []byte("\texport API_KEY   =   old # keep\n")
	f := ParseDotenv(in)
	_, err := f.Set("API_KEY", "new", SetOptions{})
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	want := "\texport API_KEY   =   new # keep\n"
	if got := string(f.Bytes()); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestDotenvLookupLastWins(t *testing.T) {
	in := []byte("A=1\nA=2\n")
	f := ParseDotenv(in)
	got, ok := f.Lookup("A")
	if !ok {
		t.Fatalf("expected key")
	}
	if got != "2" {
		t.Fatalf("got %q want %q", got, "2")
	}
}

func TestSplitValueAndCommentUnquotedHashWithoutSpaceNotAComment(t *testing.T) {
	value, comment := splitValueAndComment("abc#def")
	if value != "abc#def" || comment != "" {
		t.Fatalf("value=%q comment=%q", value, comment)
	}
}

func TestSplitValueAndCommentUnquotedWithSpaceHashIsComment(t *testing.T) {
	value, comment := splitValueAndComment("abc   # note")
	if value != "abc" || comment != "   # note" {
		t.Fatalf("value=%q comment=%q", value, comment)
	}
}

func TestSplitValueAndCommentSingleQuotedHashNotComment(t *testing.T) {
	value, comment := splitValueAndComment("'abc#def'")
	if value != "'abc#def'" || comment != "" {
		t.Fatalf("value=%q comment=%q", value, comment)
	}
}

func TestSplitValueAndCommentDoubleQuotedCommentAfterQuote(t *testing.T) {
	value, comment := splitValueAndComment("\"a\\\"#b\"   # note")
	if value != "\"a\\\"#b\"" || comment != "   # note" {
		t.Fatalf("value=%q comment=%q", value, comment)
	}
}

func TestDotenvSetInSectionPreservesNextSectionDivider(t *testing.T) {
	in := []byte("" +
		"# ------------------------------------------------------------------------------\n" +
		"# [one]\n" +
		"A=1\n" +
		"\n" +
		"# ------------------------------------------------------------------------------\n" +
		"# [two]\n" +
		"B=2\n")
	f := ParseDotenv(in)
	_, err := f.Set("C", "3", SetOptions{Section: "one"})
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	want := "" +
		"# ------------------------------------------------------------------------------\n" +
		"# [one]\n" +
		"A=1\n" +
		"C=3\n" +
		"\n" +
		"# ------------------------------------------------------------------------------\n" +
		"# [two]\n" +
		"B=2\n"
	if got := string(f.Bytes()); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestWriteDotenvFileAtomicPreservesMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env.dev")
	if err := os.WriteFile(path, []byte("A=1\n"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	if err := WriteDotenvFileAtomic(path, []byte("A=2\n")); err != nil {
		t.Fatalf("WriteDotenvFileAtomic: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode() & os.ModePerm; mode != 0o600 {
		t.Fatalf("mode=%#o want %#o", mode, os.FileMode(0o600))
	}
}

func TestWriteDotenvFileAtomicCreatesMissingDirectories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "vault", ".env.dev")
	if err := WriteDotenvFileAtomic(path, []byte("A=1\n")); err != nil {
		t.Fatalf("WriteDotenvFileAtomic: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "A=1\n" {
		t.Fatalf("got %q", string(got))
	}
}

func TestWriteDotenvFileAtomicRejectsSymlinkTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.env")
	if err := os.WriteFile(target, []byte("A=1\n"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	link := filepath.Join(dir, "link.env")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	if err := WriteDotenvFileAtomic(link, []byte("A=2\n")); err == nil {
		t.Fatalf("expected symlink write rejection")
	}
}

func TestWriteDotenvFileAtomicSymlinkOverrideRequiresTruthyValue(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.env")
	if err := os.WriteFile(target, []byte("A=1\n"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	link := filepath.Join(dir, "link.env")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	t.Setenv("SI_VAULT_ALLOW_SYMLINK_ENV_FILE", "0")
	if err := WriteDotenvFileAtomic(link, []byte("A=2\n")); err == nil {
		t.Fatalf("expected rejection when override is non-truthy")
	}

	t.Setenv("SI_VAULT_ALLOW_SYMLINK_ENV_FILE", "1")
	if err := WriteDotenvFileAtomic(link, []byte("A=3\n")); err != nil {
		t.Fatalf("expected truthy override to allow write: %v", err)
	}
}

func TestSplitValueAndCommentCommentOnlyRHS(t *testing.T) {
	value, comment := splitValueAndComment("   # note")
	if value != "" || comment != "   # note" {
		t.Fatalf("value=%q comment=%q", value, comment)
	}
}

func TestDotenvSetNoOpWhenValueUnchanged(t *testing.T) {
	in := []byte("A=1\n")
	f := ParseDotenv(in)
	changed, err := f.Set("A", "1", SetOptions{})
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if changed {
		t.Fatalf("expected no change")
	}
	if got := string(f.Bytes()); got != "A=1\n" {
		t.Fatalf("got %q", got)
	}
}

func TestDotenvSetInSectionPreservesEqSpacingOnUpdate(t *testing.T) {
	in := []byte("" +
		"# ------------------------------------------------------------------------------\n" +
		"# [stripe]\n" +
		"STRIPE_A  =  1 # note\n")
	f := ParseDotenv(in)
	changed, err := f.Set("STRIPE_A", "2", SetOptions{Section: "stripe"})
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if !changed {
		t.Fatalf("expected change")
	}
	want := "" +
		"# ------------------------------------------------------------------------------\n" +
		"# [stripe]\n" +
		"STRIPE_A  =  2 # note\n"
	if got := string(f.Bytes()); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
