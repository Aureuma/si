package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
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
	CodexCommand     string
	SeedCriticPrompt string
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
	codexCommand   string
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
	executor := codexTurnExecutor{
		actorContainer: cfg.ActorContainer,
		codexCommand:   cfg.CodexCommand,
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
			raw, err := executor.CriticTurn(stepCtx, strings.TrimSpace(cfg.SeedCriticPrompt))
			if err != nil {
				return "", "", err
			}
			report := extractWorkReport(raw)
			if report == "" {
				return raw, "", errors.New("seed critic output missing report")
			}
			return raw, report, nil
		})
		if err != nil {
			return err
		}
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
	turn := state.Turn + 1
	failures := 0
	for ctx.Err() == nil {
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
		report := extractWorkReport(raw)
		if report == "" {
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
		report := extractWorkReport(raw)
		if report == "" {
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

func (e codexTurnExecutor) ActorTurn(ctx context.Context, prompt string) (string, error) {
	cmd := []string{
		"exec", "-i", e.actorContainer, "bash", "-lc",
		codexExecShellScript(e.codexCommand, prompt),
	}
	return runCommand(ctx, "docker", cmd...)
}

func (e codexTurnExecutor) CriticTurn(ctx context.Context, prompt string) (string, error) {
	return runCommand(ctx, "bash", "-lc", codexExecShellScript(e.codexCommand, prompt))
}

func runCommand(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text != "" {
			return text, fmt.Errorf("%w: %s", err, text)
		}
		return text, err
	}
	return text, nil
}

func codexExecShellScript(codexCommand string, prompt string) string {
	codexCommand = strings.TrimSpace(codexCommand)
	if codexCommand == "" {
		codexCommand = "codex --dangerously-bypass-approvals-and-sandbox exec"
	}
	return fmt.Sprintf(
		"export TERM=xterm-256color COLORTERM=truecolor HOME=/root CODEX_HOME=/root/.codex; cd /workspace 2>/dev/null || true; %s %s",
		codexCommand,
		shellSingleQuote(prompt),
	)
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
Changes:
Validation:
Open Questions:
Next Step for Critic:
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
Risks:
Required Fixes:
Verification Steps:
Next Actor Prompt:
Continue Loop: yes|no
%s
`, cfg.DyadName, turn, cfg.Goal, lastCriticReport, actorReport, reportBeginMarker, reportEndMarker))
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
		CodexCommand:     envOr("DYAD_LOOP_CODEX_COMMAND", "codex --dangerously-bypass-approvals-and-sandbox exec"),
		SeedCriticPrompt: strings.TrimSpace(os.Getenv("DYAD_LOOP_SEED_CRITIC_PROMPT")),
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

func shellSingleQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
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
