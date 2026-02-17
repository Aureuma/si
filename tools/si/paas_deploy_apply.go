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

const defaultPaasHealthCheckCommand = "docker compose -f compose.yaml ps --status running --services | grep -q ."

type paasApplyOptions struct {
	Enabled              bool
	SelectedTargets      []string
	BundleDir            string
	ReleaseID            string
	RemoteRoot           string
	ApplyTimeout         time.Duration
	HealthTimeout        time.Duration
	HealthCommand        string
	RollbackOnFailure    bool
	RollbackBundleDir    string
	RollbackReleaseID    string
	RollbackApplyTimeout time.Duration
}

type paasApplyResult struct {
	AppliedTargets      []string
	HealthyTargets      []string
	RolledBackTargets   []string
	RollbackReleaseID   string
	HealthCommand       string
	HealthChecksEnabled bool
}

func applyPaasReleaseToTargets(opts paasApplyOptions) (paasApplyResult, error) {
	result := paasApplyResult{
		AppliedTargets:      []string{},
		HealthyTargets:      []string{},
		RolledBackTargets:   []string{},
		RollbackReleaseID:   strings.TrimSpace(opts.RollbackReleaseID),
		HealthCommand:       strings.TrimSpace(opts.HealthCommand),
		HealthChecksEnabled: strings.TrimSpace(opts.HealthCommand) != "",
	}
	if !opts.Enabled {
		return result, nil
	}
	targets, err := resolvePaasDeployTargets(opts.SelectedTargets)
	if err != nil {
		return result, newPaasOperationFailure(
			paasFailureTargetResolution,
			"target_resolve",
			"",
			"verify --target/--targets values or set a default via `si paas target use`",
			err,
		)
	}
	for _, target := range targets {
		ctx, cancel := context.WithTimeout(context.Background(), opts.ApplyTimeout)
		err := runPaasRemoteComposeApply(ctx, target, opts.BundleDir, opts.ReleaseID, opts.RemoteRoot)
		cancel()
		if err != nil {
			return result, err
		}
		result.AppliedTargets = append(result.AppliedTargets, target.Name)

		if strings.TrimSpace(opts.HealthCommand) == "" {
			continue
		}
		ctx, cancel = context.WithTimeout(context.Background(), opts.HealthTimeout)
		err = runPaasRemoteHealthCheck(ctx, target, opts.ReleaseID, opts.RemoteRoot, opts.HealthCommand)
		cancel()
		if err == nil {
			result.HealthyTargets = append(result.HealthyTargets, target.Name)
			continue
		}
		if !opts.RollbackOnFailure {
			return result, err
		}
		if strings.TrimSpace(opts.RollbackReleaseID) == "" || strings.TrimSpace(opts.RollbackBundleDir) == "" {
			return result, newPaasOperationFailure(
				paasFailureRollbackResolve,
				"rollback_resolve",
				target.Name,
				"provide a valid previous release to rollback or deploy a known-good baseline first",
				fmt.Errorf("health check failed and no rollback release is available: %w", err),
			)
		}
		ctx, cancel = context.WithTimeout(context.Background(), opts.RollbackApplyTimeout)
		rollbackErr := runPaasRemoteComposeApply(ctx, target, opts.RollbackBundleDir, opts.RollbackReleaseID, opts.RemoteRoot)
		cancel()
		if rollbackErr != nil {
			return result, newPaasOperationFailure(
				paasFailureRollbackApply,
				"rollback_apply",
				target.Name,
				"fix rollback transport/runtime failure on the target and rerun `si paas rollback`",
				fmt.Errorf("health check failed and rollback to %s failed: %w", opts.RollbackReleaseID, rollbackErr),
			)
		}
		result.RolledBackTargets = append(result.RolledBackTargets, target.Name)
		return result, newPaasOperationFailure(
			paasFailureHealthCheck,
			"health_check",
			target.Name,
			"inspect target logs and service health, then redeploy after fixing readiness failures",
			fmt.Errorf("health check failed and rollback to %s was applied: %w", opts.RollbackReleaseID, err),
		)
	}
	return result, nil
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
		return newPaasOperationFailure(
			paasFailureRemoteApply,
			"remote_apply",
			target.Name,
			"verify remote release path and retry",
			fmt.Errorf("invalid remote release directory"),
		)
	}
	if _, err := runPaasSSHCommand(ctx, target, fmt.Sprintf("mkdir -p %s", quoteSingle(releaseDir))); err != nil {
		return newPaasOperationFailure(
			paasFailureRemoteApply,
			"remote_prepare",
			target.Name,
			"verify SSH access and remote filesystem permissions",
			err,
		)
	}
	paths := []string{
		filepath.Join(strings.TrimSpace(localBundleDir), "compose.yaml"),
		filepath.Join(strings.TrimSpace(localBundleDir), "release.json"),
	}
	for _, src := range paths {
		if err := runPaasSCPUpload(ctx, target, src, releaseDir); err != nil {
			return newPaasOperationFailure(
				paasFailureRemoteUpload,
				"remote_upload",
				target.Name,
				"verify SCP connectivity, SSH credentials, and remote directory write access",
				err,
			)
		}
	}
	remoteCmd := fmt.Sprintf("cd %s && docker compose -f compose.yaml pull && docker compose -f compose.yaml up -d --remove-orphans", quoteSingle(releaseDir))
	if _, err := runPaasSSHCommand(ctx, target, remoteCmd); err != nil {
		return newPaasOperationFailure(
			paasFailureRemoteApply,
			"remote_apply",
			target.Name,
			"validate Docker/Compose runtime state on target and rerun deploy",
			err,
		)
	}
	return nil
}

func runPaasRemoteHealthCheck(ctx context.Context, target paasTarget, releaseID, remoteRoot, healthCommand string) error {
	command := strings.TrimSpace(healthCommand)
	if command == "" {
		command = defaultPaasHealthCheckCommand
	}
	releaseDir := path.Join(strings.TrimSpace(remoteRoot), sanitizePaasReleasePathSegment(releaseID))
	remoteCmd := fmt.Sprintf("cd %s && %s", quoteSingle(releaseDir), command)
	if _, err := runPaasSSHCommand(ctx, target, remoteCmd); err != nil {
		return newPaasOperationFailure(
			paasFailureHealthCheck,
			"health_check",
			target.Name,
			"verify service readiness and health command behavior before retrying deploy",
			err,
		)
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
