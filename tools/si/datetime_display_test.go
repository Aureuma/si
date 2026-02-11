package main

import (
	"testing"
	"time"
)

func TestFormatDateWithGitHubRelativeFutureDays(t *testing.T) {
	now := time.Date(2026, time.February, 12, 10, 0, 0, 0, time.UTC)
	target := time.Date(2026, time.February, 14, 9, 0, 0, 0, time.UTC)
	got := formatDateWithGitHubRelative(target, now)
	if got != "Feb 14, in 2 days" {
		t.Fatalf("unexpected future day format %q", got)
	}
}

func TestFormatDateWithGitHubRelativeFutureHours(t *testing.T) {
	now := time.Date(2026, time.February, 12, 10, 0, 0, 0, time.UTC)
	target := time.Date(2026, time.February, 12, 13, 0, 0, 0, time.UTC)
	got := formatDateWithGitHubRelative(target, now)
	if got != "Feb 12, in 3 hours" {
		t.Fatalf("unexpected future hour format %q", got)
	}
}

func TestFormatDateWithGitHubRelativeFutureMinutes(t *testing.T) {
	now := time.Date(2026, time.February, 12, 10, 0, 0, 0, time.UTC)
	target := now.Add(45 * time.Minute)
	got := formatDateWithGitHubRelative(target, now)
	if got != "Feb 12, in 45 minutes" {
		t.Fatalf("unexpected future minute format %q", got)
	}
}

func TestFormatDateWithGitHubRelativePastDays(t *testing.T) {
	now := time.Date(2026, time.February, 12, 10, 0, 0, 0, time.UTC)
	target := time.Date(2026, time.February, 10, 11, 0, 0, 0, time.UTC)
	got := formatDateWithGitHubRelative(target, now)
	if got != "Feb 10, 1 day ago" {
		t.Fatalf("unexpected past day format %q", got)
	}
}

func TestFormatISODateWithGitHubRelativeFallback(t *testing.T) {
	now := time.Date(2026, time.February, 12, 10, 0, 0, 0, time.UTC)
	raw := "not-an-iso-date"
	got := formatISODateWithGitHubRelative(raw, now)
	if got != raw {
		t.Fatalf("expected fallback to raw value, got %q", got)
	}
}

func TestFormatISODateWithGitHubRelative(t *testing.T) {
	now := time.Date(2026, time.February, 12, 10, 0, 0, 0, time.UTC)
	raw := "2026-02-14T09:00:00Z"
	got := formatISODateWithGitHubRelative(raw, now)
	if got != "Feb 14, in 2 days" {
		t.Fatalf("unexpected ISO format %q", got)
	}
}
