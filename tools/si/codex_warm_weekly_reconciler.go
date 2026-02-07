package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	shared "si/agents/shared/docker"
)

const (
	warmWeeklyStateVersion        = 1
	warmWeeklyReconcileSchedule   = "0 0 * * * *"
	warmWeeklyReconcileJobName    = "si-warmup-reconcile"
	warmWeeklyMinUsageDelta       = 0.05
	warmWeeklyResetJitterMinutes  = 2
	warmWeeklyAutostartMarkerName = "autostart.v1"
	warmWeeklyDisabledMarkerName  = "disabled.v1"
	warmWeeklyReconcileConfigName = "warmup-reconcile.ini"
)

type warmWeeklyState struct {
	Version   int                                `json:"version"`
	UpdatedAt string                             `json:"updated_at,omitempty"`
	Profiles  map[string]*warmWeeklyProfileState `json:"profiles,omitempty"`
}

type warmWeeklyProfileState struct {
	ProfileID         string  `json:"profile_id"`
	LastAttempt       string  `json:"last_attempt,omitempty"`
	LastResult        string  `json:"last_result,omitempty"`
	LastError         string  `json:"last_error,omitempty"`
	LastWeeklyUsedPct float64 `json:"last_weekly_used_pct,omitempty"`
	LastWeeklyReset   string  `json:"last_weekly_reset,omitempty"`
	LastUsageDelta    float64 `json:"last_usage_delta,omitempty"`
	NextDue           string  `json:"next_due,omitempty"`
	FailureCount      int     `json:"failure_count,omitempty"`
	Paused            bool    `json:"paused,omitempty"`
}

type warmWeeklyReconcileOptions struct {
	ProfileKeys    []string
	ForceBootstrap bool
	Quiet          bool
	MaxAttempts    int
	Prompt         string
	Trigger        string
}

type warmWeeklyReconcileSummary struct {
	Scanned int
	Warmed  int
	Ready   int
	Skipped int
	Paused  int
	Failed  int
}

func printWarmupUsage() {
	fmt.Print(colorizeHelp(`si warmup <enable|reconcile|status|disable> [flags]

Commands:
  si warmup enable [--profile <profile>] [--quiet] [--no-reconcile]
  si warmup reconcile [--profile <profile>] [--force-bootstrap] [--quiet] [--max-attempts N] [--prompt <text>]
  si warmup status [--json]
  si warmup disable [--quiet]

Legacy compatibility:
  si warmup [--profile <profile>] [--ofelia-install|--ofelia-write|--ofelia-remove] ...
`))
}

func cmdWarmupEnable(args []string) {
	fs := flag.NewFlagSet("warmup enable", flag.ExitOnError)
	profiles := multiFlag{}
	fs.Var(&profiles, "profile", "codex profile name/email (repeatable)")
	quiet := fs.Bool("quiet", false, "suppress non-error output")
	noReconcile := fs.Bool("no-reconcile", false, "skip immediate reconcile")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si warmup enable [--profile <profile>] [--quiet] [--no-reconcile]")
		return
	}
	if err := ensureWarmWeeklyReconcileScheduler(); err != nil {
		fatal(err)
	}
	if err := setWarmWeeklyDisabled(false); err != nil {
		warnf("warmup enable: failed to clear disabled marker: %v", err)
	}
	if err := writeWarmWeeklyAutostartMarker(); err != nil {
		warnf("warmup enable: failed to write marker: %v", err)
	}
	if !*quiet {
		successf("warmup scheduler enabled")
	}
	if *noReconcile {
		return
	}
	opts := warmWeeklyReconcileOptions{
		ProfileKeys:    profiles,
		ForceBootstrap: true,
		Quiet:          *quiet,
		MaxAttempts:    3,
		Prompt:         weeklyWarmPrompt,
		Trigger:        "enable",
	}
	if _, err := runWarmWeeklyReconcile(opts); err != nil {
		fatal(err)
	}
}

func cmdWarmupDisable(args []string) {
	fs := flag.NewFlagSet("warmup disable", flag.ExitOnError)
	quiet := fs.Bool("quiet", false, "suppress non-error output")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si warmup disable [--quiet]")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := removeOfeliaWarmContainer(ctx, defaultOfeliaName); err != nil && !errors.Is(err, os.ErrNotExist) {
		warnf("warmup disable: scheduler remove failed: %v", err)
	}
	if err := setWarmWeeklyDisabled(true); err != nil {
		warnf("warmup disable: failed to set disabled marker: %v", err)
	}
	if !*quiet {
		successf("warmup scheduler disabled")
	}
}

