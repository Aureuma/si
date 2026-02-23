package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type paasBlueGreenPolicyStore struct {
	Apps map[string]paasBlueGreenAppPolicy `json:"apps,omitempty"`
}

type paasBlueGreenAppPolicy struct {
	Targets map[string]paasBlueGreenTargetPolicy `json:"targets,omitempty"`
}

type paasBlueGreenTargetPolicy struct {
	ActiveSlot string            `json:"active_slot,omitempty"`
	Slots      map[string]string `json:"slots,omitempty"`
	UpdatedAt  string            `json:"updated_at,omitempty"`
}

type paasBlueGreenTargetResult struct {
	Target     string
	FromSlot   string
	ToSlot     string
	Applied    bool
	Cutover    bool
	RolledBack bool
	Err        error
}

func cmdPaasDeployBlueGreen(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas deploy bluegreen", flag.ExitOnError)
	app := fs.String("app", "", "app slug")
	target := fs.String("target", "", "single target id")
	targets := fs.String("targets", "", "target ids csv or all")
	release := fs.String("release", "", "release identifier")
	composeFile := fs.String("compose-file", "compose.yaml", "compose file path")
	bundleRoot := fs.String("bundle-root", "", "release bundle root path (defaults to context-scoped state root)")
	applyRemote := fs.Bool("apply", false, "upload bundle and apply compose-only blue/green rollout on remote targets")
	remoteDir := fs.String("remote-dir", defaultPaasReleaseRemoteDir, "remote release root directory")
	applyTimeout := fs.String("apply-timeout", "2m", "per-target remote apply timeout")
	healthCmd := fs.String("health-cmd", defaultPaasHealthCheckCommand, "health command run before and after cutover")
	healthTimeout := fs.String("health-timeout", "45s", "per-target health check timeout")
	cutoverTimeout := fs.String("cutover-timeout", "45s", "per-target cutover command timeout")
	continueOnError := fs.Bool("continue-on-error", false, "continue blue/green rollout on target errors")
	keepStandby := fs.Bool("keep-standby", true, "keep previous active slot running after successful cutover")
	cutoverCmd := fs.String("cutover-cmd", "", "optional remote cutover command template (supports {{app}}, {{target}}, {{release}}, {{from_slot}}, {{to_slot}}, {{state_dir}}, {{state_file}}, {{project}})")
	vaultFile := fs.String("vault-file", "", "explicit vault env file path")
	allowPlaintextSecrets := fs.Bool("allow-plaintext-secrets", false, "allow plaintext secret assignments in compose file (unsafe)")
	allowUntrustedVault := fs.Bool("allow-untrusted-vault", false, "allow deploy with untrusted vault fingerprint (unsafe)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si paas deploy bluegreen [--app <slug>] [--target <id>] [--targets <id1,id2|all>] [--release <id>] [--compose-file <path>] [--bundle-root <path>] [--apply] [--remote-dir <path>] [--apply-timeout <duration>] [--health-cmd <command>] [--health-timeout <duration>] [--cutover-timeout <duration>] [--continue-on-error] [--keep-standby[=true|false]] [--cutover-cmd <command>] [--vault-file <path>] [--allow-plaintext-secrets] [--allow-untrusted-vault] [--json]")
		return
	}

	applyTimeoutValue, err := time.ParseDuration(strings.TrimSpace(*applyTimeout))
	if err != nil || applyTimeoutValue <= 0 {
		failPaasDeployBlueGreen(jsonOut, newPaasOperationFailure(
			paasFailureInvalidArgument,
			"flag_validation",
			"",
			"pass a positive duration for --apply-timeout (for example 2m)",
			fmt.Errorf("invalid --apply-timeout %q", strings.TrimSpace(*applyTimeout)),
		), nil)
	}
	healthTimeoutValue, err := time.ParseDuration(strings.TrimSpace(*healthTimeout))
	if err != nil || healthTimeoutValue <= 0 {
		failPaasDeployBlueGreen(jsonOut, newPaasOperationFailure(
			paasFailureInvalidArgument,
			"flag_validation",
			"",
			"pass a positive duration for --health-timeout (for example 45s)",
			fmt.Errorf("invalid --health-timeout %q", strings.TrimSpace(*healthTimeout)),
		), nil)
	}
	cutoverTimeoutValue, err := time.ParseDuration(strings.TrimSpace(*cutoverTimeout))
	if err != nil || cutoverTimeoutValue <= 0 {
		failPaasDeployBlueGreen(jsonOut, newPaasOperationFailure(
			paasFailureInvalidArgument,
			"flag_validation",
			"",
			"pass a positive duration for --cutover-timeout (for example 45s)",
			fmt.Errorf("invalid --cutover-timeout %q", strings.TrimSpace(*cutoverTimeout)),
		), nil)
	}
	resolvedRemoteDir, err := resolvePaasRemoteDir(strings.TrimSpace(*remoteDir))
	if err != nil {
		failPaasDeployBlueGreen(jsonOut, newPaasOperationFailure(
			paasFailureInvalidArgument,
			"flag_validation",
			"",
			"pass an absolute --remote-dir path (for example /opt/si/paas/releases)",
			err,
		), nil)
	}

	composeGuardrail, err := enforcePaasPlaintextSecretGuardrail(strings.TrimSpace(*composeFile), *allowPlaintextSecrets)
	if err != nil {
		failPaasDeployBlueGreen(jsonOut, newPaasOperationFailure(
			paasFailurePlaintextSecrets,
			"precheck",
			"",
			"replace inline secret literals with variables and `si paas secret set`",
			err,
		), nil)
	}
	vaultGuardrail, err := runPaasVaultDeployGuardrail(strings.TrimSpace(*vaultFile), *allowUntrustedVault)
	if err != nil {
		failPaasDeployBlueGreen(jsonOut, newPaasOperationFailure(
			paasFailureVaultTrust,
			"precheck",
			"",
			"establish trust using `si vault trust accept` or pass --allow-untrusted-vault (unsafe)",
			err,
		), nil)
	}

	resolvedApp := strings.TrimSpace(*app)
	if resolvedApp == "" {
		resolvedApp = "default-app"
	}
	resolvedRelease := strings.TrimSpace(*release)
	if resolvedRelease == "" {
		resolvedRelease = generatePaasReleaseID()
	}
	selectedTargets := normalizeTargets(*target, *targets)
	preparedCompose, err := preparePaasComposeForDeploy(paasComposePrepareOptions{
		App:         resolvedApp,
		ReleaseID:   resolvedRelease,
		ComposeFile: strings.TrimSpace(*composeFile),
		Strategy:    "bluegreen",
		Targets:     selectedTargets,
	})
	if err != nil {
		failPaasDeployBlueGreen(jsonOut, newPaasOperationFailure(
			paasFailureInvalidArgument,
			"compose_resolve",
			"",
			"fix compose magic-variable placeholders/add-on merge conflicts and retry deploy",
			err,
		), nil)
	}
	defer func() {
		_ = os.Remove(strings.TrimSpace(preparedCompose.ResolvedComposePath))
	}()
	bundleDir, bundleMetaPath, err := ensurePaasReleaseBundle(
		resolvedApp,
		resolvedRelease,
		strings.TrimSpace(preparedCompose.ResolvedComposePath),
		strings.TrimSpace(*bundleRoot),
		"bluegreen",
		selectedTargets,
		map[string]string{
			"compose_secret_guardrail":  composeGuardrail["compose_secret_guardrail"],
			"compose_secret_findings":   composeGuardrail["compose_secret_findings"],
			"magic_variable_resolution": "validated",
			"addon_merge_validation":    "validated",
			"addon_fragments":           intString(len(preparedCompose.AddonArtifacts)),
			"vault_file":                vaultGuardrail.File,
			"vault_recipients":          intString(vaultGuardrail.RecipientCount),
			"vault_trust":               boolString(vaultGuardrail.Trusted),
		},
	)
	if err != nil {
		failPaasDeployBlueGreen(jsonOut, newPaasOperationFailure(
			paasFailureBundleCreate,
			"bundle_create",
			"",
			"verify compose file path and state root permissions",
			err,
		), nil)
	}
	if err := materializePaasComposeBundleArtifacts(bundleDir, preparedCompose); err != nil {
		failPaasDeployBlueGreen(jsonOut, newPaasOperationFailure(
			paasFailureBundleCreate,
			"bundle_materialize",
			"",
			"verify bundle write permissions and rerun deploy",
			err,
		), nil)
	}
	releaseID := filepath.Base(bundleDir)

	activeSlots := map[string]string{}
	previousSlots := map[string]string{}
	appliedTargets := []string{}
	rolledBackTargets := []string{}
	failedTargets := []string{}
	skippedTargets := []string{}
	statusByTarget := map[string]string{}
	targetOrder := []string{}
	firstFailure := error(nil)

	if *applyRemote {
		resolvedTargetRows, err := resolvePaasDeployTargets(selectedTargets)
		if err != nil {
			failPaasDeployBlueGreen(jsonOut, newPaasOperationFailure(
				paasFailureTargetResolution,
				"target_resolve",
				"",
				"verify --target/--targets values or set a default via `si paas target use`",
				err,
			), nil)
		}
		store, err := loadPaasBlueGreenPolicyStore()
		if err != nil {
			failPaasDeployBlueGreen(jsonOut, newPaasOperationFailure(
				paasFailureUnknown,
				"policy_load",
				"",
				"verify local state permissions and rerun deploy",
				err,
			), nil)
		}
		appKey := sanitizePaasReleasePathSegment(resolvedApp)
		appPolicy := store.Apps[appKey]
		if appPolicy.Targets == nil {
			appPolicy.Targets = map[string]paasBlueGreenTargetPolicy{}
		}
		targetRowsByName := map[string]paasTarget{}
		for _, row := range resolvedTargetRows {
			targetRowsByName[row.Name] = row
			targetOrder = append(targetOrder, row.Name)
			statusByTarget[row.Name] = "pending"
		}
		for i, row := range resolvedTargetRows {
			targetKey := sanitizePaasReleasePathSegment(row.Name)
			policy := normalizePaasBlueGreenTargetPolicy(appPolicy.Targets[targetKey])
			fromSlot := policy.ActiveSlot
			toSlot := flipPaasBlueGreenSlot(fromSlot)
			previousSlots[row.Name] = fromSlot
			activeSlots[row.Name] = fromSlot

			result := runPaasBlueGreenDeployOnTarget(paasBlueGreenDeployTargetOptions{
				Target:          row,
				App:             resolvedApp,
				ReleaseID:       releaseID,
				BundleDir:       bundleDir,
				RemoteRoot:      resolvedRemoteDir,
				FromSlot:        fromSlot,
				ToSlot:          toSlot,
				PreviousRelease: strings.TrimSpace(policy.Slots[fromSlot]),
				ApplyTimeout:    applyTimeoutValue,
				HealthTimeout:   healthTimeoutValue,
				CutoverTimeout:  cutoverTimeoutValue,
				HealthCommand:   strings.TrimSpace(*healthCmd),
				CutoverCommand:  strings.TrimSpace(*cutoverCmd),
				KeepStandby:     *keepStandby,
			})
			if result.Err != nil {
				if firstFailure == nil {
					firstFailure = result.Err
				}
				failedTargets = appendUniqueString(failedTargets, row.Name)
				if result.RolledBack {
					rolledBackTargets = appendUniqueString(rolledBackTargets, row.Name)
					statusByTarget[row.Name] = "rolled_back"
				} else {
					statusByTarget[row.Name] = "failed"
				}
				if !*continueOnError {
					for _, tail := range resolvedTargetRows[i+1:] {
						statusByTarget[tail.Name] = "skipped"
						skippedTargets = appendUniqueString(skippedTargets, tail.Name)
					}
					break
				}
				continue
			}
			appliedTargets = appendUniqueString(appliedTargets, row.Name)
			statusByTarget[row.Name] = "ok"
			policy.ActiveSlot = toSlot
			if policy.Slots == nil {
				policy.Slots = map[string]string{}
			}
			policy.Slots[toSlot] = releaseID
			policy.UpdatedAt = utcNowRFC3339()
			appPolicy.Targets[targetKey] = policy
			activeSlots[row.Name] = toSlot
		}
		store.Apps[appKey] = appPolicy
		if err := savePaasBlueGreenPolicyStore(store); err != nil {
			failPaasDeployBlueGreen(jsonOut, newPaasOperationFailure(
				paasFailureUnknown,
				"policy_save",
				"",
				"verify local state permissions and rerun deploy",
				err,
			), nil)
		}
		if len(appliedTargets) > 0 {
			if err := recordPaasSuccessfulReleaseWithRemoteDir(resolvedApp, releaseID, resolvedRemoteDir); err != nil {
				failPaasDeployBlueGreen(jsonOut, newPaasOperationFailure(
					paasFailureUnknown,
					"state_record",
					"",
					"verify local state permissions and rerun deploy",
					err,
				), nil)
			}
		}
		for _, name := range targetOrder {
			if strings.TrimSpace(statusByTarget[name]) == "pending" {
				statusByTarget[name] = "skipped"
				skippedTargets = appendUniqueString(skippedTargets, name)
			}
		}
		if firstFailure != nil {
			failPaasDeployBlueGreen(jsonOut, firstFailure, map[string]string{
				"app":               resolvedApp,
				"release":           releaseID,
				"failed_targets":    formatTargets(failedTargets),
				"rolled_back":       formatTargets(rolledBackTargets),
				"skipped_targets":   formatTargets(skippedTargets),
				"target_statuses":   formatPaasTargetStatuses(buildPaasBlueGreenStatuses(targetRowsByName, statusByTarget)),
				"active_slots":      formatPaasBlueGreenSlots(activeSlots),
				"previous_slots":    formatPaasBlueGreenSlots(previousSlots),
				"continue_on_error": boolString(*continueOnError),
			})
		}
	}

	fields := map[string]string{
		"app":                      resolvedApp,
		"apply":                    boolString(*applyRemote),
		"apply_timeout":            applyTimeoutValue.String(),
		"bundle_dir":               bundleDir,
		"bundle_metadata":          bundleMetaPath,
		"compose_files":            strings.Join(preparedCompose.ComposeFiles, ","),
		"compose_file":             strings.TrimSpace(*composeFile),
		"compose_secret_guardrail": composeGuardrail["compose_secret_guardrail"],
		"compose_secret_findings":  composeGuardrail["compose_secret_findings"],
		"magic_variable_count":     intString(len(preparedCompose.MagicVariables)),
		"addon_fragments":          intString(len(preparedCompose.AddonArtifacts)),
		"cutover_timeout":          cutoverTimeoutValue.String(),
		"health_cmd":               strings.TrimSpace(*healthCmd),
		"health_timeout":           healthTimeoutValue.String(),
		"release":                  releaseID,
		"remote_dir":               resolvedRemoteDir,
		"targets":                  formatTargets(selectedTargets),
		"applied_targets":          formatTargets(appliedTargets),
		"failed_targets":           formatTargets(failedTargets),
		"rolled_back_targets":      formatTargets(rolledBackTargets),
		"skipped_targets":          formatTargets(skippedTargets),
		"active_slots":             formatPaasBlueGreenSlots(activeSlots),
		"previous_slots":           formatPaasBlueGreenSlots(previousSlots),
		"keep_standby":             boolString(*keepStandby),
		"rollback_policy":          "candidate-slot health gate; if post-cutover health fails, restore previous slot per target",
		"vault_file":               vaultGuardrail.File,
		"vault_recipients":         intString(vaultGuardrail.RecipientCount),
		"vault_trust":              boolString(vaultGuardrail.Trusted),
	}
	if !vaultGuardrail.Trusted {
		fields["vault_trust_warning"] = vaultGuardrail.TrustWarning
	}
	if *applyRemote {
		fields["target_statuses"] = formatPaasTargetStatuses(buildPaasBlueGreenStatuses(nil, statusByTarget))
	}
	if eventPath := recordPaasDeployEvent("deploy bluegreen", "succeeded", fields, nil); strings.TrimSpace(eventPath) != "" {
		fields["event_log"] = eventPath
	}
	printPaasScaffold("deploy bluegreen", fields, jsonOut)
}

