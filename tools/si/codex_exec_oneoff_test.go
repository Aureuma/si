package main

import "testing"

func TestExtractCodexExecOutputFromRoleBlocks(t *testing.T) {
	raw := `OpenAI Codex v0.98.0
--------
user
Reply with exactly ONEOFF_OK and nothing else.
codex
ONEOFF_OK
tokens used
789`
	got := extractCodexExecOutput(raw)
	if got != "ONEOFF_OK" {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestExtractCodexExecOutputFromRoleBlocksMultiLine(t *testing.T) {
	raw := `user
Explain
codex
Line one
Line two
tokens used
1200`
	got := extractCodexExecOutput(raw)
	if got != "Line one\nLine two" {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestExtractCodexExecOutputFallback(t *testing.T) {
	raw := "final answer only"
	got := extractCodexExecOutput(raw)
	if got != "final answer only" {
		t.Fatalf("unexpected output: %q", got)
	}
}
