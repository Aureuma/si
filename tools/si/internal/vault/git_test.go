package vault

import "testing"

func TestValidateGitIndexPath(t *testing.T) {
	ok := []string{"a/b.env", ".env.dev", "vault/.env.prod"}
	for _, p := range ok {
		got, err := validateGitIndexPath(p)
		if err != nil {
			t.Fatalf("%q: %v", p, err)
		}
		if got == "" {
			t.Fatalf("%q cleaned to empty", p)
		}
	}

	bad := []string{"", "../x", "/abs", "-nasty", "a/\x00b"}
	for _, p := range bad {
		if _, err := validateGitIndexPath(p); err == nil {
			t.Fatalf("expected error for %q", p)
		}
	}
}

func TestValidateGitRefName(t *testing.T) {
	ok := []string{"main", "origin/main", "release/v1.2.3", "feature_a"}
	for _, ref := range ok {
		if err := validateGitRefName(ref); err != nil {
			t.Fatalf("%q: %v", ref, err)
		}
	}

	bad := []string{"", "-x", "a b", "a..b", "a//b", "a@{b", "bad^ref", "bad:ref", "bad\\ref"}
	for _, ref := range bad {
		if err := validateGitRefName(ref); err == nil {
			t.Fatalf("expected error for %q", ref)
		}
	}
}
