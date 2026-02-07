package main

import "testing"

func TestSplitProfileNameAndFlags(t *testing.T) {
	name, filtered := splitProfileNameAndFlags([]string{"cadma", "--no-status", "--json"})
	if name != "cadma" {
		t.Fatalf("unexpected name %q", name)
	}
	if len(filtered) != 2 || filtered[0] != "--no-status" || filtered[1] != "--json" {
		t.Fatalf("unexpected filtered flags: %v", filtered)
	}

	name, filtered = splitProfileNameAndFlags([]string{"--json", "--no-status", "cadma"})
	if name != "cadma" {
		t.Fatalf("unexpected name %q", name)
	}
	if len(filtered) != 2 || filtered[0] != "--json" || filtered[1] != "--no-status" {
		t.Fatalf("unexpected filtered flags: %v", filtered)
	}

	name, filtered = splitProfileNameAndFlags([]string{"cadma", "extra"})
	if name != "cadma" {
		t.Fatalf("unexpected name %q", name)
	}
	if len(filtered) != 1 || filtered[0] != "extra" {
		t.Fatalf("unexpected filtered args: %v", filtered)
	}
}
