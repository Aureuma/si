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
	Strategy             string
	MaxParallel          int
	ContinueOnError      bool
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

type paasTargetApplyStatus struct {
	Target string
	Status string
}

type paasApplyResult struct {
	AppliedTargets      []string
	HealthyTargets      []string
	RolledBackTargets   []string
	FailedTargets       []string
	SkippedTargets      []string
	TargetStatuses      []paasTargetApplyStatus
	FanoutPlan          string
	RollbackReleaseID   string
	HealthCommand       string
	HealthChecksEnabled bool
}

type paasApplyBatch struct {
	Phase       string
	Targets     []paasTarget
	Parallelism int
}

type paasSingleTargetApplyResult struct {
	Target     string
	Applied    bool
	Healthy    bool
	RolledBack bool
	Err        error
}

func applyPaasReleaseToTargets(opts paasApplyOptions) (paasApplyResult, error) {
	resolvedStrategy := strings.ToLower(strings.TrimSpace(opts.Strategy))
	if !isValidDeployStrategy(resolvedStrategy) {
		resolvedStrategy = "serial"
	}
	resolvedMaxParallel := opts.MaxParallel
	if resolvedMaxParallel < 1 {
		resolvedMaxParallel = 1
	}
	result := paasApplyResult{
		AppliedTargets:      []string{},
		HealthyTargets:      []string{},
		RolledBackTargets:   []string{},
		FailedTargets:       []string{},
		SkippedTargets:      []string{},
		TargetStatuses:      []paasTargetApplyStatus{},
		RollbackReleaseID:   strings.TrimSpace(opts.RollbackReleaseID),
		HealthCommand:       strings.TrimSpace(opts.HealthCommand),
		HealthChecksEnabled: strings.TrimSpace(opts.HealthCommand) != "",
		FanoutPlan:          "",
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
	statusByTarget := map[string]string{}
	for _, target := range targets {
		statusByTarget[target.Name] = "pending"
	}
	batches := planPaasApplyBatches(targets, resolvedStrategy, resolvedMaxParallel)
	result.FanoutPlan = formatPaasApplyFanoutPlan(batches)

	failures := []error{}
	stopFurtherBatches := false
	for batchIndex, batch := range batches {
		if stopFurtherBatches {
			break
		}
		batchResults := executePaasApplyBatch(opts, batch)
		batchFailed := false
		for _, row := range batchResults {
			if row.Applied {
				result.AppliedTargets = appendUniqueString(result.AppliedTargets, row.Target)
			}
			if row.Healthy {
				result.HealthyTargets = appendUniqueString(result.HealthyTargets, row.Target)
			}
			if row.RolledBack {
				result.RolledBackTargets = appendUniqueString(result.RolledBackTargets, row.Target)
			}
			if row.Err != nil {
				result.FailedTargets = appendUniqueString(result.FailedTargets, row.Target)
				failures = append(failures, row.Err)
				batchFailed = true
				if row.RolledBack {
					statusByTarget[row.Target] = "rolled_back"
				} else {
					statusByTarget[row.Target] = "failed"
				}
				continue
			}
			statusByTarget[row.Target] = "ok"
		}

		if batchFailed {
			if resolvedStrategy == "canary" && batchIndex == 0 {
				markPaasSkippedTargets(statusByTarget, batches[batchIndex+1:], &result.SkippedTargets)
				stopFurtherBatches = true
				continue
			}
			if !opts.ContinueOnError {
				markPaasSkippedTargets(statusByTarget, batches[batchIndex+1:], &result.SkippedTargets)
				stopFurtherBatches = true
			}
		}
	}

	result.TargetStatuses = make([]paasTargetApplyStatus, 0, len(targets))
	for _, target := range targets {
		status := strings.TrimSpace(statusByTarget[target.Name])
		if status == "" || status == "pending" {
			status = "skipped"
			result.SkippedTargets = appendUniqueString(result.SkippedTargets, target.Name)
		}
		result.TargetStatuses = append(result.TargetStatuses, paasTargetApplyStatus{
			Target: target.Name,
			Status: status,
		})
	}
	if len(failures) > 0 {
		return result, aggregatePaasApplyFailures(failures)
	}
	return result, nil
}

func planPaasApplyBatches(targets []paasTarget, strategy string, maxParallel int) []paasApplyBatch {
	if len(targets) == 0 {
		return nil
	}
	if maxParallel < 1 {
		maxParallel = 1
	}
	batches := make([]paasApplyBatch, 0, len(targets))
	switch strategy {
	case "parallel":
		batches = append(batches, paasApplyBatch{
			Phase:       "parallel",
			Targets:     append([]paasTarget(nil), targets...),
			Parallelism: minInt(maxParallel, len(targets)),
		})
	case "rolling":
		for start := 0; start < len(targets); start += maxParallel {
			end := minInt(start+maxParallel, len(targets))
			batches = append(batches, paasApplyBatch{
				Phase:       "rolling",
				Targets:     append([]paasTarget(nil), targets[start:end]...),
				Parallelism: minInt(maxParallel, end-start),
			})
		}
	case "canary":
		batches = append(batches, paasApplyBatch{
			Phase:       "canary",
			Targets:     append([]paasTarget(nil), targets[0]),
			Parallelism: 1,
		})
		if len(targets) > 1 {
			rest := targets[1:]
			for start := 0; start < len(rest); start += maxParallel {
				end := minInt(start+maxParallel, len(rest))
				batches = append(batches, paasApplyBatch{
					Phase:       "rolling",
					Targets:     append([]paasTarget(nil), rest[start:end]...),
					Parallelism: minInt(maxParallel, end-start),
				})
			}
		}
	default:
		for _, target := range targets {
			batches = append(batches, paasApplyBatch{
				Phase:       "serial",
				Targets:     append([]paasTarget(nil), target),
				Parallelism: 1,
			})
		}
	}
	return batches
}

func executePaasApplyBatch(opts paasApplyOptions, batch paasApplyBatch) []paasSingleTargetApplyResult {
	if len(batch.Targets) == 0 {
		return nil
	}
	parallelism := batch.Parallelism
	if parallelism < 1 {
		parallelism = 1
	}
	type batchItem struct {
		Index  int
		Result paasSingleTargetApplyResult
	}
	out := make([]paasSingleTargetApplyResult, len(batch.Targets))
	sem := make(chan struct{}, parallelism)
	results := make(chan batchItem, len(batch.Targets))
	for i, target := range batch.Targets {
		sem <- struct{}{}
		go func(index int, row paasTarget) {
			defer func() {
				<-sem
			}()
			results <- batchItem{
				Index:  index,
				Result: runPaasApplyOnTarget(opts, row),
			}
		}(i, target)
	}
	for i := 0; i < len(batch.Targets); i++ {
		item := <-results
		out[item.Index] = item.Result
	}
	return out
}

func runPaasApplyOnTarget(opts paasApplyOptions, target paasTarget) paasSingleTargetApplyResult {
	result := paasSingleTargetApplyResult{
		Target: target.Name,
	}
	ctx, cancel := context.WithTimeout(context.Background(), opts.ApplyTimeout)
	err := runPaasRemoteComposeApply(ctx, target, opts.BundleDir, opts.ReleaseID, opts.RemoteRoot)
	cancel()
	if err != nil {
		result.Err = err
		return result
	}
	result.Applied = true

	if strings.TrimSpace(opts.HealthCommand) == "" {
		return result
	}
	ctx, cancel = context.WithTimeout(context.Background(), opts.HealthTimeout)
	err = runPaasRemoteHealthCheck(ctx, target, opts.BundleDir, opts.ReleaseID, opts.RemoteRoot, opts.HealthCommand)
	cancel()
	if err == nil {
		result.Healthy = true
		return result
	}
	if !opts.RollbackOnFailure {
		result.Err = err
		return result
	}
	if strings.TrimSpace(opts.RollbackReleaseID) == "" || strings.TrimSpace(opts.RollbackBundleDir) == "" {
		result.Err = newPaasOperationFailure(
			paasFailureRollbackResolve,
			"rollback_resolve",
			target.Name,
			"provide a valid previous release to rollback or deploy a known-good baseline first",
			fmt.Errorf("health check failed and no rollback release is available: %w", err),
		)
		return result
	}
	ctx, cancel = context.WithTimeout(context.Background(), opts.RollbackApplyTimeout)
	rollbackErr := runPaasRemoteComposeApply(ctx, target, opts.RollbackBundleDir, opts.RollbackReleaseID, opts.RemoteRoot)
	cancel()
	if rollbackErr != nil {
		result.Err = newPaasOperationFailure(
			paasFailureRollbackApply,
			"rollback_apply",
			target.Name,
			"fix rollback transport/runtime failure on the target and rerun `si paas rollback`",
			fmt.Errorf("health check failed and rollback to %s failed: %w", opts.RollbackReleaseID, rollbackErr),
		)
		return result
	}
	result.RolledBack = true
	result.Err = newPaasOperationFailure(
		paasFailureHealthCheck,
		"health_check",
		target.Name,
		"inspect target logs and service health, then redeploy after fixing readiness failures",
		fmt.Errorf("health check failed and rollback to %s was applied: %w", opts.RollbackReleaseID, err),
	)
	return result
}

func aggregatePaasApplyFailures(failures []error) error {
	if len(failures) == 0 {
		return nil
	}
	first := asPaasOperationFailure(failures[0])
	if len(failures) == 1 {
		return newPaasOperationFailure(first.Code, first.Stage, first.Target, first.Remediation, first.Err)
	}
	return newPaasOperationFailure(
		first.Code,
		first.Stage,
		first.Target,
		first.Remediation,
		fmt.Errorf("%s (and %d additional target failure(s))", errString(first.Err), len(failures)-1),
	)
}

func markPaasSkippedTargets(statusByTarget map[string]string, remaining []paasApplyBatch, skippedTargets *[]string) {
	for _, batch := range remaining {
		for _, target := range batch.Targets {
			statusByTarget[target.Name] = "skipped"
			*skippedTargets = appendUniqueString(*skippedTargets, target.Name)
		}
	}
}

func appendUniqueString(values []string, value string) []string {
	item := strings.TrimSpace(value)
	if item == "" {
		return values
	}
	for _, existing := range values {
		if strings.EqualFold(strings.TrimSpace(existing), item) {
			return values
		}
	}
	return append(values, item)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func formatPaasApplyFanoutPlan(batches []paasApplyBatch) string {
	if len(batches) == 0 {
		return ""
	}
	parts := make([]string, 0, len(batches))
	for _, batch := range batches {
		names := make([]string, 0, len(batch.Targets))
		for _, target := range batch.Targets {
			names = append(names, target.Name)
		}
		parts = append(parts, fmt.Sprintf("%s(%s)", strings.TrimSpace(batch.Phase), strings.Join(names, "+")))
	}
	return strings.Join(parts, ";")
}

func formatPaasTargetStatuses(statuses []paasTargetApplyStatus) string {
	if len(statuses) == 0 {
		return ""
	}
	parts := make([]string, 0, len(statuses))
	for _, status := range statuses {
		target := strings.TrimSpace(status.Target)
		state := strings.TrimSpace(status.Status)
		if target == "" || state == "" {
			continue
		}
		parts = append(parts, target+":"+state)
	}
	return strings.Join(parts, ",")
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
	bundleFiles, err := resolvePaasBundleUploadFiles(localBundleDir)
	if err != nil {
		return newPaasOperationFailure(
			paasFailureBundleCreate,
			"bundle_discover",
			target.Name,
			"verify local release bundle contents and retry",
			err,
		)
	}
	composeFiles, err := readPaasComposeFilesManifest(localBundleDir)
	if err != nil {
		return newPaasOperationFailure(
			paasFailureBundleCreate,
			"bundle_manifest",
			target.Name,
			"fix compose bundle manifest and rerun deploy",
			err,
		)
	}
	composeArgs := buildPaasComposeFileArgs(composeFiles)
	if _, err := runPaasSSHCommand(ctx, target, fmt.Sprintf("mkdir -p %s", quoteSingle(releaseDir))); err != nil {
		return newPaasOperationFailure(
			paasFailureRemoteApply,
			"remote_prepare",
			target.Name,
			"verify SSH access and remote filesystem permissions",
			err,
		)
	}
	for _, fileName := range bundleFiles {
		src := filepath.Join(strings.TrimSpace(localBundleDir), fileName)
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
	remoteCmd := fmt.Sprintf("cd %s && docker compose %s pull && docker compose %s up -d --remove-orphans", quoteSingle(releaseDir), composeArgs, composeArgs)
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

func runPaasRemoteHealthCheck(ctx context.Context, target paasTarget, localBundleDir, releaseID, remoteRoot, healthCommand string) error {
	command := strings.TrimSpace(healthCommand)
	if command == "" || command == defaultPaasHealthCheckCommand {
		composeFiles := []string{"compose.yaml"}
		if strings.TrimSpace(localBundleDir) != "" {
			resolved, err := readPaasComposeFilesManifest(localBundleDir)
			if err != nil {
				return newPaasOperationFailure(
					paasFailureBundleCreate,
					"bundle_manifest",
					target.Name,
					"fix compose bundle manifest and rerun deploy",
					err,
				)
			}
			composeFiles = resolved
		}
		command = buildPaasDefaultHealthCheckCommand(composeFiles)
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
