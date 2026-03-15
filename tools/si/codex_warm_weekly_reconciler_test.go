package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestWarmWeeklyBackoffDuration(t *testing.T) {
	if got := warmWeeklyBackoffDuration(1); got != 15*time.Minute {
		t.Fatalf("expected 15m, got %s", got)
	}
	if got := warmWeeklyBackoffDuration(2); got != 30*time.Minute {
		t.Fatalf("expected 30m, got %s", got)
	}
	if got := warmWeeklyBackoffDuration(8); got != 24*time.Hour {
		t.Fatalf("expected clamp at 24h, got %s", got)
	}
}

func TestSetWarmWeeklyFullLimitFailure(t *testing.T) {
	now := time.Date(2026, 2, 16, 17, 0, 0, 0, time.UTC)
	entry := &warmWeeklyProfileState{}
	setWarmWeeklyFullLimitFailure(entry, now, fmt.Errorf("x"))
	if entry.LastResult != "failed" {
		t.Fatalf("expected failed result, got %q", entry.LastResult)
	}
	if entry.FailureCount != 1 {
		t.Fatalf("expected failure count 1, got %d", entry.FailureCount)
	}
	if got := parseWarmWeeklyTime(entry.NextDue); got.IsZero() || !got.Equal(now.Add(warmWeeklyFullRetryInterval)) {
		t.Fatalf("expected next_due at fixed retry interval, got %q", entry.NextDue)
	}
}

func TestWarmWeeklyBootstrapSucceeded(t *testing.T) {
	// Non-force: delta can be a valid success signal when both readings are known.
	if !warmWeeklyBootstrapSucceeded(false, false, 5.0, true, true, 5.2, true, true) {
		t.Fatalf("expected warm success for positive usage delta")
	}
	if warmWeeklyBootstrapSucceeded(false, false, 5.0, true, false, 5.2, true, false) {
		t.Fatalf("expected warm failure when reset timing is still unavailable")
	}

	// Full weekly (100% remaining): reset timing alone is not enough; usage must drop.
	if warmWeeklyBootstrapSucceeded(false, false, 0.0, true, false, 0.0, true, true) {
		t.Fatalf("expected warm failure when usage is still 100%%")
	}
	if warmWeeklyBootstrapSucceeded(false, false, 0.0, true, false, 0.4, true, true) {
		t.Fatalf("expected warm failure while weekly still rounds to 100%% remaining")
	}
	if !warmWeeklyBootstrapSucceeded(false, false, 0.0, true, false, 1.0, true, true) {
		t.Fatalf("expected warm success once usage drops below 100%% with reset timing available")
	}
	if warmWeeklyBootstrapSucceeded(false, false, 0.0, true, false, 1.0, true, false) {
		t.Fatalf("expected warm failure when reset timing is still unavailable")
	}

	// Weekly rollover: a successful warm run should count even when deltas are 0.
	if !warmWeeklyBootstrapSucceeded(false, true, 27.0, true, true, 27.0, true, true) {
		t.Fatalf("expected warm success on weekly rollover with stable percentages")
	}

	// Force mode should not bypass the strict 100%-weekly rule.
	if warmWeeklyBootstrapSucceeded(true, false, 0.0, true, true, 0.0, true, true) {
		t.Fatalf("expected force warm to fail when usage is still at 100%%")
	}
	if !warmWeeklyBootstrapSucceeded(true, false, 20.0, true, true, 20.0, true, true) {
		t.Fatalf("expected force warm to treat stable non-full usage signals as success")
	}
}

