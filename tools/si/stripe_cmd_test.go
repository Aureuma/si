package main

import (
	"strings"
	"testing"
)

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

func TestResolveStripeLogPath(t *testing.T) {
	t.Setenv("SI_STRIPE_LOG_FILE", "/tmp/custom-stripe.log")
	if got := resolveStripeLogPath(Settings{}); got != "/tmp/custom-stripe.log" {
		t.Fatalf("expected env log path, got %q", got)
	}
	t.Setenv("SI_STRIPE_LOG_FILE", "")
	got := resolveStripeLogPath(Settings{})
	if !strings.Contains(got, ".si/logs/stripe.log") {
		t.Fatalf("expected default log path, got %q", got)
	}
}
