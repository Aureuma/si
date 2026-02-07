package main

import (
	"errors"
	"net/http"
	"testing"
)

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

func TestApplyProfileStatusResultAuthFailureDowngradesAuth(t *testing.T) {
	item := codexProfileSummary{
		ID:                "cadma",
		AuthCached:        true,
		AuthUpdated:       "2026-02-07T18:00:00Z",
		FiveHourLeftPct:   99,
		FiveHourRemaining: 299,
		WeeklyLeftPct:     99,
		WeeklyRemaining:   10000,
	}
	res := profileStatusResult{
		ID: "cadma",
		Err: &usageAPIError{
			StatusCode: http.StatusUnauthorized,
			Code:       "token_expired",
			Message:    "expired",
		},
	}
	applyProfileStatusResult(&item, res)
	if item.AuthCached {
		t.Fatalf("expected AuthCached to be downgraded")
	}
	if item.AuthUpdated != "" {
		t.Fatalf("expected AuthUpdated to clear, got %q", item.AuthUpdated)
	}
	if item.FiveHourLeftPct != -1 || item.WeeklyLeftPct != -1 {
		t.Fatalf("expected limits to reset, got %+v", item)
	}
	if item.FiveHourRemaining != -1 || item.WeeklyRemaining != -1 {
		t.Fatalf("expected remaining timers to reset, got %+v", item)
	}
}

func TestApplyProfileStatusResultNonExpiredErrorSetsStatusError(t *testing.T) {
	item := codexProfileSummary{ID: "cadma"}
	res := profileStatusResult{ID: "cadma", Err: errors.New("boom")}
	applyProfileStatusResult(&item, res)
	if item.StatusError != "boom" {
		t.Fatalf("unexpected status error %q", item.StatusError)
	}
}

func TestApplyProfileStatusResultSuccess(t *testing.T) {
	item := codexProfileSummary{
		ID:                "cadma",
		FiveHourLeftPct:   -1,
		FiveHourRemaining: -1,
		WeeklyLeftPct:     -1,
		WeeklyRemaining:   -1,
	}
	res := profileStatusResult{
		ID: "cadma",
		Status: codexStatus{
			FiveHourLeftPct:   42.5,
			FiveHourReset:     "2026-02-08T00:00:00Z",
			FiveHourRemaining: 151,
			WeeklyLeftPct:     88.8,
			WeeklyReset:       "2026-02-14T00:00:00Z",
			WeeklyRemaining:   10080,
		},
	}
	applyProfileStatusResult(&item, res)
	if item.FiveHourLeftPct != 42.5 || item.WeeklyLeftPct != 88.8 {
		t.Fatalf("unexpected usage limits: %+v", item)
	}
	if item.FiveHourRemaining != 151 || item.WeeklyRemaining != 10080 {
		t.Fatalf("expected remaining durations to be populated: %+v", item)
	}
	if item.FiveHourReset == "" || item.WeeklyReset == "" {
		t.Fatalf("expected reset timestamps to be populated: %+v", item)
	}
}