func TestWarmWeeklyNeedsWarmAttempt(t *testing.T) {
	if !warmWeeklyNeedsWarmAttempt(true, 5, true, true, false) {
		t.Fatalf("expected force mode to always warm")
	}
	if !warmWeeklyNeedsWarmAttempt(false, 0, false, true, false) {
		t.Fatalf("expected warm when usage signal is missing")
	}
	if !warmWeeklyNeedsWarmAttempt(false, 0, true, true, false) {
		t.Fatalf("expected warm while weekly remains at 100%%")
	}
	if !warmWeeklyNeedsWarmAttempt(false, 0.7, true, true, false) {
		t.Fatalf("expected warm while weekly still rounds to 100%%")
	}
	if !warmWeeklyNeedsWarmAttempt(false, 10, true, true, true) {
		t.Fatalf("expected warm when weekly window advances")
	}
	if !warmWeeklyNeedsWarmAttempt(false, 10, true, false, false) {
		t.Fatalf("expected warm when reset signal is missing")
	}
	if warmWeeklyNeedsWarmAttempt(false, 10, true, true, false) {
		t.Fatalf("expected no warm when usage already consumed in this reset")
	}
}

func TestWarmWeeklyResetWindowAdvanced(t *testing.T) {
	prev := time.Date(2026, 2, 13, 14, 33, 56, 0, time.UTC)
	curr := time.Date(2026, 2, 21, 14, 33, 56, 0, time.UTC)
	if !warmWeeklyResetWindowAdvanced(prev, curr, true) {
		t.Fatalf("expected window-advanced to be true")
	}
	if warmWeeklyResetWindowAdvanced(prev, prev, true) {
		t.Fatalf("expected window-advanced to be false for same reset")
	}
	if warmWeeklyResetWindowAdvanced(time.Time{}, curr, true) {
		t.Fatalf("expected window-advanced to be false when previous reset is unknown")
	}
}

func TestWeeklyUsedPercent(t *testing.T) {
	payload := usagePayload{
		RateLimit: &usageRateLimit{
			Secondary: &usageWindow{UsedPercent: 12.34},
		},
	}
	used, ok := weeklyUsedPercent(payload)
	if !ok || used != 12.34 {
		t.Fatalf("unexpected used percent: ok=%v used=%v", ok, used)
	}
}

func TestWarmWeeklyAtFullLimit(t *testing.T) {
	if !warmWeeklyAtFullLimit(0.0) {
		t.Fatalf("expected full limit at 0.0")
	}
	if !warmWeeklyAtFullLimit(0.9) {
		t.Fatalf("expected full limit when usage is below 1%%")
	}
	if warmWeeklyAtFullLimit(1.0) {
		t.Fatalf("did not expect full limit at 1.0")
	}
}

func TestRenderWarmWeeklyReconcileConfig(t *testing.T) {
	cfg := renderWarmWeeklyReconcileConfig("/home/si/.si", "aureuma/si:local", 1001, 1001)
	if !strings.Contains(cfg, fmt.Sprintf("volume = %s:%s", warmWeeklyBinaryVolumeName, warmWeeklyBinaryDir)) {
		t.Fatalf("expected binary volume mount in config, got: %q", cfg)
	}
	if !strings.Contains(cfg, "volume = /var/run/docker.sock:/var/run/docker.sock") {
		t.Fatalf("expected docker socket mount in config, got: %q", cfg)
	}
	if !strings.Contains(cfg, "command = "+warmWeeklyReconcileScriptPath) {
		t.Fatalf("expected wrapper script command in config, got: %q", cfg)
	}
	if !strings.Contains(cfg, "user = root") {
		t.Fatalf("expected reconcile job to run as root for docker socket access, got: %q", cfg)
	}
	if !strings.Contains(cfg, "environment = SI_HOST_UID=1001") || !strings.Contains(cfg, "environment = SI_HOST_GID=1001") {
		t.Fatalf("expected host uid/gid env in config, got: %q", cfg)
	}
}

