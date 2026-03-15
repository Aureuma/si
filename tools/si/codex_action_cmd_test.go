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
