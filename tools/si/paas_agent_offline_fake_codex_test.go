package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractPaasAgentWorkReport(t *testing.T) {
	raw := "fake-codex ready\n<<WORK_REPORT_BEGIN>>\nSummary:\n- member: actor\n<<WORK_REPORT_END>>\n"
	got := extractPaasAgentWorkReport(raw)
	if !strings.Contains(got, "Summary:") || !strings.Contains(got, "member: actor") {
		t.Fatalf("unexpected extracted report: %q", got)
	}
}

func TestExecutePaasAgentActionDeferredWithoutOfflineMode(t *testing.T) {
	plan := paasAgentRuntimeAdapterPlan{
		Ready:  true,
		Prompt: "test prompt",
	}
	got, err := executePaasAgentAction(plan)
	if err != nil {
		t.Fatalf("execute action: %v", err)
	}
	if got.Mode != paasAgentExecutionModeDeferred || got.Executed {
		t.Fatalf("expected deferred non-executed action, got %#v", got)
	}
}

func TestExecutePaasAgentActionOfflineFakeCodex(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("repoRoot: %v", err)
	}
	fakeCodexPath := filepath.Join(root, "tools", "dyad", "fake-codex.sh")
	t.Setenv(paasAgentOfflineFakeCodexEnvKey, "true")
	t.Setenv(paasAgentOfflineFakeCodexCmdEnvKey, quoteSingle(fakeCodexPath))

	plan := paasAgentRuntimeAdapterPlan{
		Ready:  true,
		Prompt: "offline smoke remediation",
	}
	got, err := executePaasAgentAction(plan)
	if err != nil {
		t.Fatalf("execute action: %v", err)
	}
	if got.Mode != paasAgentExecutionModeOfflineFakeCodex || !got.Executed {
		t.Fatalf("expected offline fake-codex execution, got %#v", got)
	}
	if !strings.Contains(got.Note, "member: actor") {
		t.Fatalf("expected deterministic actor report note, got %q", got.Note)
	}
}
