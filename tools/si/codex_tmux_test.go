package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
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
	noTmux := false
	args := consumeRunContainerModeFlags([]string{"berylla", "--tmux", "bash"}, &tmux, &noTmux)
	if !tmux {
		t.Fatalf("expected tmux to be enabled")
	}
	if noTmux {
		t.Fatalf("expected no-tmux to remain false")
	}
	if len(args) != 2 || args[0] != "berylla" || args[1] != "bash" {
		t.Fatalf("unexpected args after consume: %v", args)
	}
}

func TestConsumeRunContainerModeFlagsStopsAtCommand(t *testing.T) {
	tmux := false
	noTmux := false
	args := consumeRunContainerModeFlags([]string{"berylla", "printf", "%s\n", "--tmux"}, &tmux, &noTmux)
	if tmux {
		t.Fatalf("expected tmux flag to remain false once command args begin")
	}
	if noTmux {
		t.Fatalf("expected no-tmux flag to remain false once command args begin")
	}
	if len(args) != 4 || args[0] != "berylla" || args[1] != "printf" || args[3] != "--tmux" {
		t.Fatalf("unexpected args after consume: %v", args)
	}
}

func TestConsumeRunContainerModeFlagsNoTmux(t *testing.T) {
	tmux := false
	noTmux := false
	args := consumeRunContainerModeFlags([]string{"berylla", "--no-tmux", "bash"}, &tmux, &noTmux)
	if tmux {
		t.Fatalf("expected tmux to remain false")
	}
	if !noTmux {
		t.Fatalf("expected no-tmux to be enabled")
	}
	if len(args) != 2 || args[0] != "berylla" || args[1] != "bash" {
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

func TestContainerCwdForHostCwdPrefersMirrorOverWorkspaceOnTie(t *testing.T) {
	info := &types.ContainerJSON{
		Mounts: []types.MountPoint{
			{Type: "bind", Source: "/home/ubuntu/Development/si", Destination: "/workspace"},
			{Type: "bind", Source: "/home/ubuntu/Development/si", Destination: "/home/ubuntu/Development/si"},
		},
	}
	got, ok := containerCwdForHostCwd(info, "/home/ubuntu/Development/si")
	if !ok {
		t.Fatalf("expected mapping to succeed")
	}
	if got != "/home/ubuntu/Development/si" {
		t.Fatalf("unexpected container cwd: %q", got)
	}
}

func TestContainerCwdForHostCwdPrefersSamePathOnTie(t *testing.T) {
	info := &types.ContainerJSON{
		Mounts: []types.MountPoint{
			{Type: "bind", Source: "/home/ubuntu/Development", Destination: "/home/si/Development"},
			{Type: "bind", Source: "/home/ubuntu/Development", Destination: "/home/ubuntu/Development"},
		},
	}
	got, ok := containerCwdForHostCwd(info, "/home/ubuntu/Development/core")
	if !ok {
		t.Fatalf("expected mapping to succeed")
	}
	if got != "/home/ubuntu/Development/core" {
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

func TestCodexTmuxShouldResetSession(t *testing.T) {
	if !codexTmuxShouldResetSession(true, false, false) {
		t.Fatalf("expected pane dead to force reset")
	}
	if codexTmuxShouldResetSession(false, true, false) {
		t.Fatalf("expected cmd hash drift to preserve live session")
	}
	if codexTmuxShouldResetSession(false, false, true) {
		t.Fatalf("expected host cwd drift to preserve live session")
	}
	if codexTmuxShouldResetSession(false, true, true) {
		t.Fatalf("expected metadata drift to preserve live session")
	}
}

func TestCodexRunShouldRecreateContainerForMissingVaultMounts(t *testing.T) {
	if codexRunShouldRecreateContainerForMissingVaultMounts(true) {
		t.Fatalf("expected tmux mode to preserve container for session continuity")
	}
	if !codexRunShouldRecreateContainerForMissingVaultMounts(false) {
		t.Fatalf("expected non-tmux mode to reconcile container mounts")
	}
}

func TestReconcileCodexRunMountDriftTmuxPreservesSession(t *testing.T) {
	called := false
	err := reconcileCodexRunMountDrift(true, "si-codex-cadma", "cadma", func() error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Fatalf("expected tmux mode to skip recreate callback")
	}
}

func TestReconcileCodexRunMountDriftNonTmuxRecreates(t *testing.T) {
	called := false
	err := reconcileCodexRunMountDrift(false, "si-codex-cadma", "cadma", func() error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatalf("expected non-tmux mode to execute recreate callback")
	}
}

func TestReconcileCodexRunMountDriftPropagatesRecreateError(t *testing.T) {
	expected := errors.New("boom")
	err := reconcileCodexRunMountDrift(false, "si-codex-cadma", "cadma", func() error {
		return expected
	})
	if !errors.Is(err, expected) {
		t.Fatalf("expected recreate error propagation, got %v", err)
	}
}

func TestEnsureCodexTmuxSessionPreservesLiveSessionOnHostCwdChange(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session := fmt.Sprintf("si-test-codex-cwd-%d", time.Now().UnixNano())
	target := session + ":0.0"
	defer func() {
		_, _ = tmuxOutput(context.Background(), "kill-session", "-t", session)
	}()

	if err := ensureCodexTmuxSession(ctx, session, target, "sleep 120", "", "hash-one", "/tmp/si-tmux-a", "", ""); err != nil {
		t.Fatalf("first ensure failed: %v", err)
	}
	pidA, err := tmuxOutput(ctx, "display-message", "-p", "-t", target, "#{pane_pid}")
	if err != nil {
		t.Fatalf("failed to fetch pane pid: %v", err)
	}

	if err := ensureCodexTmuxSession(ctx, session, target, "sleep 120", "", "hash-one", "/tmp/si-tmux-b", "", ""); err != nil {
		t.Fatalf("second ensure failed: %v", err)
	}
	pidB, err := tmuxOutput(ctx, "display-message", "-p", "-t", target, "#{pane_pid}")
	if err != nil {
		t.Fatalf("failed to fetch pane pid after cwd change: %v", err)
	}

	if strings.TrimSpace(pidA) == "" || strings.TrimSpace(pidB) == "" {
		t.Fatalf("expected non-empty pane pids, got %q and %q", pidA, pidB)
	}
	if strings.TrimSpace(pidA) != strings.TrimSpace(pidB) {
		t.Fatalf("expected live tmux session to persist across cwd changes; pane pid changed from %q to %q", pidA, pidB)
	}
}

func TestEnsureCodexTmuxSessionPreservesLiveSessionOnCmdHashChange(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session := fmt.Sprintf("si-test-codex-hash-%d", time.Now().UnixNano())
	target := session + ":0.0"
	defer func() {
		_, _ = tmuxOutput(context.Background(), "kill-session", "-t", session)
	}()

	if err := ensureCodexTmuxSession(ctx, session, target, "sleep 120", "", "hash-before", "/tmp/si-tmux-hash", "", ""); err != nil {
		t.Fatalf("first ensure failed: %v", err)
	}
	pidA, err := tmuxOutput(ctx, "display-message", "-p", "-t", target, "#{pane_pid}")
	if err != nil {
		t.Fatalf("failed to fetch pane pid: %v", err)
	}

	if err := ensureCodexTmuxSession(ctx, session, target, "sleep 120", "", "hash-after", "/tmp/si-tmux-hash", "", ""); err != nil {
		t.Fatalf("second ensure failed: %v", err)
	}
	pidB, err := tmuxOutput(ctx, "display-message", "-p", "-t", target, "#{pane_pid}")
	if err != nil {
		t.Fatalf("failed to fetch pane pid after hash change: %v", err)
	}

	if strings.TrimSpace(pidA) == "" || strings.TrimSpace(pidB) == "" {
		t.Fatalf("expected non-empty pane pids, got %q and %q", pidA, pidB)
	}
	if strings.TrimSpace(pidA) != strings.TrimSpace(pidB) {
		t.Fatalf("expected live tmux session to persist across hash changes; pane pid changed from %q to %q", pidA, pidB)
	}
}

func TestApplyTmuxSessionDefaultsSetsDestroyUnattachedOff(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session := fmt.Sprintf("si-test-tmux-defaults-%d", time.Now().UnixNano())
	defer func() {
		_, _ = tmuxOutput(context.Background(), "kill-session", "-t", session)
	}()

	if _, err := tmuxOutput(ctx, "new-session", "-d", "-s", session, "sleep", "120"); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	applyTmuxSessionDefaults(ctx, session)

	out, err := tmuxOutput(ctx, "show-options", "-v", "-t", session, "destroy-unattached")
	if err != nil {
		t.Fatalf("failed to read destroy-unattached option: %v", err)
	}
	if strings.TrimSpace(out) != "off" {
		t.Fatalf("expected destroy-unattached off, got %q", out)
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

func TestCodexContainerWorkspaceMatchesRequiresHostSiMount(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".si"), 0o700); err != nil {
		t.Fatalf("mkdir .si: %v", err)
	}
	desiredHost := "/home/ubuntu/Development/si"
	mirror := desiredHost
	info := &types.ContainerJSON{
		Config: &container.Config{
			WorkingDir: mirror,
			Env: []string{
				"SI_WORKSPACE_MIRROR=" + mirror,
				"SI_WORKSPACE_HOST=" + desiredHost,
			},
		},
		Mounts: []types.MountPoint{
			{Type: "bind", Source: desiredHost, Destination: "/workspace"},
			{Type: "bind", Source: desiredHost, Destination: mirror},
		},
	}
	if codexContainerWorkspaceMatches(info, desiredHost, mirror, "") {
		t.Fatalf("expected match to fail when host ~/.si mount is missing")
	}
	info.Mounts = append(info.Mounts, types.MountPoint{
		Type:        "bind",
		Source:      filepath.Join(home, ".si"),
		Destination: "/home/si/.si",
	})
	if !codexContainerWorkspaceMatches(info, desiredHost, mirror, "") {
		t.Fatalf("expected match when workspace and ~/.si mounts are present")
	}
}

func TestCodexContainerWorkspaceMatchesRequiresHostDockerAndGoToolingMounts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".si"), 0o700); err != nil {
		t.Fatalf("mkdir .si: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".docker"), 0o700); err != nil {
		t.Fatalf("mkdir .docker: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".local", "share", "si", "go"), 0o700); err != nil {
		t.Fatalf("mkdir host go dir: %v", err)
	}
	desiredHost := "/home/ubuntu/Development/si"
	mirror := desiredHost
	info := &types.ContainerJSON{
		Config: &container.Config{
			WorkingDir: mirror,
			Env: []string{
				"SI_WORKSPACE_MIRROR=" + mirror,
				"SI_WORKSPACE_HOST=" + desiredHost,
			},
		},
		Mounts: []types.MountPoint{
			{Type: "bind", Source: desiredHost, Destination: "/workspace"},
			{Type: "bind", Source: desiredHost, Destination: mirror},
			{Type: "bind", Source: filepath.Join(home, ".si"), Destination: "/home/si/.si"},
		},
	}
	if codexContainerWorkspaceMatches(info, desiredHost, mirror, "") {
		t.Fatalf("expected match to fail when host docker/go mounts are missing")
	}
	info.Mounts = append(info.Mounts,
		types.MountPoint{
			Type:        "bind",
			Source:      filepath.Join(home, ".docker"),
			Destination: "/home/si/.docker",
		},
		types.MountPoint{
			Type:        "bind",
			Source:      filepath.Join(home, ".local", "share", "si", "go"),
			Destination: "/home/si/.local/share/si/go",
		},
	)
	if !codexContainerWorkspaceMatches(info, desiredHost, mirror, "") {
		t.Fatalf("expected match when host docker/go mounts are present")
	}
}

func TestCodexContainerWorkspaceSource(t *testing.T) {
	info := &types.ContainerJSON{
		Mounts: []types.MountPoint{
			{Type: "bind", Source: "/tmp/other", Destination: "/tmp/other"},
			{Type: "volume", Source: "workspace", Destination: "/workspace"},
			{Type: "bind", Source: "/home/ubuntu/Development/si", Destination: "/workspace"},
		},
	}
	if got := codexContainerWorkspaceSource(info); got != "/home/ubuntu/Development/si" {
		t.Fatalf("unexpected workspace source: %q", got)
	}
}

func TestCodexContainerWorkspaceSourceMissing(t *testing.T) {
	info := &types.ContainerJSON{
		Mounts: []types.MountPoint{
			{Type: "bind", Source: "/tmp/other", Destination: "/tmp/other"},
		},
	}
	if got := codexContainerWorkspaceSource(info); got != "" {
		t.Fatalf("expected empty workspace source, got %q", got)
	}
}

func TestCodexContainerWorkspaceMatchesRequiresVaultEnvFileMount(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".si"), 0o700); err != nil {
		t.Fatalf("mkdir .si: %v", err)
	}
	vaultFile := filepath.Join(t.TempDir(), ".env.vault")
	if err := os.WriteFile(vaultFile, []byte("KEY=value\n"), 0o600); err != nil {
		t.Fatalf("write vault file: %v", err)
	}
	desiredHost := "/home/ubuntu/Development/si"
	mirror := desiredHost
	info := &types.ContainerJSON{
		Config: &container.Config{
			WorkingDir: mirror,
			Env: []string{
				"SI_WORKSPACE_MIRROR=" + mirror,
				"SI_WORKSPACE_HOST=" + desiredHost,
			},
		},
		Mounts: []types.MountPoint{
			{Type: "bind", Source: desiredHost, Destination: "/workspace"},
			{Type: "bind", Source: desiredHost, Destination: mirror},
			{Type: "bind", Source: filepath.Join(home, ".si"), Destination: "/home/si/.si"},
		},
	}
	if codexContainerWorkspaceMatches(info, desiredHost, mirror, vaultFile) {
		t.Fatalf("expected match to fail when vault env file mount is missing")
	}
	info.Mounts = append(info.Mounts, types.MountPoint{
		Type:        "bind",
		Source:      vaultFile,
		Destination: filepath.ToSlash(vaultFile),
	})
	if !codexContainerWorkspaceMatches(info, desiredHost, mirror, vaultFile) {
		t.Fatalf("expected match when vault env file mount is present")
	}
}

func TestCodexContainerWorkspaceMatchesRequiresDevelopmentMount(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".si"), 0o700); err != nil {
		t.Fatalf("mkdir .si: %v", err)
	}
	desiredHost := filepath.Join(home, "Development", "si")
	mirror := filepath.ToSlash(desiredHost)
	info := &types.ContainerJSON{
		Config: &container.Config{
			WorkingDir: mirror,
			Env: []string{
				"SI_WORKSPACE_MIRROR=" + mirror,
				"SI_WORKSPACE_HOST=" + desiredHost,
			},
		},
		Mounts: []types.MountPoint{
			{Type: "bind", Source: desiredHost, Destination: "/workspace"},
			{Type: "bind", Source: desiredHost, Destination: mirror},
			{Type: "bind", Source: filepath.Join(home, ".si"), Destination: "/home/si/.si"},
		},
	}
	if codexContainerWorkspaceMatches(info, desiredHost, mirror, "") {
		t.Fatalf("expected match to fail when ~/Development mount is missing")
	}
	info.Mounts = append(info.Mounts, types.MountPoint{
		Type:        "bind",
		Source:      filepath.Join(home, "Development"),
		Destination: "/home/si/Development",
	})
	if codexContainerWorkspaceMatches(info, desiredHost, mirror, "") {
		t.Fatalf("expected match to fail when same-path ~/Development mount is missing")
	}
	info.Mounts = append(info.Mounts, types.MountPoint{
		Type:        "bind",
		Source:      filepath.Join(home, "Development"),
		Destination: filepath.ToSlash(filepath.Join(home, "Development")),
	})
	if !codexContainerWorkspaceMatches(info, desiredHost, mirror, "") {
		t.Fatalf("expected match when ~/Development mount is present")
	}
}