func cmdWarmupReconcile(args []string) {
	fs := flag.NewFlagSet("warmup reconcile", flag.ExitOnError)
	profiles := multiFlag{}
	fs.Var(&profiles, "profile", "codex profile name/email (repeatable)")
	forceBootstrap := fs.Bool("force-bootstrap", false, "force warm attempts even when profile already has weekly usage")
	quiet := fs.Bool("quiet", false, "suppress non-error output")
	maxAttempts := fs.Int("max-attempts", 3, "max warm attempts per profile")
	prompt := fs.String("prompt", "", "override warm prompt")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si warmup reconcile [--profile <profile>] [--force-bootstrap] [--quiet] [--max-attempts N] [--prompt <text>]")
		return
	}
	opts := warmWeeklyReconcileOptions{
		ProfileKeys:    profiles,
		ForceBootstrap: *forceBootstrap,
		Quiet:          *quiet,
		MaxAttempts:    *maxAttempts,
		Prompt:         strings.TrimSpace(*prompt),
		Trigger:        "manual",
	}
	if opts.Prompt == "" {
		opts.Prompt = weeklyWarmPrompt
	}
	summary, err := runWarmWeeklyReconcile(opts)
	if err != nil {
		fatal(err)
	}
	if !opts.Quiet {
		fmt.Printf("%s scanned=%d warmed=%d ready=%d skipped=%d paused=%d failed=%d\n",
			styleHeading("Warmup reconcile:"),
			summary.Scanned, summary.Warmed, summary.Ready, summary.Skipped, summary.Paused, summary.Failed)
	}
}

func cmdWarmupStatus(args []string) {
	fs := flag.NewFlagSet("warmup status", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si warmup status [--json]")
		return
	}
	state, err := loadWarmWeeklyState()
	if err != nil {
		fatal(err)
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(state); err != nil {
			fatal(err)
		}
		return
	}
	printWarmWeeklyState(state)
}

func runWarmWeeklyReconcile(opts warmWeeklyReconcileOptions) (warmWeeklyReconcileSummary, error) {
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 3
	}
	if opts.MaxAttempts > 3 {
		opts.MaxAttempts = 3
	}
	unlock, err := acquireCodexLock("warmup-reconcile", "global", 2*time.Second, 2*time.Hour)
	if err != nil {
		return warmWeeklyReconcileSummary{}, err
	}
	defer unlock()

	now := time.Now()
	state, err := loadWarmWeeklyState()
	if err != nil {
		return warmWeeklyReconcileSummary{}, err
	}
	if state.Profiles == nil {
		state.Profiles = map[string]*warmWeeklyProfileState{}
	}
	pruneWarmWeeklyState(state)

	profiles := selectWarmWeeklyProfiles(opts.ProfileKeys)
	if len(profiles) == 0 {
		state.UpdatedAt = now.UTC().Format(time.RFC3339)
		_ = saveWarmWeeklyState(state)
		return warmWeeklyReconcileSummary{}, nil
	}
	selectedProfilesOnly := len(opts.ProfileKeys) > 0

	execOpts := defaultWarmWeeklyExecOptions()
	execOpts.Quiet = opts.Quiet
	summary := warmWeeklyReconcileSummary{}

	for _, profile := range profiles {
		summary.Scanned++
		entry := state.Profiles[profile.ID]
		if entry == nil {
			entry = &warmWeeklyProfileState{ProfileID: profile.ID}
			state.Profiles[profile.ID] = entry
		}
		nextDue := parseWarmWeeklyTime(entry.NextDue)
		if !opts.ForceBootstrap && !selectedProfilesOnly && !nextDue.IsZero() && nextDue.After(now) {
			summary.Skipped++
			appendWarmWeeklyLog("debug", "skip_not_due", profile.ID, map[string]interface{}{"next_due": entry.NextDue, "trigger": opts.Trigger})
			continue
		}
		outcome := reconcileWarmWeeklyProfile(now, profile, entry, opts, execOpts)
		switch outcome {
		case "warmed":
			summary.Warmed++
		case "ready":
			summary.Ready++
		case "paused":
			summary.Paused++
		case "failed":
			summary.Failed++
		default:
			summary.Skipped++
		}
	}

	state.Version = warmWeeklyStateVersion
	state.UpdatedAt = now.UTC().Format(time.RFC3339)
	if err := saveWarmWeeklyState(state); err != nil {
		return summary, err
	}
	return summary, nil
}

