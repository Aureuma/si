package main

import (
	"context"
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
	report := fmt.Sprintf("ACTOR REPORT TURN %d", turn)
	return "prefix\n" + reportBeginMarker + "\n" + report + "\n" + reportEndMarker + "\nsuffix", nil
}

func (f *fakeTurnExecutor) CriticTurn(_ context.Context, prompt string) (string, error) {
	f.criticPrompts = append(f.criticPrompts, prompt)
	turn := len(f.criticPrompts)
	want := fmt.Sprintf("ACTOR REPORT TURN %d", turn)
	if !strings.Contains(prompt, want) {
		return "", fmt.Errorf("critic prompt missing actor report %q", want)
	}
	report := fmt.Sprintf("CRITIC REPORT TURN %d", turn)
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
	if state.LastActorReport != "ACTOR REPORT TURN 3" {
		t.Fatalf("unexpected last actor report: %q", state.LastActorReport)
	}
	if state.LastCriticReport != "CRITIC REPORT TURN 3" {
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

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
