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
	if !warmWeeklyBootstrapSucceeded(0.0, 0.2) {
		t.Fatalf("expected warm success for positive usage delta")
	}
	if warmWeeklyBootstrapSucceeded(0.0, 0.0) {
		t.Fatalf("expected warm failure for unchanged usage")
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