func reconcileWarmWeeklyProfile(now time.Time, profile codexProfile, entry *warmWeeklyProfileState, opts warmWeeklyReconcileOptions, execOpts weeklyWarmExecOptions) string {
	entry.ProfileID = profile.ID
	entry.LastAttempt = now.UTC().Format(time.RFC3339)

	auth, err := loadProfileAuthTokens(profile)
	if err != nil {
		setWarmWeeklyFailure(entry, now, fmt.Errorf("load auth: %w", err))
		appendWarmWeeklyLog("warn", "profile_auth_missing", profile.ID, map[string]interface{}{"error": err.Error()})
		return "failed"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	payloadBefore, err := fetchUsagePayload(ctx, profileUsageURL(), auth)
	cancel()
	if err != nil {
		if isAuthFailureError(err) {
			entry.Paused = true
			entry.LastResult = "paused"
			entry.LastError = err.Error()
			entry.NextDue = now.Add(6 * time.Hour).UTC().Format(time.RFC3339)
			entry.FailureCount++
			appendWarmWeeklyLog("warn", "profile_paused_auth", profile.ID, map[string]interface{}{"error": err.Error()})
			return "paused"
		}
		setWarmWeeklyFailure(entry, now, fmt.Errorf("fetch usage: %w", err))
		appendWarmWeeklyLog("warn", "profile_usage_fetch_failed", profile.ID, map[string]interface{}{"error": err.Error()})
		return "failed"
	}

	usedBefore, usedKnown := weeklyUsedPercent(payloadBefore)
	resetAt, windowSeconds, resetKnown := weeklyResetTime(payloadBefore, now)
	if resetKnown {
		resetAt = normalizeResetTime(resetAt, windowSeconds, now)
		entry.LastWeeklyReset = resetAt.UTC().Format(time.RFC3339)
	} else {
		entry.LastWeeklyReset = ""
	}
	entry.LastWeeklyUsedPct = usedBefore

	needsWarm := warmWeeklyNeedsBootstrap(opts.ForceBootstrap, usedBefore, usedKnown, resetKnown)
	if !needsWarm {
		entry.Paused = false
		entry.LastResult = "ready"
		entry.LastError = ""
		entry.LastUsageDelta = 0
		entry.FailureCount = 0
		entry.NextDue = warmWeeklyNextDue(now, resetAt, resetKnown).UTC().Format(time.RFC3339)
		appendWarmWeeklyLog("info", "profile_ready", profile.ID, map[string]interface{}{"weekly_used_pct": usedBefore, "next_due": entry.NextDue})
		return "ready"
	}

	lastErr := error(nil)
	usedAfter := usedBefore
	success := false
	for attempt := 1; attempt <= opts.MaxAttempts; attempt++ {
		prompt := warmWeeklyPromptForAttempt(opts.Prompt, attempt)
		if err := runWeeklyWarmPrompt(profile, prompt, execOpts); err != nil {
			lastErr = fmt.Errorf("attempt %d run failed: %w", attempt, err)
			appendWarmWeeklyLog("warn", "warm_attempt_failed", profile.ID, map[string]interface{}{"attempt": attempt, "error": err.Error()})
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		payloadAfter, err := fetchUsagePayload(ctx, profileUsageURL(), auth)
		cancel()
		if err != nil {
			lastErr = fmt.Errorf("attempt %d verify failed: %w", attempt, err)
			appendWarmWeeklyLog("warn", "warm_verify_failed", profile.ID, map[string]interface{}{"attempt": attempt, "error": err.Error()})
			continue
		}
		if value, ok := weeklyUsedPercent(payloadAfter); ok {
			usedAfter = value
		}
		if ok := warmWeeklyBootstrapSucceeded(usedBefore, usedAfter); ok {
			success = true
			break
		}
	}

	delta := usedAfter - usedBefore
	entry.LastUsageDelta = delta
	entry.LastWeeklyUsedPct = usedAfter

	if success {
		entry.Paused = false
		entry.LastResult = "warmed"
		entry.LastError = ""
		entry.FailureCount = 0
		entry.NextDue = warmWeeklyNextDue(now, resetAt, resetKnown).UTC().Format(time.RFC3339)
		appendWarmWeeklyLog("info", "profile_warmed", profile.ID, map[string]interface{}{"weekly_used_before": usedBefore, "weekly_used_after": usedAfter, "delta": delta, "next_due": entry.NextDue})
		return "warmed"
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("warm did not consume enough usage (before=%.3f after=%.3f delta=%.3f)", usedBefore, usedAfter, delta)
	}
	setWarmWeeklyFailure(entry, now, lastErr)
	appendWarmWeeklyLog("warn", "profile_warm_failed", profile.ID, map[string]interface{}{"weekly_used_before": usedBefore, "weekly_used_after": usedAfter, "delta": delta, "error": lastErr.Error()})
	return "failed"
}

func defaultWarmWeeklyExecOptions() weeklyWarmExecOptions {
	settings := loadSettingsOrDefault()
	model := strings.TrimSpace(settings.Codex.Exec.Model)
	if model == "" {
		model = envOr("CODEX_MODEL", "gpt-5.2-codex")
	}
	effort := strings.TrimSpace(settings.Codex.Exec.Effort)
	if effort == "" {
		effort = envOr("CODEX_REASONING_EFFORT", "medium")
	}
	return weeklyWarmExecOptions{
		Image:         envOr("SI_CODEX_IMAGE", "aureuma/si:local"),
		Workspace:     envOr("SI_WORKSPACE_HOST", ""),
		Workdir:       "/workspace",
		Network:       envOr("SI_NETWORK", shared.DefaultNetwork),
		CodexVolume:   envOr("SI_CODEX_EXEC_VOLUME", ""),
		GHVolume:      "",
		Model:         model,
		Effort:        effort,
		DisableMCP:    true,
		OutputOnly:    true,
		KeepContainer: false,
		DockerSocket:  true,
	}
}

func warmWeeklyPromptForAttempt(base string, attempt int) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = weeklyWarmPrompt
	}
	switch attempt {
	case 1:
		return base
	case 2:
		return base + "\n\nAdd one compact comparison table of top 3 risks and mitigations."
	default:
		extra := strings.Repeat("Expand each risk with one concrete operational example and one measurable indicator. ", 8)
		return base + "\n\n" + extra
	}
}

