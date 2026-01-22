package main

import "testing"

func TestExtractReportLinesSingle(t *testing.T) {
	raw := "› Hello\n\n• Hi there!\n\n› Next"
	segments := parsePromptSegments(raw)
	if len(segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(segments))
	}
	report := extractReportLines(segments[0].Lines)
	if report != "• Hi there!" {
		t.Fatalf("unexpected report: %q", report)
	}
}

func TestExtractReportLinesMultiline(t *testing.T) {
	raw := "› Prompt\n\n• The ocean covers most of Earth and helps regulate the climate. Its depths hold\n  vast ecosystems that are still largely unexplored.\n\n› Next"
	segments := parsePromptSegments(raw)
	if len(segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(segments))
	}
	report := extractReportLines(segments[0].Lines)
	expected := "• The ocean covers most of Earth and helps regulate the climate. Its depths hold\n  vast ecosystems that are still largely unexplored."
	if report != expected {
		t.Fatalf("unexpected report: %q", report)
	}
}

func TestExtractReportLinesStopsOnWarning(t *testing.T) {
	raw := "› Prompt\n\n• Hello!\n\n⚠ Heads up, you have less than 5% left.\n\n› Next"
	segments := parsePromptSegments(raw)
	if len(segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(segments))
	}
	report := extractReportLines(segments[0].Lines)
	if report != "• Hello!" {
		t.Fatalf("unexpected report: %q", report)
	}
}

func TestStripANSI(t *testing.T) {
	raw := "\x1b[32m• Hello\x1b[0m"
	clean := stripANSI(raw)
	if clean != "• Hello" {
		t.Fatalf("unexpected stripped value: %q", clean)
	}
}

func TestParsePromptSegmentsWithANSI(t *testing.T) {
	raw := "› Prompt\n\x1b[32m• Hello\x1b[0m\n\n› Next"
	segments := parsePromptSegments(stripANSI(raw))
	if len(segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(segments))
	}
	report := extractReportLines(segments[0].Lines)
	if report != "• Hello" {
		t.Fatalf("unexpected report: %q", report)
	}
}

func TestExtractReportSkipsWorking(t *testing.T) {
	raw := "› Prompt\n\n• Working (0s • esc to interrupt)\n\n• Hello!\n\n› Next"
	segments := parsePromptSegments(raw)
	if len(segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(segments))
	}
	report := extractReportLines(segments[0].Lines)
	if report != "• Hello!" {
		t.Fatalf("unexpected report: %q", report)
	}
}

func TestExtractReportIncludesWorkedLine(t *testing.T) {
	raw := "› Prompt\n\n• Done.\n\nWorked for 0m 2s\n\n› Next"
	segments := parsePromptSegments(raw)
	if len(segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(segments))
	}
	report := extractReportLines(segments[0].Lines)
	expected := "• Done.\nWorked for 0m 2s"
	if report != expected {
		t.Fatalf("unexpected report: %q", report)
	}
}
