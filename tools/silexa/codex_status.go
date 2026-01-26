package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	shared "silexa/agents/shared/docker"
)

type codexStatus struct {
	Source            string  `json:"source,omitempty"`
	Raw               string  `json:"raw,omitempty"`
	Model             string  `json:"model,omitempty"`
	ReasoningEffort   string  `json:"reasoning_effort,omitempty"`
	Summaries         string  `json:"summaries,omitempty"`
	Directory         string  `json:"directory,omitempty"`
	Approval          string  `json:"approval,omitempty"`
	Sandbox           string  `json:"sandbox,omitempty"`
	Agents            string  `json:"agents,omitempty"`
	AccountEmail      string  `json:"account_email,omitempty"`
	AccountPlan       string  `json:"account_plan,omitempty"`
	Session           string  `json:"session,omitempty"`
	ContextLeftPct    float64 `json:"context_left_pct,omitempty"`
	ContextUsed       string  `json:"context_used,omitempty"`
	ContextTotal      string  `json:"context_total,omitempty"`
	FiveHourLeftPct   float64 `json:"five_hour_left_pct,omitempty"`
	FiveHourReset     string  `json:"five_hour_reset,omitempty"`
	FiveHourRemaining int     `json:"five_hour_remaining_minutes,omitempty"`
	WeeklyLeftPct     float64 `json:"weekly_left_pct,omitempty"`
	WeeklyReset       string  `json:"weekly_reset,omitempty"`
	WeeklyRemaining   int     `json:"weekly_remaining_minutes,omitempty"`
}

type statusOptions struct {
	Debug               bool
	KeepTmux            bool
	CaptureMode         string
	StatusOnly          bool
	StatusAttempts      int
	StatusWindow        time.Duration
	StatusDeadline      time.Duration
	RetryDelay          time.Duration
	PromptLines         int
	RequireContextHint  bool
	AllowMcpStartup     bool
	LockTimeout         time.Duration
	LockStaleAfter      time.Duration
	CleanupStaleSession bool
	TmuxPrefix          string
}

const tmuxStatusPrefix = "si-codex-status-"

