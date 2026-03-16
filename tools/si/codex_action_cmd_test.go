package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCmdCodexStopDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"action\":\"stop\",\"name\":\"ferma\",\"container_name\":\"si-codex-ferma\",\"output\":\"stopped\"}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output := captureOutputForTest(t, func() {
		cmdCodexStop([]string{"ferma"})
	})
	if !strings.Contains(output, "stopped") {
		t.Fatalf("unexpected output: %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "codex\nstop\nferma\n--format\njson" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestCmdCodexStartDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"action\":\"start\",\"name\":\"ferma\",\"container_name\":\"si-codex-ferma\",\"output\":\"started\"}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output := captureOutputForTest(t, func() {
		cmdCodexStart([]string{"ferma"})
	})
	if !strings.Contains(output, "started") {
		t.Fatalf("unexpected output: %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "codex\nstart\nferma\n--format\njson" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestCmdCodexLogsDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	_ = captureOutputForTest(t, func() {
		cmdCodexLogs([]string{"ferma", "--tail", "25"})
	})
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "codex\nlogs\nferma\n--tail\n25" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestCmdCodexTailDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	_ = captureOutputForTest(t, func() {
		cmdCodexTail([]string{"ferma", "--tail", "25"})
	})
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "codex\ntail\nferma\n--tail\n25" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestCmdCodexCloneDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"name\":\"ferma\",\"repo\":\"acme/repo\",\"container_name\":\"si-codex-ferma\",\"output\":\"cloned\"}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output := captureOutputForTest(t, func() {
		cmdCodexClone([]string{"ferma", "acme/repo", "--gh-pat", "tok"})
	})
	if !strings.Contains(output, "repo acme/repo cloned in si-codex-ferma") {
		t.Fatalf("unexpected output: %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "codex\nclone\nferma\nacme/repo\n--format\njson\n--gh-pat\ntok" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestCmdCodexListDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '[{\"name\":\"si-codex-ferma\",\"state\":\"running\"}]'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output := captureOutputForTest(t, func() {
		cmdCodexList([]string{"--json"})
	})
	if !strings.Contains(output, "\"name\":\"si-codex-ferma\"") {
		t.Fatalf("unexpected output: %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "codex\nlist\n--format\njson" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestCmdCodexExecDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\ncase \"$2\" in\n  remove-plan)\n    printf '%s\\n' '{\"name\":\"ferma\",\"container_name\":\"si-codex-ferma\",\"slug\":\"ferma\",\"codex_volume\":\"si-codex-ferma\",\"gh_volume\":\"si-gh-ferma\"}'\n    ;;\n  exec)\n    ;;\n  *)\n    printf 'unexpected command: %s\\n' \"$2\" >&2\n    exit 1\n    ;;\nesac\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	_ = captureOutputForTest(t, func() {
		cmdCodexExec([]string{"ferma", "--no-tmux", "--", "git", "status"})
	})
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	argsText := strings.TrimSpace(string(argsData))
	if !strings.Contains(argsText, "codex\nexec\nferma") {
		t.Fatalf("unexpected Rust CLI args: %q", argsText)
	}
	if !strings.Contains(argsText, "\n--interactive=true") {
		t.Fatalf("unexpected Rust CLI args: %q", argsText)
	}
	if !strings.Contains(argsText, "\n--tty=false") {
		t.Fatalf("unexpected Rust CLI args: %q", argsText)
	}
	if !strings.Contains(argsText, "\n--env\nSI_TERM_TITLE=ferma") {
		t.Fatalf("unexpected Rust CLI args: %q", argsText)
	}
	if !strings.HasSuffix(argsText, "\n--\n--\ngit\nstatus") {
		t.Fatalf("unexpected Rust CLI args: %q", argsText)
	}
}

func TestCmdCodexRemoveDelegatesToRustCLIWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '{\"name\":\"ferma\",\"container_name\":\"si-codex-ferma\",\"profile_id\":\"\",\"codex_volume\":\"si-codex-ferma\",\"gh_volume\":\"si-gh-ferma\",\"output\":\"removed\"}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	output := captureOutputForTest(t, func() {
		cmdCodexRemove([]string{"ferma"})
	})
	if !strings.Contains(output, "codex container si-codex-ferma removed") {
		t.Fatalf("unexpected output: %q", output)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if strings.TrimSpace(string(argsData)) != "codex\nremove\nferma\n--format\njson" {
		t.Fatalf("unexpected Rust CLI args: %q", string(argsData))
	}
}

