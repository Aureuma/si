package main

import (
	"strings"
	"testing"

	"github.com/docker/docker/api/types"
)

func TestCodexTmuxSessionName(t *testing.T) {
	got := codexTmuxSessionName("si-codex-einsteina")
	if got != "si-codex-pane-einsteina" {
		t.Fatalf("unexpected tmux session name %q", got)
	}
}

func TestValidateRunTmuxArgs(t *testing.T) {
	if err := validateRunTmuxArgs(true, []string{"bash"}); err == nil {
		t.Fatalf("expected tmux+cmd validation error")
	}
	if err := validateRunTmuxArgs(true, nil); err != nil {
		t.Fatalf("unexpected tmux validation error: %v", err)
	}
}

func TestConsumeRunContainerModeFlags(t *testing.T) {
	tmux := false
	args := consumeRunContainerModeFlags([]string{"berylla", "--tmux", "bash"}, &tmux)
	if !tmux {
		t.Fatalf("expected tmux to be enabled")
	}
	if len(args) != 2 || args[0] != "berylla" || args[1] != "bash" {
		t.Fatalf("unexpected args after consume: %v", args)
	}
}

func TestConsumeRunContainerModeFlagsStopsAtCommand(t *testing.T) {
	tmux := false
	args := consumeRunContainerModeFlags([]string{"berylla", "printf", "%s\n", "--tmux"}, &tmux)
	if tmux {
		t.Fatalf("expected tmux flag to remain false once command args begin")
	}
	if len(args) != 4 || args[0] != "berylla" || args[1] != "printf" || args[3] != "--tmux" {
		t.Fatalf("unexpected args after consume: %v", args)
	}
}

func TestBuildCodexTmuxCommandUsesBypassFlag(t *testing.T) {
	cmd := buildCodexTmuxCommand("si-codex-berylla", "/home/ubuntu/Development/si")
	if !strings.Contains(cmd, "codex --dangerously-bypass-approvals-and-sandbox") {
		t.Fatalf("expected tmux command to use codex bypass flag, got: %s", cmd)
	}
	if !strings.Contains(cmd, "exec bash -il") {
		t.Fatalf("expected tmux command to keep pane alive with interactive shell, got: %s", cmd)
	}
	if !strings.Contains(cmd, "sudo -n") {
		t.Fatalf("expected tmux command to keep sudo fallback, got: %s", cmd)
	}
	if !strings.Contains(cmd, "/home/ubuntu/Development/si") {
		t.Fatalf("expected tmux command to cd to host cwd, got: %s", cmd)
	}
}

func TestContainerCwdForHostCwdExactMatch(t *testing.T) {
	info := &types.ContainerJSON{
		Mounts: []types.MountPoint{
			{Type: "bind", Source: "/home/ubuntu/Development/si", Destination: "/workspace"},
		},
	}
	got, ok := containerCwdForHostCwd(info, "/home/ubuntu/Development/si")
	if !ok {
		t.Fatalf("expected mapping to succeed")
	}
	if got != "/workspace" {
		t.Fatalf("unexpected container cwd: %q", got)
	}
}

func TestContainerCwdForHostCwdLongestPrefixWins(t *testing.T) {
	info := &types.ContainerJSON{
		Mounts: []types.MountPoint{
			{Type: "bind", Source: "/home/ubuntu", Destination: "/mnt/ubuntu"},
			{Type: "bind", Source: "/home/ubuntu/Development/si", Destination: "/workspace"},
		},
	}
	got, ok := containerCwdForHostCwd(info, "/home/ubuntu/Development/si/tools/si")
	if !ok {
		t.Fatalf("expected mapping to succeed")
	}
	if got != "/workspace/tools/si" {
		t.Fatalf("unexpected container cwd: %q", got)
	}
}

func TestContainerCwdForHostCwdNoMatch(t *testing.T) {
	info := &types.ContainerJSON{
		Mounts: []types.MountPoint{
			{Type: "bind", Source: "/opt/project", Destination: "/workspace"},
		},
	}
	if got, ok := containerCwdForHostCwd(info, "/home/ubuntu/Development/si"); ok || got != "" {
		t.Fatalf("expected mapping to fail, got ok=%v cwd=%q", ok, got)
	}
}

func TestBuildTmuxCodexCommandUsesBypassFlag(t *testing.T) {
	cmd := buildTmuxCodexCommand("abc123")
	if !strings.Contains(cmd, "codex --dangerously-bypass-approvals-and-sandbox") {
		t.Fatalf("expected report/status tmux command to use codex bypass flag, got: %s", cmd)
	}
}

func TestIsTmuxPaneDeadOutput(t *testing.T) {
	if !isTmuxPaneDeadOutput("1\n") {
		t.Fatalf("expected pane_dead output to be true")
	}
	if isTmuxPaneDeadOutput("0\n") {
		t.Fatalf("expected pane_dead output to be false")
	}
}

func TestEnvValue(t *testing.T) {
	env := []string{"CODEX_MODEL=gpt-5.2-codex", "CODEX_REASONING_EFFORT=high"}
	if got := envValue(env, "CODEX_MODEL"); got != "gpt-5.2-codex" {
		t.Fatalf("unexpected env value %q", got)
	}
	if got := envValue(env, "MISSING"); got != "" {
		t.Fatalf("expected empty missing env value, got %q", got)
	}
}

func TestCodexAuthVolumeFromContainerInfo(t *testing.T) {
	info := &types.ContainerJSON{
		Mounts: []types.MountPoint{
			{
				Type:        "volume",
				Name:        "si-codex-america",
				Destination: "/home/si/.codex",
			},
		},
	}
	if got := codexAuthVolumeFromContainerInfo(info); got != "si-codex-america" {
		t.Fatalf("unexpected auth volume %q", got)
	}
}

func TestCodexContainerConfigTargets(t *testing.T) {
	targets := codexContainerConfigTargets()
	if len(targets) != 2 {
		t.Fatalf("unexpected config target count: %d", len(targets))
	}
	if targets[0].Path != "/home/si/.codex/config.toml" || targets[0].Owner != "si:si" {
		t.Fatalf("unexpected first target: %+v", targets[0])
	}
	if targets[1].Path != "/root/.codex/config.toml" || targets[1].Owner != "root:root" {
		t.Fatalf("unexpected second target: %+v", targets[1])
	}
}