func TestWarmWeeklyReconcileConfigCurrent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configPath, err := defaultWarmWeeklyReconcileConfigPath()
	if err != nil {
		t.Fatalf("defaultWarmWeeklyReconcileConfigPath: %v", err)
	}
	if warmWeeklyReconcileConfigCurrent() {
		t.Fatalf("expected missing config to be treated as stale")
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}

	legacy := "[job-run \"si-warmup-reconcile\"]\nschedule = 0 0 * * * *\nimage = aureuma/si:local\ncommand = /usr/local/bin/si warmup reconcile --quiet\nvolume = /tmp/si:/usr/local/bin/si:ro\n"
	if err := os.WriteFile(configPath, []byte(legacy), 0o600); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}
	if warmWeeklyReconcileConfigCurrent() {
		t.Fatalf("expected legacy config without binary volume to be stale")
	}

	current := renderWarmWeeklyReconcileConfig(filepath.Join(home, ".si"), "aureuma/si:local", 1001, 1001)
	if err := os.WriteFile(configPath, []byte(current), 0o600); err != nil {
		t.Fatalf("write current config: %v", err)
	}
	if !warmWeeklyReconcileConfigCurrent() {
		t.Fatalf("expected current config to be healthy")
	}
}

func TestMaybeAutoRepairWarmupSchedulerLaunchesEnable(t *testing.T) {
	origRequested := warmWeeklyAutostartRequestedFn
	origHealth := warmWeeklySchedulerHealthFn
	origLaunch := launchWarmupCommandAsyncFn
	origArg0 := os.Args[0]
	defer func() {
		warmWeeklyAutostartRequestedFn = origRequested
		warmWeeklySchedulerHealthFn = origHealth
		launchWarmupCommandAsyncFn = origLaunch
		os.Args[0] = origArg0
	}()
	os.Args[0] = "si"

	warmWeeklyAutostartRequestedFn = func() (bool, string) { return true, "marker" }
	warmWeeklySchedulerHealthFn = func() (bool, string, error) { return false, "container_missing", nil }
	launches := make([][]string, 0, 1)
	launchWarmupCommandAsyncFn = func(args ...string) error {
		launches = append(launches, append([]string(nil), args...))
		return nil
	}

	maybeAutoRepairWarmupScheduler("status")
	if len(launches) != 1 {
		t.Fatalf("expected one launch, got %d", len(launches))
	}
	want := []string{"warmup", "enable", "--quiet", "--no-reconcile"}
	if !reflect.DeepEqual(launches[0], want) {
		t.Fatalf("unexpected launch args: got=%v want=%v", launches[0], want)
	}
}

func TestWarmWeeklyAutostartRequestedFallsBackToLoggedInProfiles(t *testing.T) {
	origLoggedInProfilesFn := loggedInProfilesFn
	defer func() {
		loggedInProfilesFn = origLoggedInProfilesFn
	}()

	home := t.TempDir()
	t.Setenv("HOME", home)

	loggedInProfilesFn = func() []codexProfile {
		return []codexProfile{{ID: "profile-alpha"}}
	}

	requested, reason := warmWeeklyAutostartRequested()
	if !requested {
		t.Fatalf("expected autostart to be requested when logged-in profiles exist")
	}
	if reason != "cached_auth" {
		t.Fatalf("expected cached_auth reason, got %q", reason)
	}
}

func TestMaybeAutoRepairWarmupSchedulerPersistsMarkerFromCachedAuth(t *testing.T) {
	origRequested := warmWeeklyAutostartRequestedFn
	origHealth := warmWeeklySchedulerHealthFn
	origLaunch := launchWarmupCommandAsyncFn
	origArg0 := os.Args[0]
	defer func() {
		warmWeeklyAutostartRequestedFn = origRequested
		warmWeeklySchedulerHealthFn = origHealth
		launchWarmupCommandAsyncFn = origLaunch
		os.Args[0] = origArg0
	}()
	os.Args[0] = "si"

	home := t.TempDir()
	t.Setenv("HOME", home)

	warmWeeklyAutostartRequestedFn = func() (bool, string) { return true, "cached_auth" }
	warmWeeklySchedulerHealthFn = func() (bool, string, error) { return true, "running", nil }
	launchWarmupCommandAsyncFn = func(args ...string) error {
		t.Fatalf("did not expect warmup launch when scheduler is already healthy")
		return nil
	}

	maybeAutoRepairWarmupScheduler("status")

	markerPath, err := warmWeeklyAutostartMarkerPath()
	if err != nil {
		t.Fatalf("warmWeeklyAutostartMarkerPath: %v", err)
	}
	if _, err := os.Stat(markerPath); err != nil {
		t.Fatalf("expected autostart marker to be written, got %v", err)
	}
}

