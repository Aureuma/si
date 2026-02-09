package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeTurnExecutor struct {
	actorPrompts  []string
	criticPrompts []string
}

func (f *fakeTurnExecutor) ActorTurn(_ context.Context, prompt string) (string, error) {
	f.actorPrompts = append(f.actorPrompts, prompt)
	turn := len(f.actorPrompts)
	if turn > 1 {
		want := fmt.Sprintf("CRITIC REPORT TURN %d", turn-1)
		if !strings.Contains(prompt, want) {
			return "", fmt.Errorf("actor prompt missing previous critic report %q", want)
		}
	}
	report := fmt.Sprintf(`Summary:
- ACTOR REPORT TURN %d
Changes:
- none
Validation:
- none
Open Questions:
- none
Next Step for Critic:
- proceed`, turn)
	return "prefix\n" + reportBeginMarker + "\n" + report + "\n" + reportEndMarker + "\nsuffix", nil
}

func (f *fakeTurnExecutor) CriticTurn(_ context.Context, prompt string) (string, error) {
	f.criticPrompts = append(f.criticPrompts, prompt)
	turn := len(f.criticPrompts)
	want := fmt.Sprintf("ACTOR REPORT TURN %d", turn)
	if !strings.Contains(prompt, want) {
		return "", fmt.Errorf("critic prompt missing actor report %q", want)
	}
	report := fmt.Sprintf(`Assessment:
- CRITIC REPORT TURN %d
Risks:
- none
Required Fixes:
- none
Verification Steps:
- none
Next Actor Prompt:
- proceed
Continue Loop: yes`, turn)
	return reportBeginMarker + "\n" + report + "\n" + reportEndMarker, nil
}

func TestExtractWorkReport(t *testing.T) {
	input := "noise\n" + reportBeginMarker + "\nhello\nworld\n" + reportEndMarker + "\nextra"
	if got := extractWorkReport(input); got != "hello\nworld" {
		t.Fatalf("unexpected report parse: %q", got)
	}
	if got := extractWorkReport("  plain output  "); got != "plain output" {
		t.Fatalf("expected fallback plain output, got %q", got)
	}
}

func TestRunTurnLoopMultiTurnClosedFeedback(t *testing.T) {
	tmp := t.TempDir()
	cfg := loopConfig{
		Enabled:       true,
		DyadName:      "testdyad",
		Goal:          "ship reliable code",
		StateDir:      tmp,
		SleepInterval: 0,
		StartupDelay:  0,
		TurnTimeout:   5 * time.Second,
		MaxTurns:      3,
		RetryMax:      1,
		RetryBase:     time.Millisecond,
	}
	exec := &fakeTurnExecutor{}
	logger := log.New(ioDiscard{}, "", 0)
	if err := os.MkdirAll(filepath.Join(tmp, "reports"), 0o700); err != nil {
		t.Fatalf("mkdir reports: %v", err)
	}
	if err := runTurnLoop(context.Background(), cfg, exec, logger); err != nil {
		t.Fatalf("runTurnLoop: %v", err)
	}
	if len(exec.actorPrompts) != 3 || len(exec.criticPrompts) != 3 {
		t.Fatalf("unexpected turn counts actor=%d critic=%d", len(exec.actorPrompts), len(exec.criticPrompts))
	}
	state, err := loadLoopState(filepath.Join(tmp, "loop-state.json"))
	if err != nil {
		t.Fatalf("loadLoopState: %v", err)
	}
	if state.Turn != 3 {
		t.Fatalf("expected state turn 3, got %d", state.Turn)
	}
	if !strings.Contains(state.LastActorReport, "ACTOR REPORT TURN 3") {
		t.Fatalf("unexpected last actor report: %q", state.LastActorReport)
	}
	if !strings.Contains(state.LastCriticReport, "CRITIC REPORT TURN 3") {
		t.Fatalf("unexpected last critic report: %q", state.LastCriticReport)
	}
	for i := 1; i <= 3; i++ {
		actorPath := filepath.Join(tmp, "reports", fmt.Sprintf("turn-%04d-actor.report.md", i))
		criticPath := filepath.Join(tmp, "reports", fmt.Sprintf("turn-%04d-critic.report.md", i))
		if _, err := os.Stat(actorPath); err != nil {
			t.Fatalf("missing actor report artifact: %v", err)
		}
		if _, err := os.Stat(criticPath); err != nil {
			t.Fatalf("missing critic report artifact: %v", err)
		}
	}
}

