package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCodexTurnExecutorRunTurnShort(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "fake-codex")
	if err := os.WriteFile(fake, []byte(fakeCodexScript), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}
	session := "si-test-" + sanitizeSessionName(t.Name())
	defer func() { _ = exec.Command("tmux", "kill-session", "-t", session).Run() }()

	e := codexTurnExecutor{
		promptLines:  1,
		allowMcp:     false,
		captureMode:  "main",
		captureLines: 2000,
		strictReport: true,
		readyTimeout: 3 * time.Second,
		turnTimeout:  4 * time.Second,
		pollInterval: 75 * time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	out, err := e.runTurn(ctx, tmuxRunner{}, session, "exec "+shellQuote(fake), "hello", "actor")
	if err != nil {
		t.Fatalf("runTurn: %v\nout:\n%s", err, out)
	}
	if !strings.Contains(out, reportBeginMarker) || !strings.Contains(out, reportEndMarker) {
		t.Fatalf("expected delimited report markers, got:\n%s", out)
	}
	report := extractDelimitedWorkReport(out)
	if !strings.Contains(report, "Summary:") {
		t.Fatalf("expected report content, got:\n%s", report)
	}
}

func TestCodexTurnExecutorRunTurnLongTailCapture(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "fake-codex")
	if err := os.WriteFile(fake, []byte(fakeCodexScript), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}
	session := "si-test-" + sanitizeSessionName(t.Name())
	defer func() { _ = exec.Command("tmux", "kill-session", "-t", session).Run() }()

	e := codexTurnExecutor{
		promptLines:  1,
		allowMcp:     false,
		captureMode:  "main",
		captureLines: 1200, // ensure we rely on tail capture, not full history
		strictReport: true,
		readyTimeout: 3 * time.Second,
		turnTimeout:  8 * time.Second,
		pollInterval: 75 * time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	out, err := e.runTurn(ctx, tmuxRunner{}, session, "exec "+shellQuote(fake), "LONG_OUTPUT", "critic")
	if err != nil {
		t.Fatalf("runTurn: %v\nout:\n%s", err, out)
	}
	report := extractDelimitedWorkReport(out)
	if report == "" {
		t.Fatalf("expected delimited report, got:\n%s", out)
	}
}

func TestCodexTurnExecutorRunTurnMultiTurnTailShiftDoesNotLoseNewReport(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "fake-codex")
	if err := os.WriteFile(fake, []byte(fakeCodexScript), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}
	session := "si-test-" + sanitizeSessionName(t.Name())
	defer func() { _ = exec.Command("tmux", "kill-session", "-t", session).Run() }()

	e := codexTurnExecutor{
		promptLines:  1,
		allowMcp:     false,
		captureMode:  "main",
		captureLines: 1200, // intentionally small to force tail window shifting across turns
		strictReport: true,
		readyTimeout: 3 * time.Second,
		turnTimeout:  10 * time.Second,
		pollInterval: 75 * time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 18*time.Second)
	defer cancel()

	// Turn 1: produce a baseline report.
	out1, err := e.runTurn(ctx, tmuxRunner{}, session, "exec "+shellQuote(fake), "short", "actor")
	if err != nil {
		t.Fatalf("turn1 runTurn: %v\nout:\n%s", err, out1)
	}
	report1 := extractDelimitedWorkReport(out1)
	if report1 == "" {
		t.Fatalf("turn1 expected delimited report, got:\n%s", out1)
	}

	// Turn 2: emit long output so tmux tail capture shifts and the baseline report scrolls out.
	out2, err := e.runTurn(ctx, tmuxRunner{}, session, "exec "+shellQuote(fake), "LONG_OUTPUT", "critic")
	if err != nil {
		t.Fatalf("turn2 runTurn: %v\nout:\n%s", err, out2)
	}
	report2 := extractDelimitedWorkReport(out2)
	if report2 == "" {
		t.Fatalf("turn2 expected delimited report, got:\n%s", out2)
	}
	if strings.TrimSpace(report2) == strings.TrimSpace(report1) {
		t.Fatalf("expected a new report body on turn2 (tail window shifted), got same as turn1:\n%s", report2)
	}
}

func TestCodexTurnExecutorRunTurnStrictRejectsUndelimited(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "fake-codex")
	if err := os.WriteFile(fake, []byte(fakeCodexNoMarkersScript), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}
	session := "si-test-" + sanitizeSessionName(t.Name())
	defer func() { _ = exec.Command("tmux", "kill-session", "-t", session).Run() }()

	e := codexTurnExecutor{
		promptLines:  1,
		allowMcp:     false,
		captureMode:  "main",
		captureLines: 1200,
		strictReport: true,
		readyTimeout: 3 * time.Second,
		turnTimeout:  4 * time.Second,
		pollInterval: 75 * time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()

	out, err := e.runTurn(ctx, tmuxRunner{}, session, "exec "+shellQuote(fake), "NO_MARKERS", "actor")
	if err == nil {
		t.Fatalf("expected strict mode failure, got success:\n%s", out)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "missing work report markers") {
		t.Fatalf("unexpected err: %v\nout:\n%s", err, out)
	}
}