type paasBlueGreenDeployTargetOptions struct {
	Target          paasTarget
	App             string
	ReleaseID       string
	BundleDir       string
	RemoteRoot      string
	FromSlot        string
	ToSlot          string
	PreviousRelease string
	ApplyTimeout    time.Duration
	HealthTimeout   time.Duration
	CutoverTimeout  time.Duration
	HealthCommand   string
	CutoverCommand  string
	KeepStandby     bool
}

func runPaasBlueGreenDeployOnTarget(opts paasBlueGreenDeployTargetOptions) paasBlueGreenTargetResult {
	result := paasBlueGreenTargetResult{
		Target:   opts.Target.Name,
		FromSlot: normalizePaasBlueGreenSlot(opts.FromSlot),
		ToSlot:   normalizePaasBlueGreenSlot(opts.ToSlot),
	}
	releaseDir, stateDir, stateFile, project := resolvePaasBlueGreenRemotePaths(opts.RemoteRoot, opts.App, opts.Target.Name, result.ToSlot, opts.ReleaseID)
	composeFiles, err := readPaasComposeFilesManifest(opts.BundleDir)
	if err != nil {
		result.Err = newPaasOperationFailure(
			paasFailureBundleCreate,
			"bundle_manifest",
			opts.Target.Name,
			"fix compose bundle manifest and rerun deploy",
			err,
		)
		return result
	}
	ctx, cancel := context.WithTimeout(context.Background(), opts.ApplyTimeout)
	err = runPaasRemoteBlueGreenComposeApply(ctx, opts.Target, opts.BundleDir, releaseDir, project, composeFiles)
	cancel()
	if err != nil {
		result.Err = err
		return result
	}
	result.Applied = true

	ctx, cancel = context.WithTimeout(context.Background(), opts.HealthTimeout)
	err = runPaasRemoteBlueGreenHealthCheck(ctx, opts.Target, releaseDir, opts.HealthCommand, project, composeFiles)
	cancel()
	if err != nil {
		result.Err = err
		return result
	}

	cutoverCommand := buildPaasBlueGreenCutoverCommand(opts.CutoverCommand, opts.App, opts.Target.Name, opts.ReleaseID, result.FromSlot, result.ToSlot, stateDir, stateFile, project)
	ctx, cancel = context.WithTimeout(context.Background(), opts.CutoverTimeout)
	_, err = runPaasSSHCommand(ctx, opts.Target, cutoverCommand)
	cancel()
	if err != nil {
		result.Err = newPaasOperationFailure(
			paasFailureRemoteApply,
			"bluegreen_cutover",
			opts.Target.Name,
			"verify cutover command behavior and remote permissions before retrying blue/green deploy",
			err,
		)
		return result
	}
	result.Cutover = true

	ctx, cancel = context.WithTimeout(context.Background(), opts.HealthTimeout)
	err = runPaasRemoteBlueGreenHealthCheck(ctx, opts.Target, releaseDir, opts.HealthCommand, project, composeFiles)
	cancel()
	if err != nil {
		rollbackProject := buildPaasBlueGreenProjectName(opts.App, opts.Target.Name, result.FromSlot)
		rollbackCommand := buildPaasBlueGreenCutoverCommand(opts.CutoverCommand, opts.App, opts.Target.Name, opts.ReleaseID, result.ToSlot, result.FromSlot, stateDir, stateFile, rollbackProject)
		ctx, rollbackCancel := context.WithTimeout(context.Background(), opts.CutoverTimeout)
		_, rollbackErr := runPaasSSHCommand(ctx, opts.Target, rollbackCommand)
		rollbackCancel()
		if rollbackErr != nil {
			result.Err = newPaasOperationFailure(
				paasFailureRollbackApply,
				"bluegreen_rollback",
				opts.Target.Name,
				"restore previous slot manually and verify cutover command template",
				fmt.Errorf("post-cutover health failed and rollback command failed: %w", rollbackErr),
			)
			return result
		}
		result.RolledBack = true
		result.Err = newPaasOperationFailure(
			paasFailureHealthCheck,
			"bluegreen_post_cutover_health",
			opts.Target.Name,
			"inspect target logs and health checks before re-attempting blue/green cutover",
			err,
		)
		return result
	}

	if !opts.KeepStandby {
		previousRelease := strings.TrimSpace(opts.PreviousRelease)
		if previousRelease != "" {
			oldReleaseDir, _, _, oldProject := resolvePaasBlueGreenRemotePaths(opts.RemoteRoot, opts.App, opts.Target.Name, result.FromSlot, previousRelease)
			ctx, cleanupCancel := context.WithTimeout(context.Background(), opts.ApplyTimeout)
			_ = runPaasRemoteBlueGreenStandbyDown(ctx, opts.Target, oldReleaseDir, oldProject)
			cleanupCancel()
		}
	}
	return result
}

