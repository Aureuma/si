package main

import (
	"strings"
	"testing"

	"github.com/docker/docker/api/types"
)

func TestAutopoieticContainerName(t *testing.T) {
	got := autopoieticContainerName("si-codex-berylla")
	if got != "si-autopoietic-berylla" {
		t.Fatalf("unexpected autopoietic container name %q", got)
	}
}

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
	autopo := false
	tmux := false
	args := consumeRunContainerModeFlags([]string{"berylla", "--autopoietic", "--tmux", "bash"}, &autopo, &tmux)
	if !autopo || !tmux {
		t.Fatalf("expected both flags to be enabled")
	}
	if len(args) != 2 || args[0] != "berylla" || args[1] != "bash" {
		t.Fatalf("unexpected args after consume: %v", args)
	}
}

func TestConsumeRunContainerModeFlagsStopsAtCommand(t *testing.T) {
	autopo := false
	tmux := false
	args := consumeRunContainerModeFlags([]string{"berylla", "printf", "%s\n", "--tmux"}, &autopo, &tmux)
	if autopo || tmux {
		t.Fatalf("expected mode flags to remain false once command args begin")
	}
	if len(args) != 4 || args[0] != "berylla" || args[1] != "printf" || args[3] != "--tmux" {
		t.Fatalf("unexpected args after consume: %v", args)
	}
}

func TestBuildCodexTmuxCommandUsesBypassFlag(t *testing.T) {
	cmd := buildCodexTmuxCommand("si-codex-berylla")
	if !strings.Contains(cmd, "codex --dangerously-bypass-approvals-and-sandbox") {
		t.Fatalf("expected tmux command to use codex bypass flag, got: %s", cmd)
	}
	if !strings.Contains(cmd, "sudo -n") {
		t.Fatalf("expected tmux command to keep sudo fallback, got: %s", cmd)
	}
}

func TestBuildTmuxCodexCommandUsesBypassFlag(t *testing.T) {
	cmd := buildTmuxCodexCommand("abc123")
	if !strings.Contains(cmd, "codex --dangerously-bypass-approvals-and-sandbox") {
		t.Fatalf("expected report/status tmux command to use codex bypass flag, got: %s", cmd)
	}
}

func TestAutopoieticCodexVolumePrefersActorMount(t *testing.T) {
	info := &types.ContainerJSON{
		Mounts: []types.MountPoint{
			{
				Type:        "volume",
				Name:        "si-codex-cadma",
				Destination: "/home/si/.codex",
			},
		},
	}
	got := autopoieticCodexVolume(info, "cadma")
	if got != "si-codex-cadma" {
		t.Fatalf("unexpected codex volume %q", got)
	}
}

func TestAutopoieticCodexVolumeFallback(t *testing.T) {
	got := autopoieticCodexVolume(nil, "gadolina")
	if got != "si-codex-gadolina" {
		t.Fatalf("unexpected fallback codex volume %q", got)
	}
}

func TestAutopoieticWorkspaceSource(t *testing.T) {
	info := &types.ContainerJSON{
		Mounts: []types.MountPoint{
			{
				Type:        "bind",
				Source:      "/home/ubuntu/Development/si",
				Destination: "/workspace",
			},
		},
	}
	got := autopoieticWorkspaceSource(info)
	if got != "/home/ubuntu/Development/si" {
		t.Fatalf("unexpected workspace source %q", got)
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