func TestCodexTurnExecutorRunTurnNonStrictAcceptsUndelimitedSectionReport(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "fake-codex")
	if err := os.WriteFile(fake, []byte(fakeCodexNoMarkersFullScript), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}
	session := "si-test-" + sanitizeSessionName(t.Name())
	defer func() { _ = exec.Command("tmux", "kill-session", "-t", session).Run() }()

	e := codexTurnExecutor{
		promptLines:  1,
		allowMcp:     false,
		captureMode:  "main",
		captureLines: 1200,
		strictReport: false,
		readyTimeout: 3 * time.Second,
		turnTimeout:  4 * time.Second,
		pollInterval: 75 * time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()

	// Include marker strings in the *input* to ensure non-strict normalization does not
	// mistake echoed prompt text for a delimited report.
	prompt := "hello " + reportBeginMarker + " placeholder " + reportEndMarker
	out, err := e.runTurn(ctx, tmuxRunner{}, session, "exec "+shellQuote(fake), prompt, "actor")
	if err != nil {
		t.Fatalf("runTurn: %v\nout:\n%s", err, out)
	}
	if !strings.Contains(out, reportBeginMarker) || !strings.Contains(out, reportEndMarker) {
		t.Fatalf("expected normalized delimited report markers, got:\n%s", out)
	}
	report := extractDelimitedWorkReport(out)
	if !strings.Contains(report, "Summary:") || !strings.Contains(report, "Next Step for Critic:") {
		t.Fatalf("expected section report content, got:\n%s", report)
	}
}

func TestCodexTurnExecutorRunTurnTaggedMarkersNormalized(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "fake-codex-tagged")
	if err := os.WriteFile(fake, []byte(fakeCodexTaggedScript), 0o755); err != nil {
		t.Fatalf("write fake codex tagged: %v", err)
	}
	session := "si-test-" + sanitizeSessionName(t.Name())
	defer func() { _ = exec.Command("tmux", "kill-session", "-t", session).Run() }()

	e := codexTurnExecutor{
		promptLines:  1,
		allowMcp:     false,
		captureMode:  "main",
		captureLines: 1200,
		strictReport: true,
		readyTimeout: 3 * time.Second,
		turnTimeout:  4 * time.Second,
		pollInterval: 75 * time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()

	out, err := e.runTurn(ctx, tmuxRunner{}, session, "exec "+shellQuote(fake), "hello", "actor")
	if err != nil {
		t.Fatalf("runTurn: %v\nout:\n%s", err, out)
	}
	// Tagged reports are normalized into the legacy delimited format for downstream consumers.
	if !strings.Contains(out, reportBeginMarker) || !strings.Contains(out, reportEndMarker) {
		t.Fatalf("expected normalized delimited report markers, got:\n%s", out)
	}
	report := extractDelimitedWorkReport(out)
	if !strings.Contains(report, "Summary:") || !strings.Contains(report, "Next Step for Critic:") {
		t.Fatalf("expected actor report content, got:\n%s", report)
	}
}

func shellQuote(s string) string {
	// Minimal single-quote safe quoting for bash -lc usage in tests.
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

const fakeCodexScript = `#!/usr/bin/env bash
set -euo pipefail
printf "› "
while IFS= read -r line; do
  if [[ "${line}" == "__EXIT__" ]]; then
    echo
    exit 0
  fi
  if [[ "${line}" == *"LONG_OUTPUT"* ]]; then
    # Emit a lot of output to stress tmux capture behavior.
    for i in $(seq 1 12000); do
      echo "line $i"
    done
  else
    echo "ok"
  fi
  echo "<<WORK_REPORT_BEGIN>>"
  if [[ "${line}" == *"LONG_OUTPUT"* ]]; then
    # Critic-format report (required by dyad loop).
    echo "Assessment:"
    echo "- prompt_len: ${#line}"
    echo "Risks:"
    echo "- none"
    echo "Required Fixes:"
    echo "- none"
    echo "Verification Steps:"
    echo "- none"
    echo "Next Actor Prompt:"
    echo "- proceed"
    echo "Continue Loop: yes"
  else
    # Actor-format report (required by dyad loop).
    echo "Summary:"
    echo "- prompt_len: ${#line}"
    echo "Changes:"
    echo "- none"
    echo "Validation:"
    echo "- none"
    echo "Open Questions:"
    echo "- none"
    echo "Next Step for Critic:"
    echo "- proceed"
  fi
  echo "<<WORK_REPORT_END>>"
  printf "› "
done
`

const fakeCodexNoMarkersScript = `#!/usr/bin/env bash
	set -euo pipefail
	printf "› "
	while IFS= read -r line; do
	  echo "Summary:"
	  echo "- undelimited"
	  printf "› "
	done
	`

const fakeCodexNoMarkersFullScript = `#!/usr/bin/env bash
		set -euo pipefail
		printf "› "
		while IFS= read -r line; do
	  echo "ok"
	  echo "Summary:"
	  echo "- prompt_len: ${#line}"
	  echo "Changes:"
	  echo "- none"
	  echo "Validation:"
	  echo "- none"
	  echo "Open Questions:"
	  echo "- none"
	  echo "Next Step for Critic:"
	  echo "- proceed"
	  printf "› "
		done
		`

const fakeCodexTaggedScript = `#!/usr/bin/env bash
set -euo pipefail
printf "› "
while IFS= read -r line; do
  turn_id=""
  if [[ "${line}" =~ \[(si-dyad-turn-id:[^]]+)\] ]]; then
    turn_id="${BASH_REMATCH[1]}"
  fi
  begin="<<WORK_REPORT_BEGIN>>"
  end="<<WORK_REPORT_END>>"
  if [[ -n "${turn_id}" ]]; then
    begin="<<WORK_REPORT_BEGIN:${turn_id}>>"
    end="<<WORK_REPORT_END:${turn_id}>>"
  fi
  echo "${begin}"
  echo "Summary:"
  echo "- tagged marker path"
  echo "Changes:"
  echo "- none"
  echo "Validation:"
  echo "- none"
  echo "Open Questions:"
  echo "- none"
  echo "Next Step for Critic:"
  echo "- proceed"
  echo "${end}"
  printf "› "
done
`
