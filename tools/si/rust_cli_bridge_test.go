package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunVersionCommandDefaultsToGoVersion(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	out := captureOutputForTest(t, func() {
		if err := runVersionCommand(); err != nil {
			t.Fatalf("runVersionCommand: %v", err)
		}
	})

	if strings.TrimSpace(out) != siVersion {
		t.Fatalf("expected Go version output %q, got %q", siVersion, out)
	}
}

func TestRunVersionCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'v-rust-bridge'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		if err := runVersionCommand(); err != nil {
			t.Fatalf("runVersionCommand: %v", err)
		}
	})

	if strings.TrimSpace(out) != "v-rust-bridge" {
		t.Fatalf("expected delegated Rust output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "version" {
		t.Fatalf("expected Rust CLI args to be 'version', got %q", string(argsData))
	}
}

func TestMaybeDispatchRustCLIReadOnlyErrorsWhenConfiguredBinaryMissing(t *testing.T) {
	t.Setenv(siRustCLIBinEnv, filepath.Join(t.TempDir(), "missing-si-rs"))

	delegated, err := maybeDispatchRustCLIReadOnly("version")
	if err == nil {
		t.Fatalf("expected missing explicit Rust CLI binary to fail")
	}
	if delegated {
		t.Fatalf("expected delegated=false on failure")
	}
	if !strings.Contains(err.Error(), siRustCLIBinEnv) {
		t.Fatalf("expected error to mention %s, got %v", siRustCLIBinEnv, err)
	}
}

func TestRunVersionCommandUsesRepoBuiltRustBinaryWhenEnabled(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	binPath := filepath.Join(dir, ".artifacts", "cargo-target", "debug", "si-rs")
	if err := os.MkdirAll(filepath.Dir(binPath), 0o755); err != nil {
		t.Fatalf("mkdir bin dir: %v", err)
	}
	script := "#!/bin/sh\nprintf '%s\\n' 'v-rust-repo'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(binPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	origRepoRoot := rustCLIRepoRoot
	origLookPath := rustCLILookPath
	t.Cleanup(func() {
		rustCLIRepoRoot = origRepoRoot
		rustCLILookPath = origLookPath
	})
	rustCLIRepoRoot = func() (string, error) { return dir, nil }
	rustCLILookPath = func(file string) (string, error) { return "", os.ErrNotExist }

	t.Setenv(siExperimentalRustCLIEnv, "1")
	t.Setenv(siRustCLIBinEnv, "")

	out := captureOutputForTest(t, func() {
		if err := runVersionCommand(); err != nil {
			t.Fatalf("runVersionCommand: %v", err)
		}
	})

	if strings.TrimSpace(out) != "v-rust-repo" {
		t.Fatalf("expected repo Rust output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "version" {
		t.Fatalf("expected Rust CLI args to be 'version', got %q", string(argsData))
	}
}

func TestRunHelpCommandDefaultsToGoUsage(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	out := captureOutputForTest(t, func() {
		if err := runHelpCommand(nil); err != nil {
			t.Fatalf("runHelpCommand: %v", err)
		}
	})

	if !strings.Contains(out, "Holistic CLI for si.") {
		t.Fatalf("expected Go usage output, got %q", out)
	}
}

func TestRunHelpCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-help'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		if err := runHelpCommand([]string{"remote-control"}); err != nil {
			t.Fatalf("runHelpCommand: %v", err)
		}
	})

	if strings.TrimSpace(out) != "rust-help" {
		t.Fatalf("expected delegated Rust help output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "help\nremote-control" {
		t.Fatalf("expected Rust CLI args to be help + remote-control, got %q", string(argsData))
	}
}

func TestRunProvidersCharacteristicsCommandDefaultsToGo(t *testing.T) {
	t.Setenv(siExperimentalRustCLIEnv, "")
	t.Setenv(siRustCLIBinEnv, "")

	delegated, err := runProvidersCharacteristicsCommand([]string{"--provider", "github", "--json"})
	if err != nil {
		t.Fatalf("runProvidersCharacteristicsCommand: %v", err)
	}
	if delegated {
		t.Fatalf("expected Go providers characteristics path by default")
	}
}

func TestRunProvidersCharacteristicsCommandDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' 'rust-providers'\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	out := captureOutputForTest(t, func() {
		delegated, err := runProvidersCharacteristicsCommand([]string{"--provider", "github", "--json"})
		if err != nil {
			t.Fatalf("runProvidersCharacteristicsCommand: %v", err)
		}
		if !delegated {
			t.Fatalf("expected providers characteristics to delegate to Rust")
		}
	})

	if strings.TrimSpace(out) != "rust-providers" {
		t.Fatalf("expected delegated Rust providers output, got %q", out)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "providers\ncharacteristics\n--provider\ngithub\n--json" {
		t.Fatalf("expected Rust CLI args to be providers characteristics + flags, got %q", string(argsData))
	}
}