func TestCriticRequestsStop(t *testing.T) {
	if !criticRequestsStop("Continue Loop: no") {
		t.Fatalf("expected stop request to be detected")
	}
	if !criticRequestsStop("Assessment: ok\n#STOP_LOOP") {
		t.Fatalf("expected hash stop marker to be detected")
	}
	if criticRequestsStop("Continue Loop: yes") {
		t.Fatalf("did not expect stop for continue=yes")
	}
}

func TestReportPlaceholderDetectionRequiresSections(t *testing.T) {
	if !actorReportLooksPlaceholder("Summary:") {
		t.Fatalf("expected actor report with only Summary to be placeholder")
	}
	if !criticReportLooksPlaceholder("Assessment:") {
		t.Fatalf("expected critic report with only Assessment to be placeholder")
	}
	if actorReportLooksPlaceholder(`Summary:
- ok
Changes:
- none
Validation:
- none
Open Questions:
- none
Next Step for Critic:
- proceed`) {
		t.Fatalf("did not expect complete actor report to be placeholder")
	}
	if criticReportLooksPlaceholder(`Assessment:
- ok
Risks:
- none
Required Fixes:
- none
Verification Steps:
- none
Next Actor Prompt:
- proceed
Continue Loop: yes`) {
		t.Fatalf("did not expect complete critic report to be placeholder")
	}
}

type fakeSeedStopExecutor struct {
	actorTurns int
}

func (f *fakeSeedStopExecutor) ActorTurn(_ context.Context, prompt string) (string, error) {
	f.actorTurns++
	if !strings.Contains(prompt, "CRITIC REPORT TURN 0") {
		return "", fmt.Errorf("actor prompt missing seed critic report")
	}
	return reportBeginMarker + "\nSummary:\n- ACTOR REPORT TURN 1\nChanges:\n- none\nValidation:\n- none\nOpen Questions:\n- none\nNext Step for Critic:\n- proceed\n" + reportEndMarker, nil
}

func (f *fakeSeedStopExecutor) CriticTurn(_ context.Context, prompt string) (string, error) {
	if strings.Contains(prompt, "Seed critic message") {
		return reportBeginMarker + "\nAssessment:\n- CRITIC REPORT TURN 0\nRisks:\n- none\nRequired Fixes:\n- none\nVerification Steps:\n- none\nNext Actor Prompt:\n- proceed\nContinue Loop: yes\n" + reportEndMarker, nil
	}
	return reportBeginMarker + "\nAssessment:\n- CRITIC REPORT TURN 1\nRisks:\n- none\nRequired Fixes:\n- none\nVerification Steps:\n- none\nNext Actor Prompt:\n- proceed\nContinue Loop: no\n" + reportEndMarker, nil
}

func TestRunTurnLoopSeedAndCriticStop(t *testing.T) {
	tmp := t.TempDir()
	cfg := loopConfig{
		Enabled:          true,
		DyadName:         "seedstop",
		Goal:             "test",
		StateDir:         tmp,
		SleepInterval:    0,
		StartupDelay:     0,
		TurnTimeout:      5 * time.Second,
		MaxTurns:         0,
		RetryMax:         1,
		RetryBase:        time.Millisecond,
		SeedCriticPrompt: "Seed critic message",
	}
	exec := &fakeSeedStopExecutor{}
	logger := log.New(ioDiscard{}, "", 0)
	if err := os.MkdirAll(filepath.Join(tmp, "reports"), 0o700); err != nil {
		t.Fatalf("mkdir reports: %v", err)
	}
	if err := runTurnLoop(context.Background(), cfg, exec, logger); err != nil {
		t.Fatalf("runTurnLoop: %v", err)
	}
	if exec.actorTurns != 1 {
		t.Fatalf("expected exactly one actor turn, got %d", exec.actorTurns)
	}
	state, err := loadLoopState(filepath.Join(tmp, "loop-state.json"))
	if err != nil {
		t.Fatalf("loadLoopState: %v", err)
	}
	if state.Turn != 1 {
		t.Fatalf("expected state turn 1, got %d", state.Turn)
	}
	if !strings.Contains(state.LastCriticReport, "Continue Loop: no") {
		t.Fatalf("expected critic stop report to be persisted, got %q", state.LastCriticReport)
	}
	seedArtifact := filepath.Join(tmp, "reports", "turn-0000-critic.report.md")
	if _, err := os.Stat(seedArtifact); err != nil {
		t.Fatalf("missing seed critic artifact: %v", err)
	}
}