func warmWeeklyNeedsBootstrap(force bool, usedBefore float64, usedKnown bool, resetKnown bool) bool {
	_ = usedBefore
	if force {
		return true
	}
	// If reset timing is present and usage data is readable, the weekly window is
	// already active even if rounded display still shows 100%.
	return !usedKnown || !resetKnown
}

func warmWeeklyBootstrapSucceeded(before, after float64) bool {
	if after-before >= warmWeeklyMinUsageDelta {
		return true
	}
	leftAfter := 100 - after
	return leftAfter < 99.95
}

func warmWeeklyNextDue(now, resetAt time.Time, resetKnown bool) time.Time {
	if resetKnown && !resetAt.IsZero() {
		return resetAt.Add(time.Duration(warmWeeklyResetJitterMinutes) * time.Minute)
	}
	return now.Add(24 * time.Hour)
}

func warmWeeklyBackoffDuration(failures int) time.Duration {
	if failures < 1 {
		failures = 1
	}
	backoff := 15 * time.Minute
	for i := 1; i < failures; i++ {
		backoff *= 2
		if backoff >= 24*time.Hour {
			return 24 * time.Hour
		}
	}
	return backoff
}

func setWarmWeeklyFailure(entry *warmWeeklyProfileState, now time.Time, err error) {
	if entry == nil {
		return
	}
	entry.Paused = false
	entry.LastResult = "failed"
	if err != nil {
		entry.LastError = err.Error()
	}
	entry.FailureCount++
	entry.NextDue = now.Add(warmWeeklyBackoffDuration(entry.FailureCount)).UTC().Format(time.RFC3339)
}

func weeklyUsedPercent(payload usagePayload) (float64, bool) {
	if payload.RateLimit == nil || payload.RateLimit.Secondary == nil {
		return 0, false
	}
	used := payload.RateLimit.Secondary.UsedPercent
	if math.IsNaN(used) || used < 0 || used > 100 {
		return 0, false
	}
	return used, true
}

