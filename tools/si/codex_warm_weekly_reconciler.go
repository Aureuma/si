package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	shared "si/agents/shared/docker"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
)

var (
	loadProfileAuthTokensFn        = loadProfileAuthTokens
	fetchUsagePayloadFn            = fetchUsagePayload
	runWeeklyWarmPromptFn          = runWeeklyWarmPrompt
	warmWeeklyAutostartRequestedFn = warmWeeklyAutostartRequested
	warmWeeklySchedulerHealthFn    = warmWeeklySchedulerHealthy
	launchWarmupCommandAsyncFn     = launchWarmupCommand
)

const (
	warmWeeklyStateVersion        = 3
	warmWeeklyReconcileSchedule   = "0 */5 * * * *"
	warmWeeklyReconcileJobName    = "si-warmup-reconcile"
	warmWeeklyBinaryVolumeName    = "si-warmup-bin"
	warmWeeklyBinaryDir           = "/opt/si-warmup/bin"
	warmWeeklyBinaryPath          = warmWeeklyBinaryDir + "/si"
	warmWeeklyReconcileScriptName = "warmup-reconcile.sh"
	warmWeeklyReconcileScriptPath = warmWeeklyBinaryDir + "/" + warmWeeklyReconcileScriptName
	warmWeeklyFullRetryInterval   = 5 * time.Minute
	warmWeeklyMinUsageDelta       = 0.05
	// Require at least 1% consumed so status drops below 100% remaining.
	warmWeeklyMinConsumedForReset = 1.0
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
	LastWeeklyUsedOK  bool    `json:"last_weekly_used_ok,omitempty"`
	LastWeeklyReset   string  `json:"last_weekly_reset,omitempty"`
	LastWarmedReset   string  `json:"last_warmed_reset,omitempty"`
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
		appendWarmWeeklyLog("error", "warmup_enable_scheduler_failed", "", map[string]interface{}{"error": err.Error()})
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
		ForceBootstrap: false,
		Quiet:          *quiet,
		MaxAttempts:    3,
		Prompt:         weeklyWarmPrompt,
		Trigger:        "enable",
	}
	if _, err := runWarmWeeklyReconcile(opts); err != nil {
		appendWarmWeeklyLog("error", "warmup_enable_reconcile_failed", "", map[string]interface{}{"error": err.Error()})
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
		forceRunForFullWeekly := entry.LastWeeklyUsedOK && warmWeeklyAtFullLimit(entry.LastWeeklyUsedPct)
		if !opts.ForceBootstrap && !selectedProfilesOnly && !forceRunForFullWeekly && !nextDue.IsZero() && nextDue.After(now) {
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
	prevReset := parseWarmWeeklyTime(entry.LastWeeklyReset)

	entry.ProfileID = profile.ID
	entry.LastAttempt = now.UTC().Format(time.RFC3339)

	auth, err := loadProfileAuthTokensFn(profile)
	if err != nil {
		setWarmWeeklyFailure(entry, now, fmt.Errorf("load auth: %w", err))
		appendWarmWeeklyLog("warn", "profile_auth_missing", profile.ID, map[string]interface{}{"error": err.Error()})
		return "failed"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	payloadBefore, err := fetchUsagePayloadFn(ctx, profileUsageURL(), auth)
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
	fullBefore := usedKnown && warmWeeklyAtFullLimit(usedBefore)
	resetAt, windowSeconds, resetKnown := weeklyResetTime(payloadBefore, now)
	if resetKnown && !fullBefore {
		resetAt = normalizeResetTime(resetAt, windowSeconds, now)
		entry.LastWeeklyReset = resetAt.UTC().Format(time.RFC3339)
	} else {
		resetAt = time.Time{}
		resetKnown = false
		entry.LastWeeklyReset = ""
	}
	entry.LastWeeklyUsedPct = usedBefore
	entry.LastWeeklyUsedOK = usedKnown

	windowAdvanced := warmWeeklyResetWindowAdvanced(prevReset, resetAt, resetKnown)
	needsWarm := warmWeeklyNeedsWarmAttempt(opts.ForceBootstrap, usedBefore, usedKnown, resetKnown, windowAdvanced)
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
	usedAfterKnown := usedKnown
	resetAtAfter := resetAt
	resetAfterKnown := resetKnown
	success := false
	for attempt := 1; attempt <= opts.MaxAttempts; attempt++ {
		prompt := warmWeeklyPromptForAttempt(opts.Prompt, attempt)
		if err := runWeeklyWarmPromptFn(profile, prompt, execOpts); err != nil {
			lastErr = fmt.Errorf("attempt %d run failed: %w", attempt, err)
			appendWarmWeeklyLog("warn", "warm_attempt_failed", profile.ID, map[string]interface{}{"attempt": attempt, "error": err.Error()})
			continue
		}

		// The usage endpoint can lag behind the actual execution; poll briefly
		// rather than treating the first read-after-write as authoritative.
		var payloadAfter usagePayload
		var fetchErr error
		for i := 0; i < 4; i++ {
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			payloadAfter, fetchErr = fetchUsagePayloadFn(ctx, profileUsageURL(), auth)
			cancel()
			if fetchErr == nil {
				break
			}
			time.Sleep(time.Duration(i+1) * 2 * time.Second)
		}
		err := fetchErr
		if err != nil {
			lastErr = fmt.Errorf("attempt %d verify failed: %w", attempt, err)
			appendWarmWeeklyLog("warn", "warm_verify_failed", profile.ID, map[string]interface{}{"attempt": attempt, "error": err.Error()})
			continue
		}

		if value, ok := weeklyUsedPercent(payloadAfter); ok {
			usedAfter = value
			usedAfterKnown = true
		} else {
			usedAfterKnown = false
		}
		resetAtCandidate, windowSecondsCandidate, okReset := weeklyResetTime(payloadAfter, now)
		if okReset {
			resetAtCandidate = normalizeResetTime(resetAtCandidate, windowSecondsCandidate, now)
			resetAtAfter = resetAtCandidate
			resetAfterKnown = true
		} else {
			resetAfterKnown = false
		}

		if ok := warmWeeklyBootstrapSucceeded(opts.ForceBootstrap, windowAdvanced, usedBefore, usedKnown, resetKnown, usedAfter, usedAfterKnown, resetAfterKnown); ok {
			success = true
			break
		}
	}

	delta := usedAfter - usedBefore
	entry.LastUsageDelta = delta
	entry.LastWeeklyUsedPct = usedAfter
	entry.LastWeeklyUsedOK = usedAfterKnown
	fullAfter := usedAfterKnown && warmWeeklyAtFullLimit(usedAfter)
	if resetAfterKnown && !fullAfter {
		entry.LastWeeklyReset = resetAtAfter.UTC().Format(time.RFC3339)
	} else {
		resetAtAfter = time.Time{}
		resetAfterKnown = false
		entry.LastWeeklyReset = ""
	}

	if success {
		entry.Paused = false
		entry.LastResult = "warmed"
		entry.LastError = ""
		entry.FailureCount = 0
		if resetAfterKnown {
			entry.LastWarmedReset = resetAtAfter.UTC().Format(time.RFC3339)
		} else if resetKnown {
			entry.LastWarmedReset = resetAt.UTC().Format(time.RFC3339)
		}
		entry.NextDue = warmWeeklyNextDue(now, resetAtAfter, resetAfterKnown).UTC().Format(time.RFC3339)
		appendWarmWeeklyLog("info", "profile_warmed", profile.ID, map[string]interface{}{"weekly_used_before": usedBefore, "weekly_used_after": usedAfter, "delta": delta, "next_due": entry.NextDue})
		return "warmed"
	}

	if lastErr == nil {
		if fullBefore {
			lastErr = fmt.Errorf("weekly remained effectively at 100%% after warm attempts (used_before=%.3f used_after=%.3f); ignoring rolling reset until usage drops to <=99%% remaining", usedBefore, usedAfter)
		} else {
			lastErr = fmt.Errorf("warm did not consume enough usage (before=%.3f after=%.3f delta=%.3f)", usedBefore, usedAfter, delta)
		}
	}
	if fullBefore {
		setWarmWeeklyFullLimitFailure(entry, now, lastErr)
	} else {
		setWarmWeeklyFailure(entry, now, lastErr)
	}
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

func warmWeeklyNeedsWarmAttempt(force bool, usedBefore float64, usedKnown bool, resetKnown bool, windowAdvanced bool) bool {
	if force {
		return true
	}
	if !usedKnown {
		return true
	}
	// 100% weekly remaining (or rounding-equivalent, e.g. 99.6%) must be actively
	// warmed; do not trust reset metadata yet.
	if warmWeeklyAtFullLimit(usedBefore) {
		return true
	}
	if !resetKnown {
		return true
	}
	if windowAdvanced {
		return true
	}
	return false
}

func warmWeeklyBootstrapSucceeded(force bool, windowAdvanced bool, before float64, beforeUsedOK bool, beforeResetOK bool, after float64, afterUsedOK bool, afterResetOK bool) bool {
	fullBefore := beforeUsedOK && warmWeeklyAtFullLimit(before)
	if fullBefore {
		// While weekly remains at 100%, OpenAI reset timestamps can roll and are not
		// stable; only trust the window once usage actually drops below 100%.
		return afterUsedOK && !warmWeeklyAtFullLimit(after) && afterResetOK
	}

	// Primary goal: make the weekly window "real" (reset/time metadata becomes available),
	// which starts/advances the timer even if percent deltas are too small to observe.
	if !beforeResetOK {
		// If we were missing reset/timer information, only treat the bootstrap as
		// successful once that information appears.
		return afterResetOK
	}
	if windowAdvanced && afterResetOK {
		// After a window rollover we only need one successful warm execution;
		// percentage deltas can remain unchanged due coarse server-side rounding.
		return true
	}
	if afterResetOK && force {
		// When force is requested, avoid marking failures just because percent moved
		// below our threshold or the endpoint is too coarse-grained.
		return true
	}
	if beforeResetOK && !beforeUsedOK && afterUsedOK {
		return true
	}
	if afterUsedOK && beforeUsedOK && after-before >= warmWeeklyMinUsageDelta {
		return true
	}
	return false
}

func warmWeeklyResetWindowAdvanced(previousReset time.Time, currentReset time.Time, currentResetKnown bool) bool {
	if !currentResetKnown || currentReset.IsZero() || previousReset.IsZero() {
		return false
	}
	return previousReset.UTC().Unix() != currentReset.UTC().Unix()
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

func setWarmWeeklyFullLimitFailure(entry *warmWeeklyProfileState, now time.Time, err error) {
	if entry == nil {
		return
	}
	entry.Paused = false
	entry.LastResult = "failed"
	if err != nil {
		entry.LastError = err.Error()
	}
	entry.FailureCount++
	entry.NextDue = now.Add(warmWeeklyFullRetryInterval).UTC().Format(time.RFC3339)
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

func warmWeeklyAtFullLimit(used float64) bool {
	return used < warmWeeklyMinConsumedForReset
}

func loadWarmWeeklyState() (warmWeeklyState, error) {
	path, err := warmWeeklyStatePath()
	if err != nil {
		return warmWeeklyState{}, err
	}
	raw, err := readLocalFile(path)
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
	// v1/v2 did not fully track warm-window state and known-zero semantics.
	if state.Version < warmWeeklyStateVersion {
		for _, row := range state.Profiles {
			if row == nil {
				continue
			}
			if row.LastWeeklyUsedOK {
				continue
			}
			if strings.TrimSpace(row.LastWeeklyReset) != "" || row.LastWeeklyUsedPct != 0 {
				row.LastWeeklyUsedOK = true
			}
			if state.Version < 3 && strings.EqualFold(strings.TrimSpace(row.LastResult), "warmed") && strings.TrimSpace(row.LastWarmedReset) == "" {
				row.LastWarmedReset = strings.TrimSpace(row.LastWeeklyReset)
			}
		}
		state.Version = warmWeeklyStateVersion
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
	headers := []string{
		styleHeading("PROFILE"),
		styleHeading("RESULT"),
		styleHeading("USED"),
		styleHeading("DELTA"),
		styleHeading("NEXT"),
		styleHeading("ERROR"),
	}
	tableRows := make([][]string, 0, len(rows))
	for _, row := range rows {
		used := "-"
		if row.LastWeeklyUsedOK {
			used = fmt.Sprintf("%.2f%%", row.LastWeeklyUsedPct)
		}
		delta := "-"
		if row.LastUsageDelta != 0 {
			delta = fmt.Sprintf("%.3f", row.LastUsageDelta)
		}
		next := formatISODateWithGitHubRelativeNow(row.NextDue)
		result := strings.TrimSpace(row.LastResult)
		if result == "" {
			result = "-"
		}
		errMsg := strings.TrimSpace(row.LastError)
		tableRows = append(tableRows, []string{
			row.ProfileID,
			styleStatus(result),
			used,
			delta,
			next,
			errMsg,
		})
	}
	printAlignedTable(headers, tableRows, 2)
}

func appendWarmWeeklyLog(level string, event string, profileID string, extra map[string]interface{}) {
	path, err := warmWeeklyLogPath()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	f, err := openLocalFileFlags(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
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
	image := strings.TrimSpace(envOr("SI_CODEX_IMAGE", "aureuma/si:local"))
	if image == "" {
		image = "aureuma/si:local"
	}
	if err := ensureWarmWeeklyBinaryVolume(exePath, image); err != nil {
		return err
	}
	inlineConfig, err := ensureWarmWeeklyReconcileConfig(configPath, siHome, image, os.Getuid(), os.Getgid())
	if err != nil {
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
		Name:         defaultOfeliaName,
		OfeliaImage:  defaultOfeliaImage,
		ConfigPath:   configPath,
		InlineConfig: inlineConfig,
		TZ:           tz,
	})
}

func ensureWarmWeeklyReconcileConfig(configPath string, siHome string, image string, hostUID int, hostGID int) (string, error) {
	configPath = strings.TrimSpace(configPath)
	siHome = strings.TrimSpace(siHome)
	image = strings.TrimSpace(image)
	if configPath == "" || siHome == "" || image == "" {
		return "", fmt.Errorf("reconcile scheduler paths are required")
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		return "", err
	}
	config := renderWarmWeeklyReconcileConfig(siHome, image, hostUID, hostGID)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		return "", err
	}
	return config, nil
}

func renderWarmWeeklyReconcileConfig(siHome string, image string, hostUID int, hostGID int) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("[job-run \"%s\"]\n", warmWeeklyReconcileJobName))
	b.WriteString(fmt.Sprintf("schedule = %s\n", warmWeeklyReconcileSchedule))
	b.WriteString(fmt.Sprintf("image = %s\n", image))
	b.WriteString(fmt.Sprintf("command = %s\n", warmWeeklyReconcileScriptPath))
	b.WriteString("user = root\n")
	b.WriteString(fmt.Sprintf("volume = %s:%s\n", warmWeeklyBinaryVolumeName, warmWeeklyBinaryDir))
	b.WriteString(fmt.Sprintf("volume = %s:/home/si/.si\n", siHome))
	b.WriteString("volume = /var/run/docker.sock:/var/run/docker.sock\n")
	if hostUID > 0 {
		b.WriteString(fmt.Sprintf("environment = SI_HOST_UID=%d\n", hostUID))
	}
	if hostGID > 0 {
		b.WriteString(fmt.Sprintf("environment = SI_HOST_GID=%d\n", hostGID))
	}
	b.WriteString("\n")
	return b.String()
}

func renderWarmWeeklyReconcileScript() []byte {
	return []byte(strings.Join([]string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		"export HOME=/home/si",
		"export CODEX_HOME=/home/si/.codex",
		fmt.Sprintf("if [ -x %s ]; then", warmWeeklyBinaryPath),
		fmt.Sprintf("  exec %s warmup reconcile --quiet", warmWeeklyBinaryPath),
		"fi",
		"exec /usr/local/bin/si warmup reconcile --quiet",
		"",
	}, "\n"))
}

func ensureWarmWeeklyBinaryVolume(executablePath string, image string) error {
	executablePath = strings.TrimSpace(executablePath)
	image = strings.TrimSpace(image)
	if executablePath == "" || image == "" {
		return fmt.Errorf("warmup binary sync requires executable and image")
	}
	bin, err := os.ReadFile(filepath.Clean(executablePath))
	if err != nil {
		return err
	}
	if len(bin) == 0 {
		return fmt.Errorf("warmup binary is empty: %s", executablePath)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client, err := shared.NewClient()
	if err != nil {
		return err
	}
	defer client.Close()
	_, _ = client.EnsureVolume(ctx, warmWeeklyBinaryVolumeName, map[string]string{
		codexLabelKey: codexLabelValue,
	})

	hostCfg := &container.HostConfig{
		AutoRemove: true,
		Mounts: []mount.Mount{{
			Type:   mount.TypeVolume,
			Source: warmWeeklyBinaryVolumeName,
			Target: warmWeeklyBinaryDir,
		}},
	}
	cfg := &container.Config{
		Image: image,
		Cmd:   []string{"bash", "-lc", "sleep 60"},
		Labels: map[string]string{
			"si.component": "warmup",
		},
	}

	id, err := client.CreateContainer(ctx, cfg, hostCfg, nil, "")
	if err != nil {
		if strings.Contains(err.Error(), "No such image") || strings.Contains(err.Error(), "not found") {
			if pullErr := execDockerCLI("pull", image); pullErr == nil {
				id, err = client.CreateContainer(ctx, cfg, hostCfg, nil, "")
			}
		}
		if err != nil {
			return err
		}
	}
	if err := client.StartContainer(ctx, id); err != nil {
		return err
	}
	defer func() {
		_ = client.RemoveContainer(context.Background(), id, true)
	}()
	if err := client.CopyFileToContainer(ctx, id, warmWeeklyBinaryPath, bin, 0o755); err != nil {
		return err
	}
	if err := client.CopyFileToContainer(ctx, id, warmWeeklyReconcileScriptPath, renderWarmWeeklyReconcileScript(), 0o755); err != nil {
		return err
	}
	return nil
}

func maybeAutoRepairWarmupScheduler(trigger string) {
	if isGoTestBinary() {
		return
	}
	trigger = strings.ToLower(strings.TrimSpace(trigger))
	switch trigger {
	case "", "warmup", "login":
		return
	}

	autostartRequested, autostartReason := warmWeeklyAutostartRequestedFn()
	if !autostartRequested {
		return
	}

	healthy, healthReason, err := warmWeeklySchedulerHealthFn()
	if err != nil {
		appendWarmWeeklyLog("debug", "warmup_scheduler_healthcheck_failed", "", map[string]interface{}{
			"trigger": trigger,
			"error":   err.Error(),
		})
		return
	}
	if healthy {
		return
	}

	if autostartReason == "legacy_state" {
		_ = writeWarmWeeklyAutostartMarker()
	}

	appendWarmWeeklyLog("warn", "warmup_scheduler_auto_repair", "", map[string]interface{}{
		"trigger":          trigger,
		"autostart_reason": autostartReason,
		"health_reason":    healthReason,
	})
	if err := launchWarmupCommandAsyncFn("warmup", "enable", "--quiet", "--no-reconcile"); err != nil {
		appendWarmWeeklyLog("warn", "warmup_scheduler_auto_repair_launch_failed", "", map[string]interface{}{
			"trigger": trigger,
			"error":   err.Error(),
		})
		return
	}
	appendWarmWeeklyLog("info", "warmup_scheduler_auto_repair_launched", "", map[string]interface{}{
		"trigger":          trigger,
		"autostart_reason": autostartReason,
		"health_reason":    healthReason,
	})
}

func isGoTestBinary() bool {
	return strings.HasSuffix(filepath.Base(os.Args[0]), ".test")
}

func warmWeeklyAutostartRequested() (bool, string) {
	if warmWeeklyDisabled() {
		return false, "disabled"
	}
	if path, err := warmWeeklyAutostartMarkerPath(); err == nil {
		if _, statErr := os.Stat(path); statErr == nil {
			return true, "marker"
		}
	}
	// Legacy fallback: previous releases could have warmup state without
	// an autostart marker; keep scheduler self-healing for those installs.
	state, err := loadWarmWeeklyState()
	if err != nil {
		return false, "none"
	}
	if len(state.Profiles) > 0 {
		return true, "legacy_state"
	}
	return false, "none"
}

func warmWeeklySchedulerHealthy() (bool, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := shared.NewClient()
	if err != nil {
		return false, "docker_unavailable", err
	}
	defer client.Close()

	id, info, err := client.ContainerByName(ctx, defaultOfeliaName)
	if err != nil {
		return false, "inspect_failed", err
	}
	if strings.TrimSpace(id) == "" || info == nil {
		return false, "container_missing", nil
	}
	if info.State == nil || !info.State.Running {
		return false, "container_not_running", nil
	}
	if !warmWeeklyReconcileConfigCurrent() {
		return false, "config_stale", nil
	}
	return true, "running", nil
}

func warmWeeklyReconcileConfigCurrent() bool {
	configPath, err := defaultWarmWeeklyReconcileConfigPath()
	if err != nil {
		return false
	}
	raw, err := readLocalFile(configPath)
	if err != nil {
		return false
	}
	cfg := string(raw)
	required := []string{
		fmt.Sprintf("[job-run \"%s\"]", warmWeeklyReconcileJobName),
		fmt.Sprintf("schedule = %s", warmWeeklyReconcileSchedule),
		fmt.Sprintf("volume = %s:%s", warmWeeklyBinaryVolumeName, warmWeeklyBinaryDir),
		"volume = /var/run/docker.sock:/var/run/docker.sock",
		fmt.Sprintf("command = %s", warmWeeklyReconcileScriptPath),
		"user = root",
		"environment = SI_HOST_UID=",
		"environment = SI_HOST_GID=",
	}
	for _, token := range required {
		if !strings.Contains(cfg, token) {
			return false
		}
	}
	return true
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
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer devNull.Close()
	// #nosec G204 -- exePath is from os.Executable and args are fixed subcommands.
	cmd := exec.Command(exePath, args...)
	cmd.Env = os.Environ()
	cmd.Stdin = devNull
	cmd.Stdout = devNull
	cmd.Stderr = devNull
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
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
