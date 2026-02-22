package main

import (
	"bytes"
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
	goBin := "go"
	// Reuse SI's managed toolchain resolution so preflight does not fail just
	// because host PATH does not currently include go.
	if _, err := os.Stat(filepath.Join(repoRoot, "tools", "si", "go.mod")); err == nil {
		resolved, err := resolveGoForSelfBuild(repoRoot, filepath.Join(repoRoot, "si"), "go")
		if err != nil {
			return fmt.Errorf("resolve go for image preflight: %w", err)
		}
		goBin = resolved
	}

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append([]string{}, os.Environ()...)
	cmd.Env = append(cmd.Env, "NO_COLOR=1")
	cmd.Env = append(cmd.Env, "SI_GO_BIN="+goBin)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		logs := strings.TrimSpace(output.String())
		if logs == "" {
			return fmt.Errorf("preflight script failed: %w", err)
		}
		return fmt.Errorf("preflight script failed: %w\n%s", err, logs)
	}
	return nil
}
