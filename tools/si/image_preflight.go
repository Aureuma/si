package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func runImageBuildPreflight(repoRoot string) error {
	repoRoot = strings.TrimSpace(repoRoot)
	if repoRoot == "" {
		return fmt.Errorf("repo root is required for image preflight")
	}
	scriptPath := filepath.Join(repoRoot, "tools", "si-image", "preflight-codex-upgrade.sh")
	if _, err := os.Stat(scriptPath); err != nil {
		return fmt.Errorf("preflight script not found: %w", err)
	}
	infof("running codex image preflight: %s", scriptPath)
	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append([]string{}, os.Environ()...)
	cmd.Env = append(cmd.Env, "NO_COLOR=1")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("preflight script failed: %w", err)
	}
	return nil
}

