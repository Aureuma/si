package main

import (
	"testing"
	"time"
)

func TestWindowUsageUsesResetCountdown(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	resetAt := now.Add(130 * time.Minute).Unix()
	window := &appRateLimitWindow{
		UsedPercent: 40,
		ResetsAt:    &resetAt,
	}
	_, remaining := windowUsage(window, 300, now)
	if remaining != 130 {
		t.Fatalf("expected reset countdown 130m, got %d", remaining)
	}
}