func TestLoadWarmWeeklyStateDelegatesToRustCLIWhenConfigured(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"version\":3,\"profiles\":{\"ferma\":{\"profile_id\":\"ferma\",\"last_result\":\"ready\"}}}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	state, err := loadWarmWeeklyState()
	if err != nil {
		t.Fatalf("loadWarmWeeklyState: %v", err)
	}
	if state.Version != 3 || state.Profiles["ferma"].LastResult != "ready" {
		t.Fatalf("unexpected state: %+v", state)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if !strings.Contains(string(argsData), "warmup\nstatus\n--path\n") {
		t.Fatalf("unexpected rust cli args: %q", string(argsData))
	}
}

func TestSaveWarmWeeklyStateDelegatesToRustCLIWhenConfigured(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	err := saveWarmWeeklyState(warmWeeklyState{
		Version: 3,
		Profiles: map[string]*warmWeeklyProfileState{
			"ferma": {ProfileID: "ferma", LastResult: "ready"},
		},
	})
	if err != nil {
		t.Fatalf("saveWarmWeeklyState: %v", err)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if !strings.Contains(string(argsData), "warmup\nstate\nwrite\n--path\n") {
		t.Fatalf("unexpected rust cli args: %q", string(argsData))
	}
}

func TestCmdWarmupStatusDelegatesToRustCLIWhenConfigured(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"version\":3}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output := captureOutputForTest(t, func() {
		cmdWarmupStatus([]string{"--json"})
	})
	if !strings.Contains(output, "{\"version\":3}") {
		t.Fatalf("unexpected output: %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "warmup\nstatus\n--format\njson" {
		t.Fatalf("unexpected rust cli args: %q", string(argsData))
	}
}

func TestWarmWeeklyAutostartRequestedUsesRustMarkerStateWhenConfigured(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"requested\":false,\"reason\":\"disabled\"}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	requested, reason := warmWeeklyAutostartRequested()
	if requested || reason != "disabled" {
		t.Fatalf("unexpected autostart result: requested=%v reason=%q", requested, reason)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if !strings.Contains(string(argsData), "warmup\nautostart-decision\n--state-path\n") {
		t.Fatalf("unexpected rust cli args: %q", string(argsData))
	}
}

func TestWarmWeeklyAutostartRequestedDelegatesDecisionToRustWhenConfigured(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"requested\":true,\"reason\":\"legacy_state\"}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	requested, reason := warmWeeklyAutostartRequested()
	if !requested || reason != "legacy_state" {
		t.Fatalf("unexpected autostart result: requested=%v reason=%q", requested, reason)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if !strings.Contains(string(argsData), "warmup\nautostart-decision\n--state-path\n") {
		t.Fatalf("unexpected rust cli args: %q", string(argsData))
	}
}

func TestWriteWarmWeeklyAutostartMarkerDelegatesToRustCLIWhenConfigured(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	if err := writeWarmWeeklyAutostartMarker(); err != nil {
		t.Fatalf("writeWarmWeeklyAutostartMarker: %v", err)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if !strings.Contains(string(argsData), "warmup\nmarker\nwrite-autostart\n--path\n") {
		t.Fatalf("unexpected rust cli args: %q", string(argsData))
	}
}

func TestSetWarmWeeklyDisabledDelegatesToRustCLIWhenConfigured(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	if err := setWarmWeeklyDisabled(true); err != nil {
		t.Fatalf("setWarmWeeklyDisabled: %v", err)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if !strings.Contains(string(argsData), "warmup\nmarker\nset-disabled\n--path\n") {
		t.Fatalf("unexpected rust cli args: %q", string(argsData))
	}
}