func runPaasRemoteBlueGreenComposeApply(ctx context.Context, target paasTarget, localBundleDir, remoteReleaseDir, projectName string, composeFiles []string) error {
	if strings.TrimSpace(remoteReleaseDir) == "" {
		return newPaasOperationFailure(
			paasFailureRemoteApply,
			"bluegreen_apply",
			target.Name,
			"verify remote release root and retry",
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
	composeArgs := buildPaasComposeFileArgs(composeFiles)
	if _, err := runPaasSSHCommand(ctx, target, fmt.Sprintf("mkdir -p %s", quoteSingle(remoteReleaseDir))); err != nil {
		return newPaasOperationFailure(
			paasFailureRemoteApply,
			"bluegreen_prepare",
			target.Name,
			"verify SSH access and remote filesystem permissions",
			err,
		)
	}
	for _, fileName := range bundleFiles {
		src := filepath.Join(strings.TrimSpace(localBundleDir), fileName)
		if err := runPaasSCPUpload(ctx, target, src, remoteReleaseDir); err != nil {
			return newPaasOperationFailure(
				paasFailureRemoteUpload,
				"bluegreen_upload",
				target.Name,
				"verify SCP connectivity, SSH credentials, and remote directory write access",
				err,
			)
		}
	}
	remoteCmd := fmt.Sprintf("cd %s && docker compose -p %s %s pull && docker compose -p %s %s up -d --remove-orphans --build", quoteSingle(remoteReleaseDir), quoteSingle(projectName), composeArgs, quoteSingle(projectName), composeArgs)
	if _, err := runPaasSSHCommand(ctx, target, remoteCmd); err != nil {
		return newPaasOperationFailure(
			paasFailureRemoteApply,
			"bluegreen_apply",
			target.Name,
			"validate Docker/Compose runtime state on target and rerun deploy",
			err,
		)
	}
	return nil
}

func runPaasRemoteBlueGreenHealthCheck(ctx context.Context, target paasTarget, releaseDir, healthCommand, project string, composeFiles []string) error {
	command := resolvePaasBlueGreenHealthCommand(healthCommand, project)
	if strings.TrimSpace(healthCommand) == "" || strings.TrimSpace(healthCommand) == defaultPaasHealthCheckCommand {
		command = fmt.Sprintf("docker compose -p %s %s ps --status running --services | grep -q .", quoteSingle(project), buildPaasComposeFileArgs(composeFiles))
	}
	remoteCmd := fmt.Sprintf("cd %s && %s", quoteSingle(releaseDir), command)
	if _, err := runPaasSSHCommand(ctx, target, remoteCmd); err != nil {
		return newPaasOperationFailure(
			paasFailureHealthCheck,
			"bluegreen_health_check",
			target.Name,
			"verify service readiness and health command behavior before retrying deploy",
			err,
		)
	}
	return nil
}

func runPaasRemoteBlueGreenStandbyDown(ctx context.Context, target paasTarget, releaseDir, project string) error {
	cmd := fmt.Sprintf("if [ -f %s ]; then cd %s && docker compose -p %s -f compose.yaml down --remove-orphans; fi", quoteSingle(path.Join(releaseDir, "compose.yaml")), quoteSingle(releaseDir), quoteSingle(project))
	_, err := runPaasSSHCommand(ctx, target, cmd)
	return err
}

func resolvePaasBlueGreenHealthCommand(raw, project string) string {
	command := strings.TrimSpace(raw)
	if command == "" || command == defaultPaasHealthCheckCommand {
		return fmt.Sprintf("docker compose -p %s -f compose.yaml ps --status running --services | grep -q .", quoteSingle(project))
	}
	replacer := strings.NewReplacer(
		"{{project}}", quoteSingle(project),
		"{{project_raw}}", project,
	)
	return strings.TrimSpace(replacer.Replace(command))
}

func buildPaasBlueGreenCutoverCommand(template, app, target, release, fromSlot, toSlot, stateDir, stateFile, project string) string {
	if strings.TrimSpace(template) == "" {
		return fmt.Sprintf("mkdir -p %s && printf '%%s\\n' %s > %s", quoteSingle(stateDir), quoteSingle(toSlot), quoteSingle(stateFile))
	}
	replacer := strings.NewReplacer(
		"{{app}}", quoteSingle(app),
		"{{target}}", quoteSingle(target),
		"{{release}}", quoteSingle(release),
		"{{from_slot}}", quoteSingle(fromSlot),
		"{{to_slot}}", quoteSingle(toSlot),
		"{{state_dir}}", quoteSingle(stateDir),
		"{{state_file}}", quoteSingle(stateFile),
		"{{project}}", quoteSingle(project),
		"{{app_raw}}", app,
		"{{target_raw}}", target,
		"{{release_raw}}", release,
		"{{from_slot_raw}}", fromSlot,
		"{{to_slot_raw}}", toSlot,
		"{{state_dir_raw}}", stateDir,
		"{{state_file_raw}}", stateFile,
		"{{project_raw}}", project,
	)
	return strings.TrimSpace(replacer.Replace(template))
}

func buildPaasBlueGreenProjectName(app, target, slot string) string {
	parts := []string{
		sanitizePaasComposeProjectSegment(app),
		sanitizePaasComposeProjectSegment(target),
		sanitizePaasComposeProjectSegment(slot),
	}
	out := strings.Join(parts, "-")
	out = strings.Trim(out, "-")
	if out == "" {
		return "si-paas-bg"
	}
	return out
}

func sanitizePaasComposeProjectSegment(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return "x"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "x"
	}
	return out
}

func resolvePaasBlueGreenRemotePaths(remoteRoot, app, target, slot, release string) (string, string, string, string) {
	root := normalizePaasRemoteDir(remoteRoot)
	appSlug := sanitizePaasReleasePathSegment(app)
	targetSlug := sanitizePaasReleasePathSegment(target)
	slotSlug := normalizePaasBlueGreenSlot(slot)
	releaseSlug := sanitizePaasReleasePathSegment(release)
	base := path.Join(root, "bluegreen", appSlug, targetSlug)
	releaseDir := path.Join(base, slotSlug, releaseSlug)
	stateDir := path.Join(base, "state")
	stateFile := path.Join(stateDir, "active_slot")
	project := buildPaasBlueGreenProjectName(appSlug, targetSlug, slotSlug)
	return releaseDir, stateDir, stateFile, project
}

func normalizePaasBlueGreenSlot(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "green":
		return "green"
	default:
		return "blue"
	}
}

