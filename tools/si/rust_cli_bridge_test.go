package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
