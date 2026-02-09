package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	reportBeginMarker = "<<WORK_REPORT_BEGIN>>"
	reportEndMarker   = "<<WORK_REPORT_END>>"
)

type loopConfig struct {
	Enabled          bool
	DyadName         string
	Role             string
	Department       string
	ActorContainer   string
	Goal             string
	StateDir         string
	SleepInterval    time.Duration
	StartupDelay     time.Duration
	TurnTimeout      time.Duration
	MaxTurns         int
	RetryMax         int
	RetryBase        time.Duration
	SeedCriticPrompt string
	PromptLines      int
	AllowMcpStartup  bool
	CaptureMode      string
	CaptureLines     int
	StrictReport     bool
	CodexStartCmd    string
	PausePoll        time.Duration
}

type loopState struct {
	Turn             int       `json:"turn"`
	LastActorReport  string    `json:"last_actor_report,omitempty"`
	LastCriticReport string    `json:"last_critic_report,omitempty"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type turnExecutor interface {
	ActorTurn(ctx context.Context, prompt string) (string, error)
	CriticTurn(ctx context.Context, prompt string) (string, error)
}

type codexTurnExecutor struct {
	actorContainer string
	actorSession   string
	criticSession  string
	promptLines    int
	allowMcp       bool
	captureMode    string
	captureLines   int
	strictReport   bool
	startCmd       string
	readyTimeout   time.Duration
	turnTimeout    time.Duration
	pollInterval   time.Duration
}

var errCriticRequestedStop = errors.New("critic requested loop stop")

func runCriticLoop(ctx context.Context, logger *log.Logger) error {
	cfg := loadLoopConfig()
	if !cfg.Enabled {
		return nil
	}
	if strings.TrimSpace(cfg.ActorContainer) == "" {
		return errors.New("dyad loop enabled but ACTOR_CONTAINER is missing")
	}
	if err := os.MkdirAll(cfg.StateDir, 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	maybeApplyHostOwnership(cfg.StateDir)
	if err := os.MkdirAll(filepath.Join(cfg.StateDir, "reports"), 0o755); err != nil {
		return fmt.Errorf("create report dir: %w", err)
	}
	maybeApplyHostOwnership(filepath.Join(cfg.StateDir, "reports"))
	logger.Printf("dyad loop enabled: dyad=%s actor=%s state_dir=%s", cfg.DyadName, cfg.ActorContainer, cfg.StateDir)
	if cfg.StartupDelay > 0 {
		logger.Printf("dyad loop startup delay: %s", cfg.StartupDelay)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(cfg.StartupDelay):
		}
	}
	if _, err := runCommand(ctx, "docker", "inspect", cfg.ActorContainer); err != nil {
		return fmt.Errorf("actor container preflight failed: %w", err)
	}
	executor, err := newCodexTurnExecutor(ctx, cfg)
	if err != nil {
		return err
	}
	return runTurnLoop(ctx, cfg, executor, logger)
}

func runTurnLoop(ctx context.Context, cfg loopConfig, executor turnExecutor, logger *log.Logger) error {
	statePath := filepath.Join(cfg.StateDir, "loop-state.json")
	state, err := loadLoopState(statePath)
	if err != nil {
		return err
	}
	if strings.TrimSpace(state.LastCriticReport) == "" && strings.TrimSpace(cfg.SeedCriticPrompt) != "" {
		seedRaw, seedReport, err := runWithRetries(ctx, cfg, "critic-seed", func(stepCtx context.Context) (string, string, error) {
			raw, err := executor.CriticTurn(stepCtx, buildSeedCriticPrompt(cfg))
			if err != nil {
				return "", "", err
			}
			report := extractDelimitedWorkReport(raw)
			if report == "" && !cfg.StrictReport {
				report = extractWorkReport(raw)
			}
			if report == "" || criticReportLooksPlaceholder(report) {
				return raw, "", errors.New("seed critic output missing report")
			}
			return raw, report, nil
		})
		if err != nil {
			state.LastCriticReport = fallbackCriticFeedback(cfg)
			state.UpdatedAt = time.Now().UTC()
			if saveErr := saveLoopState(statePath, state); saveErr != nil {
				logger.Printf("dyad seed fallback state save warning: %v", saveErr)
			}
			logger.Printf("dyad seed critic report failed: %v; using fallback guidance", err)
		} else {
			if err := writeTurnArtifacts(cfg.StateDir, 0, "critic", strings.TrimSpace(cfg.SeedCriticPrompt), seedRaw, seedReport); err != nil {
				logger.Printf("seed critic artifact warning: %v", err)
			}
			state.LastCriticReport = seedReport
			state.UpdatedAt = time.Now().UTC()
			if err := saveLoopState(statePath, state); err != nil {
				logger.Printf("dyad seed state save warning: %v", err)
			}
			logger.Printf("dyad seed critic report initialized")
			if criticRequestsStop(seedReport) {
				logger.Printf("dyad loop stop requested by critic seed report")
				return nil
			}
		}
	}
	turn := state.Turn + 1
	failures := 0
	pausedNotified := false
	for ctx.Err() == nil {
		stopRequestedByControl, pauseRequestedByControl := readLoopControl(cfg.StateDir)
		if stopRequestedByControl {
			logger.Printf("dyad loop stop requested by control file")
			return nil
		}
		if pauseRequestedByControl {
			if !pausedNotified {
				logger.Printf("dyad loop paused by control file")
				pausedNotified = true
			}
			pauseFor := cfg.PausePoll
			if pauseFor <= 0 {
				pauseFor = 5 * time.Second
			}
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(pauseFor):
			}
			continue
		}
		pausedNotified = false
		if cfg.MaxTurns > 0 && turn > cfg.MaxTurns {
			return nil
		}
		stopRequested, err := runSingleTurn(ctx, cfg, turn, &state, executor, logger)
		if err != nil {
			if errors.Is(err, errCriticRequestedStop) {
				if err := saveLoopState(statePath, state); err != nil {
					logger.Printf("dyad state save warning: %v", err)
				}
				logger.Printf("dyad loop stopped by critic at turn %d", turn)
				return nil
			}
			failures++
			backoff := retryBackoff(cfg.RetryBase, failures)
			logger.Printf("dyad turn %d failed: %v (backoff %s)", turn, err, backoff)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(backoff):
				continue
			}
		}
		if err := saveLoopState(statePath, state); err != nil {
			logger.Printf("dyad state save warning: %v", err)
		}
		if stopRequested {
			logger.Printf("dyad loop stopped by critic at turn %d", turn)
			return nil
		}
		failures = 0
		turn++
		if cfg.SleepInterval > 0 {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(cfg.SleepInterval):
			}
		}
	}
	return nil
}

func runSingleTurn(ctx context.Context, cfg loopConfig, turn int, state *loopState, executor turnExecutor, logger *log.Logger) (bool, error) {
	actorPrompt := buildActorPrompt(cfg, turn, state.LastCriticReport)
	actorRaw, actorReport, err := runWithRetries(ctx, cfg, "actor", func(stepCtx context.Context) (string, string, error) {
		raw, err := executor.ActorTurn(stepCtx, actorPrompt)
		if err != nil {
			return "", "", err
		}
		report := extractDelimitedWorkReport(raw)
		if report == "" && !cfg.StrictReport {
			report = extractWorkReport(raw)
		}
		if report == "" || actorReportLooksPlaceholder(report) {
			return raw, "", errors.New("actor output missing report")
		}
		return raw, report, nil
	})
	if err != nil {
		return false, err
	}
	if err := writeTurnArtifacts(cfg.StateDir, turn, "actor", actorPrompt, actorRaw, actorReport); err != nil {
		logger.Printf("actor artifact warning: %v", err)
	}

	criticPrompt := buildCriticPrompt(cfg, turn, actorReport, state.LastCriticReport)
	criticRaw, criticReport, err := runWithRetries(ctx, cfg, "critic", func(stepCtx context.Context) (string, string, error) {
		raw, err := executor.CriticTurn(stepCtx, criticPrompt)
		if err != nil {
			return "", "", err
		}
		report := extractDelimitedWorkReport(raw)
		if report == "" && !cfg.StrictReport {
			report = extractWorkReport(raw)
		}
		if report == "" || criticReportLooksPlaceholder(report) {
			return raw, "", errors.New("critic output missing report")
		}
		return raw, report, nil
	})
	if err != nil {
		return false, err
	}
	if err := writeTurnArtifacts(cfg.StateDir, turn, "critic", criticPrompt, criticRaw, criticReport); err != nil {
		logger.Printf("critic artifact warning: %v", err)
	}

	state.Turn = turn
	state.LastActorReport = actorReport
	state.LastCriticReport = criticReport
	state.UpdatedAt = time.Now().UTC()
	logger.Printf("dyad turn %d complete", turn)
	if criticRequestsStop(criticReport) {
		return true, errCriticRequestedStop
	}
	return false, nil
}

func runWithRetries(ctx context.Context, cfg loopConfig, label string, fn func(context.Context) (string, string, error)) (string, string, error) {
	maxAttempts := cfg.RetryMax
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	var lastErr error
	var raw string
	var report string
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		stepCtx, cancel := context.WithTimeout(ctx, cfg.TurnTimeout)
		tmpRaw, tmpReport, err := fn(stepCtx)
		cancel()
		raw = tmpRaw
		report = tmpReport
		if err == nil {
			return raw, report, nil
		}
		lastErr = err
		if attempt == maxAttempts {
			break
		}
		select {
		case <-ctx.Done():
			return raw, report, ctx.Err()
		case <-time.After(retryBackoff(cfg.RetryBase, attempt)):
		}
	}
	return raw, report, fmt.Errorf("%s turn failed after retries: %w", label, lastErr)
}

func newCodexTurnExecutor(ctx context.Context, cfg loopConfig) (codexTurnExecutor, error) {
	sessionSuffix := sanitizeSessionName(cfg.DyadName)
	startCmd, err := interactiveCodexCommand(cfg.CodexStartCmd)
	if err != nil {
		return codexTurnExecutor{}, err
	}
	exec := codexTurnExecutor{
		actorContainer: cfg.ActorContainer,
		actorSession:   fmt.Sprintf("si-dyad-%s-actor", sessionSuffix),
		criticSession:  fmt.Sprintf("si-dyad-%s-critic", sessionSuffix),
		promptLines:    cfg.PromptLines,
		allowMcp:       cfg.AllowMcpStartup,
		captureMode:    cfg.CaptureMode,
		captureLines:   cfg.CaptureLines,
		strictReport:   cfg.StrictReport,
		startCmd:       startCmd,
		readyTimeout:   cfg.TurnTimeout / 3,
		turnTimeout:    cfg.TurnTimeout,
		pollInterval:   350 * time.Millisecond,
	}
	if exec.promptLines <= 0 {
		exec.promptLines = 3
	}
	if exec.captureMode != "main" && exec.captureMode != "alt" {
		exec.captureMode = "main"
	}
	if exec.captureLines <= 0 {
		exec.captureLines = 8000
	}
	if exec.captureLines < 500 {
		exec.captureLines = 500
	}
	if exec.captureLines > 50000 {
		exec.captureLines = 50000
	}
	if exec.readyTimeout <= 0 {
		exec.readyTimeout = 30 * time.Second
	}
	if exec.pollInterval <= 0 {
		exec.pollInterval = 350 * time.Millisecond
	}

	if _, err := runCommand(ctx, "tmux", "-V"); err != nil {
		return codexTurnExecutor{}, fmt.Errorf("tmux preflight failed in critic container: %w", err)
	}
	if _, err := runCommand(ctx, "docker", "exec", cfg.ActorContainer, "tmux", "-V"); err != nil {
		return codexTurnExecutor{}, fmt.Errorf("tmux preflight failed in actor container: %w", err)
	}
	return exec, nil
}

func (e codexTurnExecutor) ActorTurn(ctx context.Context, prompt string) (string, error) {
	if err := ensureActorContainerRunning(ctx, e.actorContainer); err != nil {
		return "", err
	}
	runner := tmuxRunner{Prefix: []string{"docker", "exec", e.actorContainer}}
	out, err := e.runTurn(ctx, runner, e.actorSession, e.startCmd, prompt, "actor")
	if err != nil {
		e.recoverSession(e.actorSession, runner, err)
	}
	return out, err
}

func (e codexTurnExecutor) CriticTurn(ctx context.Context, prompt string) (string, error) {
	runner := tmuxRunner{}
	out, err := e.runTurn(ctx, runner, e.criticSession, e.startCmd, prompt, "critic")
	if err != nil {
		e.recoverSession(e.criticSession, runner, err)
	}
	return out, err
}

func (e codexTurnExecutor) runTurn(ctx context.Context, runner tmuxRunner, session string, startCmd string, prompt string, role string) (string, error) {
	paneTarget, readyOutput, err := e.ensureInteractiveSession(ctx, runner, session, startCmd)
	if err != nil {
		return "", err
	}
	cleanReady := stripANSI(readyOutput)
	baselineReportEnd := strings.LastIndex(cleanReady, reportEndMarker)
	if err := tmuxSendKeys(ctx, runner, paneTarget, "C-u"); err != nil {
		return "", err
	}
	normalizedPrompt := normalizeInteractivePrompt(prompt)
	if err := tmuxSendLiteral(ctx, runner, paneTarget, normalizedPrompt); err != nil {
		return "", err
	}
	if err := sleepContext(ctx, 150*time.Millisecond); err != nil {
		return "", err
	}
	if err := tmuxSendKeys(ctx, runner, paneTarget, "C-m"); err != nil {
		return "", err
	}
	return e.waitForTurnCompletion(ctx, runner, paneTarget, baselineReportEnd, role)
}

func (e codexTurnExecutor) recoverSession(session string, runner tmuxRunner, cause error) {
	if !isRecoverableTurnErr(cause) {
		return
	}
	recoverCtx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	_, _ = runner.Output(recoverCtx, "kill-session", "-t", session)
}

func (e codexTurnExecutor) ensureInteractiveSession(ctx context.Context, runner tmuxRunner, session, startCmd string) (string, string, error) {
	paneTarget := session + ":0.0"
	if _, err := runner.Output(ctx, "has-session", "-t", session); err != nil {
		if _, err := runner.Output(ctx, "new-session", "-d", "-s", session, "bash", "-lc", startCmd); err != nil {
			return "", "", err
		}
	}
	_, _ = runner.Output(ctx, "set-option", "-t", session, "remain-on-exit", "off")
	if out, err := runner.Output(ctx, "display-message", "-p", "-t", paneTarget, "#{pane_dead}"); err == nil && isTmuxPaneDeadOutput(out) {
		_, _ = runner.Output(ctx, "kill-session", "-t", session)
		if _, err := runner.Output(ctx, "new-session", "-d", "-s", session, "bash", "-lc", startCmd); err != nil {
			return "", "", err
		}
		_, _ = runner.Output(ctx, "set-option", "-t", session, "remain-on-exit", "off")
	}
	_, _ = runner.Output(ctx, "resize-pane", "-t", paneTarget, "-x", "160", "-y", "60")
	output, err := e.waitForPromptReady(ctx, runner, paneTarget)
	if err != nil {
		return "", "", err
	}
	return paneTarget, output, nil
}

func (e codexTurnExecutor) waitForPromptReady(ctx context.Context, runner tmuxRunner, target string) (string, error) {
	captureOpts := statusOptions{CaptureMode: e.captureMode, CaptureLines: e.captureLines}
	deadline := time.Now().Add(e.readyTimeout)
	var lastOutput string
	for time.Now().Before(deadline) {
		output, err := tmuxCapture(ctx, runner, target, captureOpts)
		if err == nil && strings.TrimSpace(output) != "" {
			lastOutput = output
		}
		clean := stripANSI(output)
		if codexPromptReady(clean, e.promptLines, e.allowMcp) {
			return output, nil
		}
		if err := sleepContext(ctx, e.pollInterval); err != nil {
			return "", err
		}
	}
	if lastOutput == "" {
		return "", errors.New("timeout waiting for codex prompt")
	}
	return lastOutput, errors.New("timeout waiting for codex prompt")
}

func (e codexTurnExecutor) waitForTurnCompletion(ctx context.Context, runner tmuxRunner, target string, baselineReportEnd int, role string) (string, error) {
	captureOpts := statusOptions{CaptureMode: e.captureMode, CaptureLines: e.captureLines}
	deadline := time.Now().Add(e.turnTimeout)
	var lastOutput string
	submitAttempts := 1
	lastSubmit := time.Now()
	for time.Now().Before(deadline) {
		output, err := tmuxCapture(ctx, runner, target, captureOpts)
		if err == nil && strings.TrimSpace(output) != "" {
			lastOutput = output
		}
		clean := stripANSI(output)
		promptReady := codexPromptReady(clean, e.promptLines, e.allowMcp)

		report, ok := extractDelimitedWorkReportAfter(clean, baselineReportEnd)
		if ok {
			report = strings.TrimSpace(report)
			if role == "actor" && actorReportLooksPlaceholder(report) {
				ok = false
			}
			if role == "critic" && criticReportLooksPlaceholder(report) {
				ok = false
			}
		}
		if !ok && !e.strictReport {
			// Legacy fallback: try to scrape a bullet-style report from the most recent prompt segment.
			segments := parsePromptSegmentsDual(clean, output)
			for i := len(segments) - 1; i >= 0; i-- {
				seg := segments[i]
				candidate := strings.TrimSpace(extractReportLinesFromLines(seg.Raw, seg.Lines, false))
				if candidate == "" {
					continue
				}
				if role == "actor" && actorReportLooksPlaceholder(candidate) {
					continue
				}
				if role == "critic" && criticReportLooksPlaceholder(candidate) {
					continue
				}
				report = candidate
				ok = true
				break
			}
		}

		if ok && report != "" {
			// If we didn't get a delimited report from the captured output (legacy mode), normalize by appending one.
			if e.strictReport || strings.Contains(clean, reportBeginMarker) {
				return output, nil
			}
			normalizedOutput := strings.TrimSpace(output)
			if normalizedOutput != "" {
				normalizedOutput += "\n"
			}
			normalizedOutput += reportBeginMarker + "\n" + report + "\n" + reportEndMarker
			return normalizedOutput, nil
		}

		if promptReady {
			if e.strictReport {
				if strings.TrimSpace(lastOutput) == "" {
					lastOutput = output
				}
				if strings.TrimSpace(lastOutput) == "" {
					return "", errors.New("codex prompt ready but missing work report markers")
				}
				return lastOutput, errors.New("codex prompt ready but missing work report markers")
			}
			if submitAttempts < 2 && time.Since(lastSubmit) > 4*time.Second {
				_ = tmuxSendKeys(ctx, runner, target, "C-m")
				submitAttempts++
				lastSubmit = time.Now()
			}
		}
		if err := sleepContext(ctx, e.pollInterval); err != nil {
			return "", err
		}
	}
	if lastOutput == "" {
		return "", errors.New("timeout waiting for codex report")
	}
	return lastOutput, errors.New("timeout waiting for codex report")
}

type tmuxRunner struct {
	Prefix []string
}

type statusOptions struct {
	CaptureMode  string
	CaptureLines int
}

func (r tmuxRunner) Output(ctx context.Context, args ...string) (string, error) {
	if len(args) == 0 {
		return "", errors.New("tmux args required")
	}
	if len(r.Prefix) == 0 {
		return runCommand(ctx, "tmux", args...)
	}
	cmdArgs := make([]string, 0, len(r.Prefix)-1+1+len(args))
	cmdArgs = append(cmdArgs, r.Prefix[1:]...)
	cmdArgs = append(cmdArgs, "tmux")
	cmdArgs = append(cmdArgs, args...)
	return runCommand(ctx, r.Prefix[0], cmdArgs...)
}

func runCommand(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := strings.TrimSpace(stdout.String())
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			if out != "" {
				return out, fmt.Errorf("%w: %s: %s", err, msg, out)
			}
			return out, fmt.Errorf("%w: %s", err, msg)
		}
		if out != "" {
			return out, fmt.Errorf("%w: %s", err, out)
		}
		return out, err
	}
	if errText := strings.TrimSpace(stderr.String()); errText != "" {
		out = strings.TrimSpace(strings.TrimSpace(out + "\n" + errText))
	}
	return out, nil
}

func ensureActorContainerRunning(ctx context.Context, container string) error {
	container = strings.TrimSpace(container)
	if container == "" {
		return errors.New("actor container required")
	}
	out, err := runCommand(ctx, "docker", "inspect", "-f", "{{.State.Running}}", container)
	if err == nil && isTrueString(out) {
		return nil
	}
	if _, startErr := runCommand(ctx, "docker", "start", container); startErr != nil {
		if err != nil {
			return fmt.Errorf("actor not running (%v) and start failed: %w", err, startErr)
		}
		return fmt.Errorf("start actor container: %w", startErr)
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		runningOut, inspectErr := runCommand(ctx, "docker", "inspect", "-f", "{{.State.Running}}", container)
		if inspectErr == nil && isTrueString(runningOut) {
			return nil
		}
		if err := sleepContext(ctx, 250*time.Millisecond); err != nil {
			return err
		}
	}
	return fmt.Errorf("actor container %s did not reach running state after restart", container)
}

func isTrueString(raw string) bool {
	raw = strings.TrimSpace(strings.ToLower(raw))
	return raw == "true" || raw == "1" || raw == "yes"
}

func isRecoverableTurnErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "timeout waiting for codex"):
		return true
	case strings.Contains(msg, "context deadline exceeded"):
		return true
	case strings.Contains(msg, "tmux"):
		return true
	case strings.Contains(msg, "pane"):
		return true
	case strings.Contains(msg, "session"):
		return true
	case strings.Contains(msg, "no such container"):
		return true
	case strings.Contains(msg, "is not running"):
		return true
	default:
		return false
	}
}

func readLoopControl(stateDir string) (stop bool, pause bool) {
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		return false, false
	}
	stopPath := filepath.Join(stateDir, "control.stop")
	pausePath := filepath.Join(stateDir, "control.pause")
	_, stopErr := os.Stat(stopPath)
	_, pauseErr := os.Stat(pausePath)
	return stopErr == nil, pauseErr == nil
}

func interactiveCodexCommand(custom string) (string, error) {
	custom = strings.TrimSpace(custom)
	if custom != "" {
		lower := strings.ToLower(custom)
		if strings.Contains(lower, "codex exec") || strings.Contains(lower, "codex-exec") {
			return "", errors.New("DYAD_CODEX_START_CMD must not use `codex exec`; dyads require interactive Codex")
		}
		return custom, nil
	}
	return strings.TrimSpace("export TERM=xterm-256color COLORTERM=truecolor COLUMNS=160 LINES=60 HOME=/root CODEX_HOME=/root/.codex; cd /workspace 2>/dev/null || true; codex --dangerously-bypass-approvals-and-sandbox"), nil
}

func normalizeInteractivePrompt(prompt string) string {
	prompt = strings.ReplaceAll(prompt, "\r\n", "\n")
	prompt = strings.ReplaceAll(prompt, "\r", "\n")
	prompt = strings.Join(strings.Fields(prompt), " ")
	return strings.TrimSpace(prompt)
}

func tmuxSendKeys(ctx context.Context, runner tmuxRunner, target string, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	args := append([]string{"send-keys", "-t", target}, keys...)
	_, err := runner.Output(ctx, args...)
	return err
}

func tmuxSendLiteral(ctx context.Context, runner tmuxRunner, target, text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	_, err := runner.Output(ctx, "send-keys", "-t", target, "-l", text)
	return err
}

func tmuxCapture(ctx context.Context, runner tmuxRunner, target string, opts statusOptions) (string, error) {
	start := "-"
	if opts.CaptureLines > 0 {
		start = fmt.Sprintf("-%d", opts.CaptureLines)
	}
	switch opts.CaptureMode {
	case "alt":
		return runner.Output(ctx, "capture-pane", "-t", target, "-p", "-J", "-S", start, "-a", "-q")
	case "main":
		return runner.Output(ctx, "capture-pane", "-t", target, "-p", "-J", "-S", start)
	default:
		return "", fmt.Errorf("unsupported tmux capture mode: %s", opts.CaptureMode)
	}
}

func sanitizeSessionName(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if out == "" {
		return "unknown"
	}
	return out
}

func codexPromptReady(output string, promptLines int, allowMcpStartup bool) bool {
	lower := strings.ToLower(output)
	if !allowMcpStartup {
		if strings.Contains(lower, "starting mcp") || strings.Contains(lower, "mcp startup") {
			return false
		}
	}
	lines := strings.Split(output, "\n")
	if promptLines <= 0 {
		promptLines = 3
	}
	seen := 0
	for i := len(lines) - 1; i >= 0 && seen < promptLines*4; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		seen++
		if strings.HasPrefix(line, "›") {
			// Treat Codex as "ready" only when the prompt is empty (no user input on the prompt line).
			rest := strings.TrimSpace(strings.TrimPrefix(line, "›"))
			if rest == "" {
				return true
			}
		}
	}
	return false
}

type promptSegment struct {
	Prompt string
	Lines  []string
	Raw    []string
}

func parsePromptSegmentsDual(clean, raw string) []promptSegment {
	cleanLines := strings.Split(clean, "\n")
	rawLines := strings.Split(raw, "\n")
	if len(rawLines) < len(cleanLines) {
		pad := make([]string, len(cleanLines)-len(rawLines))
		rawLines = append(rawLines, pad...)
	}
	if len(cleanLines) < len(rawLines) {
		pad := make([]string, len(rawLines)-len(cleanLines))
		cleanLines = append(cleanLines, pad...)
	}
	segments := make([]promptSegment, 0, 8)
	var current *promptSegment
	for i, line := range cleanLines {
		rawLine := rawLines[i]
		trimmed := strings.TrimLeft(line, " ")
		if strings.HasPrefix(trimmed, "›") {
			if current != nil {
				segments = append(segments, *current)
			}
			prompt := strings.TrimSpace(strings.TrimPrefix(trimmed, "›"))
			current = &promptSegment{Prompt: prompt}
			continue
		}
		if current != nil {
			current.Lines = append(current.Lines, line)
			current.Raw = append(current.Raw, rawLine)
		}
	}
	if current != nil {
		segments = append(segments, *current)
	}
	return segments
}

func extractReportLinesFromLines(rawLines, cleanLines []string, ansi bool) string {
	max := len(cleanLines)
	if len(rawLines) < max {
		max = len(rawLines)
	}
	type block struct {
		raw   []string
		clean []string
	}
	var blocks []block
	var current block
	inReport := false
	workedLineRaw := ""
	workedLineClean := ""
	for i := 0; i < max; i++ {
		raw := strings.TrimRight(rawLines[i], " \t")
		clean := strings.TrimRight(cleanLines[i], " \t")
		cleanCore := strings.TrimLeft(clean, " ")
		if strings.Contains(strings.ToLower(cleanCore), "worked for") {
			workedLineRaw = raw
			workedLineClean = clean
		}
		if strings.HasPrefix(cleanCore, "• ") {
			inReport = true
			current.raw = append(current.raw, raw)
			current.clean = append(current.clean, clean)
			continue
		}
		if !inReport {
			continue
		}
		if strings.TrimSpace(clean) == "" {
			if len(current.raw) > 0 {
				blocks = append(blocks, current)
				current = block{}
			}
			inReport = false
			continue
		}
		if strings.HasPrefix(clean, "  ") {
			current.raw = append(current.raw, raw)
			current.clean = append(current.clean, clean)
			continue
		}
		core := strings.TrimSpace(clean)
		if strings.HasPrefix(core, "⚠") || strings.HasPrefix(core, "Tip:") || strings.HasPrefix(core, "›") {
			if len(current.raw) > 0 {
				blocks = append(blocks, current)
			}
			current = block{}
			break
		}
		if strings.HasPrefix(core, "• Starting MCP") || strings.HasPrefix(core, "• Starting") {
			if len(current.raw) > 0 {
				blocks = append(blocks, current)
			}
			current = block{}
			break
		}
		current.raw = append(current.raw, raw)
		current.clean = append(current.clean, clean)
	}
	if len(current.raw) > 0 {
		blocks = append(blocks, current)
	}
	for i := len(blocks) - 1; i >= 0; i-- {
		block := blocks[i]
		if len(block.raw) == 0 {
			continue
		}
		if isTransientReport(block.clean) {
			continue
		}
		out := block.clean
		workedLine := workedLineClean
		if ansi {
			out = block.raw
			workedLine = workedLineRaw
		}
		for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
			out = out[:len(out)-1]
		}
		if workedLine != "" && !containsLine(out, workedLine) {
			out = append(out, workedLine)
		}
		return strings.Join(out, "\n")
	}
	return ""
}

func isTransientReport(lines []string) bool {
	if len(lines) == 0 {
		return true
	}
	head := strings.TrimSpace(lines[0])
	if strings.HasPrefix(head, "• Working") || strings.Contains(head, "esc to interrupt") {
		return true
	}
	if strings.HasPrefix(head, "• Starting MCP") {
		return true
	}
	return false
}

func containsLine(lines []string, needle string) bool {
	for _, line := range lines {
		if line == needle {
			return true
		}
	}
	return false
}

func extractDelimitedWorkReport(output string) string {
	clean := strings.TrimSpace(strings.ReplaceAll(output, "\r\n", "\n"))
	if clean == "" {
		return ""
	}
	start := strings.LastIndex(clean, reportBeginMarker)
	end := strings.LastIndex(clean, reportEndMarker)
	if start >= 0 && end > start {
		body := strings.TrimSpace(clean[start+len(reportBeginMarker) : end])
		if body != "" {
			return body
		}
	}
	return ""
}

func extractDelimitedWorkReportAfter(output string, afterEnd int) (string, bool) {
	clean := strings.TrimSpace(strings.ReplaceAll(output, "\r\n", "\n"))
	if clean == "" {
		return "", false
	}
	end := strings.LastIndex(clean, reportEndMarker)
	if end < 0 || end <= afterEnd {
		return "", false
	}
	start := strings.LastIndex(clean[:end], reportBeginMarker)
	if start < 0 || start <= afterEnd {
		return "", false
	}
	body := strings.TrimSpace(clean[start+len(reportBeginMarker) : end])
	if body == "" {
		return "", false
	}
	return body, true
}

var (
	ansiCSI = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	ansiOSC = regexp.MustCompile(`\x1b\][^\x07]*(\x07|\x1b\\)`)
)

func stripANSI(s string) string {
	if s == "" {
		return s
	}
	out := ansiCSI.ReplaceAllString(s, "")
	return ansiOSC.ReplaceAllString(out, "")
}

func isTmuxPaneDeadOutput(out string) bool {
	out = strings.TrimSpace(out)
	return out == "1" || strings.EqualFold(out, "true") || strings.EqualFold(out, "yes")
}

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func buildActorPrompt(cfg loopConfig, turn int, criticFeedback string) string {
	if strings.TrimSpace(criticFeedback) == "" {
		criticFeedback = "No prior critic feedback. Start with a concrete plan and execution report."
	}
	return strings.TrimSpace(fmt.Sprintf(`
