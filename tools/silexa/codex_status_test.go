package main

import (
	"strings"
	"testing"
)

func TestParseCodexStatus(t *testing.T) {
	raw := `
╭───────────────────────────────────────────────────────────────────╮
│  >_ OpenAI Codex (v0.88.0)                                        │
│                                                                   │
│ Visit https://chatgpt.com/codex/settings/usage for up-to-date     │
│ information on rate limits and credits                            │
│                                                                   │
│  Model:            gpt-5.2-codex (reasoning high, summaries auto) │
│  Directory:        ~/Development/Silexa                           │
│  Approval:         never                                          │
│  Sandbox:          danger-full-access                             │
│  Agents.md:        <none>                                         │
│  Account:          maps-android.5t@icloud.com (Plus)              │
│  Session:          019be3cb-d448-7ca1-8076-360d9d851d43           │
│                                                                   │
│  Context window:   56% left (120K used / 258K)                    │
│  5h limit:         [█████████░░░░░░░░░░░] 44% left (resets 08:45) │
│  Weekly limit:     [█████░░░░░░░░░░░░░░░] 24% left (resets 22:55) │
╰───────────────────────────────────────────────────────────────────╯
`
	got := parseCodexStatus(raw)
	if got.Model != "gpt-5.2-codex" {
		t.Fatalf("model parse failed: %q", got.Model)
	}
	if got.ReasoningEffort != "high" {
		t.Fatalf("reasoning parse failed: %q", got.ReasoningEffort)
	}
	if got.Summaries != "auto" {
		t.Fatalf("summaries parse failed: %q", got.Summaries)
	}
	if got.AccountEmail != "maps-android.5t@icloud.com" {
		t.Fatalf("account email parse failed: %q", got.AccountEmail)
	}
	if got.AccountPlan != "Plus" {
		t.Fatalf("account plan parse failed: %q", got.AccountPlan)
	}
	if got.Session != "019be3cb-d448-7ca1-8076-360d9d851d43" {
		t.Fatalf("session parse failed: %q", got.Session)
	}
	if got.ContextLeftPct != 56 {
		t.Fatalf("context pct parse failed: %v", got.ContextLeftPct)
	}
	if got.FiveHourLeftPct != 44 {
		t.Fatalf("5h pct parse failed: %v", got.FiveHourLeftPct)
	}
	if got.WeeklyLeftPct != 24 {
		t.Fatalf("weekly pct parse failed: %v", got.WeeklyLeftPct)
	}
}

func TestParseModelLineVariants(t *testing.T) {
	line := "Model:          gpt-5.2-codex (reasoning none, summaries auto)"
	model, reasoning, summaries := parseModelLine(line)
	if model != "gpt-5.2-codex" {
		t.Fatalf("model parse failed: %q", model)
	}
	if reasoning != "none" {
		t.Fatalf("reasoning parse failed: %q", reasoning)
	}
	if summaries != "auto" {
		t.Fatalf("summaries parse failed: %q", summaries)
	}
}

func TestExtractStatusBlock(t *testing.T) {
	raw := `
╭────────────╮
│ >_ OpenAI  │
╰────────────╯

╭─────────────────────────────────────────────────────────────────╮
│  >_ OpenAI Codex (v0.88.0)                                      │
│                                                                 │
│ Visit https://chatgpt.com/codex/settings/usage for up-to-date   │
│ information on rate limits and credits                          │
│                                                                 │
│  Model:          gpt-5.2-codex (reasoning high, summaries auto) │
╰─────────────────────────────────────────────────────────────────╯
`
	block := extractStatusBlock(raw)
	if block == "" {
		t.Fatal("expected status block")
	}
	if !strings.Contains(block, "Visit https://chatgpt.com/codex/settings/usage") {
		t.Fatalf("unexpected block: %q", block)
	}
}
