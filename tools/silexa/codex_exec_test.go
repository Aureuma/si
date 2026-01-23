package main

import (
	"strings"
	"testing"
)

func TestExtractCodexExecOutputSimple(t *testing.T) {
	raw := "Hello world\n"
	out := extractCodexExecOutput(raw)
	if out != "Hello world" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestExtractCodexExecOutputFiltersNoise(t *testing.T) {
	raw := "Model: gpt-5.2-codex\nDirectory: /workspace\nTip: try again\n\u2022 Working (0s)\nFinal answer\n"
	out := extractCodexExecOutput(raw)
	if out != "Final answer" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestExtractCodexExecOutputUsesLastBlock(t *testing.T) {
	raw := "First line\n\nSecond block line 1\nSecond block line 2\n"
	out := extractCodexExecOutput(raw)
	expected := "Second block line 1\nSecond block line 2"
	if out != expected {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestExtractCodexExecOutputStripsBoxLines(t *testing.T) {
	raw := "\u256d\u2500\u256e\n\u2502 OpenAI Codex \u2502\n\u2570\u2500\u256f\n\nAnswer line\n"
	out := extractCodexExecOutput(raw)
	if out != "Answer line" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestExtractCodexExecOutputStripsANSI(t *testing.T) {
	raw := "\x1b[32mHello\x1b[0m\n"
	out := extractCodexExecOutput(raw)
	if out != "Hello" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestBuildNoMcpConfig(t *testing.T) {
	cfg := buildNoMcpConfig("gpt-5.2-codex", "high")
	if cfg == "" {
		t.Fatal("expected config content")
	}
	if strings.Contains(cfg, "[mcp]") {
		t.Fatalf("config should not include MCP section: %q", cfg)
	}
	if !strings.Contains(cfg, "web_search_request = false") {
		t.Fatalf("expected web_search_request=false: %q", cfg)
	}
}

func TestNormalizeReasoningEffort(t *testing.T) {
	if normalizeReasoningEffort("low") != "medium" {
		t.Fatalf("expected low to map to medium")
	}
	if normalizeReasoningEffort("high") != "high" {
		t.Fatalf("expected high to stay high")
	}
}

func TestIsValidSlug(t *testing.T) {
	if !isValidSlug("alpha-1") {
		t.Fatalf("expected slug to be valid")
	}
	if isValidSlug("alpha one") {
		t.Fatalf("expected slug with space to be invalid")
	}
}

func TestBuildCodexExecCommand(t *testing.T) {
	opts := codexExecOneOffOptions{Model: "gpt-5.2-codex", Effort: "medium", Workdir: "/workspace"}
	cmd := buildCodexExecCommand(opts, "Do the thing")
	if len(cmd) == 0 {
		t.Fatal("expected command")
	}
	if cmd[len(cmd)-2] != "exec" {
		t.Fatalf("expected exec subcommand, got %v", cmd)
	}
	if cmd[len(cmd)-1] != "Do the thing" {
		t.Fatalf("expected prompt at end, got %v", cmd)
	}
}