func TestCmdCodexRemoveAllUsesBatchFlow(t *testing.T) {
	prev := runCodexRemoveAllFn
	t.Cleanup(func() {
		runCodexRemoveAllFn = prev
	})

	called := false
	runCodexRemoveAllFn = func(removeVolumes bool) error {
		called = true
		if !removeVolumes {
			t.Fatalf("expected removeVolumes to be true")
		}
		return nil
	}

	_ = captureOutputForTest(t, func() {
		cmdCodexRemove([]string{"--all", "--volumes"})
	})

	if !called {
		t.Fatalf("expected batch remove flow")
	}
}

func TestCmdCodexSpawnUsesRustPlanBeforeExecution(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	settingsDir := filepath.Join(home, ".si", "codex")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	settingsToml := "schema_version = 1\n[codex.profiles.entries.ferma]\nname = \"Ferma\"\nemail = \"ferma@example.com\"\n"
	if err := os.WriteFile(filepath.Join(settingsDir, "settings.toml"), []byte(settingsToml), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	workspace := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + shellSingleQuote(argsPath) + "\ncase \"$2\" in\n  remove-plan)\n    printf '%s\\n' '{\"name\":\"ferma\",\"container_name\":\"si-codex-ferma\",\"slug\":\"ferma\",\"codex_volume\":\"si-codex-ferma\",\"gh_volume\":\"si-gh-ferma\"}'\n    ;;\n  spawn-plan)\n    printf '%s\\n' '{\"name\":\"ferma\",\"container_name\":\"si-codex-ferma\",\"workspace_host\":\"" + filepath.ToSlash(workspace) + "\",\"workspace_primary_target\":\"/workspace\",\"workspace_mirror_target\":\"" + filepath.ToSlash(workspace) + "\",\"workdir\":\"" + filepath.ToSlash(workspace) + "\",\"codex_volume\":\"codex-rust\",\"skills_volume\":\"skills-rust\",\"gh_volume\":\"gh-rust\",\"image\":\"image-rust\",\"network_name\":\"si-rust\",\"docker_socket\":true,\"detach\":true,\"clean_slate\":false,\"env\":[\"HOME=/home/si\"]}'\n    ;;\n  spawn-spec)\n    printf '%s\\n' '{\"container_name\":\"si-codex-ferma\",\"image\":\"image-rust\",\"working_dir\":\"" + filepath.ToSlash(workspace) + "\",\"network\":\"si-rust\",\"restart_policy\":\"unless-stopped\",\"command\":[\"bash\",\"-lc\",\"sleep infinity\"],\"env\":[{\"key\":\"HOME\",\"value\":\"/home/si\"}],\"volume_mounts\":[{\"source\":\"codex-rust\",\"target\":\"/home/si/.codex\",\"read_only\":false},{\"source\":\"skills-rust\",\"target\":\"/home/si/.codex/skills\",\"read_only\":false},{\"source\":\"gh-rust\",\"target\":\"/home/si/.config/gh\",\"read_only\":false}],\"bind_mounts\":[]}'\n    ;;\n  *)\n    printf 'unexpected command: %s\\n' \"$2\" >&2\n    exit 1\n    ;;\nesac\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	prev := observeCodexSpawnPreparedFn
	t.Cleanup(func() {
		observeCodexSpawnPreparedFn = prev
	})
	var got codexSpawnPrepared
	observeCodexSpawnPreparedFn = func(prepared codexSpawnPrepared) (bool, error) {
		got = prepared
		return true, nil
	}

	_ = captureOutputForTest(t, func() {
		cmdCodexSpawn([]string{"ferma", "--profile", "ferma", "--workspace", workspace, "--image", "old-image", "--network", "old-net"})
	})

	if got.Name != "ferma" || got.ContainerName != "si-codex-ferma" {
		t.Fatalf("unexpected prepared spawn: %#v", got)
	}
	if got.Image != "image-rust" || got.NetworkName != "si-rust" {
		t.Fatalf("unexpected prepared spawn: %#v", got)
	}
	if got.CodexVolume != "codex-rust" || got.SkillsVolume != "skills-rust" || got.GHVolume != "gh-rust" {
		t.Fatalf("unexpected prepared spawn: %#v", got)
	}
	if got.Workdir != filepath.ToSlash(workspace) || got.DesiredWorkspaceHost != filepath.ToSlash(workspace) {
		t.Fatalf("unexpected prepared spawn: %#v", got)
	}
	if got.ProfileID != "ferma" || !got.DelegatedSpawnPlan || !got.HasRustSpec {
		t.Fatalf("unexpected prepared spawn: %#v", got)
	}

	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	if !strings.Contains(string(argsData), "codex\nspawn-spec") {
		t.Fatalf("unexpected rust cli args: %q", string(argsData))
	}
}

func TestCmdCodexExecTmuxReachesAttachBoundary(t *testing.T) {
	prev := observeCodexExecTmuxPreparedFn
	t.Cleanup(func() {
		observeCodexExecTmuxPreparedFn = prev
	})

	var got codexExecTmuxPrepared
	observeCodexExecTmuxPreparedFn = func(prepared codexExecTmuxPrepared) (bool, error) {
		got = prepared
		return true, nil
	}

	_ = captureOutputForTest(t, func() {
		cmdCodexExec([]string{"ferma", "--tmux"})
	})

	if got.Name != "ferma" || got.ContainerName != "si-codex-ferma" || !got.TmuxMode {
		t.Fatalf("unexpected prepared tmux exec: %#v", got)
	}
}

func TestCodexDelegatedLifecycleSmoke(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >>" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '--' >>" + shellSingleQuote(argsPath) + "\ncase \"$2\" in\n  start)\n    printf '%s\\n' '{\"action\":\"start\",\"name\":\"ferma\",\"container_name\":\"si-codex-ferma\",\"output\":\"started\"}'\n    ;;\n  status-read)\n    if [ \"$5\" = \"json\" ]; then\n      printf '%s\\n' '{\"source\":\"app-server\",\"account_email\":\"ferma@example.com\",\"account_plan\":\"pro\",\"model\":\"gpt-5.2-codex\",\"reasoning_effort\":\"medium\",\"five_hour_left_pct\":75,\"weekly_left_pct\":88}'\n    else\n      printf '%s\\n' 'model=gpt-5.2-codex'\n    fi\n    ;;\n  logs)\n    printf '%s\\n' 'log line'\n    ;;\n  clone)\n    printf '%s\\n' '{\"name\":\"ferma\",\"repo\":\"acme/repo\",\"container_name\":\"si-codex-ferma\",\"output\":\"cloned\"}'\n    ;;\n  stop)\n    printf '%s\\n' '{\"action\":\"stop\",\"name\":\"ferma\",\"container_name\":\"si-codex-ferma\",\"output\":\"stopped\"}'\n    ;;\n  remove)\n    printf '%s\\n' '{\"name\":\"ferma\",\"container_name\":\"si-codex-ferma\",\"profile_id\":\"\",\"codex_volume\":\"si-codex-ferma\",\"gh_volume\":\"si-gh-ferma\",\"output\":\"removed\"}'\n    ;;\n  *)\n    printf 'unexpected command: %s\\n' \"$2\" >&2\n    exit 1\n    ;;\nesac\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	startOutput := captureOutputForTest(t, func() {
		cmdCodexStart([]string{"ferma"})
	})
	if !strings.Contains(startOutput, "started") {
		t.Fatalf("unexpected start output: %q", startOutput)
	}

	statusOutput := captureOutputForTest(t, func() {
		cmdCodexStatus([]string{"ferma", "--json"})
	})
	if !strings.Contains(statusOutput, "\"model\":\"gpt-5.2-codex\"") {
		t.Fatalf("unexpected status output: %q", statusOutput)
	}

	_ = captureOutputForTest(t, func() {
		cmdCodexLogs([]string{"ferma", "--tail", "25"})
	})

	cloneOutput := captureOutputForTest(t, func() {
		cmdCodexClone([]string{"ferma", "acme/repo", "--gh-pat", "tok"})
	})
	if !strings.Contains(cloneOutput, "repo acme/repo cloned in si-codex-ferma") {
		t.Fatalf("unexpected clone output: %q", cloneOutput)
	}

	stopOutput := captureOutputForTest(t, func() {
		cmdCodexStop([]string{"ferma"})
	})
	if !strings.Contains(stopOutput, "stopped") {
		t.Fatalf("unexpected stop output: %q", stopOutput)
	}

	removeOutput := captureOutputForTest(t, func() {
		cmdCodexRemove([]string{"ferma"})
	})
	if !strings.Contains(removeOutput, "codex container si-codex-ferma removed") {
		t.Fatalf("unexpected remove output: %q", removeOutput)
	}

	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	argsText := string(argsData)
	for _, expected := range []string{
		"codex\nstart\nferma\n--format\njson",
		"codex\nstatus-read\nferma\n--format\njson",
		"codex\nlogs\nferma\n--tail\n25",
		"codex\nclone\nferma\nacme/repo\n--format\njson\n--gh-pat\ntok",
		"codex\nstop\nferma\n--format\njson",
		"codex\nremove\nferma\n--format\njson",
	} {
		if !strings.Contains(argsText, expected) {
			t.Fatalf("expected delegated args %q in %q", expected, argsText)
		}
	}
}