func cmdCodexStatus(args []string) {
fs := flag.NewFlagSet("status", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output json")
	showRaw := fs.Bool("raw", false, "include raw status output")
	timeout := fs.Duration("timeout", 25*time.Second, "status timeout")
	tmuxCapture := fs.String("tmux-capture", "main", "tmux capture mode: alt|main")
	tmuxKeep := fs.Bool("tmux-keep", false, "keep tmux session after run")
	statusOnly := fs.Bool("status-only", false, "return only the /status box output")
	debug := fs.Bool("debug", false, "debug tmux status capture")
	statusAttempts := fs.Int("status-attempts", 4, "max /status attempts")
	statusWindow := fs.Duration("status-window", 3*time.Second, "window to wait after sending /status")
	statusDeadline := fs.Duration("status-deadline", 30*time.Second, "max time to wait for /status output")
	retryDelay := fs.Duration("retry-delay", 6*time.Second, "delay between /status retries")
	promptLines := fs.Int("prompt-lines", 12, "lines to scan for prompt readiness")
	requireContext := fs.Bool("require-context", true, "require context hint before sending /status")
	allowMcp := fs.Bool("allow-mcp-startup", false, "allow sending /status during MCP startup")
	lockTimeout := fs.Duration("lock-timeout", 2*time.Second, "lock wait time")
	lockStale := fs.Duration("lock-stale", 5*time.Minute, "lock staleness before removal")
	cleanupSessions := fs.Bool("cleanup-stale-sessions", true, "cleanup stale tmux sessions")
	_ = fs.Parse(args)
	if fs.NArg() < 1 {
		printUsage("usage: si status <name> [--json] [--raw] [--status-only]")
		return
	}
	name := fs.Arg(0)
	containerName := codexContainerName(name)

	opts := statusOptions{
		Debug:               *debug,
		KeepTmux:            *tmuxKeep,
		CaptureMode:         strings.ToLower(strings.TrimSpace(*tmuxCapture)),
		StatusOnly:          *statusOnly,
		StatusAttempts:      *statusAttempts,
		StatusWindow:        *statusWindow,
		StatusDeadline:      *statusDeadline,
		RetryDelay:          *retryDelay,
		PromptLines:         *promptLines,
		RequireContextHint:  *requireContext,
		AllowMcpStartup:     *allowMcp,
		LockTimeout:         *lockTimeout,
		LockStaleAfter:      *lockStale,
		CleanupStaleSession: *cleanupSessions,
		TmuxPrefix:          tmuxStatusPrefix,
	}
	if opts.CaptureMode == "" {
		opts.CaptureMode = "auto"
	}
	if opts.StatusAttempts < 1 {
		opts.StatusAttempts = 1
	}
	if opts.PromptLines < 4 {
		opts.PromptLines = 4
	}
	if opts.StatusWindow <= 0 {
		opts.StatusWindow = 3 * time.Second
	}
	if opts.StatusDeadline <= 0 {
		opts.StatusDeadline = 30 * time.Second
	}
	if opts.RetryDelay <= 0 {
		opts.RetryDelay = 6 * time.Second
	}
	if opts.LockTimeout <= 0 {
		opts.LockTimeout = 2 * time.Second
	}
	if opts.LockStaleAfter <= 0 {
		opts.LockStaleAfter = 5 * time.Minute
	}
	switch opts.CaptureMode {
	case "alt", "main":
	default:
		fatal(fmt.Errorf("invalid tmux capture mode: %s", opts.CaptureMode))
	}

	unlock, lockErr := acquireStatusLock(name, opts)
	if lockErr != nil {
		fatal(lockErr)
	}
	defer unlock()
	fail := func(err error) {
		unlock()
		fatal(err)
	}

	client, err := shared.NewClient()
	if err != nil {
		fail(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	id, _, err := client.ContainerByName(ctx, containerName)
	if err != nil {
		fail(err)
	}
	if id == "" {
		fail(fmt.Errorf("codex container %s not found", containerName))
	}

	parsed := codexStatus{}
	raw, err := fetchCodexStatus(ctx, client, id, opts)
	if err != nil {
		fail(err)
	}
	if strings.TrimSpace(raw) != "" {
		parseInput := raw
		if block := extractStatusBlock(raw); block != "" {
			parseInput = block
			if opts.StatusOnly {
				raw = block
			}
		} else if opts.StatusOnly {
			raw = strings.TrimSpace(raw)
		}
		parsed = parseCodexStatus(parseInput)
		parsed.Source = "status"
	}
	if *showRaw {
		parsed.Raw = raw
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(parsed); err != nil {
			fail(err)
		}
		return
	}

	printCodexStatus(parsed)
}

func fetchCodexStatus(ctx context.Context, client *shared.Client, containerID string, opts statusOptions) (string, error) {
	if err := ensureTmuxAvailable(); err != nil {
		return "", err
	}
	if opts.CleanupStaleSession {
		cleanupStaleTmuxSessions(ctx, opts.TmuxPrefix, 30*time.Minute, opts)
	}
	tmuxCtx, tmuxCancel := context.WithTimeout(ctx, 45*time.Second)
	defer tmuxCancel()
	return fetchCodexStatusViaTmux(tmuxCtx, containerID, opts)
}

func fetchCodexStatusViaTmux(ctx context.Context, containerID string, opts statusOptions) (string, error) {
	session := fmt.Sprintf("%s%s-%d", opts.TmuxPrefix, containerID, time.Now().UnixNano())
	paneTarget := session + ":0.0"
	cmd := buildTmuxCodexCommand(containerID)
	if opts.KeepTmux {
		cmd = cmd + "; exec bash"
	}
	_, _ = tmuxOutput(ctx, "kill-session", "-t", session)
	if _, err := tmuxOutput(ctx, "new-session", "-d", "-s", session, "bash", "-lc", cmd); err != nil {
		return "", err
	}
	if opts.Debug {
		debugf(opts, "tmux session: %s", session)
	}
	defer func() {
		if opts.KeepTmux {
			return
		}
		_, _ = tmuxOutput(context.Background(), "kill-session", "-t", session)
	}()

	_, _ = tmuxOutput(ctx, "resize-pane", "-t", paneTarget, "-x", "160", "-y", "60")

	var lastOutput string
	statusSent := false
	exitSent := false
	statusDeadline := time.Time{}
	lastStatusSend := time.Time{}
	statusAttempts := 0
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			break
		}
		output, err := tmuxCapture(ctx, paneTarget, opts)
		if err == nil && strings.TrimSpace(output) != "" {
			lastOutput = output
		} else if err != nil && opts.Debug {
			debugf(opts, "tmux capture error: %v", err)
		}
		lower := strings.ToLower(output)

		if strings.Contains(lower, "allow codex to work in this folder without asking for approval") ||
			strings.Contains(lower, "allow codex to work in this folder") ||
			strings.Contains(lower, "ask me to approve edits and commands") ||
			strings.Contains(lower, "require approval of edits and commands") {
			_ = tmuxSendKeys(ctx, paneTarget, "2", "Enter")
		}
		if strings.Contains(lower, "press enter to continue") ||
			strings.Contains(lower, "press enter to confirm") ||
			strings.Contains(lower, "try new model") {
			_ = tmuxSendKeys(ctx, paneTarget, "Enter")
		}

		if !statusSent && shouldSendStatus(output, opts) {
			debugf(opts, "sending /status")
			_ = tmuxSendKeys(ctx, paneTarget, "C-u")
			_ = tmuxSendLiteral(ctx, paneTarget, "/status")
			_ = tmuxSendKeys(ctx, paneTarget, "Enter")
			statusSent = true
			lastStatusSend = time.Now()
			statusAttempts = 1
			statusDeadline = time.Now().Add(opts.StatusDeadline)
			if snapshot, ok := waitForStatusSnapshot(ctx, paneTarget, opts); ok {
				lastOutput = snapshot
				_ = tmuxSendLiteral(ctx, paneTarget, "/exit")
				_ = tmuxSendKeys(ctx, paneTarget, "Enter")
				exitSent = true
				break
			}
		}

		if statusSent && (strings.Contains(lower, "context window") || strings.Contains(lower, "5h limit") || strings.Contains(lower, "weekly limit")) {
			_ = tmuxSendLiteral(ctx, paneTarget, "/exit")
			_ = tmuxSendKeys(ctx, paneTarget, "Enter")
			exitSent = true
			break
		}
		if statusSent && !statusDeadline.IsZero() && time.Since(lastStatusSend) > opts.RetryDelay && isPromptReady(output, opts) && statusAttempts < opts.StatusAttempts {
			debugf(opts, "retrying /status attempt %d", statusAttempts+1)
			_ = tmuxSendKeys(ctx, paneTarget, "C-u")
			_ = tmuxSendLiteral(ctx, paneTarget, "/status")
			_ = tmuxSendKeys(ctx, paneTarget, "Enter")
			lastStatusSend = time.Now()
			statusAttempts++
			if snapshot, ok := waitForStatusSnapshot(ctx, paneTarget, opts); ok {
				lastOutput = snapshot
				_ = tmuxSendLiteral(ctx, paneTarget, "/exit")
				_ = tmuxSendKeys(ctx, paneTarget, "Enter")
				exitSent = true
				break
			}
		}
		if statusSent && !statusDeadline.IsZero() && time.Now().After(statusDeadline) {
			break
		}

		time.Sleep(500 * time.Millisecond)
	}
	if !exitSent {
		_ = tmuxSendLiteral(ctx, paneTarget, "/exit")
		_ = tmuxSendKeys(ctx, paneTarget, "Enter")
	}
	time.Sleep(1 * time.Second)
	output, err := tmuxCapture(ctx, paneTarget, opts)
	if err == nil && strings.TrimSpace(output) != "" {
		lastOutput = output
	} else if err != nil && opts.Debug {
		debugf(opts, "final tmux capture error: %v", err)
	}
	if strings.TrimSpace(lastOutput) == "" {
		return "", errors.New("tmux capture empty")
	}
	return lastOutput, nil
}

func buildTmuxCodexCommand(containerID string) string {
	inner := "export TERM=xterm-256color COLORTERM=truecolor COLUMNS=160 LINES=60 HOME=/home/si CODEX_HOME=/home/si/.codex; codex"
	base := fmt.Sprintf("docker exec -it %s bash -lc %q", containerID, inner)
	return fmt.Sprintf("%s || sudo -n %s", base, base)
}

func shouldSendStatus(output string, opts statusOptions) bool {
	lower := strings.ToLower(output)
	if !strings.Contains(lower, "openai codex") {
		return false
	}
	if opts.RequireContextHint && !strings.Contains(lower, "context left") {
		return false
	}
	return isPromptReady(output, opts)
}

func isPromptReady(output string, opts statusOptions) bool {
	lines := strings.Split(output, "\n")
	seen := 0
	for i := len(lines) - 1; i >= 0 && seen < opts.PromptLines; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		seen++
		lower := strings.ToLower(line)
		if !opts.AllowMcpStartup && (strings.Contains(lower, "starting mcp") || strings.Contains(lower, "mcp startup")) {
			return false
		}
		if strings.HasPrefix(line, "›") {
			return true
		}
	}
	return false
}