func TestRunTurnLoopHonorsPriorCriticStopOnRestart(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "reports"), 0o700); err != nil {
		t.Fatalf("mkdir reports: %v", err)
	}
	// Persist a stopped state as if a prior run converged.
	statePath := filepath.Join(tmp, "loop-state.json")
	if err := saveLoopState(statePath, loopState{
		Turn:             7,
		LastCriticReport: "Assessment: done\nContinue Loop: no\n",
		UpdatedAt:        time.Now().UTC(),
	}); err != nil {
		t.Fatalf("saveLoopState: %v", err)
	}
	cfg := loopConfig{
		Enabled:       true,
		DyadName:      "restartstop",
		Goal:          "test",
		StateDir:      tmp,
		SleepInterval: 0,
		StartupDelay:  0,
		TurnTimeout:   2 * time.Second,
		MaxTurns:      0,
		RetryMax:      1,
		RetryBase:     time.Millisecond,
	}
	exec := &countingExecutor{}
	logger := log.New(ioDiscard{}, "", 0)
	if err := runTurnLoop(context.Background(), cfg, exec, logger); err != nil {
		t.Fatalf("runTurnLoop: %v", err)
	}
	if exec.actorTurns != 0 || exec.criticTurns != 0 {
		t.Fatalf("expected zero turns after prior stop, got actor=%d critic=%d", exec.actorTurns, exec.criticTurns)
	}
}

func TestReadLoopControl(t *testing.T) {
	tmp := t.TempDir()
	stop, pause := readLoopControl(tmp)
	if stop || pause {
		t.Fatalf("expected no control flags by default, got stop=%v pause=%v", stop, pause)
	}
	if err := os.WriteFile(filepath.Join(tmp, "control.pause"), []byte("1\n"), 0o644); err != nil {
		t.Fatalf("write pause control: %v", err)
	}
	stop, pause = readLoopControl(tmp)
	if stop || !pause {
		t.Fatalf("expected pause only, got stop=%v pause=%v", stop, pause)
	}
	if err := os.WriteFile(filepath.Join(tmp, "control.stop"), []byte("1\n"), 0o644); err != nil {
		t.Fatalf("write stop control: %v", err)
	}
	stop, pause = readLoopControl(tmp)
	if !stop || !pause {
		t.Fatalf("expected stop+pause, got stop=%v pause=%v", stop, pause)
	}
}

func TestRecoverableTurnErrors(t *testing.T) {
	cases := []string{
		"timeout waiting for codex report",
		"context deadline exceeded",
		"tmux: can't find session",
		"no such container",
		"container is not running",
	}
	for _, tc := range cases {
		if !isRecoverableTurnErr(errors.New(tc)) {
			t.Fatalf("expected recoverable for %q", tc)
		}
	}
	if isRecoverableTurnErr(errors.New("unexpected parser mismatch")) {
		t.Fatalf("did not expect arbitrary parser mismatch to be recoverable")
	}
}

type countingExecutor struct {
	actorTurns  int
	criticTurns int
}

func (c *countingExecutor) ActorTurn(_ context.Context, _ string) (string, error) {
	c.actorTurns++
	return reportBeginMarker + "\nSummary:\n- ACTOR\nChanges:\n- none\nValidation:\n- none\nOpen Questions:\n- none\nNext Step for Critic:\n- proceed\n" + reportEndMarker, nil
}

func (c *countingExecutor) CriticTurn(_ context.Context, _ string) (string, error) {
	c.criticTurns++
	return reportBeginMarker + "\nAssessment:\n- CRITIC\nRisks:\n- none\nRequired Fixes:\n- none\nVerification Steps:\n- none\nNext Actor Prompt:\n- proceed\nContinue Loop: yes\n" + reportEndMarker, nil
}

func TestRunTurnLoopControlStopFile(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "reports"), 0o700); err != nil {
		t.Fatalf("mkdir reports: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "control.stop"), []byte("1\n"), 0o644); err != nil {
		t.Fatalf("write stop control: %v", err)
	}
	cfg := loopConfig{
		Enabled:       true,
		DyadName:      "controlstop",
		Goal:          "test",
		StateDir:      tmp,
		SleepInterval: 0,
		StartupDelay:  0,
		TurnTimeout:   2 * time.Second,
		MaxTurns:      3,
		RetryMax:      1,
		RetryBase:     time.Millisecond,
		PausePoll:     100 * time.Millisecond,
	}
	exec := &countingExecutor{}
	logger := log.New(ioDiscard{}, "", 0)
	if err := runTurnLoop(context.Background(), cfg, exec, logger); err != nil {
		t.Fatalf("runTurnLoop: %v", err)
	}
	if exec.actorTurns != 0 || exec.criticTurns != 0 {
		t.Fatalf("expected zero turns under control.stop, got actor=%d critic=%d", exec.actorTurns, exec.criticTurns)
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
