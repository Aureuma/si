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

	shared "si/agents/shared/docker"
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

func cmdCodexStatus(args []string) {
	nameArg, filtered := splitNameAndFlags(args, codexStatusBoolFlags())
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output json")
	showRaw := fs.Bool("raw", false, "include raw app-server output")
	timeout := fs.Duration("timeout", 20*time.Second, "status timeout")
	profileKey := fs.String("profile", "", "show codex profile status for profile id/name/email")
	profiles := fs.Bool("profiles", false, "list codex profiles")
	noStatus := fs.Bool("no-status", false, "disable usage status lookup for profile output")
	_ = fs.Parse(filtered)
	withProfileStatus := !*noStatus

	name := codexContainerSlug(strings.TrimSpace(nameArg))
	if name == "" && fs.NArg() > 0 {
		name = codexContainerSlug(strings.TrimSpace(fs.Arg(0)))
	}
	if fs.NArg() > 1 {
		printUsage("usage: si status [name|profile] [--json] [--raw] [--profile <profile>] [--profiles] [--no-status]")
		return
	}
	if *profiles {
		if name != "" || strings.TrimSpace(*profileKey) != "" {
			printUsage("usage: si status [name|profile] [--json] [--raw] [--profile <profile>] [--profiles] [--no-status]")
			return
		}
		listCodexProfiles(*jsonOut, withProfileStatus)
		return
	}
	if strings.TrimSpace(*profileKey) != "" {
		if name != "" {
			printUsage("usage: si status [name|profile] [--json] [--raw] [--profile <profile>] [--profiles] [--no-status]")
			return
		}
		showCodexProfile(*profileKey, *jsonOut, withProfileStatus)
		return
	}
	if name == "" {
		listCodexProfiles(*jsonOut, withProfileStatus)
		return
	}

	profileCandidate, hasProfileCandidate := codexProfileByKey(name)
	containerName := codexContainerName(name)

	client, err := shared.NewClient()
	if err != nil {
		if hasProfileCandidate {
			showCodexProfile(profileCandidate.ID, *jsonOut, withProfileStatus)
			return
		}
		fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	id, info, err := client.ContainerByName(ctx, containerName)
	if err != nil {
		fatal(err)
	}
	if id == "" {
		if hasProfileCandidate {
			showCodexProfile(profileCandidate.ID, *jsonOut, withProfileStatus)
			return
		}
		fatal(fmt.Errorf("codex container %s not found", containerName))
	}

	parsed, raw, err := fetchCodexAppServerStatus(ctx, client, id)
	if err != nil {
		if isAuthFailureError(err) {
			if hasProfileCandidate {
				showCodexProfile(profileCandidate.ID, *jsonOut, withProfileStatus)
				return
			}
			if info != nil && info.Config != nil {
				if labelKey := strings.TrimSpace(info.Config.Labels[codexProfileLabelKey]); labelKey != "" {
					if fallbackProfile, ok := codexProfileByKey(labelKey); ok {
						showCodexProfile(fallbackProfile.ID, *jsonOut, withProfileStatus)
						return
					}
				}
			}
			if fallbackProfile, ok := codexProfileByKey(name); ok {
				showCodexProfile(fallbackProfile.ID, *jsonOut, withProfileStatus)
				return
			}
		}
		fatal(err)
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

func codexStatusBoolFlags() map[string]bool {
	return map[string]bool{
		"json":      true,
		"raw":       true,
		"profiles":  true,
		"no-status": true,
	}
}

func buildTmuxCodexCommand(containerID string) string {
	inner := "export TERM=xterm-256color COLORTERM=truecolor COLUMNS=160 LINES=60 HOME=/home/si CODEX_HOME=/home/si/.codex; codex"
	base := fmt.Sprintf("docker exec -it %s bash -lc %q", containerID, inner)
	return fmt.Sprintf("%s || sudo -n %s", base, base)
}

func isPromptReady(output string, opts statusOptions) bool {
	if !opts.AllowMcpStartup {
		lower := strings.ToLower(output)
		if strings.Contains(lower, "starting mcp") || strings.Contains(lower, "mcp startup") {
			return false
		}
	}
	return true
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
		fmt.Printf("  %s %s\n", styleSection("5h limit:"), formatLimitDetail(s.FiveHourLeftPct, s.FiveHourReset, s.FiveHourRemaining))
	}
	if s.WeeklyLeftPct >= 0 {
		fmt.Printf("  %s %s\n", styleSection("Weekly limit:"), formatLimitDetail(s.WeeklyLeftPct, s.WeeklyReset, s.WeeklyRemaining))
	}
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

func fetchCodexAppServerStatus(ctx context.Context, client *shared.Client, containerID string) (codexStatus, string, error) {
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
			return codexStatus{}, raw, fmt.Errorf("%w: %s", err, raw)
		}
		return codexStatus{}, raw, err
	}
	totalLimitMin := 300
	if val := strings.TrimSpace(os.Getenv("CODEX_PLAN_LIMIT_MINUTES")); val != "" {
		if parsed, parseErr := strconv.Atoi(val); parseErr == nil && parsed > 0 {
			totalLimitMin = parsed
		}
	}
	usage, parseErr := parseAppServerUsageOutput(raw, totalLimitMin)
	if parseErr != nil {
		return codexStatus{}, raw, parseErr
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
	}, raw, nil
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

func buildAppServerInput() []byte {
	reqs := []appServerRequest{
		{
			JSONRPC: "2.0",
			ID:      appServerInitID,
			Method:  "initialize",
			Params: map[string]interface{}{
				"clientInfo": map[string]string{
					"name":    "si",
					"version": "1.2.0",
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
		remainingPct, remainingMinutes := windowUsage(rateResp.RateLimits.Primary, totalLimitMin, time.Now())
		usage.RemainingPct = remainingPct
		usage.RemainingMinutes = remainingMinutes
		usage.PrimaryReset = formatReset(rateResp.RateLimits.Primary.ResetsAt)
	}
	if rateResp.RateLimits.Secondary != nil {
		remainingPct, remainingMinutes := windowUsage(rateResp.RateLimits.Secondary, 0, time.Now())
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

func windowUsage(window *appRateLimitWindow, fallbackMinutes int, now time.Time) (float64, int) {
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
	if window.ResetsAt != nil && *window.ResetsAt > 0 {
		resetAt := time.Unix(*window.ResetsAt, 0)
		if resetAt.After(now) {
			remainingMinutes = int(math.Ceil(resetAt.Sub(now).Minutes()))
		}
	}
	if remainingMinutes <= 0 && windowMinutes > 0 {
		remainingMinutes = int(math.Round(float64(windowMinutes) * remaining / 100.0))
	}
	return remaining, remainingMinutes
}

func formatReset(raw *int64) string {
	if raw == nil || *raw == 0 {
		return ""
	}
	return formatResetAt(time.Unix(*raw, 0).Local())
}

func formatResetAt(t time.Time) string {
	now := time.Now().In(t.Location())
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return t.Format("15:04")
	}
	if t.Year() == now.Year() {
		return t.Format("15:04 on 2 Jan")
	}
	return t.Format("15:04 on 2 Jan 2006")
}
