package main

import (
	"testing"
	"time"
)

func TestWarmWeeklyBackoffDuration(t *testing.T) {
	if got := warmWeeklyBackoffDuration(1); got != 15*time.Minute {
		t.Fatalf("expected 15m, got %s", got)
	}
	if got := warmWeeklyBackoffDuration(2); got != 30*time.Minute {
		t.Fatalf("expected 30m, got %s", got)
	}
	if got := warmWeeklyBackoffDuration(8); got != 24*time.Hour {
		t.Fatalf("expected clamp at 24h, got %s", got)
	}
}

func TestWarmWeeklyBootstrapSucceeded(t *testing.T) {
	// Non-force: delta can be a valid success signal when both readings are known.
	if !warmWeeklyBootstrapSucceeded(false, 0.0, true, true, 0.2, true, true) {
		t.Fatalf("expected warm success for positive usage delta")
	}
	if warmWeeklyBootstrapSucceeded(false, 0.0, true, false, 0.2, true, false) {
		t.Fatalf("expected warm failure when reset timing is still unavailable")
	}

	// Bootstrapping: becoming aware of reset timing is success even if percent deltas are tiny/zero.
	if !warmWeeklyBootstrapSucceeded(false, 0.0, true, false, 0.0, true, true) {
		t.Fatalf("expected warm success when reset timing becomes available")
	}
	if warmWeeklyBootstrapSucceeded(false, 0.0, true, false, 0.0, true, false) {
		t.Fatalf("expected warm failure when reset timing is still unavailable")
	}

	// Force mode: avoid false failures when the endpoint is too coarse-grained to show deltas.
	if !warmWeeklyBootstrapSucceeded(true, 0.0, true, true, 0.0, true, true) {
		t.Fatalf("expected force warm to treat stable reset/usage signals as success")
	}
}

func TestWarmWeeklyNeedsBootstrap(t *testing.T) {
	if !warmWeeklyNeedsBootstrap(true, 0, true, true) {
		t.Fatalf("expected force bootstrap to always warm")
	}
	if warmWeeklyNeedsBootstrap(false, 0, true, true) {
		t.Fatalf("expected no bootstrap when usage+reset signals are available")
	}
	if !warmWeeklyNeedsBootstrap(false, 0, false, true) {
		t.Fatalf("expected bootstrap when usage signal is missing")
	}
	if !warmWeeklyNeedsBootstrap(false, 0, true, false) {
		t.Fatalf("expected bootstrap when reset signal is missing")
	}
}

func TestWeeklyUsedPercent(t *testing.T) {
	payload := usagePayload{
		RateLimit: &usageRateLimit{
			Secondary: &usageWindow{UsedPercent: 12.34},
		},
	}
	used, ok := weeklyUsedPercent(payload)
	if !ok || used != 12.34 {
		t.Fatalf("unexpected used percent: ok=%v used=%v", ok, used)
	}
}
