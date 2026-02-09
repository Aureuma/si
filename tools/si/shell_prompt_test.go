package main

import "testing"

func TestShellSingleQuote(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty", input: "", want: "''"},
		{name: "simple", input: "hello", want: "'hello'"},
		{name: "with apostrophe", input: "don't", want: "'don'\\''t'"},
		{name: "spaces", input: "hello world", want: "'hello world'"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := shellSingleQuote(tc.input)
			if got != tc.want {
				t.Fatalf("shellSingleQuote(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestBoolToInt(t *testing.T) {
	if boolToInt(true) != 1 {
		t.Fatalf("boolToInt(true) = %d, want 1", boolToInt(true))
	}
	if boolToInt(false) != 0 {
		t.Fatalf("boolToInt(false) = %d, want 0", boolToInt(false))
	}
}