func tmuxSendKeys(ctx context.Context, target string, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	_, err := tmuxOutput(ctx, append([]string{"send-keys", "-t", target}, keys...)...)
	return err
}

func tmuxSendLiteral(ctx context.Context, target string, text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	_, err := tmuxOutput(ctx, "send-keys", "-t", target, "-l", text)
	return err
}

func tmuxCapture(ctx context.Context, target string, opts statusOptions) (string, error) {
	switch opts.CaptureMode {
	case "alt":
		out, err := tmuxOutput(ctx, "capture-pane", "-t", target, "-p", "-J", "-S", "-", "-a", "-q")
		return out, err
	case "main":
		return tmuxOutput(ctx, "capture-pane", "-t", target, "-p", "-J", "-S", "-")
	}
	return "", fmt.Errorf("unsupported tmux capture mode: %s", opts.CaptureMode)
}

func waitForStatusSnapshot(ctx context.Context, target string, opts statusOptions) (string, bool) {
	deadline := time.Now().Add(opts.StatusWindow)
	var lastOutput string
	for time.Now().Before(deadline) {
		output, err := tmuxCapture(ctx, target, opts)
		if err == nil && strings.TrimSpace(output) != "" {
			lastOutput = output
		}
		lower := strings.ToLower(output)
		if strings.Contains(lower, "5h limit") || strings.Contains(lower, "weekly limit") || strings.Contains(lower, "context window") || strings.Contains(lower, "account:") {
			return output, true
		}
		time.Sleep(200 * time.Millisecond)
	}
	if strings.TrimSpace(lastOutput) == "" {
		return "", false
	}
	return lastOutput, false
}

