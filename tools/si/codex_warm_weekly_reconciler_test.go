package main

import (
	"fmt"
	"strings"
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
	if !warmWeeklyBootstrapSucceeded(false, false, 0.0, true, true, 0.2, true, true) {
		t.Fatalf("expected warm success for positive usage delta")
	}
	if warmWeeklyBootstrapSucceeded(false, false, 0.0, true, false, 0.2, true, false) {
		t.Fatalf("expected warm failure when reset timing is still unavailable")
	}

	// Bootstrapping: becoming aware of reset timing is success even if percent deltas are tiny/zero.
	if !warmWeeklyBootstrapSucceeded(false, false, 0.0, true, false, 0.0, true, true) {
		t.Fatalf("expected warm success when reset timing becomes available")
	}
	if warmWeeklyBootstrapSucceeded(false, false, 0.0, true, false, 0.0, true, false) {
		t.Fatalf("expected warm failure when reset timing is still unavailable")
	}

	// Weekly rollover: a successful warm run should count even when deltas are 0.
	if !warmWeeklyBootstrapSucceeded(false, true, 0.0, true, true, 0.0, true, true) {
		t.Fatalf("expected warm success on weekly rollover with stable percentages")
	}

	// Force mode: avoid false failures when the endpoint is too coarse-grained to show deltas.
	if !warmWeeklyBootstrapSucceeded(true, false, 0.0, true, true, 0.0, true, true) {
		t.Fatalf("expected force warm to treat stable reset/usage signals as success")
	}
}

func TestWarmWeeklyNeedsWarmAttempt(t *testing.T) {
	if !warmWeeklyNeedsWarmAttempt(true, true, true, false, "ready") {
		t.Fatalf("expected force mode to always warm")
	}
	if !warmWeeklyNeedsWarmAttempt(false, false, true, false, "ready") {
		t.Fatalf("expected warm when usage signal is missing")
	}
	if !warmWeeklyNeedsWarmAttempt(false, true, false, false, "ready") {
		t.Fatalf("expected warm when reset signal is missing")
	}
	if !warmWeeklyNeedsWarmAttempt(false, true, true, true, "ready") {
		t.Fatalf("expected warm when weekly window advances")
	}
	if !warmWeeklyNeedsWarmAttempt(false, true, true, false, "failed") {
		t.Fatalf("expected retry when prior attempt failed")
	}
	if warmWeeklyNeedsWarmAttempt(false, true, true, false, "ready") {
		t.Fatalf("expected no warm when signals are stable and prior result was ready")
	}
}

func TestWarmWeeklyResetWindowAdvanced(t *testing.T) {
	prev := time.Date(2026, 2, 13, 14, 33, 56, 0, time.UTC)
	curr := time.Date(2026, 2, 21, 14, 33, 56, 0, time.UTC)
	if !warmWeeklyResetWindowAdvanced(prev, curr, true) {
		t.Fatalf("expected window-advanced to be true")
	}
	if warmWeeklyResetWindowAdvanced(prev, prev, true) {
		t.Fatalf("expected window-advanced to be false for same reset")
	}
	if warmWeeklyResetWindowAdvanced(time.Time{}, curr, true) {
		t.Fatalf("expected window-advanced to be false when previous reset is unknown")
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

func TestRenderWarmWeeklyReconcileConfig(t *testing.T) {
	cfg := renderWarmWeeklyReconcileConfig("/home/si/.si", "aureuma/si:local")
	if !strings.Contains(cfg, fmt.Sprintf("volume = %s:%s", warmWeeklyBinaryVolumeName, warmWeeklyBinaryDir)) {
		t.Fatalf("expected binary volume mount in config, got: %q", cfg)
	}
	if !strings.Contains(cfg, "if [ -x "+warmWeeklyBinaryPath+" ]; then "+warmWeeklyBinaryPath+" warmup reconcile --quiet; else /usr/local/bin/si warmup reconcile --quiet; fi") {
		t.Fatalf("expected fallback command in config, got: %q", cfg)
	}
}
