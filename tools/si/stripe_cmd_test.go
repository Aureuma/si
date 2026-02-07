package main

import "testing"

func TestNormalizeStripeEnvironment(t *testing.T) {
	if got := normalizeStripeEnvironment(" LIVE "); got != "live" {
		t.Fatalf("expected live, got %q", got)
	}
	if got := normalizeStripeEnvironment("sandbox"); got != "sandbox" {
		t.Fatalf("expected sandbox, got %q", got)
	}
	if got := normalizeStripeEnvironment("test"); got != "" {
		t.Fatalf("expected empty for unsupported env, got %q", got)
	}
}