func loadWarmWeeklyState() (warmWeeklyState, error) {
	path, err := warmWeeklyStatePath()
	if err != nil {
		return warmWeeklyState{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return warmWeeklyState{Version: warmWeeklyStateVersion, Profiles: map[string]*warmWeeklyProfileState{}}, nil
		}
		return warmWeeklyState{}, err
	}
	var state warmWeeklyState
	if err := json.Unmarshal(raw, &state); err != nil {
		return warmWeeklyState{}, err
	}
	if state.Version == 0 {
		state.Version = warmWeeklyStateVersion
	}
	if state.Profiles == nil {
		state.Profiles = map[string]*warmWeeklyProfileState{}
	}
	return state, nil
}

func saveWarmWeeklyState(state warmWeeklyState) error {
	path, err := warmWeeklyStatePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), "warmup-state-*.json")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func pruneWarmWeeklyState(state warmWeeklyState) {
	if state.Profiles == nil {
		return
	}
	known := map[string]struct{}{}
	for _, profile := range codexProfiles() {
		if strings.TrimSpace(profile.ID) != "" {
			known[profile.ID] = struct{}{}
		}
	}
	for id := range state.Profiles {
		if _, ok := known[id]; !ok {
			delete(state.Profiles, id)
		}
	}
}

func printWarmWeeklyState(state warmWeeklyState) {
	rows := make([]*warmWeeklyProfileState, 0, len(state.Profiles))
	for _, row := range state.Profiles {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].ProfileID < rows[j].ProfileID
	})
	if len(rows) == 0 {
		infof("warmup state is empty")
		return
	}
	widthProfile := displayWidth("PROFILE")
	widthResult := displayWidth("RESULT")
	widthUsed := displayWidth("USED")
	widthDelta := displayWidth("DELTA")
	widthNext := displayWidth("NEXT")
	for _, row := range rows {
		if w := displayWidth(row.ProfileID); w > widthProfile {
			widthProfile = w
		}
		if w := displayWidth(row.LastResult); w > widthResult {
			widthResult = w
		}
		used := "-"
		if row.LastWeeklyUsedPct > 0 {
			used = fmt.Sprintf("%.2f%%", row.LastWeeklyUsedPct)
		}
		if w := displayWidth(used); w > widthUsed {
			widthUsed = w
		}
		delta := "-"
		if row.LastUsageDelta != 0 {
			delta = fmt.Sprintf("%.3f", row.LastUsageDelta)
		}
		if w := displayWidth(delta); w > widthDelta {
			widthDelta = w
		}
		next := strings.TrimSpace(row.NextDue)
		if next == "" {
			next = "-"
		}
		if w := displayWidth(next); w > widthNext {
			widthNext = w
		}
	}
	fmt.Printf("%s %s %s %s %s %s\n",
		padRightANSI(styleHeading("PROFILE"), widthProfile),
		padRightANSI(styleHeading("RESULT"), widthResult),
		padRightANSI(styleHeading("USED"), widthUsed),
		padRightANSI(styleHeading("DELTA"), widthDelta),
		padRightANSI(styleHeading("NEXT"), widthNext),
		styleHeading("ERROR"),
	)
	for _, row := range rows {
		used := "-"
		if row.LastWeeklyUsedPct > 0 {
			used = fmt.Sprintf("%.2f%%", row.LastWeeklyUsedPct)
		}
		delta := "-"
		if row.LastUsageDelta != 0 {
			delta = fmt.Sprintf("%.3f", row.LastUsageDelta)
		}
		next := strings.TrimSpace(row.NextDue)
		if next == "" {
			next = "-"
		}
		result := strings.TrimSpace(row.LastResult)
		if result == "" {
			result = "-"
		}
		errMsg := strings.TrimSpace(row.LastError)
		fmt.Printf("%s %s %s %s %s %s\n",
			padRightANSI(row.ProfileID, widthProfile),
			padRightANSI(styleStatus(result), widthResult),
			padRightANSI(used, widthUsed),
			padRightANSI(delta, widthDelta),
			padRightANSI(next, widthNext),
			errMsg,
		)
	}
}