func TestCodexDelegatedSpawnAndRemoveProfileMatrix(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	settingsDir := filepath.Join(home, ".si", "codex")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	settingsToml := strings.Join([]string{
		"schema_version = 1",
		"[codex.profiles.entries.ferma]",
		`name = "Ferma"`,
		`email = "ferma@example.com"`,
		"[codex.profiles.entries.berylla]",
		`name = "Berylla"`,
		`email = "berylla@example.com"`,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(settingsDir, "settings.toml"), []byte(settingsToml), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	scriptPath := filepath.Join(dir, "si-rs")
	workspace := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >>" + shellSingleQuote(argsPath) + "\nprintf '%s\\n' '--' >>" + shellSingleQuote(argsPath) + "\ncmd=\"$2\"\nargs=\"$*\"\ncase \"$cmd:$args\" in\n  remove-plan:*ferma*)\n    printf '%s\\n' '{\"name\":\"ferma\",\"container_name\":\"si-codex-ferma\",\"slug\":\"ferma\",\"codex_volume\":\"si-codex-ferma\",\"gh_volume\":\"si-gh-ferma\"}'\n    ;;\n  remove-plan:*berylla*)\n    printf '%s\\n' '{\"name\":\"berylla\",\"container_name\":\"si-codex-berylla\",\"slug\":\"berylla\",\"codex_volume\":\"si-codex-berylla\",\"gh_volume\":\"si-gh-berylla\"}'\n    ;;\n  spawn-plan:*ferma*)\n    printf '%s\\n' '{\"name\":\"ferma\",\"container_name\":\"si-codex-ferma\",\"workspace_host\":\"" + filepath.ToSlash(workspace) + "\",\"workspace_primary_target\":\"/workspace\",\"workspace_mirror_target\":\"" + filepath.ToSlash(workspace) + "\",\"workdir\":\"" + filepath.ToSlash(workspace) + "\",\"codex_volume\":\"codex-ferma-rust\",\"skills_volume\":\"skills-ferma-rust\",\"gh_volume\":\"gh-ferma-rust\",\"image\":\"image-ferma-rust\",\"network_name\":\"si-ferma-rust\",\"docker_socket\":true,\"detach\":true,\"clean_slate\":false,\"env\":[\"HOME=/home/si\"]}'\n    ;;\n  spawn-plan:*berylla*)\n    printf '%s\\n' '{\"name\":\"berylla\",\"container_name\":\"si-codex-berylla\",\"workspace_host\":\"" + filepath.ToSlash(workspace) + "\",\"workspace_primary_target\":\"/workspace\",\"workspace_mirror_target\":\"" + filepath.ToSlash(workspace) + "\",\"workdir\":\"" + filepath.ToSlash(workspace) + "\",\"codex_volume\":\"codex-berylla-rust\",\"skills_volume\":\"skills-berylla-rust\",\"gh_volume\":\"gh-berylla-rust\",\"image\":\"image-berylla-rust\",\"network_name\":\"si-berylla-rust\",\"docker_socket\":true,\"detach\":true,\"clean_slate\":false,\"env\":[\"HOME=/home/si\"]}'\n    ;;\n  spawn-spec:*ferma*)\n    printf '%s\\n' '{\"container_name\":\"si-codex-ferma\",\"image\":\"image-ferma-rust\",\"working_dir\":\"" + filepath.ToSlash(workspace) + "\",\"network\":\"si-ferma-rust\",\"restart_policy\":\"unless-stopped\",\"command\":[\"bash\",\"-lc\",\"sleep infinity\"],\"env\":[{\"key\":\"HOME\",\"value\":\"/home/si\"}],\"volume_mounts\":[],\"bind_mounts\":[]}'\n    ;;\n  spawn-spec:*berylla*)\n    printf '%s\\n' '{\"container_name\":\"si-codex-berylla\",\"image\":\"image-berylla-rust\",\"working_dir\":\"" + filepath.ToSlash(workspace) + "\",\"network\":\"si-berylla-rust\",\"restart_policy\":\"unless-stopped\",\"command\":[\"bash\",\"-lc\",\"sleep infinity\"],\"env\":[{\"key\":\"HOME\",\"value\":\"/home/si\"}],\"volume_mounts\":[],\"bind_mounts\":[]}'\n    ;;\n  remove:*ferma*)\n    printf '%s\\n' '{\"name\":\"ferma\",\"container_name\":\"si-codex-ferma\",\"profile_id\":\"ferma\",\"codex_volume\":\"si-codex-ferma\",\"gh_volume\":\"si-gh-ferma\",\"output\":\"removed\"}'\n    ;;\n  remove:*berylla*)\n    printf '%s\\n' '{\"name\":\"berylla\",\"container_name\":\"si-codex-berylla\",\"profile_id\":\"berylla\",\"codex_volume\":\"si-codex-berylla\",\"gh_volume\":\"si-gh-berylla\",\"output\":\"removed\"}'\n    ;;\n  *)\n    printf 'unexpected command: %s %s\\n' \"$cmd\" \"$args\" >&2\n    exit 1\n    ;;\nesac\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv(siRustCLIBinEnv, scriptPath)
	t.Setenv(siExperimentalRustCLIEnv, "")

	cases := []struct {
		name            string
		wantImage       string
		wantNetwork     string
		wantCodexVolume string
		wantRemoveText  string
	}{
		{name: "ferma", wantImage: "image-ferma-rust", wantNetwork: "si-ferma-rust", wantCodexVolume: "codex-ferma-rust", wantRemoveText: "si-codex-ferma"},
		{name: "berylla", wantImage: "image-berylla-rust", wantNetwork: "si-berylla-rust", wantCodexVolume: "codex-berylla-rust", wantRemoveText: "si-codex-berylla"},
	}

	prev := observeCodexSpawnPreparedFn
	t.Cleanup(func() {
		observeCodexSpawnPreparedFn = prev
	})
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var got codexSpawnPrepared
			observeCodexSpawnPreparedFn = func(prepared codexSpawnPrepared) (bool, error) {
				got = prepared
				return true, nil
			}

			_ = captureOutputForTest(t, func() {
				cmdCodexSpawn([]string{tc.name, "--profile", tc.name, "--workspace", workspace})
			})
			if got.Name != tc.name || got.ProfileID != tc.name {
				t.Fatalf("unexpected prepared spawn: %#v", got)
			}
			if got.Image != tc.wantImage || got.NetworkName != tc.wantNetwork || got.CodexVolume != tc.wantCodexVolume {
				t.Fatalf("unexpected prepared spawn: %#v", got)
			}

			removeOutput := captureOutputForTest(t, func() {
				cmdCodexRemove([]string{tc.name})
			})
			if !strings.Contains(removeOutput, tc.wantRemoveText) {
				t.Fatalf("unexpected remove output: %q", removeOutput)
			}
		})
	}

	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	argsText := string(argsData)
	for _, expected := range []string{
		"codex\nspawn-plan\n--format\njson",
		"--name\nferma",
		"codex\nspawn-spec\n--format\njson",
		"codex\nremove\nferma\n--format\njson",
		"--name\nberylla",
		"codex\nremove\nberylla\n--format\njson",
	} {
		if !strings.Contains(argsText, expected) {
			t.Fatalf("expected delegated args %q in %q", expected, argsText)
		}
	}
}
