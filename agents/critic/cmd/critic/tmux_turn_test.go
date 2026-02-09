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