func appendWarmWeeklyLog(level string, event string, profileID string, extra map[string]interface{}) {
	path, err := warmWeeklyLogPath()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	entry := map[string]interface{}{
		"time":  time.Now().UTC().Format(time.RFC3339Nano),
		"level": strings.TrimSpace(level),
		"event": strings.TrimSpace(event),
	}
	if strings.TrimSpace(profileID) != "" {
		entry["profile"] = strings.TrimSpace(profileID)
	}
	for key, val := range extra {
		entry[key] = val
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_, _ = f.Write(append(data, '\n'))
}

func ensureWarmWeeklyReconcileScheduler() error {
	configPath, err := defaultWarmWeeklyReconcileConfigPath()
	if err != nil {
		return err
	}
	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	siHome := filepath.Join(home, ".si")
	if err := ensureWarmWeeklyReconcileConfig(configPath, exePath, siHome); err != nil {
		return err
	}

	tz := strings.TrimSpace(os.Getenv("TZ"))
	if tz == "" {
		if loc := time.Now().Location(); loc != nil {
			tz = loc.String()
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return ensureOfeliaWarmContainer(ctx, ofeliaWarmOptions{
		Name:        defaultOfeliaName,
		OfeliaImage: defaultOfeliaImage,
		ConfigPath:  configPath,
		TZ:          tz,
	})
}

func ensureWarmWeeklyReconcileConfig(configPath string, executablePath string, siHome string) error {
	configPath = strings.TrimSpace(configPath)
	executablePath = strings.TrimSpace(executablePath)
	siHome = strings.TrimSpace(siHome)
	if configPath == "" || executablePath == "" || siHome == "" {
		return fmt.Errorf("reconcile scheduler paths are required")
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		return err
	}
	image := strings.TrimSpace(envOr("SI_CODEX_IMAGE", "aureuma/si:local"))
	if image == "" {
		image = "aureuma/si:local"
	}
	command := fmt.Sprintf("/bin/bash -lc 'export HOME=/home/si CODEX_HOME=/home/si/.codex; %s warmup reconcile --quiet'", shellSingleQuote("/usr/local/bin/si"))

	var b strings.Builder
	b.WriteString(fmt.Sprintf("[job-run \"%s\"]\n", warmWeeklyReconcileJobName))
	b.WriteString(fmt.Sprintf("schedule = %s\n", warmWeeklyReconcileSchedule))
	b.WriteString(fmt.Sprintf("image = %s\n", image))
	b.WriteString(fmt.Sprintf("command = %s\n", command))
	b.WriteString(fmt.Sprintf("volume = %s:/usr/local/bin/si:ro\n", executablePath))
	b.WriteString(fmt.Sprintf("volume = %s:/home/si/.si\n", siHome))
	b.WriteString("\n")
	return os.WriteFile(configPath, []byte(b.String()), 0o600)
}

func triggerWarmupAfterLogin(profile codexProfile) {
	profileID := strings.TrimSpace(profile.ID)
	if profileID == "" {
		return
	}
	if warmWeeklyDisabled() {
		appendWarmWeeklyLog("info", "login_trigger_skipped_disabled", profileID, nil)
		return
	}
	if err := launchWarmupCommand("warmup", "enable", "--quiet", "--profile", profileID); err != nil {
		appendWarmWeeklyLog("warn", "login_trigger_failed", profileID, map[string]interface{}{"error": err.Error()})
	}
}

func launchWarmupCommand(args ...string) error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exePath, args...)
	cmd.Env = os.Environ()
	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Start()
}

func warmWeeklyDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".si", "warmup"), nil
}

func warmWeeklyStatePath() (string, error) {
	dir, err := warmWeeklyDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "state.json"), nil
}

func warmWeeklyLogPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".si", "logs", "warmup.log"), nil
}

func warmWeeklyAutostartMarkerPath() (string, error) {
	dir, err := warmWeeklyDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, warmWeeklyAutostartMarkerName), nil
}

func warmWeeklyDisabledMarkerPath() (string, error) {
	dir, err := warmWeeklyDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, warmWeeklyDisabledMarkerName), nil
}

func writeWarmWeeklyAutostartMarker() error {
	path, err := warmWeeklyAutostartMarkerPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o600)
}

func warmWeeklyDisabled() bool {
	path, err := warmWeeklyDisabledMarkerPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

func setWarmWeeklyDisabled(disabled bool) error {
	path, err := warmWeeklyDisabledMarkerPath()
	if err != nil {
		return err
	}
	if !disabled {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte("disabled\n"), 0o600)
}

func defaultWarmWeeklyReconcileConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".si", "ofelia", warmWeeklyReconcileConfigName), nil
}

func parseWarmWeeklyTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}
