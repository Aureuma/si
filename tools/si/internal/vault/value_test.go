package vault

import "testing"

func TestNormalizeDotenvValueSingleQuoted(t *testing.T) {
	got, err := NormalizeDotenvValue("'hello world'")
	if err != nil {
		t.Fatalf("NormalizeDotenvValue: %v", err)
	}
	if got != "hello world" {
		t.Fatalf("got %q", got)
	}
}

func TestNormalizeDotenvValueDoubleQuotedEscapes(t *testing.T) {
	got, err := NormalizeDotenvValue("\"line1\\nline2\\t\\\"x\\\"\"")
	if err != nil {
		t.Fatalf("NormalizeDotenvValue: %v", err)
	}
	if got != "line1\nline2\t\"x\"" {
		t.Fatalf("got %q", got)
	}
}

func TestNormalizeDotenvValueUnquotedTrimmed(t *testing.T) {
	got, err := NormalizeDotenvValue("   abc   ")
	if err != nil {
		t.Fatalf("NormalizeDotenvValue: %v", err)
	}
	if got != "abc" {
		t.Fatalf("got %q", got)
	}
}

func TestNormalizeDotenvValueEmpty(t *testing.T) {
	got, err := NormalizeDotenvValue("   ")
	if err != nil {
		t.Fatalf("NormalizeDotenvValue: %v", err)
	}
	if got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestNormalizeDotenvValueInvalidDoubleQuoteReturnsError(t *testing.T) {
	if _, err := NormalizeDotenvValue("\"unterminated"); err == nil {
		t.Fatalf("expected error")
	}
}