You are the ACTOR in dyad "%s". Turn %d.

Objective:
%s

Critic feedback to apply:
%s

Do one focused work iteration and output ONLY:
%s
Summary:
- ...
Changes:
- ...
Validation:
- ...
Open Questions:
- ...
Next Step for Critic:
- ...
%s
`, cfg.DyadName, turn, cfg.Goal, criticFeedback, reportBeginMarker, reportEndMarker))
}

func buildCriticPrompt(cfg loopConfig, turn int, actorReport string, lastCriticReport string) string {
	if strings.TrimSpace(lastCriticReport) == "" {
		lastCriticReport = "No prior critic report."
	}
	return strings.TrimSpace(fmt.Sprintf(`
You are the CRITIC in dyad "%s". Turn %d.

Objective:
%s

Previous critic report:
%s

Actor report to review:
%s

Produce a strict critique and the next actionable instructions for the actor. Output ONLY:
%s
Assessment:
- ...
Risks:
- ...
Required Fixes:
- ...
Verification Steps:
- ...
Next Actor Prompt:
- ...
Continue Loop:
- yes|no
%s
`, cfg.DyadName, turn, cfg.Goal, lastCriticReport, actorReport, reportBeginMarker, reportEndMarker))
}

func buildSeedCriticPrompt(cfg loopConfig) string {
	seed := strings.TrimSpace(cfg.SeedCriticPrompt)
	return strings.TrimSpace(fmt.Sprintf(`
