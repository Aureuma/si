package main

import "testing"

func TestParseUsageWithWeekly(t *testing.T) {
	raw := "Status: ok\nSigned in as: jane.doe@example.com\n5-hour remaining: 25% (1h 15m)\nWeekly remaining: 80% (20h 0m)\n"
	usage := parseUsage(raw, 300)
	if usage.Email != "jane.doe@example.com" {
		t.Fatalf("expected email parsed, got %q", usage.Email)
	}
	if usage.RemainingPct != 25 {
		t.Fatalf("expected remaining pct 25, got %.1f", usage.RemainingPct)
	}
	if usage.RemainingMinutes != 75 {
		t.Fatalf("expected remaining minutes 75, got %d", usage.RemainingMinutes)
	}
	if usage.WeeklyRemainingPct != 80 {
		t.Fatalf("expected weekly remaining pct 80, got %.1f", usage.WeeklyRemainingPct)
	}
	if usage.WeeklyRemainingMinutes != 1200 {
		t.Fatalf("expected weekly remaining minutes 1200, got %d", usage.WeeklyRemainingMinutes)
	}
}

func TestParseUsageUsedPctFallback(t *testing.T) {
	raw := "Usage: 40% used\nWeekly: 10% used\n"
	usage := parseUsage(raw, 300)
	if usage.RemainingPct != 60 {
		t.Fatalf("expected remaining pct 60, got %.1f", usage.RemainingPct)
	}
	if usage.WeeklyRemainingPct != 90 {
		t.Fatalf("expected weekly remaining pct 90, got %.1f", usage.WeeklyRemainingPct)
	}
}

func TestParseUsageSpacedEmail(t *testing.T) {
	raw := "Account: jane @ example . com\nRemaining: 50%\n"
	usage := parseUsage(raw, 300)
	if usage.Email != "jane@example.com" {
		t.Fatalf("expected spaced email parsed, got %q", usage.Email)
	}
}

func TestParseUsageModelAndReasoning(t *testing.T) {
	raw := "Model: gpt-5.1-codex-max\nReasoning effort: high\nRemaining: 10%\n"
	usage := parseUsage(raw, 300)
	if usage.Model != "gpt-5.1-codex-max" {
		t.Fatalf("expected model parsed, got %q", usage.Model)
	}
	if usage.ReasoningEffort != "high" {
		t.Fatalf("expected reasoning parsed, got %q", usage.ReasoningEffort)
	}
}

func TestParseUsageModelAndReasoningSameLine(t *testing.T) {
	raw := "Model: gpt-4.1 (Reasoning level: medium)\n"
	usage := parseUsage(raw, 300)
	if usage.Model != "gpt-4.1" {
		t.Fatalf("expected model parsed, got %q", usage.Model)
	}
	if usage.ReasoningEffort != "medium" {
		t.Fatalf("expected reasoning parsed, got %q", usage.ReasoningEffort)
	}
}
