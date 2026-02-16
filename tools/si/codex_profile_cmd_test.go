package main

import (
	"errors"
	"net/http"
	"strings"
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

func TestApplyProfileStatusResultAuthFailureKeepsAuthAndSetsError(t *testing.T) {
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
	if !item.AuthCached {
		t.Fatalf("expected AuthCached to remain true")
	}
	if item.AuthUpdated != "2026-02-07T18:00:00Z" {
		t.Fatalf("expected AuthUpdated to remain unchanged, got %q", item.AuthUpdated)
	}
	if item.StatusError == "" {
		t.Fatalf("expected auth failure to surface as status error")
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

func TestProfileAuthLabel(t *testing.T) {
	if got := profileAuthLabel(codexProfileSummary{AuthCached: true}); got != "Logged-In" {
		t.Fatalf("expected Logged-In, got %q", got)
	}
	if got := profileAuthLabel(codexProfileSummary{AuthCached: true, StatusError: "token expired"}); got != "Missing" {
		t.Fatalf("expected Missing for auth status failure, got %q", got)
	}
	if got := profileAuthLabel(codexProfileSummary{AuthCached: false, StatusError: "auth cache missing"}); got != "Missing" {
		t.Fatalf("expected Missing, got %q", got)
	}
	if got := profileAuthLabel(codexProfileSummary{AuthCached: false}); got != "Missing" {
		t.Fatalf("expected Missing, got %q", got)
	}
}

func TestProfileLimitDisplayForMissingAuth(t *testing.T) {
	item := codexProfileSummary{
		AuthCached:  false,
		StatusError: "auth cache not found",
	}
	if got := profileFiveHourDisplay(item); got != "-" {
		t.Fatalf("unexpected 5H display %q", got)
	}
	if got := profileWeeklyDisplay(item); got != "-" {
		t.Fatalf("unexpected WEEKLY display %q", got)
	}
}

func TestProfileLimitDisplayForLoggedInAuthError(t *testing.T) {
	item := codexProfileSummary{
		AuthCached:  true,
		StatusError: "token refresh failed",
	}
	if got := profileFiveHourDisplay(item); got != "-" {
		t.Fatalf("unexpected 5H display %q", got)
	}
	if got := profileWeeklyDisplay(item); got != "-" {
		t.Fatalf("unexpected WEEKLY display %q", got)
	}
}

func TestProfileStatusWarningsSkipsAuthErrors(t *testing.T) {
	items := []codexProfileSummary{
		{ID: "america", AuthCached: false, StatusError: "auth cache missing"},
		{ID: "cadma", AuthCached: true, StatusError: "token expired"},
		{ID: "berylla", AuthCached: true, StatusError: "upstream temporary failure"},
	}
	warnings := profileStatusWarnings(items)
	if len(warnings) != 1 {
		t.Fatalf("expected one non-auth warning, got %d (%v)", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0], "profile berylla status error:") {
		t.Fatalf("unexpected warning content: %q", warnings[0])
	}
}

func TestSummarizeProfileStatusError(t *testing.T) {
	if got := summarizeProfileStatusError("america", "auth cache not found at /tmp/auth.json; run `si login`"); got != "auth cache missing; run `si login america`" {
		t.Fatalf("unexpected auth-cache summary: %q", got)
	}
	if got := summarizeProfileStatusError("cadma", "usage token expired; refresh failed (usage api status 401 (refresh_token_reused): reused)"); got != "token refresh failed (refresh token reused); run `si login cadma`" {
		t.Fatalf("unexpected refresh summary: %q", got)
	}
	if got := summarizeProfileStatusError("cadma", "usage api status 401 (token_expired): Provided authentication token is expired."); got != "token expired; run `si login cadma`" {
		t.Fatalf("unexpected token-expired summary: %q", got)
	}
}

func TestProfileNameForTable(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "emoji_prefixed_name", in: "❇️ Berylla", want: "❇️ Berylla"},
		{name: "ascii_name_unchanged", in: "Berylla", want: "Berylla"},
		{name: "single_token_emoji_unchanged", in: "❇️", want: "❇️"},
		{name: "leading_spaces_trimmed", in: "   🧲 Gadolina  ", want: "🧲 Gadolina"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := profileNameForTable(tc.in)
			if got != tc.want {
				t.Fatalf("profileNameForTable(%q)=%q want=%q", tc.in, got, tc.want)
			}
		})
	}
}

func TestProfileEmailForTable(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "standard_email", in: "7shayan7@gmail.com", want: "7shayan7@g…"},
		{name: "subdomain_email", in: "name@mail.example.com", want: "name@m…"},
		{name: "missing_at", in: "not-an-email", want: "not-an-email"},
		{name: "empty_domain", in: "name@", want: "name@"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := profileEmailForTable(tc.in)
			if got != tc.want {
				t.Fatalf("profileEmailForTable(%q)=%q want=%q", tc.in, got, tc.want)
			}
		})
	}
}