You are the CRITIC in dyad "%s". This is seed turn 0.

Objective:
%s

Seed instruction from user:
%s

Output ONLY:
%s
Assessment:
- ...
Risks:
- ...
Required Fixes:
- ...
Verification Steps:
- ...
Next Actor Prompt:
- ...
Continue Loop:
- yes|no
%s
`, cfg.DyadName, cfg.Goal, seed, reportBeginMarker, reportEndMarker))
}

func fallbackCriticFeedback(cfg loopConfig) string {
	seed := strings.TrimSpace(cfg.SeedCriticPrompt)
	if seed != "" {
		return seed
	}
	return "Provide one concrete, low-risk task for the actor and require a substantive work report."
}

func extractWorkReport(output string) string {
	clean := strings.TrimSpace(strings.ReplaceAll(output, "\r\n", "\n"))
	if clean == "" {
		return ""
	}
	start := strings.LastIndex(clean, reportBeginMarker)
	end := strings.LastIndex(clean, reportEndMarker)
	if start >= 0 && end > start {
		body := strings.TrimSpace(clean[start+len(reportBeginMarker) : end])
		if body != "" {
			return body
		}
	}
	return clean
}

func writeTurnArtifacts(stateDir string, turn int, member string, prompt string, raw string, report string) error {
	member = strings.TrimSpace(member)
	if member == "" {
		return errors.New("member required")
	}
	base := filepath.Join(stateDir, "reports", fmt.Sprintf("turn-%04d-%s", turn, member))
	if err := os.WriteFile(base+".prompt.txt", []byte(prompt+"\n"), 0o644); err != nil {
		return err
	}
	maybeApplyHostOwnership(base + ".prompt.txt")
	if err := os.WriteFile(base+".raw.txt", []byte(strings.TrimSpace(raw)+"\n"), 0o644); err != nil {
		return err
	}
	maybeApplyHostOwnership(base + ".raw.txt")
	if err := os.WriteFile(base+".report.md", []byte(strings.TrimSpace(report)+"\n"), 0o644); err != nil {
		return err
	}
	maybeApplyHostOwnership(base + ".report.md")
	return nil
}

func loadLoopState(path string) (loopState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return loopState{}, nil
		}
		return loopState{}, err
	}
	var state loopState
	if err := json.Unmarshal(data, &state); err != nil {
		return loopState{}, err
	}
	return state, nil
}

func saveLoopState(path string, state loopState) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	maybeApplyHostOwnership(dir)
	tmp, err := os.CreateTemp(dir, "state-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	payload, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(payload, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	maybeApplyHostOwnership(path)
	return nil
}

func loadLoopConfig() loopConfig {
	dyad := envOr("DYAD_NAME", "unknown")
	member := strings.ToLower(envOr("DYAD_MEMBER", "critic"))
	defaultEnabled := member == "critic" && strings.TrimSpace(os.Getenv("ACTOR_CONTAINER")) != ""
	enabled := envBool("DYAD_LOOP_ENABLED", defaultEnabled)
	stateDir := strings.TrimSpace(os.Getenv("DYAD_STATE_DIR"))
	if stateDir == "" {
		stateDir = filepath.Join("/workspace", ".si", "dyad", dyad)
	}
	goal := strings.TrimSpace(os.Getenv("DYAD_LOOP_GOAL"))
	if goal == "" {
		goal = "Continuously improve the task outcome through actor execution and critic review."
	}
	return loopConfig{
		Enabled:          enabled,
		DyadName:         dyad,
		Role:             envOr("ROLE", "critic"),
		Department:       envOr("DEPARTMENT", "unknown"),
		ActorContainer:   strings.TrimSpace(os.Getenv("ACTOR_CONTAINER")),
		Goal:             goal,
		StateDir:         stateDir,
		SleepInterval:    envDurationSeconds("DYAD_LOOP_SLEEP_SECONDS", 20),
		StartupDelay:     envDurationSeconds("DYAD_LOOP_STARTUP_DELAY_SECONDS", 2),
		TurnTimeout:      envDurationSeconds("DYAD_LOOP_TURN_TIMEOUT_SECONDS", 900),
		MaxTurns:         envInt("DYAD_LOOP_MAX_TURNS", 0),
		RetryMax:         envInt("DYAD_LOOP_RETRY_MAX", 3),
		RetryBase:        envDurationSeconds("DYAD_LOOP_RETRY_BASE_SECONDS", 2),
		SeedCriticPrompt: strings.TrimSpace(os.Getenv("DYAD_LOOP_SEED_CRITIC_PROMPT")),
		PromptLines:      envInt("DYAD_LOOP_PROMPT_LINES", 3),
		AllowMcpStartup:  envBool("DYAD_LOOP_ALLOW_MCP_STARTUP", false),
		CaptureMode:      strings.TrimSpace(strings.ToLower(envOr("DYAD_LOOP_TMUX_CAPTURE", "main"))),
		CaptureLines:     envInt("DYAD_LOOP_TMUX_CAPTURE_LINES", 8000),
		StrictReport:     envBool("DYAD_LOOP_STRICT_REPORT", true),
		CodexStartCmd:    strings.TrimSpace(os.Getenv("DYAD_CODEX_START_CMD")),
		PausePoll:        envDurationSeconds("DYAD_LOOP_PAUSE_POLL_SECONDS", 5),
	}
}

func envBool(key string, def bool) bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if raw == "" {
		return def
	}
	switch raw {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}

func envInt(key string, def int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return value
}

func envDurationSeconds(key string, defSeconds int) time.Duration {
	value := envInt(key, defSeconds)
	if value < 0 {
		value = 0
	}
	return time.Duration(value) * time.Second
}

func retryBackoff(base time.Duration, attempt int) time.Duration {
	if base <= 0 {
		base = time.Second
	}
	if attempt < 1 {
		attempt = 1
	}
	multiplier := math.Pow(2, float64(attempt-1))
	d := time.Duration(float64(base) * multiplier)
	max := 30 * time.Second
	if d > max {
		return max
	}
	return d
}

func criticRequestsStop(report string) bool {
	lines := strings.Split(strings.ToLower(strings.TrimSpace(report)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.Contains(line, "continue loop: no") || strings.Contains(line, "continue_loop=no") || strings.Contains(line, "loop_continue=false") {
			return true
		}
		if strings.Contains(line, "stop loop: yes") || strings.Contains(line, "stop_loop=true") || strings.Contains(line, "#stop_loop") {
			return true
		}
	}
	return false
}

func actorReportLooksPlaceholder(report string) bool {
	norm := normalizeReportText(report)
	if strings.Contains(norm, "summary: changes: validation: open questions: next step for critic:") {
		return true
	}
	if strings.Contains(norm, "summary: - ...") && strings.Contains(norm, "changes: - ...") && strings.Contains(norm, "validation: - ...") {
		return true
	}
	if strings.Contains(norm, "<at least") || strings.Contains(norm, "<specific") || strings.Contains(norm, "<what you") {
		return true
	}
	if strings.Count(norm, "...") >= 2 && reportBulletCount(report) <= 2 {
		return true
	}
	if strings.Contains(norm, "summary:") && strings.Contains(norm, "changes:") && reportBulletCount(report) < 2 {
		return true
	}
	return false
}

func criticReportLooksPlaceholder(report string) bool {
	norm := normalizeReportText(report)
	if strings.Contains(norm, "assessment: risks: required fixes: verification steps: next actor prompt: continue loop: yes|no") {
		return true
	}
	if strings.Contains(norm, "assessment: - ...") &&
		strings.Contains(norm, "required fixes: - ...") &&
		strings.Contains(norm, "verification steps: - ...") &&
		strings.Contains(norm, "next actor prompt: - ...") {
		return true
	}
	if strings.Contains(norm, "continue loop: <yes|no>") || strings.Contains(norm, "<clear actionable") || strings.Contains(norm, "<single concrete") {
		return true
	}
	if strings.Count(norm, "...") >= 2 && reportBulletCount(report) <= 3 {
		return true
	}
	if strings.Contains(norm, "assessment:") && strings.Contains(norm, "required fixes:") && reportBulletCount(report) < 3 {
		return true
	}
	return false
}

func normalizeReportText(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.Join(strings.Fields(s), " ")
	return s
}

func reportBulletCount(report string) int {
	lines := strings.Split(strings.ReplaceAll(report, "\r\n", "\n"), "\n")
	count := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") || strings.HasPrefix(trimmed, "• ") {
			count++
		}
	}
	return count
}

func maybeApplyHostOwnership(path string) {
	uid, gid, ok := hostOwnership()
	if !ok {
		return
	}
	_ = os.Chown(path, uid, gid)
}

func hostOwnership() (int, int, bool) {
	uid := envInt("SI_HOST_UID", 0)
	gid := envInt("SI_HOST_GID", 0)
	if uid <= 0 || gid <= 0 {
		return 0, 0, false
	}
	return uid, gid, true
}
