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

func cmdCodexStatus(args []string) {
	fs := flag.NewFlagSet("codex status", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output json")
	showRaw := fs.Bool("raw", false, "include raw status output")
	forceApp := fs.Bool("app-server", false, "use codex app-server (skip /status)")
	timeout := fs.Duration("timeout", 25*time.Second, "status timeout")
	_ = fs.Parse(args)
	if fs.NArg() < 1 {
		fmt.Println("usage: si codex status <name> [--json] [--raw]")
		return
	}
	name := fs.Arg(0)
	containerName := codexContainerName(name)

	client, err := shared.NewClient()
	if err != nil {
		fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	id, _, err := client.ContainerByName(ctx, containerName)
	if err != nil {
		fatal(err)
	}
	if id == "" {
		fatal(fmt.Errorf("codex container %s not found", containerName))
	}

	parsed := codexStatus{}
	raw := ""
	if !*forceApp {
		raw, err = fetchCodexStatus(ctx, client, id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "codex /status tmux error: %v\n", err)
		}
		if strings.TrimSpace(raw) != "" {
			parsed = parseCodexStatus(raw)
			parsed.Source = "status"
		}
	}
	if *forceApp || looksLikePanic(raw) || (parsed.Model == "" && parsed.AccountEmail == "" && parsed.ContextLeftPct <= 0) {
		fallbackCtx, fbCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer fbCancel()
		fallback, fbErr := fetchCodexAppServerStatus(fallbackCtx, client, id)
		if fbErr == nil {
			parsed = fallback
			parsed.Source = "app-server"
		} else if !*forceApp {
			fmt.Fprintf(os.Stderr, "codex /status failed; app-server fallback error: %v\n", fbErr)
		}
	}
	if *showRaw {
		parsed.Raw = raw
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(parsed); err != nil {
			fatal(err)
		}
		return
	}

	printCodexStatus(parsed)
}

func fetchCodexStatus(ctx context.Context, client *shared.Client, containerID string) (string, error) {
	tmuxCtx, tmuxCancel := context.WithTimeout(ctx, 45*time.Second)
	defer tmuxCancel()
	return fetchCodexStatusViaTmux(tmuxCtx, containerID)
}

func fetchCodexStatusViaTmux(ctx context.Context, containerID string) (string, error) {
	session := fmt.Sprintf("si-codex-status-%d", time.Now().UnixNano())
	paneTarget := session + ":0.0"
	cmd := buildTmuxCodexCommand(containerID)
	if _, err := tmuxOutput(ctx, "new-session", "-d", "-s", session, "bash", "-lc", cmd); err != nil {
		return "", err
	}
	defer func() {
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
		output, err := tmuxCapture(ctx, paneTarget)
		if err == nil && strings.TrimSpace(output) != "" {
			lastOutput = output
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

		if !statusSent && shouldSendStatus(output) {
			_ = tmuxSendKeys(ctx, paneTarget, "C-u")
			_ = tmuxSendLiteral(ctx, paneTarget, "/status")
			_ = tmuxSendKeys(ctx, paneTarget, "Enter")
			statusSent = true
			lastStatusSend = time.Now()
			statusAttempts = 1
			statusDeadline = time.Now().Add(30 * time.Second)
			if snapshot, ok := captureStatusSnapshot(ctx, paneTarget); ok {
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
		if statusSent && !statusDeadline.IsZero() && time.Since(lastStatusSend) > 6*time.Second && isPromptReady(output) && statusAttempts < 4 {
			_ = tmuxSendKeys(ctx, paneTarget, "C-u")
			_ = tmuxSendLiteral(ctx, paneTarget, "/status")
			_ = tmuxSendKeys(ctx, paneTarget, "Enter")
			lastStatusSend = time.Now()
			statusAttempts++
			if snapshot, ok := captureStatusSnapshot(ctx, paneTarget); ok {
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
	output, err := tmuxCapture(ctx, paneTarget)
	if err == nil && strings.TrimSpace(output) != "" {
		lastOutput = output
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

func shouldSendStatus(output string) bool {
	lower := strings.ToLower(output)
	if !strings.Contains(lower, "openai codex") {
		return false
	}
	if !strings.Contains(lower, "context left") {
		return false
	}
	return isPromptReady(output)
}

func isPromptReady(output string) bool {
	lines := strings.Split(output, "\n")
	seen := 0
	for i := len(lines) - 1; i >= 0 && seen < 12; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		seen++
		lower := strings.ToLower(line)
		if strings.Contains(lower, "starting mcp") || strings.Contains(lower, "mcp startup") {
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

func tmuxCapture(ctx context.Context, target string) (string, error) {
	out, err := tmuxOutput(ctx, "capture-pane", "-t", target, "-p", "-J", "-S", "-", "-a", "-q")
	if strings.TrimSpace(out) != "" {
		return out, nil
	}
	outFallback, fallbackErr := tmuxOutput(ctx, "capture-pane", "-t", target, "-p", "-J", "-S", "-")
	if strings.TrimSpace(outFallback) != "" {
		return outFallback, nil
	}
	if err != nil {
		return outFallback, err
	}
	return outFallback, fallbackErr
}

func captureStatusSnapshot(ctx context.Context, target string) (string, bool) {
	time.Sleep(250 * time.Millisecond)
	output, err := tmuxCapture(ctx, target)
	if err != nil {
		return "", false
	}
	lower := strings.ToLower(output)
	if strings.Contains(lower, "5h limit") || strings.Contains(lower, "weekly limit") || strings.Contains(lower, "context window") || strings.Contains(lower, "account:") {
		return output, true
	}
	return output, false
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
	fmt.Println("Codex status:")
	if s.Source != "" {
		fmt.Printf("  Source: %s\n", s.Source)
	}
	if s.AccountEmail != "" {
		if s.AccountPlan != "" {
			fmt.Printf("  Account: %s (%s)\n", s.AccountEmail, s.AccountPlan)
		} else {
			fmt.Printf("  Account: %s\n", s.AccountEmail)
		}
	}
	if s.Model != "" {
		if s.ReasoningEffort != "" {
			fmt.Printf("  Model: %s (reasoning %s)\n", s.Model, s.ReasoningEffort)
		} else {
			fmt.Printf("  Model: %s\n", s.Model)
		}
	}
	if s.Session != "" {
		fmt.Printf("  Session: %s\n", s.Session)
	}
	if s.ContextLeftPct >= 0 {
		if s.ContextUsed != "" && s.ContextTotal != "" {
			fmt.Printf("  Context: %.0f%% left (%s / %s)\n", s.ContextLeftPct, s.ContextUsed, s.ContextTotal)
		} else {
			fmt.Printf("  Context: %.0f%% left\n", s.ContextLeftPct)
		}
	}
	if s.FiveHourLeftPct >= 0 {
		if s.FiveHourReset != "" {
			fmt.Printf("  5h limit: %.0f%% left (resets %s)\n", s.FiveHourLeftPct, s.FiveHourReset)
		} else {
			fmt.Printf("  5h limit: %.0f%% left\n", s.FiveHourLeftPct)
		}
	}
	if s.WeeklyLeftPct >= 0 {
		if s.WeeklyReset != "" {
			fmt.Printf("  Weekly limit: %.0f%% left (resets %s)\n", s.WeeklyLeftPct, s.WeeklyReset)
		} else {
			fmt.Printf("  Weekly limit: %.0f%% left\n", s.WeeklyLeftPct)
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
	reasonRe := regexp.MustCompile(`(?i)\breasoning\s+([A-Za-z0-9._-]+)`)
	if match := reasonRe.FindStringSubmatch(line); len(match) == 2 {
		reasoning = match[1]
	}
	sumRe := regexp.MustCompile(`(?i)\bsummaries?\s+([A-Za-z0-9._-]+)`)
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