func tmuxOutput(ctx context.Context, args ...string) (string, error) {
	if len(args) == 0 {
		return "", errors.New("tmux args required")
	}
	cmd := exec.CommandContext(ctx, "tmux", args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return stdout.String(), fmt.Errorf("%w: %s", err, msg)
		}
		return stdout.String(), err
	}
	return stdout.String(), nil
}

func ensureTmuxAvailable() error {
	if _, err := exec.LookPath("tmux"); err != nil {
		return fmt.Errorf("tmux not found in PATH: %w", err)
	}
	return nil
}

func debugf(opts statusOptions, format string, args ...interface{}) {
	if !opts.Debug {
		return
	}
	msg := fmt.Sprintf(format, args...)
	_, _ = fmt.Fprintf(os.Stderr, "%s %s\n", styleDim("[codex-status]"), msg)
}

func acquireStatusLock(name string, opts statusOptions) (func(), error) {
	return acquireCodexLock("status", name, opts.LockTimeout, opts.LockStaleAfter)
}

func acquireCodexLock(kind, name string, timeout, staleAfter time.Duration) (func(), error) {
	lockPath := filepath.Join(os.TempDir(), fmt.Sprintf("si-codex-%s-%s.lock", kind, name))
	deadline := time.Now().Add(timeout)
	for {
		f, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			_, _ = fmt.Fprintf(f, "pid=%d time=%s\n", os.Getpid(), time.Now().Format(time.RFC3339))
			_ = f.Close()
			return func() { _ = os.Remove(lockPath) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		info, statErr := os.Stat(lockPath)
		if statErr == nil && time.Since(info.ModTime()) > staleAfter {
			_ = os.Remove(lockPath)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("another %s capture is running (lock: %s)", kind, lockPath)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func cleanupStaleTmuxSessions(ctx context.Context, prefix string, maxAge time.Duration, opts statusOptions) {
	if strings.TrimSpace(prefix) == "" {
		return
	}
	out, err := tmuxOutput(ctx, "list-sessions", "-F", "#{session_name} #{session_created}")
	if err != nil {
		return
	}
	now := time.Now()
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		name := parts[0]
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		created, parseErr := strconv.ParseInt(parts[1], 10, 64)
		if parseErr != nil {
			continue
		}
		if now.Sub(time.Unix(created, 0)) > maxAge {
			_, _ = tmuxOutput(ctx, "kill-session", "-t", name)
		}
	}
}

func extractStatusBlock(raw string) string {
	lines := strings.Split(raw, "\n")
	anchor := -1
	for i, line := range lines {
		if strings.Contains(line, "Visit https://chatgpt.com/codex/settings/usage") {
			anchor = i
			break
		}
	}
	if anchor == -1 {
		return ""
	}
	start := anchor
	for start >= 0 {
		if strings.Contains(lines[start], "╭") || strings.Contains(lines[start], "┌") {
			break
		}
		start--
	}
	end := anchor
	for end < len(lines) {
		if strings.Contains(lines[end], "╰") || strings.Contains(lines[end], "└") {
			break
		}
		end++
	}
	if start < 0 || end >= len(lines) || start > end {
		return ""
	}
	return strings.Join(lines[start:end+1], "\n")
}

func parseCodexStatus(raw string) codexStatus {
	clean := stripANSI(raw)
	clean = stripInvisibles(clean)
	lines := strings.Split(clean, "\n")
	out := codexStatus{}
	for _, line := range lines {
		trim := strings.TrimSpace(stripBoxChars(line))
		if trim == "" {
			continue
		}
		lower := strings.ToLower(trim)
		switch {
		case strings.HasPrefix(lower, "model:"):
			model, reasoning, summaries := parseModelLine(trim)
			out.Model = model
			out.ReasoningEffort = reasoning
			out.Summaries = summaries
		case strings.HasPrefix(lower, "directory:"):
			out.Directory = strings.TrimSpace(strings.TrimPrefix(trim, "Directory:"))
		case strings.HasPrefix(lower, "approval:"):
			out.Approval = strings.TrimSpace(strings.TrimPrefix(trim, "Approval:"))
		case strings.HasPrefix(lower, "sandbox:"):
			out.Sandbox = strings.TrimSpace(strings.TrimPrefix(trim, "Sandbox:"))
		case strings.HasPrefix(lower, "agents.md:"):
			out.Agents = strings.TrimSpace(strings.TrimPrefix(trim, "Agents.md:"))
		case strings.HasPrefix(lower, "account:"):
			email, plan := parseAccountLine(trim)
			out.AccountEmail = email
			out.AccountPlan = plan
		case strings.HasPrefix(lower, "session:"):
			out.Session = strings.TrimSpace(strings.TrimPrefix(trim, "Session:"))
		case strings.HasPrefix(lower, "context window:"):
			pct, used, total := parseContextLine(trim)
			out.ContextLeftPct = pct
			out.ContextUsed = used
			out.ContextTotal = total
		case strings.Contains(lower, "limit:"):
			limitLabel := strings.TrimSpace(strings.SplitN(trim, "limit:", 2)[0])
			pct, reset := parseLimitLine(trim)
			if strings.Contains(strings.ToLower(limitLabel), "week") {
				out.WeeklyLeftPct = pct
				out.WeeklyReset = reset
			} else if strings.Contains(strings.ToLower(limitLabel), "5h") || strings.Contains(strings.ToLower(limitLabel), "5 h") || strings.Contains(strings.ToLower(limitLabel), "5hour") || strings.Contains(strings.ToLower(limitLabel), "5 hour") {
				out.FiveHourLeftPct = pct
				out.FiveHourReset = reset
				if mins := parseLimitMinutes(limitLabel); mins > 0 && pct >= 0 {
					out.FiveHourRemaining = int(math.Round(float64(mins) * pct / 100.0))
				}
			} else if strings.Contains(strings.ToLower(limitLabel), "hour") || strings.Contains(strings.ToLower(limitLabel), "h") {
				out.FiveHourLeftPct = pct
				out.FiveHourReset = reset
				if mins := parseLimitMinutes(limitLabel); mins > 0 && pct >= 0 {
					out.FiveHourRemaining = int(math.Round(float64(mins) * pct / 100.0))
				}
			}
		}
	}
	return out
}

func printCodexStatus(s codexStatus) {
	fmt.Println(styleHeading("Codex status:"))
	if s.Source != "" {
		fmt.Printf("  %s %s\n", styleSection("Source:"), styleArg(s.Source))
	}
	if s.AccountEmail != "" {
		if s.AccountPlan != "" {
			fmt.Printf("  %s %s (%s)\n", styleSection("Account:"), styleArg(s.AccountEmail), styleArg(s.AccountPlan))
		} else {
			fmt.Printf("  %s %s\n", styleSection("Account:"), styleArg(s.AccountEmail))
		}
	}
	if s.Model != "" {
		if s.ReasoningEffort != "" {
			fmt.Printf("  %s %s (reasoning %s)\n", styleSection("Model:"), styleArg(s.Model), styleArg(s.ReasoningEffort))
		} else {
			fmt.Printf("  %s %s\n", styleSection("Model:"), styleArg(s.Model))
		}
	}
	if s.Session != "" {
		fmt.Printf("  %s %s\n", styleSection("Session:"), styleArg(s.Session))
	}
	if s.ContextLeftPct >= 0 {
		if s.ContextUsed != "" && s.ContextTotal != "" {
			fmt.Printf("  %s %.0f%% left (%s / %s)\n", styleSection("Context:"), s.ContextLeftPct, s.ContextUsed, s.ContextTotal)
		} else {
			fmt.Printf("  %s %.0f%% left\n", styleSection("Context:"), s.ContextLeftPct)
		}
	}
	if s.FiveHourLeftPct >= 0 {
		if s.FiveHourReset != "" {
			fmt.Printf("  %s %.0f%% left (resets %s)\n", styleSection("5h limit:"), s.FiveHourLeftPct, s.FiveHourReset)
		} else {
			fmt.Printf("  %s %.0f%% left\n", styleSection("5h limit:"), s.FiveHourLeftPct)
		}
	}
	if s.WeeklyLeftPct >= 0 {
		if s.WeeklyReset != "" {
			fmt.Printf("  %s %.0f%% left (resets %s)\n", styleSection("Weekly limit:"), s.WeeklyLeftPct, s.WeeklyReset)
		} else {
			fmt.Printf("  %s %.0f%% left\n", styleSection("Weekly limit:"), s.WeeklyLeftPct)
		}
	}
}

func parseModelLine(line string) (string, string, string) {
	model := ""
	reasoning := ""
	summaries := ""
	modelRe := regexp.MustCompile(`(?i)\bmodel\b\s*:\s*([A-Za-z0-9._:-]+)`)
	if match := modelRe.FindStringSubmatch(line); len(match) == 2 {
		model = match[1]
	}
	reasonRe := regexp.MustCompile(`(?i)\breasoning\b[^A-Za-z0-9]+([A-Za-z0-9._-]+)`)
	if match := reasonRe.FindStringSubmatch(line); len(match) == 2 {
		reasoning = match[1]
	}
	sumRe := regexp.MustCompile(`(?i)\bsummaries?\b[^A-Za-z0-9]+([A-Za-z0-9._-]+)`)
	if match := sumRe.FindStringSubmatch(line); len(match) == 2 {
		summaries = match[1]
	}
	return model, reasoning, summaries
}

func parseAccountLine(line string) (string, string) {
	emailRe := regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	email := ""
	plan := ""
	if match := emailRe.FindString(line); match != "" {
		email = match
	}
	planRe := regexp.MustCompile(`\(([^)]+)\)`)
	if match := planRe.FindStringSubmatch(line); len(match) == 2 {
		plan = strings.TrimSpace(match[1])
	}
	return email, plan
}

func parseContextLine(line string) (float64, string, string) {
	percentRe := regexp.MustCompile(`(\d+(?:\.\d+)?)\s*%\s*left`)
	pct := -1.0
	if match := percentRe.FindStringSubmatch(line); len(match) == 2 {
		pct, _ = strconv.ParseFloat(match[1], 64)
	}
	usageRe := regexp.MustCompile(`\(([^)]+)\)`)
	used := ""
	total := ""
	if match := usageRe.FindStringSubmatch(line); len(match) == 2 {
		parts := strings.Split(match[1], "/")
		if len(parts) == 2 {
			used = strings.TrimSpace(parts[0])
			total = strings.TrimSpace(parts[1])
		}
	}
	return pct, used, total
}

func parseLimitLine(line string) (float64, string) {
	percentRe := regexp.MustCompile(`(\d+(?:\.\d+)?)\s*%\s*left`)
	resetRe := regexp.MustCompile(`(?i)resets\s+([0-9:]+)`)
	pct := -1.0
	reset := ""
	if match := percentRe.FindStringSubmatch(line); len(match) == 2 {
		pct, _ = strconv.ParseFloat(match[1], 64)
	}
	if match := resetRe.FindStringSubmatch(line); len(match) == 2 {
		reset = match[1]
	}
	return pct, reset
}

func parseLimitMinutes(label string) int {
	lower := strings.ToLower(label)
	hoursRe := regexp.MustCompile(`(\d+)\s*h`)
	minsRe := regexp.MustCompile(`(\d+)\s*m`)
	if match := hoursRe.FindStringSubmatch(lower); len(match) == 2 {
		if h, err := strconv.Atoi(match[1]); err == nil {
			return h * 60
		}
	}
	if match := minsRe.FindStringSubmatch(lower); len(match) == 2 {
		if m, err := strconv.Atoi(match[1]); err == nil {
			return m
		}
	}
	return 0
}

func stripBoxChars(s string) string {
	if s == "" {
		return s
	}
	return strings.TrimFunc(s, func(r rune) bool {
		switch r {
		case '│', '╭', '╮', '╰', '╯', '─', '┌', '┐', '└', '┘', '|':
			return true
		default:
			return r == ' '
		}
	})
}

func stripANSI(s string) string {
	if s == "" {
		return s
	}
	reCSI := regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	reOSC := regexp.MustCompile(`\x1b\][^\x07]*(\x07|\x1b\\)`)
	out := reCSI.ReplaceAllString(s, "")
	out = reOSC.ReplaceAllString(out, "")
	return out
}

func stripInvisibles(s string) string {
	if s == "" {
		return s
	}
	return strings.Map(func(r rune) rune {
		switch r {
		case 0, '\u200b', '\u200c', '\u200d', '\ufeff':
			return -1
		default:
			return r
		}
	}, s)
}

func looksLikePanic(raw string) bool {
	lower := strings.ToLower(raw)
	return strings.Contains(lower, "panicked") || strings.Contains(lower, "wrapping.rs")
}

type appServerRequest struct {
	JSONRPC string      `json:"jsonrpc,omitempty"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type appServerEnvelope struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *appServerError `json:"error"`
}

type appServerError struct {
	Message string `json:"message"`
}

type appRateLimitsResponse struct {
	RateLimits appRateLimitSnapshot `json:"rateLimits"`
}

type appRateLimitSnapshot struct {
	Primary   *appRateLimitWindow `json:"primary"`
	Secondary *appRateLimitWindow `json:"secondary"`
}

type appRateLimitWindow struct {
	UsedPercent        int    `json:"usedPercent"`
	WindowDurationMins *int64 `json:"windowDurationMins"`
	ResetsAt           *int64 `json:"resetsAt"`
}

type appAccountResponse struct {
	Account            *appAccount `json:"account"`
	RequiresOpenaiAuth bool        `json:"requiresOpenaiAuth"`
}

type appAccount struct {
	Type     string `json:"type"`
	Email    string `json:"email"`
	PlanType string `json:"planType"`
}

type appConfigResponse struct {
	Config appConfig `json:"config"`
}

type appConfig struct {
	Model                *string `json:"model"`
	ModelReasoningEffort *string `json:"model_reasoning_effort"`
}

const (
	appServerInitID       = 1
	appServerRateLimitsID = 2
	appServerAccountID    = 3
	appServerConfigID     = 4
)

type appUsage struct {
	RemainingPct           float64
	RemainingMinutes       int
	WeeklyRemainingPct     float64
	WeeklyRemainingMinutes int
	Email                  string
	Model                  string
	ReasoningEffort        string
	PlanType               string
	PrimaryReset           string
	SecondaryReset         string
}

func fetchCodexAppServerStatus(ctx context.Context, client *shared.Client, containerID string) (codexStatus, error) {
	input := buildAppServerInput()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	env := []string{"HOME=/home/si", "CODEX_HOME=/home/si/.codex", "TERM=xterm-256color"}
	cmd := []string{"codex", "app-server"}
	err := client.Exec(ctx, containerID, cmd, shared.ExecOptions{Env: env}, bytes.NewReader(input), &stdout, &stderr)
	raw := strings.TrimSpace(stdout.String())
	if stderr.Len() > 0 {
		raw = strings.TrimSpace(raw + "\n" + strings.TrimSpace(stderr.String()))
	}
	if err != nil {
		if raw != "" {
			return codexStatus{}, fmt.Errorf("%w: %s", err, raw)
		}
		return codexStatus{}, err
	}
	totalLimitMin := 300
	if val := strings.TrimSpace(os.Getenv("CODEX_PLAN_LIMIT_MINUTES")); val != "" {
		if parsed, parseErr := strconv.Atoi(val); parseErr == nil && parsed > 0 {
			totalLimitMin = parsed
		}
	}
	usage, parseErr := parseAppServerUsageOutput(raw, totalLimitMin)
	if parseErr != nil {
		return codexStatus{}, parseErr
	}
	return codexStatus{
		Source:            "app-server",
		Model:             usage.Model,
		ReasoningEffort:   usage.ReasoningEffort,
		AccountEmail:      usage.Email,
		AccountPlan:       usage.PlanType,
		FiveHourLeftPct:   usage.RemainingPct,
		FiveHourRemaining: usage.RemainingMinutes,
		FiveHourReset:     usage.PrimaryReset,
		WeeklyLeftPct:     usage.WeeklyRemainingPct,
		WeeklyRemaining:   usage.WeeklyRemainingMinutes,
		WeeklyReset:       usage.SecondaryReset,
	}, nil
}

func buildAppServerInput() []byte {
	reqs := []appServerRequest{
		{
			JSONRPC: "2.0",
			ID:      appServerInitID,
			Method:  "initialize",
			Params: map[string]interface{}{
				"clientInfo": map[string]string{
					"name":    "si",
					"version": "0.0.0",
				},
			},
		},
		{
			JSONRPC: "2.0",
			ID:      appServerRateLimitsID,
			Method:  "account/rateLimits/read",
			Params:  nil,
		},
		{
			JSONRPC: "2.0",
			ID:      appServerAccountID,
			Method:  "account/read",
			Params:  map[string]interface{}{},
		},
		{
			JSONRPC: "2.0",
			ID:      appServerConfigID,
			Method:  "config/read",
			Params:  map[string]interface{}{},
		},
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, req := range reqs {
		_ = enc.Encode(req)
	}
	return buf.Bytes()
}

func parseAppServerUsageOutput(raw string, totalLimitMin int) (appUsage, error) {
	usage := appUsage{RemainingPct: -1, WeeklyRemainingPct: -1}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return usage, errors.New("empty app-server output")
	}
	var rateResp appRateLimitsResponse
	var accountResp appAccountResponse
	var configResp appConfigResponse
	var rateSeen bool
	var rateErr error

	scanner := bufio.NewScanner(strings.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 4096), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var envelope appServerEnvelope
		if err := json.Unmarshal([]byte(line), &envelope); err != nil {
			continue
		}
		id, ok := parseAppServerID(envelope.ID)
		if !ok {
			continue
		}
		if envelope.Error != nil {
			if id == appServerRateLimitsID {
				msg := strings.TrimSpace(envelope.Error.Message)
				if msg == "" {
					msg = "rate limits request failed"
				}
				rateErr = errors.New(msg)
			}
			continue
		}
		switch id {
		case appServerRateLimitsID:
			if err := json.Unmarshal(envelope.Result, &rateResp); err == nil {
				rateSeen = true
			}
		case appServerAccountID:
			_ = json.Unmarshal(envelope.Result, &accountResp)
		case appServerConfigID:
			_ = json.Unmarshal(envelope.Result, &configResp)
		}
	}
	if err := scanner.Err(); err != nil {
		return usage, err
	}
	if rateErr != nil {
		return usage, rateErr
	}
	if !rateSeen {
		return usage, errors.New("rate limits missing")
	}
	if rateResp.RateLimits.Primary != nil {
		remainingPct, remainingMinutes := windowUsage(rateResp.RateLimits.Primary, totalLimitMin)
		usage.RemainingPct = remainingPct
		usage.RemainingMinutes = remainingMinutes
		usage.PrimaryReset = formatReset(rateResp.RateLimits.Primary.ResetsAt)
	}
	if rateResp.RateLimits.Secondary != nil {
		remainingPct, remainingMinutes := windowUsage(rateResp.RateLimits.Secondary, 0)
		usage.WeeklyRemainingPct = remainingPct
		usage.WeeklyRemainingMinutes = remainingMinutes
		usage.SecondaryReset = formatReset(rateResp.RateLimits.Secondary.ResetsAt)
	}
	if accountResp.Account != nil && strings.EqualFold(strings.TrimSpace(accountResp.Account.Type), "chatgpt") {
		usage.Email = strings.TrimSpace(accountResp.Account.Email)
		usage.PlanType = strings.TrimSpace(accountResp.Account.PlanType)
	}
	if configResp.Config.Model != nil {
		usage.Model = strings.TrimSpace(*configResp.Config.Model)
	}
	if configResp.Config.ModelReasoningEffort != nil {
		usage.ReasoningEffort = strings.TrimSpace(*configResp.Config.ModelReasoningEffort)
	}
	return usage, nil
}

func parseAppServerID(raw json.RawMessage) (int, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	var id int
	if err := json.Unmarshal(raw, &id); err == nil {
		return id, true
	}
	var idStr string
	if err := json.Unmarshal(raw, &idStr); err == nil {
		parsed, parseErr := strconv.Atoi(idStr)
		if parseErr == nil {
			return parsed, true
		}
	}
	return 0, false
}

func windowUsage(window *appRateLimitWindow, fallbackMinutes int) (float64, int) {
	if window == nil {
		return -1, 0
	}
	used := float64(window.UsedPercent)
	if used < 0 || used > 100 {
		return -1, 0
	}
	remaining := 100 - used
	windowMinutes := 0
	if window.WindowDurationMins != nil {
		windowMinutes = int(*window.WindowDurationMins)
	} else if fallbackMinutes > 0 {
		windowMinutes = fallbackMinutes
	}
	remainingMinutes := 0
	if windowMinutes > 0 {
		remainingMinutes = int(math.Round(float64(windowMinutes) * remaining / 100.0))
	}
	return remaining, remainingMinutes
}

func formatReset(raw *int64) string {
	if raw == nil || *raw == 0 {
		return ""
	}
	return time.Unix(*raw, 0).Local().Format("15:04")
}
