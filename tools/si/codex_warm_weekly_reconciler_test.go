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

func TestWarmWeeklyBootstrapSucceeded(t *testing.T) {
	// Non-force: delta can be a valid success signal when both readings are known.
	if !warmWeeklyBootstrapSucceeded(false, false, 0.0, true, true, 0.2, true, true) {
		t.Fatalf("expected warm success for positive usage delta")
	}
	if warmWeeklyBootstrapSucceeded(false, false, 0.0, true, false, 0.2, true, false) {
		t.Fatalf("expected warm failure when reset timing is still unavailable")
	}

	// Bootstrapping: becoming aware of reset timing is success even if percent deltas are tiny/zero.
	if !warmWeeklyBootstrapSucceeded(false, false, 0.0, true, false, 0.0, true, true) {
		t.Fatalf("expected warm success when reset timing becomes available")
	}
	if warmWeeklyBootstrapSucceeded(false, false, 0.0, true, false, 0.0, true, false) {
		t.Fatalf("expected warm failure when reset timing is still unavailable")
	}

	// Weekly rollover: a successful warm run should count even when deltas are 0.
	if !warmWeeklyBootstrapSucceeded(false, true, 0.0, true, true, 0.0, true, true) {
		t.Fatalf("expected warm success on weekly rollover with stable percentages")
	}

	// Force mode: avoid false failures when the endpoint is too coarse-grained to show deltas.
	if !warmWeeklyBootstrapSucceeded(true, false, 0.0, true, true, 0.0, true, true) {
		t.Fatalf("expected force warm to treat stable reset/usage signals as success")
	}
}

func TestWarmWeeklyNeedsWarmAttempt(t *testing.T) {
	if !warmWeeklyNeedsWarmAttempt(true, 5, true, true, false, "ready", false) {
		t.Fatalf("expected force mode to always warm")
	}
	if !warmWeeklyNeedsWarmAttempt(false, 0, false, true, false, "ready", false) {
		t.Fatalf("expected warm when usage signal is missing")
	}
	if !warmWeeklyNeedsWarmAttempt(false, 0, true, false, false, "ready", false) {
		t.Fatalf("expected warm when reset signal is missing")
	}
	if !warmWeeklyNeedsWarmAttempt(false, 0, true, true, true, "ready", false) {
		t.Fatalf("expected warm when weekly window advances")
	}
	if !warmWeeklyNeedsWarmAttempt(false, 1, true, true, false, "failed", true) {
		t.Fatalf("expected retry when prior attempt failed")
	}
	if !warmWeeklyNeedsWarmAttempt(false, 0, true, true, false, "ready", false) {
		t.Fatalf("expected first zero-usage window to warm")
	}
	if warmWeeklyNeedsWarmAttempt(false, 0, true, true, false, "ready", true) {
		t.Fatalf("expected no warm when this reset was already warmed")
	}
	if warmWeeklyNeedsWarmAttempt(false, 2, true, true, false, "ready", false) {
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

func TestRenderWarmWeeklyReconcileConfig(t *testing.T) {
	cfg := renderWarmWeeklyReconcileConfig("/home/si/.si", "aureuma/si:local", 1001, 1001)
	if !strings.Contains(cfg, fmt.Sprintf("volume = %s:%s", warmWeeklyBinaryVolumeName, warmWeeklyBinaryDir)) {
		t.Fatalf("expected binary volume mount in config, got: %q", cfg)
	}
	if !strings.Contains(cfg, "command = "+warmWeeklyReconcileScriptPath) {
		t.Fatalf("expected wrapper script command in config, got: %q", cfg)
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