func flipPaasBlueGreenSlot(current string) string {
	if normalizePaasBlueGreenSlot(current) == "blue" {
		return "green"
	}
	return "blue"
}

func resolvePaasBlueGreenPolicyStorePathForContext(contextName string) (string, error) {
	contextDir, err := resolvePaasContextDir(contextName)
	if err != nil {
		return "", err
	}
	return filepath.Join(contextDir, "bluegreen.json"), nil
}

func loadPaasBlueGreenPolicyStoreForContext(contextName string) (paasBlueGreenPolicyStore, error) {
	path, err := resolvePaasBlueGreenPolicyStorePathForContext(contextName)
	if err != nil {
		return paasBlueGreenPolicyStore{}, err
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- local state path derived from context root.
	if err != nil {
		if os.IsNotExist(err) {
			return paasBlueGreenPolicyStore{Apps: map[string]paasBlueGreenAppPolicy{}}, nil
		}
		return paasBlueGreenPolicyStore{}, err
	}
	var store paasBlueGreenPolicyStore
	if err := json.Unmarshal(raw, &store); err != nil {
		return paasBlueGreenPolicyStore{}, fmt.Errorf("invalid blue/green policy store: %w", err)
	}
	if store.Apps == nil {
		store.Apps = map[string]paasBlueGreenAppPolicy{}
	}
	for appKey, appPolicy := range store.Apps {
		if appPolicy.Targets == nil {
			appPolicy.Targets = map[string]paasBlueGreenTargetPolicy{}
		}
		for targetKey, targetPolicy := range appPolicy.Targets {
			appPolicy.Targets[targetKey] = normalizePaasBlueGreenTargetPolicy(targetPolicy)
		}
		store.Apps[appKey] = appPolicy
	}
	return store, nil
}

func loadPaasBlueGreenPolicyStore() (paasBlueGreenPolicyStore, error) {
	return loadPaasBlueGreenPolicyStoreForContext(currentPaasContext())
}

func savePaasBlueGreenPolicyStoreForContext(contextName string, store paasBlueGreenPolicyStore) error {
	path, err := resolvePaasBlueGreenPolicyStorePathForContext(contextName)
	if err != nil {
		return err
	}
	if store.Apps == nil {
		store.Apps = map[string]paasBlueGreenAppPolicy{}
	}
	for appKey, appPolicy := range store.Apps {
		if appPolicy.Targets == nil {
			appPolicy.Targets = map[string]paasBlueGreenTargetPolicy{}
		}
		for targetKey, targetPolicy := range appPolicy.Targets {
			appPolicy.Targets[targetKey] = normalizePaasBlueGreenTargetPolicy(targetPolicy)
		}
		store.Apps[appKey] = appPolicy
	}
	raw, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

func savePaasBlueGreenPolicyStore(store paasBlueGreenPolicyStore) error {
	return savePaasBlueGreenPolicyStoreForContext(currentPaasContext(), store)
}

func normalizePaasBlueGreenTargetPolicy(in paasBlueGreenTargetPolicy) paasBlueGreenTargetPolicy {
	out := paasBlueGreenTargetPolicy{
		ActiveSlot: normalizePaasBlueGreenSlot(in.ActiveSlot),
		Slots:      map[string]string{},
		UpdatedAt:  strings.TrimSpace(in.UpdatedAt),
	}
	if in.Slots != nil {
		for slot, release := range in.Slots {
			key := normalizePaasBlueGreenSlot(slot)
			out.Slots[key] = strings.TrimSpace(release)
		}
	}
	return out
}

func formatPaasBlueGreenSlots(values map[string]string) string {
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value := strings.TrimSpace(values[key])
		if strings.TrimSpace(key) == "" || value == "" {
			continue
		}
		parts = append(parts, key+":"+value)
	}
	return strings.Join(parts, ",")
}

func buildPaasBlueGreenStatuses(targetRows map[string]paasTarget, statusByTarget map[string]string) []paasTargetApplyStatus {
	if len(statusByTarget) == 0 {
		return nil
	}
	out := make([]paasTargetApplyStatus, 0, len(statusByTarget))
	if len(targetRows) > 0 {
		keys := make([]string, 0, len(targetRows))
		for key := range targetRows {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			status := strings.TrimSpace(statusByTarget[key])
			if status == "" {
				continue
			}
			out = append(out, paasTargetApplyStatus{Target: key, Status: status})
		}
		return out
	}
	keys := make([]string, 0, len(statusByTarget))
	for key := range statusByTarget {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		status := strings.TrimSpace(statusByTarget[key])
		if status == "" {
			continue
		}
		out = append(out, paasTargetApplyStatus{Target: key, Status: status})
	}
	return out
}

func failPaasDeployBlueGreen(jsonOut bool, err error, fields map[string]string) {
	failure := asPaasOperationFailure(err)
	alertFields := copyPaasFields(fields)
	alertFields = appendPaasAlertDispatchField(alertFields, "error_code", failure.Code)
	alertFields = appendPaasAlertDispatchField(alertFields, "error_stage", failure.Stage)
	alertFields = appendPaasAlertDispatchField(alertFields, "error_target", failure.Target)
	if historyPath := emitPaasOperationalAlert(
		"deploy bluegreen failure",
		"critical",
		failure.Target,
		errString(failure.Err),
		failure.Remediation,
		alertFields,
	); strings.TrimSpace(historyPath) != "" {
		if fields == nil {
			fields = map[string]string{}
		}
		fields["alert_history"] = historyPath
	}
	_ = recordPaasDeployEvent("deploy bluegreen", "failed", fields, err)
	failPaasCommand("deploy bluegreen", jsonOut, err, fields)
}
