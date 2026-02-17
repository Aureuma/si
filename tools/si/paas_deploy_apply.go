package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const paasSCPBinEnvKey = "SI_PAAS_SCP_BIN"

func applyPaasReleaseToTargets(apply bool, selectedTargets []string, bundleDir, releaseID, remoteRoot string, timeout time.Duration) ([]string, error) {
	if !apply {
		return nil, nil
	}
	targets, err := resolvePaasDeployTargets(selectedTargets)
	if err != nil {
		return nil, err
	}
	applied := make([]string, 0, len(targets))
	for _, target := range targets {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		err := runPaasRemoteComposeApply(ctx, target, bundleDir, releaseID, remoteRoot)
		cancel()
		if err != nil {
			return applied, fmt.Errorf("target %s apply failed: %w", target.Name, err)
		}
		applied = append(applied, target.Name)
	}
	return applied, nil
}

func resolvePaasDeployTargets(selectedTargets []string) ([]paasTarget, error) {
	store, err := loadPaasTargetStore(currentPaasContext())
	if err != nil {
		return nil, err
	}
	if len(store.Targets) == 0 {
		return nil, fmt.Errorf("no targets configured")
	}
	if len(selectedTargets) == 0 {
		current := strings.TrimSpace(store.CurrentTarget)
		if current == "" {
			return nil, fmt.Errorf("no target selected: pass --target/--targets or run `si paas target use --target <id>`")
		}
		idx := findPaasTarget(store, current)
		if idx == -1 {
			return nil, fmt.Errorf("current target %q not found", current)
		}
		return []paasTarget{store.Targets[idx]}, nil
	}
	if len(selectedTargets) == 1 && strings.EqualFold(strings.TrimSpace(selectedTargets[0]), "all") {
		return append([]paasTarget(nil), store.Targets...), nil
	}
	out := make([]paasTarget, 0, len(selectedTargets))
	for _, raw := range selectedTargets {
		needle := strings.TrimSpace(raw)
		if needle == "" {
			continue
		}
		idx := findPaasTarget(store, needle)
		if idx == -1 {
			return nil, fmt.Errorf("target %q not found", needle)
		}
		out = append(out, store.Targets[idx])
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no targets resolved")
	}
	return out, nil
}

func runPaasRemoteComposeApply(ctx context.Context, target paasTarget, localBundleDir, releaseID, remoteRoot string) error {
	releaseDir := path.Join(strings.TrimSpace(remoteRoot), sanitizePaasReleasePathSegment(releaseID))
	if strings.TrimSpace(releaseDir) == "" {
		return fmt.Errorf("invalid remote release directory")
	}
	if _, err := runPaasSSHCommand(ctx, target, fmt.Sprintf("mkdir -p %s", quoteSingle(releaseDir))); err != nil {
		return err
	}
	paths := []string{
		filepath.Join(strings.TrimSpace(localBundleDir), "compose.yaml"),
		filepath.Join(strings.TrimSpace(localBundleDir), "release.json"),
	}
	for _, src := range paths {
		if err := runPaasSCPUpload(ctx, target, src, releaseDir); err != nil {
			return err
		}
	}
	remoteCmd := fmt.Sprintf("cd %s && docker compose -f compose.yaml pull && docker compose -f compose.yaml up -d --remove-orphans", quoteSingle(releaseDir))
	if _, err := runPaasSSHCommand(ctx, target, remoteCmd); err != nil {
		return err
	}
	return nil
}

func runPaasSCPUpload(ctx context.Context, target paasTarget, srcPath, remoteDir string) error {
	scpBin := strings.TrimSpace(os.Getenv(paasSCPBinEnvKey))
	if scpBin == "" {
		scpBin = "scp"
	}
	absSrc, err := filepath.Abs(strings.TrimSpace(srcPath))
	if err != nil {
		return err
	}
	dest := fmt.Sprintf("%s@%s:%s/", target.User, target.Host, strings.TrimSpace(remoteDir))
	args := []string{
		"-P", fmt.Sprintf("%d", target.Port),
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ConnectTimeout=5",
		absSrc,
		dest,
	}
	cmd := exec.CommandContext(ctx, scpBin, args...)
	var stderr bytes.Buffer
	cmd.Stdout = ioDiscard{}
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}