func TestMaybeRunRustCodexContainerActionDelegatesAndReturnsOutput(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' 'si-codex-ferma'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output, delegated, err := maybeRunRustCodexContainerAction("stop", "ferma")
	if err != nil {
		t.Fatalf("maybeRunRustCodexContainerAction: %v", err)
	}
	if !delegated {
		t.Fatalf("expected stop action to delegate to Rust")
	}
	if output != "si-codex-ferma" {
		t.Fatalf("expected trimmed Rust output, got %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "codex\nstop\nferma" {
		t.Fatalf("expected Rust CLI args to be codex stop ferma, got %q", string(argsData))
	}
}

func TestMaybeRunRustVaultTrustLookupDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"found\":true,\"matches\":false,\"repo_root\":\"/repo\",\"file\":\"/repo/.env\",\"expected_fingerprint\":\"expected\",\"stored_fingerprint\":\"stored\",\"trusted_at\":\"2030-01-01T00:00:00Z\"}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	lookup, delegated, err := maybeRunRustVaultTrustLookup("/tmp/trust.json", "/repo", "/repo/.env", "expected")
	if err != nil {
		t.Fatalf("maybeRunRustVaultTrustLookup: %v", err)
	}
	if !delegated {
		t.Fatalf("expected vault trust lookup to delegate to Rust")
	}
	if !lookup.Found || lookup.Matches {
		t.Fatalf("unexpected lookup result: %+v", lookup)
	}
	if lookup.StoredFingerprint != "stored" {
		t.Fatalf("unexpected stored fingerprint: %+v", lookup)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "vault\ntrust\nlookup\n--path\n/tmp/trust.json\n--repo-root\n/repo\n--file\n/repo/.env\n--fingerprint\nexpected\n--format\njson" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeLoadRustWarmupStateDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"version\":3,\"updated_at\":\"2030-01-01T00:00:00Z\",\"profiles\":{\"ferma\":{\"profile_id\":\"ferma\",\"last_result\":\"ready\",\"next_due\":\"2030-01-02T00:00:00Z\"}}}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	state, delegated, err := maybeLoadRustWarmupState("/tmp/warmup-state.json")
	if err != nil {
		t.Fatalf("maybeLoadRustWarmupState: %v", err)
	}
	if !delegated {
		t.Fatalf("expected warmup state to delegate to Rust")
	}
	if state.Version != 3 || state.Profiles["ferma"].LastResult != "ready" {
		t.Fatalf("unexpected state: %+v", state)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "warmup\nstatus\n--path\n/tmp/warmup-state.json\n--format\njson" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeSaveRustWarmupStateDelegatesAndWritesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	delegated, err := maybeSaveRustWarmupState("/tmp/warmup-state.json", warmWeeklyState{
		Version: 3,
		Profiles: map[string]*warmWeeklyProfileState{
			"ferma": {ProfileID: "ferma", LastResult: "ready"},
		},
	})
	if err != nil {
		t.Fatalf("maybeSaveRustWarmupState: %v", err)
	}
	if !delegated {
		t.Fatalf("expected warmup state save to delegate to Rust")
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if !strings.Contains(string(argsData), "warmup\nstate\nwrite\n--path\n/tmp/warmup-state.json\n--state-json\n") {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeReadRustWarmupMarkerStateDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"disabled\":true,\"autostart_present\":false}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	state, delegated, err := maybeReadRustWarmupMarkerState("/tmp/autostart.v1", "/tmp/disabled.v1")
	if err != nil {
		t.Fatalf("maybeReadRustWarmupMarkerState: %v", err)
	}
	if !delegated {
		t.Fatalf("expected warmup marker state to delegate to Rust")
	}
	if !state.Disabled || state.AutostartPresent {
		t.Fatalf("unexpected marker state: %+v", state)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "warmup\nmarker\nshow\n--autostart-path\n/tmp/autostart.v1\n--disabled-path\n/tmp/disabled.v1\n--format\njson" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeWriteRustWarmupAutostartMarkerDelegates(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	delegated, err := maybeWriteRustWarmupAutostartMarker("/tmp/autostart.v1")
	if err != nil {
		t.Fatalf("maybeWriteRustWarmupAutostartMarker: %v", err)
	}
	if !delegated {
		t.Fatalf("expected warmup autostart marker to delegate to Rust")
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "warmup\nmarker\nwrite-autostart\n--path\n/tmp/autostart.v1" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeSetRustWarmupDisabledDelegates(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	delegated, err := maybeSetRustWarmupDisabled("/tmp/disabled.v1", true)
	if err != nil {
		t.Fatalf("maybeSetRustWarmupDisabled: %v", err)
	}
	if !delegated {
		t.Fatalf("expected warmup disabled marker to delegate to Rust")
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "warmup\nmarker\nset-disabled\n--path\n/tmp/disabled.v1\n--disabled=true" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeReadRustWarmupAutostartDecisionDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"requested\":true,\"reason\":\"legacy_state\"}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	decision, delegated, err := maybeReadRustWarmupAutostartDecision("/tmp/state.json", "/tmp/autostart.v1", "/tmp/disabled.v1")
	if err != nil {
		t.Fatalf("maybeReadRustWarmupAutostartDecision: %v", err)
	}
	if !delegated {
		t.Fatalf("expected warmup autostart decision to delegate to Rust")
	}
	if !decision.Requested || decision.Reason != "legacy_state" {
		t.Fatalf("unexpected decision: %+v", decision)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "warmup\nautostart-decision\n--state-path\n/tmp/state.json\n--autostart-path\n/tmp/autostart.v1\n--disabled-path\n/tmp/disabled.v1\n--format\njson" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeRunRustWarmupStatusDelegatesAndReturnsOutput(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' 'warmup-status'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output, delegated, err := maybeRunRustWarmupStatus(true)
	if err != nil {
		t.Fatalf("maybeRunRustWarmupStatus: %v", err)
	}
	if !delegated {
		t.Fatalf("expected warmup status to delegate to Rust")
	}
	if strings.TrimSpace(output) != "warmup-status" {
		t.Fatalf("unexpected output: %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "warmup\nstatus\n--format\njson" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeLoadRustFortSessionStateDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"profile_id\":\"ferma\",\"agent_id\":\"si-codex-ferma\",\"session_id\":\"session-123\",\"host\":\"https://fort.example.test\",\"container_host\":\"http://fort.internal:8088\",\"access_token_path\":\"/tmp/access.token\",\"refresh_token_path\":\"/tmp/refresh.token\",\"access_expires_at\":\"2030-01-01T00:00:00Z\",\"refresh_expires_at\":\"2030-02-01T00:00:00Z\",\"updated_at\":\"2030-01-01T00:00:00Z\"}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	state, delegated, err := maybeLoadRustFortSessionState("/tmp/session.json")
	if err != nil {
		t.Fatalf("maybeLoadRustFortSessionState: %v", err)
	}
	if !delegated {
		t.Fatalf("expected fort session state to delegate to Rust")
	}
	if state.ProfileID != "ferma" || state.SessionID != "session-123" {
		t.Fatalf("unexpected state: %+v", state)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "fort\nsession-state\nshow\n--path\n/tmp/session.json\n--format\njson" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeClassifyRustFortSessionStateDelegatesAndParsesVariant(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"Revoked\":{\"snapshot\":{\"profile_id\":\"ferma\"},\"reason\":\"RefreshUnauthorized\"}}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	classification, delegated, err := maybeClassifyRustFortSessionState("/tmp/session.json", 123)
	if err != nil {
		t.Fatalf("maybeClassifyRustFortSessionState: %v", err)
	}
	if !delegated {
		t.Fatalf("expected fort session classification to delegate to Rust")
	}
	if classification.State != "revoked" || classification.Reason != "RefreshUnauthorized" {
		t.Fatalf("unexpected classification: %+v", classification)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "fort\nsession-state\nclassify\n--path\n/tmp/session.json\n--now-unix\n123\n--format\njson" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeLoadRustFortRuntimeAgentStateDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"profile_id\":\"ferma\",\"pid\":4242,\"command_path\":\"/tmp/si\",\"started_at\":\"2030-01-01T00:00:00Z\",\"updated_at\":\"2030-01-01T00:00:01Z\"}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	state, delegated, err := maybeLoadRustFortRuntimeAgentState("/tmp/runtime-agent.json")
	if err != nil {
		t.Fatalf("maybeLoadRustFortRuntimeAgentState: %v", err)
	}
	if !delegated {
		t.Fatalf("expected fort runtime agent state to delegate to Rust")
	}
	if state.ProfileID != "ferma" || state.PID != 4242 {
		t.Fatalf("unexpected state: %+v", state)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "fort\nruntime-agent-state\nshow\n--path\n/tmp/runtime-agent.json\n--format\njson" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeSaveRustFortSessionStateDelegatesAndWritesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	delegated, err := maybeSaveRustFortSessionState("/tmp/session.json", fortProfileSessionState{
		ProfileID: "ferma",
		AgentID:   "si-codex-ferma",
		SessionID: "session-123",
	})
	if err != nil {
		t.Fatalf("maybeSaveRustFortSessionState: %v", err)
	}
	if !delegated {
		t.Fatalf("expected fort session state write to delegate to Rust")
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if !strings.Contains(string(argsData), "fort\nsession-state\nwrite\n--path\n/tmp/session.json\n--state-json\n") {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
	if !strings.Contains(string(argsData), "\"profile_id\":\"ferma\"") {
		t.Fatalf("expected marshaled session state json in args, got %q", string(argsData))
	}
}

func TestMaybeSaveRustFortRuntimeAgentStateDelegatesAndWritesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	delegated, err := maybeSaveRustFortRuntimeAgentState("/tmp/runtime-agent.json", fortProfileRuntimeAgentState{
		ProfileID: "ferma",
		PID:       4242,
	})
	if err != nil {
		t.Fatalf("maybeSaveRustFortRuntimeAgentState: %v", err)
	}
	if !delegated {
		t.Fatalf("expected fort runtime agent state write to delegate to Rust")
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if !strings.Contains(string(argsData), "fort\nruntime-agent-state\nwrite\n--path\n/tmp/runtime-agent.json\n--state-json\n") {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
	if !strings.Contains(string(argsData), "\"profile_id\":\"ferma\"") {
		t.Fatalf("expected marshaled runtime state json in args, got %q", string(argsData))
	}
}

func TestMaybeApplyRustFortSessionRefreshOutcomeDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"state\":{\"profile_id\":\"alpha\",\"agent_id\":\"si-codex-alpha\",\"session_id\":\"rfs_existing\",\"access_expires_at\":\"2030-01-01T00:00:00Z\",\"refresh_expires_at\":\"2030-02-01T00:00:00Z\"},\"classification\":{\"state\":\"resumable\"}}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	transition, delegated, err := maybeApplyRustFortSessionRefreshOutcome("/tmp/session.json", fortSessionRefreshResult{
		AccessExpiresAt: "2030-01-01T00:00:00Z",
	}, time.Unix(100, 0).UTC())
	if err != nil {
		t.Fatalf("maybeApplyRustFortSessionRefreshOutcome: %v", err)
	}
	if !delegated {
		t.Fatalf("expected fort refresh outcome to delegate to Rust")
	}
	if transition.State.AccessExpiresAt != "2030-01-01T00:00:00Z" {
		t.Fatalf("unexpected transitioned state: %+v", transition)
	}
	if transition.Classification.State != "resumable" {
		t.Fatalf("unexpected transitioned classification: %+v", transition)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if !strings.Contains(string(argsData), "fort\nsession-state\nrefresh-outcome\n--path\n/tmp/session.json") {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
	if !strings.Contains(string(argsData), "--access-expires-at-unix\n1893456000") {
		t.Fatalf("expected access expiry unix arg, got %q", string(argsData))
	}
}

func TestMaybeRunRustFortSessionTeardownDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"state\":\"closed\"}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	classification, delegated, err := maybeRunRustFortSessionTeardown("/tmp/session.json", time.Unix(100, 0).UTC())
	if err != nil {
		t.Fatalf("maybeRunRustFortSessionTeardown: %v", err)
	}
	if !delegated {
		t.Fatalf("expected fort teardown to delegate to Rust")
	}
	if classification.State != "closed" {
		t.Fatalf("unexpected teardown classification: %+v", classification)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "fort\nsession-state\nteardown\n--path\n/tmp/session.json\n--now-unix\n100\n--format\njson" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeBuildRustDyadSpawnPlanDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"dyad\":\"alpha\",\"role\":\"ios\",\"network_name\":\"si\",\"workspace_host\":\"/workspace\",\"configs_host\":\"/configs-src\",\"codex_volume\":\"si-codex-alpha\",\"skills_volume\":\"si-codex-skills\",\"forward_ports\":\"1455-1465\",\"docker_socket\":true,\"actor\":{\"member\":\"actor\",\"container_name\":\"si-actor-alpha\",\"image\":\"actor:latest\",\"env\":[],\"bind_mounts\":[],\"command\":[]},\"critic\":{\"member\":\"critic\",\"container_name\":\"si-critic-alpha\",\"image\":\"critic:latest\",\"env\":[],\"bind_mounts\":[],\"command\":[]}}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	plan, delegated, err := maybeBuildRustDyadSpawnPlan(rustDyadSpawnPlanRequest{
		Name:         "alpha",
		Role:         "ios",
		ActorImage:   "actor:latest",
		CriticImage:  "critic:latest",
		Workspace:    "/workspace",
		Configs:      "/configs-src",
		ForwardPorts: "1455-1465",
		DockerSocket: true,
	})
	if err != nil {
		t.Fatalf("maybeBuildRustDyadSpawnPlan: %v", err)
	}
	if !delegated {
		t.Fatalf("expected dyad spawn plan to delegate to Rust")
	}
	if plan.Actor.ContainerName != "si-actor-alpha" || plan.Critic.ContainerName != "si-critic-alpha" {
		t.Fatalf("unexpected dyad plan: %+v", plan)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if !strings.Contains(string(argsData), "dyad\nspawn-plan\n--name\nalpha") {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeStartRustDyadSpawnDelegatesToRustCLI(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' 'started'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	delegated, err := maybeStartRustDyadSpawn(rustDyadSpawnPlanRequest{
		Name:         "alpha",
		Role:         "ios",
		ActorImage:   "actor:latest",
		CriticImage:  "critic:latest",
		Workspace:    "/workspace",
		Configs:      "/configs-src",
		ForwardPorts: "1455-1465",
		DockerSocket: true,
	})
	if err != nil {
		t.Fatalf("maybeStartRustDyadSpawn: %v", err)
	}
	if !delegated {
		t.Fatalf("expected dyad spawn start to delegate to Rust")
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if !strings.Contains(string(argsData), "dyad\nspawn-start\n--name\nalpha") {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
	if strings.Contains(string(argsData), "--format\njson") {
		t.Fatalf("did not expect spawn-start to pass --format json: %q", string(argsData))
	}
}

func TestMaybeRunRustDyadContainerActionDelegatesAndReturnsOutput(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' 'dyad-started'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output, delegated, err := maybeRunRustDyadContainerAction("start", "alpha")
	if err != nil {
		t.Fatalf("maybeRunRustDyadContainerAction: %v", err)
	}
	if !delegated {
		t.Fatalf("expected dyad start to delegate to Rust")
	}
	if strings.TrimSpace(output) != "dyad-started" {
		t.Fatalf("unexpected output %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "dyad\nstart\nalpha" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeRunRustDyadRemoveDelegatesAndReturnsOutput(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' 'dyad-removed'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output, delegated, err := maybeRunRustDyadRemove("alpha")
	if err != nil {
		t.Fatalf("maybeRunRustDyadRemove: %v", err)
	}
	if !delegated {
		t.Fatalf("expected dyad remove to delegate to Rust")
	}
	if strings.TrimSpace(output) != "dyad-removed" {
		t.Fatalf("unexpected output %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "dyad\nremove\nalpha" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeRunRustDyadExecDelegatesToRustCLI(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	delegated, err := maybeRunRustDyadExec("alpha", "critic", true, []string{"bash", "-lc", "echo hi"})
	if err != nil {
		t.Fatalf("maybeRunRustDyadExec: %v", err)
	}
	if !delegated {
		t.Fatalf("expected dyad exec to delegate to Rust")
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "dyad\nexec\nalpha\n--member\ncritic\n--tty=true\n--\nbash\n-lc\necho hi" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeRunRustDyadCleanupDelegatesAndReturnsOutput(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' 'removed=2'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output, delegated, err := maybeRunRustDyadCleanup()
	if err != nil {
		t.Fatalf("maybeRunRustDyadCleanup: %v", err)
	}
	if !delegated {
		t.Fatalf("expected dyad cleanup to delegate to Rust")
	}
	if strings.TrimSpace(output) != "removed=2" {
		t.Fatalf("unexpected output %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "dyad\ncleanup" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeRunRustDyadLogsDelegatesAndReturnsOutput(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' 'critic logs'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output, delegated, err := maybeRunRustDyadLogs("alpha", "critic", 42)
	if err != nil {
		t.Fatalf("maybeRunRustDyadLogs: %v", err)
	}
	if !delegated {
		t.Fatalf("expected dyad logs to delegate to Rust")
	}
	if strings.TrimSpace(output) != "critic logs" {
		t.Fatalf("unexpected output %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "dyad\nlogs\nalpha\n--member\ncritic\n--tail\n42" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeRunRustDyadListDelegatesAndReturnsOutput(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '[{\"dyad\":\"alpha\",\"role\":\"ios\",\"actor\":\"running\",\"critic\":\"exited\"}]'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output, delegated, err := maybeRunRustDyadList(true)
	if err != nil {
		t.Fatalf("maybeRunRustDyadList: %v", err)
	}
	if !delegated {
		t.Fatalf("expected dyad list to delegate to Rust")
	}
	if !strings.Contains(output, "\"dyad\":\"alpha\"") {
		t.Fatalf("unexpected output %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "dyad\nlist\n--format\njson" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeRunRustDyadStatusDelegatesAndReturnsOutput(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' 'dyad-status'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output, delegated, err := maybeRunRustDyadStatus("alpha", true)
	if err != nil {
		t.Fatalf("maybeRunRustDyadStatus: %v", err)
	}
	if !delegated {
		t.Fatalf("expected dyad status to delegate to Rust")
	}
	if strings.TrimSpace(output) != "dyad-status" {
		t.Fatalf("unexpected output: %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "dyad\nstatus\nalpha\n--format\njson" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeReadRustDyadStatusDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"dyad\":\"alpha\",\"found\":true,\"actor\":{\"name\":\"si-actor-alpha\",\"id\":\"actor-id\",\"status\":\"running\"},\"critic\":{\"name\":\"si-critic-alpha\",\"id\":\"critic-id\",\"status\":\"exited\"}}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	status, delegated, err := maybeReadRustDyadStatus("alpha")
	if err != nil {
		t.Fatalf("maybeReadRustDyadStatus: %v", err)
	}
	if !delegated {
		t.Fatalf("expected dyad status to delegate to Rust")
	}
	if status == nil || status.Actor == nil || status.Actor.Status != "running" {
		t.Fatalf("unexpected status %+v", status)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "dyad\nstatus\nalpha\n--format\njson" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeReadRustDyadPeekPlanDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"dyad\":\"alpha\",\"member\":\"critic\",\"actor_container_name\":\"si-actor-alpha\",\"critic_container_name\":\"si-critic-alpha\",\"actor_session_name\":\"si-dyad-alpha-actor\",\"critic_session_name\":\"si-dyad-alpha-critic\",\"peek_session_name\":\"peek-main\",\"actor_attach_command\":\"actor-cmd\",\"critic_attach_command\":\"critic-cmd\"}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	plan, delegated, err := maybeReadRustDyadPeekPlan("alpha", "critic", "peek-main")
	if err != nil {
		t.Fatalf("maybeReadRustDyadPeekPlan: %v", err)
	}
	if !delegated {
		t.Fatalf("expected dyad peek plan to delegate to Rust")
	}
	if plan == nil || plan.PeekSessionName != "peek-main" || plan.CriticAttachCommand != "critic-cmd" {
		t.Fatalf("unexpected plan %+v", plan)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "dyad\npeek-plan\nalpha\n--member\ncritic\n--format\njson\n--session\npeek-main" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeApplyRustFortUnauthorizedRefreshOutcomeDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"state\":{\"profile_id\":\"alpha\",\"agent_id\":\"si-codex-alpha\",\"session_id\":\"\"},\"classification\":{\"state\":\"revoked\",\"reason\":\"RefreshUnauthorized\"}}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	transition, delegated, err := maybeApplyRustFortUnauthorizedRefreshOutcome("/tmp/session.json", time.Unix(100, 0).UTC())
	if err != nil {
		t.Fatalf("maybeApplyRustFortUnauthorizedRefreshOutcome: %v", err)
	}
	if !delegated {
		t.Fatalf("expected unauthorized fort refresh outcome to delegate to Rust")
	}
	if transition.State.SessionID != "" || transition.Classification.State != "revoked" {
		t.Fatalf("unexpected unauthorized transition: %+v", transition)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "fort\nsession-state\nrefresh-outcome\n--path\n/tmp/session.json\n--outcome\nunauthorized\n--now-unix\n100\n--format\njson" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestMaybeRunRustCodexLogsDelegatesAndReturnsOutput(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' 'log line'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output, delegated, err := maybeRunRustCodexLogs("ferma", "25", true)
	if err != nil {
		t.Fatalf("maybeRunRustCodexLogs: %v", err)
	}
	if !delegated {
		t.Fatalf("expected logs action to delegate to Rust")
	}
	if strings.TrimSpace(output) != "log line" {
		t.Fatalf("expected Rust logs output, got %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "codex\ntail\nferma\n--tail\n25" {
		t.Fatalf("expected Rust CLI args to be codex tail ferma --tail 25, got %q", string(argsData))
	}
}

func TestMaybeRunRustCodexCloneDelegatesAndReturnsOutput(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' 'cloned'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output, delegated, err := maybeRunRustCodexClone("ferma", "acme/repo", "token-123")
	if err != nil {
		t.Fatalf("maybeRunRustCodexClone: %v", err)
	}
	if !delegated {
		t.Fatalf("expected clone action to delegate to Rust")
	}
	if strings.TrimSpace(output) != "cloned" {
		t.Fatalf("expected Rust clone output, got %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "codex\nclone\nferma\nacme/repo\n--gh-pat\ntoken-123" {
		t.Fatalf("expected Rust CLI args to be codex clone ferma acme/repo --gh-pat token-123, got %q", string(argsData))
	}
}

func TestMaybeRunRustCodexExecDelegatesAndReturnsOutput(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' 'exec-output'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output, delegated, err := maybeRunRustCodexExec("ferma", "/workspace/project", true, false, []string{"A=1", "B=2"}, []string{"git", "status"})
	if err != nil {
		t.Fatalf("maybeRunRustCodexExec: %v", err)
	}
	if !delegated {
		t.Fatalf("expected exec action to delegate to Rust")
	}
	if strings.TrimSpace(output) != "exec-output" {
		t.Fatalf("expected Rust exec output, got %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "codex\nexec\nferma\n--interactive=true\n--tty=false\n--workdir\n/workspace/project\n--env\nA=1\n--env\nB=2\n--\ngit\nstatus" {
		t.Fatalf("expected Rust CLI args to be codex exec payload, got %q", string(argsData))
	}
}

func TestMaybeRunRustCodexListDelegatesForTextOutput(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' 'si-codex-ferma\trunning\taureuma/si:local'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output, delegated, err := maybeRunRustCodexList(false)
	if err != nil {
		t.Fatalf("maybeRunRustCodexList: %v", err)
	}
	if !delegated {
		t.Fatalf("expected list action to delegate to Rust")
	}
	if strings.TrimSpace(output) != "si-codex-ferma\trunning\taureuma/si:local" {
		t.Fatalf("unexpected Rust list output %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "codex\nlist\n--format\ntext" {
		t.Fatalf("expected Rust CLI args to be codex list --format text, got %q", string(argsData))
	}
}

func TestMaybeRunRustCodexListDelegatesForJSONOutput(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '[]'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output, delegated, err := maybeRunRustCodexList(true)
	if err != nil {
		t.Fatalf("maybeRunRustCodexList: %v", err)
	}
	if !delegated {
		t.Fatalf("expected json list action to delegate to Rust")
	}
	if strings.TrimSpace(output) != "[]" {
		t.Fatalf("unexpected Rust list output %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "codex\nlist\n--format\njson" {
		t.Fatalf("expected Rust CLI args to be codex list --format json, got %q", string(argsData))
	}
}

func TestMaybeReadRustCodexStatusDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"source\":\"app-server\",\"model\":\"gpt-5.2-codex\",\"five_hour_left_pct\":75,\"weekly_left_pct\":88}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	status, delegated, err := maybeReadRustCodexStatus("ferma", true)
	if err != nil {
		t.Fatalf("maybeReadRustCodexStatus: %v", err)
	}
	if !delegated {
		t.Fatalf("expected status action to delegate to Rust")
	}
	if status == nil || status.Model != "gpt-5.2-codex" || status.FiveHourLeftPct != 75 || status.WeeklyLeftPct != 88 {
		t.Fatalf("unexpected status payload: %#v", status)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "codex\nstatus-read\nferma\n--format\njson\n--raw" {
		t.Fatalf("expected Rust CLI args to be codex status-read ferma --format json --raw, got %q", string(argsData))
	}
}

func TestMaybeBuildRustCodexRespawnPlanDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"effective_name\":\"ferma\",\"profile_id\":\"ferma\",\"remove_targets\":[\"alpha\",\"ferma\"]}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	plan, delegated, err := maybeBuildRustCodexRespawnPlan("ferma", "ferma", []string{"si-codex-alpha", "ferma"})
	if err != nil {
		t.Fatalf("maybeBuildRustCodexRespawnPlan: %v", err)
	}
	if !delegated {
		t.Fatalf("expected respawn plan to delegate to Rust")
	}
	if plan == nil || plan.EffectiveName != "ferma" || len(plan.RemoveTargets) != 2 || plan.RemoveTargets[0] != "alpha" {
		t.Fatalf("unexpected respawn plan payload: %#v", plan)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "codex\nrespawn-plan\nferma\n--format\njson\n--profile-id\nferma\n--profile-container\nsi-codex-alpha\n--profile-container\nferma" {
		t.Fatalf("unexpected respawn plan args %q", string(argsData))
	}
}

func TestMaybeReadRustCodexTmuxPlanDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"session_name\":\"si-codex-pane-profile-beta\",\"target\":\"si-codex-pane-profile-beta:0.0\",\"launch_command\":\"launch-cmd\",\"resume_command\":\"resume-cmd\"}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	plan, delegated, err := maybeReadRustCodexTmuxPlan("profile-beta", "/workspace/app", "sess-123", "profile-beta")
	if err != nil {
		t.Fatalf("maybeReadRustCodexTmuxPlan: %v", err)
	}
	if !delegated {
		t.Fatalf("expected codex tmux plan to delegate to Rust")
	}
	if plan == nil || plan.SessionName != "si-codex-pane-profile-beta" || plan.ResumeCommand != "resume-cmd" {
		t.Fatalf("unexpected tmux plan %#v", plan)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "codex\ntmux-plan\nprofile-beta\n--format\njson\n--start-dir\n/workspace/app\n--resume-session-id\nsess-123\n--resume-profile\nprofile-beta" {
		t.Fatalf("unexpected tmux plan args %q", string(argsData))
	}
}

func TestMaybeReadRustCodexTmuxCommandDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"container\":\"abc123\",\"launch_command\":\"launch-cmd\"}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	command, delegated, err := maybeReadRustCodexTmuxCommand("abc123")
	if err != nil {
		t.Fatalf("maybeReadRustCodexTmuxCommand: %v", err)
	}
	if !delegated {
		t.Fatalf("expected codex tmux command to delegate to Rust")
	}
	if command == nil || command.Container != "abc123" || command.LaunchCommand != "launch-cmd" {
		t.Fatalf("unexpected tmux command %#v", command)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "codex\ntmux-command\n--container\nabc123\n--format\njson" {
		t.Fatalf("unexpected tmux command args %q", string(argsData))
	}
}

func TestMaybeParseRustCodexReportCaptureDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	stdinPath := filepath.Join(dir, "stdin.json")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\ncat >" + shellSingleQuote(stdinPath) + "\nprintf '%s\\n' '{\"segments\":[{\"prompt\":\"first\",\"lines\":[\"line a\"],\"raw\":[\"line a\"]},{\"prompt\":\"second\",\"lines\":[\"• Done\",\"Worked for 5s\"],\"raw\":[\"• Done\",\"Worked for 5s\"]}],\"report\":\"• Done\\nWorked for 5s\"}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	parsed, delegated, err := maybeParseRustCodexReportCapture("› first\nline a\n› second\n• Done\nWorked for 5s", "› first\nline a\n› second\n• Done\nWorked for 5s", 1, false)
	if err != nil {
		t.Fatalf("maybeParseRustCodexReportCapture: %v", err)
	}
	if !delegated {
		t.Fatalf("expected codex report parse to delegate to Rust")
	}
	if parsed == nil || parsed.Report != "• Done\nWorked for 5s" || len(parsed.Segments) != 2 {
		t.Fatalf("unexpected report parse %#v", parsed)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "codex\nreport-parse\n--format\njson" {
		t.Fatalf("unexpected report parse args %q", string(argsData))
	}
	stdinData, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatalf("read stdin file: %v", err)
	}
	if !strings.Contains(string(stdinData), "\"prompt_index\":1") {
		t.Fatalf("expected stdin payload to contain prompt_index, got %q", string(stdinData))
	}
}

func TestBuildRustCodexSpawnPlanArgsIncludesPlannerFlags(t *testing.T) {
	args := buildRustCodexSpawnPlanArgs(rustCodexSpawnPlanRequest{
		Name:          "ferma",
		ProfileID:     "ferma",
		Workspace:     "/tmp/workspace",
		Workdir:       "/workspace/project",
		CodexVolume:   "si-codex-ferma",
		SkillsVolume:  "si-codex-skills",
		GHVolume:      "si-gh-ferma",
		Repo:          "acme/repo",
		GHPAT:         "token-123",
		DockerSocket:  true,
		Detach:        false,
		CleanSlate:    true,
		Image:         "aureuma/si:test",
		Network:       "si-test",
		VaultEnvFile:  "/tmp/workspace/.env",
		IncludeHostSI: true,
	})
	got := strings.Join(args, "\n")
	wantParts := []string{
		"codex",
		"spawn-plan",
		"--format",
		"json",
		"--workspace",
		"/tmp/workspace",
		"--name",
		"ferma",
		"--profile-id",
		"ferma",
		"--workdir",
		"/workspace/project",
		"--codex-volume",
		"si-codex-ferma",
		"--skills-volume",
		"si-codex-skills",
		"--gh-volume",
		"si-gh-ferma",
		"--repo",
		"acme/repo",
		"--gh-pat",
		"token-123",
		"--image",
		"aureuma/si:test",
		"--network",
		"si-test",
		"--vault-env-file",
		"/tmp/workspace/.env",
		"--docker-socket=true",
		"--detach=false",
		"--clean-slate=true",
		"--include-host-si=true",
	}
	for _, part := range wantParts {
		if !strings.Contains(got, part) {
			t.Fatalf("expected args to contain %q, got %q", part, got)
		}
	}
}

func TestMaybeBuildRustCodexSpawnPlanDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	plan := rustCodexSpawnPlan{
		Name:                   "ferma",
		ContainerName:          "si-codex-ferma",
		Image:                  "aureuma/si:test",
		NetworkName:            "si",
		WorkspaceHost:          "/tmp/workspace",
		WorkspacePrimaryTarget: "/workspace",
		WorkspaceMirrorTarget:  "/tmp/workspace",
		Workdir:                "/tmp/workspace",
		CodexVolume:            "si-codex-ferma",
		SkillsVolume:           "si-codex-skills",
		GHVolume:               "si-gh-ferma",
		DockerSocket:           true,
		Detach:                 true,
		Env:                    []string{"HOME=/home/si"},
		Mounts: []rustCodexSpawnPlanMount{
			{Source: "/tmp/workspace", Target: "/workspace"},
		},
	}
	payload, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s' " + shellSingleQuote(string(payload)) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	got, delegated, err := maybeBuildRustCodexSpawnPlan(rustCodexSpawnPlanRequest{
		Name:          "ferma",
		Workspace:     "/tmp/workspace",
		DockerSocket:  true,
		Detach:        true,
		IncludeHostSI: true,
	})
	if err != nil {
		t.Fatalf("maybeBuildRustCodexSpawnPlan: %v", err)
	}
	if !delegated {
		t.Fatalf("expected Rust spawn plan delegation")
	}
	if got == nil {
		t.Fatalf("expected spawn plan payload")
	}
	if got.ContainerName != "si-codex-ferma" {
		t.Fatalf("expected parsed container name, got %#v", got)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if !strings.Contains(string(argsData), "codex\nspawn-plan") {
		t.Fatalf("expected codex spawn-plan invocation, got %q", string(argsData))
	}
}

func TestBuildRustCodexSpawnSpecArgsIncludesSpecFlags(t *testing.T) {
	args := buildRustCodexSpawnSpecArgs(rustCodexSpawnSpecRequest{
		rustCodexSpawnPlanRequest: rustCodexSpawnPlanRequest{
			Name:          "ferma",
			Workspace:     "/tmp/workspace",
			DockerSocket:  true,
			Detach:        true,
			CleanSlate:    false,
			IncludeHostSI: true,
		},
		Command: "echo hello",
		Env:     []string{"FORT_TOKEN=abc"},
		Labels:  []string{"si.codex.profile=ferma"},
		Ports:   []string{"3000:3000"},
	})
	got := strings.Join(args, "\n")
	if !strings.Contains(got, "codex\nspawn-spec") {
		t.Fatalf("expected spawn-spec subcommand, got %q", got)
	}
	if !strings.Contains(got, "--cmd\necho hello") {
		t.Fatalf("expected command flag, got %q", got)
	}
	if !strings.Contains(got, "--env\nFORT_TOKEN=abc") {
		t.Fatalf("expected env flag, got %q", got)
	}
	if !strings.Contains(got, "--label\nsi.codex.profile=ferma") {
		t.Fatalf("expected label flag, got %q", got)
	}
	if !strings.Contains(got, "--port\n3000:3000") {
		t.Fatalf("expected port flag, got %q", got)
	}
}

func TestMaybeBuildRustCodexSpawnSpecDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	spec := rustCodexSpawnSpec{
		Image:         "aureuma/si:test",
		Name:          "si-codex-ferma",
		Network:       "si",
		RestartPolicy: "unless-stopped",
		WorkingDir:    "/tmp/workspace",
		Command:       []string{"bash", "-lc", "echo hello"},
		Env:           []rustCodexSpawnSpecEnv{{Key: "HOME", Value: "/home/si"}},
		BindMounts:    []rustCodexSpawnPlanMount{{Source: "/tmp/workspace", Target: "/workspace"}},
		VolumeMounts:  []rustCodexSpawnSpecVolume{{Source: "si-codex-ferma", Target: "/home/si/.codex"}},
	}
	payload, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s' " + shellSingleQuote(string(payload)) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	got, delegated, err := maybeBuildRustCodexSpawnSpec(rustCodexSpawnSpecRequest{
		rustCodexSpawnPlanRequest: rustCodexSpawnPlanRequest{
			Name:          "ferma",
			Workspace:     "/tmp/workspace",
			DockerSocket:  true,
			Detach:        true,
			IncludeHostSI: true,
		},
		Command: "echo hello",
	})
	if err != nil {
		t.Fatalf("maybeBuildRustCodexSpawnSpec: %v", err)
	}
	if !delegated {
		t.Fatalf("expected Rust spawn spec delegation")
	}
	if got == nil || got.Name != "si-codex-ferma" {
		t.Fatalf("expected parsed spawn spec, got %#v", got)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if !strings.Contains(string(argsData), "codex\nspawn-spec") {
		t.Fatalf("expected codex spawn-spec invocation, got %q", string(argsData))
	}
}

func TestBuildRustCodexSpawnStartArgsIncludesStartFlags(t *testing.T) {
	args := buildRustCodexSpawnStartArgs(rustCodexSpawnSpecRequest{
		rustCodexSpawnPlanRequest: rustCodexSpawnPlanRequest{
			Name:          "ferma",
			Workspace:     "/tmp/workspace",
			DockerSocket:  true,
			Detach:        true,
			IncludeHostSI: true,
		},
		Command: "echo hello",
		Env:     []string{"FORT_TOKEN=abc"},
		Labels:  []string{"si.codex.profile=ferma"},
		Ports:   []string{"3000:3000"},
	})
	got := strings.Join(args, "\n")
	if !strings.Contains(got, "codex\nspawn-start") {
		t.Fatalf("expected spawn-start subcommand, got %q", got)
	}
	if !strings.Contains(got, "--label\nsi.codex.profile=ferma") {
		t.Fatalf("expected label flag, got %q", got)
	}
}

func TestMaybeStartRustCodexSpawnDelegatesAndReturnsContainerID(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' 'container-id-123'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	got, delegated, err := maybeStartRustCodexSpawn(rustCodexSpawnSpecRequest{
		rustCodexSpawnPlanRequest: rustCodexSpawnPlanRequest{
			Name:          "ferma",
			Workspace:     "/tmp/workspace",
			DockerSocket:  true,
			Detach:        true,
			IncludeHostSI: true,
		},
		Command: "echo hello",
		Labels:  []string{"si.codex.profile=ferma"},
	})
	if err != nil {
		t.Fatalf("maybeStartRustCodexSpawn: %v", err)
	}
	if !delegated {
		t.Fatalf("expected Rust spawn-start delegation")
	}
	if got != "container-id-123" {
		t.Fatalf("expected container id, got %q", got)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if !strings.Contains(string(argsData), "codex\nspawn-start") {
		t.Fatalf("expected codex spawn-start invocation, got %q", string(argsData))
	}
}

func TestMaybeBuildRustCodexRemoveArtifactsDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	payload := `{"name":"ferma","container_name":"si-codex-ferma","slug":"ferma","codex_volume":"si-codex-ferma","gh_volume":"si-gh-ferma"}`
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s' " + shellSingleQuote(payload) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	got, delegated, err := maybeBuildRustCodexRemoveArtifacts("ferma")
	if err != nil {
		t.Fatalf("maybeBuildRustCodexRemoveArtifacts: %v", err)
	}
	if !delegated {
		t.Fatalf("expected Rust remove-plan delegation")
	}
	if got == nil || got.ContainerName != "si-codex-ferma" || got.CodexVolume != "si-codex-ferma" {
		t.Fatalf("unexpected remove artifacts: %#v", got)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if !strings.Contains(string(argsData), "codex\nremove-plan\nferma") {
		t.Fatalf("expected codex remove-plan invocation, got %q", string(argsData))
	}
}

func TestMaybeRunRustCodexRemoveDelegatesAndReturnsOutput(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' 'removed'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output, delegated, err := maybeRunRustCodexRemove("ferma", true)
	if err != nil {
		t.Fatalf("maybeRunRustCodexRemove: %v", err)
	}
	if !delegated {
		t.Fatalf("expected codex remove to delegate to Rust")
	}
	if strings.TrimSpace(output) != "removed" {
		t.Fatalf("unexpected output %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "codex\nremove\nferma\n--volumes" {
		t.Fatalf("unexpected Rust CLI args %q", string(argsData))
	}
}

func TestMaybeClearRustFortSessionStateDelegates(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	delegated, err := maybeClearRustFortSessionState("/tmp/session.json")
	if err != nil {
		t.Fatalf("maybeClearRustFortSessionState: %v", err)
	}
	if !delegated {
		t.Fatalf("expected fort session clear to delegate to Rust")
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "fort\nsession-state\nclear\n--path\n/tmp/session.json" {
		t.Fatalf("unexpected Rust CLI args %q", string(argsData))
	}
}

func TestMaybeClearRustFortRuntimeAgentStateDelegates(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	delegated, err := maybeClearRustFortRuntimeAgentState("/tmp/runtime-agent.json")
	if err != nil {
		t.Fatalf("maybeClearRustFortRuntimeAgentState: %v", err)
	}
	if !delegated {
		t.Fatalf("expected fort runtime-agent clear to delegate to Rust")
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "fort\nruntime-agent-state\nclear\n--path\n/tmp/runtime-agent.json" {
		t.Fatalf("unexpected Rust CLI args %q", string(argsData))
	}
}

func TestMaybeLoadRustCodexFortBootstrapDelegatesAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	payload := `{"profile_id":"ferma","agent_id":"si-codex-ferma","session_id":"sess-1","host_url":"http://127.0.0.1:8088","container_host_url":"http://host.docker.internal:8088/","access_token_path":"/tmp/access.token","refresh_token_path":"/tmp/refresh.token","access_token_container_path":"/home/si/.si/access.token","refresh_token_container_path":"/home/si/.si/refresh.token"}`
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' " + shellSingleQuote(payload) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	boot, delegated, err := maybeLoadRustCodexFortBootstrap(
		"/tmp/session.json",
		"ferma",
		"/tmp/access.token",
		"/tmp/refresh.token",
		"/home/si/.si/access.token",
		"/home/si/.si/refresh.token",
	)
	if err != nil {
		t.Fatalf("maybeLoadRustCodexFortBootstrap: %v", err)
	}
	if !delegated {
		t.Fatalf("expected fort bootstrap view to delegate to Rust")
	}
	if boot == nil || boot.AgentID != "si-codex-ferma" || boot.ContainerHostURL != "http://host.docker.internal:8088/" {
		t.Fatalf("unexpected bootstrap view %#v", boot)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if !strings.Contains(string(argsData), "fort\nsession-state\nbootstrap-view\n--path\n/tmp/session.json") {
		t.Fatalf("unexpected Rust CLI args %q", string(argsData))
	}
}
