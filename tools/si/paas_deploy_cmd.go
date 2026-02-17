package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func cmdPaasDeploy(args []string) {
	if len(args) > 0 {
		switch strings.ToLower(strings.TrimSpace(args[0])) {
		case "prune":
			cmdPaasDeployPrune(args[1:])
			return
		}
	}
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas deploy", flag.ExitOnError)
	app := fs.String("app", "", "app slug")
	target := fs.String("target", "", "single target id")
	targets := fs.String("targets", "", "target ids csv or all")
	strategy := fs.String("strategy", "serial", "fan-out strategy (serial|rolling|canary|parallel)")
	maxParallel := fs.Int("max-parallel", 1, "maximum parallel target operations")
	continueOnError := fs.Bool("continue-on-error", false, "continue deployment on target errors")
	release := fs.String("release", "", "release identifier")
	composeFile := fs.String("compose-file", "compose.yaml", "compose file path")
	bundleRoot := fs.String("bundle-root", "", "release bundle root path (defaults to context-scoped state root)")
	applyRemote := fs.Bool("apply", false, "upload bundle and apply docker compose on remote targets")
	remoteDir := fs.String("remote-dir", "/opt/si/paas/releases", "remote release root directory")
	applyTimeout := fs.String("apply-timeout", "2m", "per-target remote apply timeout")
	healthCmd := fs.String("health-cmd", defaultPaasHealthCheckCommand, "remote health command executed after apply")
	healthTimeout := fs.String("health-timeout", "45s", "per-target health check timeout")
	rollbackOnFailure := fs.Bool("rollback-on-failure", true, "attempt rollback to previous known-good release when health checks fail")
	rollbackTimeout := fs.String("rollback-timeout", "2m", "per-target rollback apply timeout")
	waitTimeout := fs.String("wait-timeout", "5m", "deployment wait timeout")
	vaultFile := fs.String("vault-file", "", "explicit vault env file path")
	allowPlaintextSecrets := fs.Bool("allow-plaintext-secrets", false, "allow plaintext secret assignments in compose file (unsafe)")
	allowUntrustedVault := fs.Bool("allow-untrusted-vault", false, "allow deploy with untrusted vault fingerprint (unsafe)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasDeployUsageText)
		return
	}
	resolvedTargets := normalizeTargets(*target, *targets)
	resolvedStrategy := strings.ToLower(strings.TrimSpace(*strategy))
	if !isValidDeployStrategy(resolvedStrategy) {
		fmt.Fprintf(os.Stderr, "invalid --strategy %q\n", *strategy)
		printUsage(paasDeployUsageText)
		return
	}
	if *maxParallel < 1 {
		fmt.Fprintln(os.Stderr, "--max-parallel must be >= 1")
		printUsage(paasDeployUsageText)
		return
	}
	applyTimeoutValue, err := time.ParseDuration(strings.TrimSpace(*applyTimeout))
	if err != nil || applyTimeoutValue <= 0 {
		failPaasDeploy(jsonOut, newPaasOperationFailure(
			paasFailureInvalidArgument,
			"flag_validation",
			"",
			"pass a positive duration for --apply-timeout (for example 2m)",
			fmt.Errorf("invalid --apply-timeout %q", strings.TrimSpace(*applyTimeout)),
		), nil)
	}
	healthTimeoutValue, err := time.ParseDuration(strings.TrimSpace(*healthTimeout))
	if err != nil || healthTimeoutValue <= 0 {
		failPaasDeploy(jsonOut, newPaasOperationFailure(
			paasFailureInvalidArgument,
			"flag_validation",
			"",
			"pass a positive duration for --health-timeout (for example 45s)",
			fmt.Errorf("invalid --health-timeout %q", strings.TrimSpace(*healthTimeout)),
		), nil)
	}
	rollbackTimeoutValue, err := time.ParseDuration(strings.TrimSpace(*rollbackTimeout))
	if err != nil || rollbackTimeoutValue <= 0 {
		failPaasDeploy(jsonOut, newPaasOperationFailure(
			paasFailureInvalidArgument,
			"flag_validation",
			"",
			"pass a positive duration for --rollback-timeout (for example 2m)",
			fmt.Errorf("invalid --rollback-timeout %q", strings.TrimSpace(*rollbackTimeout)),
		), nil)
	}
	composeGuardrail, err := enforcePaasPlaintextSecretGuardrail(strings.TrimSpace(*composeFile), *allowPlaintextSecrets)
	if err != nil {
		failPaasDeploy(jsonOut, newPaasOperationFailure(
			paasFailurePlaintextSecrets,
			"precheck",
			"",
			"replace inline secret literals with variables and `si paas secret set`",
			err,
		), nil)
	}
	vaultGuardrail, err := runPaasVaultDeployGuardrail(strings.TrimSpace(*vaultFile), *allowUntrustedVault)
	if err != nil {
		failPaasDeploy(jsonOut, newPaasOperationFailure(
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
	bundleDir, bundleMetaPath, err := ensurePaasReleaseBundle(
		resolvedApp,
		strings.TrimSpace(*release),
		strings.TrimSpace(*composeFile),
		strings.TrimSpace(*bundleRoot),
		resolvedStrategy,
		resolvedTargets,
		map[string]string{
			"compose_secret_guardrail": composeGuardrail["compose_secret_guardrail"],
			"compose_secret_findings":  composeGuardrail["compose_secret_findings"],
			"vault_file":               vaultGuardrail.File,
			"vault_recipients":         intString(vaultGuardrail.RecipientCount),
			"vault_trust":              boolString(vaultGuardrail.Trusted),
		},
	)
	if err != nil {
		failPaasDeploy(jsonOut, newPaasOperationFailure(
			paasFailureBundleCreate,
			"bundle_create",
			"",
			"verify compose file path and state root permissions",
			err,
		), nil)
	}
	releaseID := filepath.Base(bundleDir)
	rollbackReleaseID := ""
	rollbackBundleDir := ""
	if *applyRemote && *rollbackOnFailure {
		rollbackReleaseID, err = resolvePaasCurrentRelease(resolvedApp)
		if err == nil && strings.TrimSpace(rollbackReleaseID) != "" && !strings.EqualFold(strings.TrimSpace(rollbackReleaseID), releaseID) {
			rollbackBundleDir, err = resolvePaasReleaseBundleDir(strings.TrimSpace(*bundleRoot), resolvedApp, rollbackReleaseID)
			if err != nil {
				rollbackReleaseID = ""
				rollbackBundleDir = ""
			}
		}
		if strings.TrimSpace(rollbackReleaseID) == "" {
			rollbackReleaseID, _ = resolveLatestPaasReleaseID(strings.TrimSpace(*bundleRoot), resolvedApp, releaseID)
			if strings.TrimSpace(rollbackReleaseID) != "" {
				rollbackBundleDir, err = resolvePaasReleaseBundleDir(strings.TrimSpace(*bundleRoot), resolvedApp, rollbackReleaseID)
				if err != nil {
					rollbackReleaseID = ""
					rollbackBundleDir = ""
				}
			}
		}
	}
	applyResult, err := applyPaasReleaseToTargets(paasApplyOptions{
		Enabled:              *applyRemote,
		SelectedTargets:      resolvedTargets,
		BundleDir:            bundleDir,
		ReleaseID:            releaseID,
		RemoteRoot:           strings.TrimSpace(*remoteDir),
		ApplyTimeout:         applyTimeoutValue,
		HealthTimeout:        healthTimeoutValue,
		HealthCommand:        strings.TrimSpace(*healthCmd),
		RollbackOnFailure:    *rollbackOnFailure,
		RollbackBundleDir:    rollbackBundleDir,
		RollbackReleaseID:    rollbackReleaseID,
		RollbackApplyTimeout: rollbackTimeoutValue,
	})
	if err != nil {
		failPaasDeploy(jsonOut, err, map[string]string{
			"release":            releaseID,
			"bundle_dir":         bundleDir,
			"rollback_candidate": rollbackReleaseID,
		})
	}
	if *applyRemote && len(applyResult.AppliedTargets) > 0 {
		if err := recordPaasSuccessfulRelease(resolvedApp, releaseID); err != nil {
			failPaasDeploy(jsonOut, newPaasOperationFailure(
				paasFailureUnknown,
				"state_record",
				"",
				"verify local state permissions and rerun deploy",
				err,
			), nil)
		}
	}
	fields := map[string]string{
		"app":                      resolvedApp,
		"apply":                    boolString(*applyRemote),
		"apply_timeout":            applyTimeoutValue.String(),
		"applied_targets":          formatTargets(applyResult.AppliedTargets),
		"bundle_dir":               bundleDir,
		"bundle_metadata":          bundleMetaPath,
		"compose_secret_guardrail": composeGuardrail["compose_secret_guardrail"],
		"compose_secret_findings":  composeGuardrail["compose_secret_findings"],
		"compose_file":             strings.TrimSpace(*composeFile),
		"continue_on_error":        boolString(*continueOnError),
		"max_parallel":             intString(*maxParallel),
		"release":                  releaseID,
		"remote_dir":               strings.TrimSpace(*remoteDir),
		"rollback_on_failure":      boolString(*rollbackOnFailure),
		"rollback_release":         rollbackReleaseID,
		"rollback_targets":         formatTargets(applyResult.RolledBackTargets),
		"rollback_timeout":         rollbackTimeoutValue.String(),
		"strategy":                 resolvedStrategy,
		"targets":                  formatTargets(resolvedTargets),
		"vault_file":               vaultGuardrail.File,
		"vault_recipients":         intString(vaultGuardrail.RecipientCount),
		"vault_trust":              boolString(vaultGuardrail.Trusted),
		"health_cmd":               applyResult.HealthCommand,
		"health_checked_targets":   formatTargets(applyResult.HealthyTargets),
		"health_timeout":           healthTimeoutValue.String(),
		"wait_timeout":             strings.TrimSpace(*waitTimeout),
	}
	if !vaultGuardrail.Trusted {
		fields["vault_trust_warning"] = vaultGuardrail.TrustWarning
	}
	if eventPath := recordPaasDeployEvent("deploy", "succeeded", fields, nil); strings.TrimSpace(eventPath) != "" {
		fields["event_log"] = eventPath
	}
	printPaasScaffold("deploy", fields, jsonOut)
}

func cmdPaasRollback(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas rollback", flag.ExitOnError)
	app := fs.String("app", "", "app slug")
	target := fs.String("target", "", "single target id")
	targets := fs.String("targets", "", "target ids csv or all")
	toRelease := fs.String("to-release", "", "release identifier to restore")
	strategy := fs.String("strategy", "serial", "fan-out strategy (serial|rolling|canary|parallel)")
	maxParallel := fs.Int("max-parallel", 1, "maximum parallel target operations")
	continueOnError := fs.Bool("continue-on-error", false, "continue rollback on target errors")
	bundleRoot := fs.String("bundle-root", "", "release bundle root path (defaults to context-scoped state root)")
	applyRemote := fs.Bool("apply", false, "upload release bundle and apply docker compose on targets")
	remoteDir := fs.String("remote-dir", "/opt/si/paas/releases", "remote release root directory")
	applyTimeout := fs.String("apply-timeout", "2m", "per-target remote apply timeout")
	healthCmd := fs.String("health-cmd", defaultPaasHealthCheckCommand, "remote health command executed after rollback")
	healthTimeout := fs.String("health-timeout", "45s", "per-target health check timeout")
	waitTimeout := fs.String("wait-timeout", "5m", "rollback wait timeout")
	vaultFile := fs.String("vault-file", "", "explicit vault env file path")
	allowUntrustedVault := fs.Bool("allow-untrusted-vault", false, "allow rollback with untrusted vault fingerprint (unsafe)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasRollbackUsageText)
		return
	}
	resolvedTargets := normalizeTargets(*target, *targets)
	resolvedStrategy := strings.ToLower(strings.TrimSpace(*strategy))
	if !isValidDeployStrategy(resolvedStrategy) {
		fmt.Fprintf(os.Stderr, "invalid --strategy %q\n", *strategy)
		printUsage(paasRollbackUsageText)
		return
	}
	if *maxParallel < 1 {
		fmt.Fprintln(os.Stderr, "--max-parallel must be >= 1")
		printUsage(paasRollbackUsageText)
		return
	}
	applyTimeoutValue, err := time.ParseDuration(strings.TrimSpace(*applyTimeout))
	if err != nil || applyTimeoutValue <= 0 {
		failPaasRollback(jsonOut, newPaasOperationFailure(
			paasFailureInvalidArgument,
			"flag_validation",
			"",
			"pass a positive duration for --apply-timeout (for example 2m)",
			fmt.Errorf("invalid --apply-timeout %q", strings.TrimSpace(*applyTimeout)),
		), nil)
	}
	healthTimeoutValue, err := time.ParseDuration(strings.TrimSpace(*healthTimeout))
	if err != nil || healthTimeoutValue <= 0 {
		failPaasRollback(jsonOut, newPaasOperationFailure(
			paasFailureInvalidArgument,
			"flag_validation",
			"",
			"pass a positive duration for --health-timeout (for example 45s)",
			fmt.Errorf("invalid --health-timeout %q", strings.TrimSpace(*healthTimeout)),
		), nil)
	}
	vaultGuardrail, err := runPaasVaultDeployGuardrail(strings.TrimSpace(*vaultFile), *allowUntrustedVault)
	if err != nil {
		failPaasRollback(jsonOut, newPaasOperationFailure(
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
	resolvedRelease := strings.TrimSpace(*toRelease)
	if *applyRemote && resolvedRelease == "" {
		resolvedRelease, err = resolvePaasPreviousRelease(resolvedApp)
		if err != nil {
			failPaasRollback(jsonOut, newPaasOperationFailure(
				paasFailureRollbackResolve,
				"rollback_resolve",
				"",
				"provide --to-release explicitly or ensure deploy history exists for this app",
				err,
			), nil)
		}
		if strings.TrimSpace(resolvedRelease) == "" {
			current, _ := resolvePaasCurrentRelease(resolvedApp)
			resolvedRelease, _ = resolveLatestPaasReleaseID(strings.TrimSpace(*bundleRoot), resolvedApp, strings.TrimSpace(current))
		}
		if strings.TrimSpace(resolvedRelease) == "" {
			failPaasRollback(jsonOut, newPaasOperationFailure(
				paasFailureRollbackResolve,
				"rollback_resolve",
				"",
				"provide --to-release explicitly or run at least two successful deployments first",
				fmt.Errorf("no rollback release resolved for app %q", resolvedApp),
			), nil)
		}
	}
	appliedTargets := []string{}
	healthyTargets := []string{}
	if *applyRemote {
		bundleDir, err := resolvePaasReleaseBundleDir(strings.TrimSpace(*bundleRoot), resolvedApp, resolvedRelease)
		if err != nil {
			failPaasRollback(jsonOut, newPaasOperationFailure(
				paasFailureRollbackBundle,
				"rollback_bundle_resolve",
				"",
				"verify bundle root and release ID; ensure compose.yaml and release.json exist for the target release",
				err,
			), nil)
		}
		applyResult, err := applyPaasReleaseToTargets(paasApplyOptions{
			Enabled:              true,
			SelectedTargets:      resolvedTargets,
			BundleDir:            bundleDir,
			ReleaseID:            resolvedRelease,
			RemoteRoot:           strings.TrimSpace(*remoteDir),
			ApplyTimeout:         applyTimeoutValue,
			HealthTimeout:        healthTimeoutValue,
			HealthCommand:        strings.TrimSpace(*healthCmd),
			RollbackOnFailure:    false,
			RollbackBundleDir:    "",
			RollbackReleaseID:    "",
			RollbackApplyTimeout: applyTimeoutValue,
		})
		if err != nil {
			failPaasRollback(jsonOut, err, map[string]string{
				"to_release": resolvedRelease,
				"bundle_dir": bundleDir,
			})
		}
		appliedTargets = applyResult.AppliedTargets
		healthyTargets = applyResult.HealthyTargets
		if err := recordPaasSuccessfulRelease(resolvedApp, resolvedRelease); err != nil {
			failPaasRollback(jsonOut, newPaasOperationFailure(
				paasFailureUnknown,
				"state_record",
				"",
				"verify local state permissions and retry rollback recording",
				err,
			), nil)
		}
	}
	fields := map[string]string{
		"app":                    resolvedApp,
		"apply":                  boolString(*applyRemote),
		"apply_timeout":          applyTimeoutValue.String(),
		"applied_targets":        formatTargets(appliedTargets),
		"bundle_root":            strings.TrimSpace(*bundleRoot),
		"continue_on_error":      boolString(*continueOnError),
		"health_cmd":             strings.TrimSpace(*healthCmd),
		"health_checked_targets": formatTargets(healthyTargets),
		"health_timeout":         healthTimeoutValue.String(),
		"max_parallel":           intString(*maxParallel),
		"remote_dir":             strings.TrimSpace(*remoteDir),
		"strategy":               resolvedStrategy,
		"targets":                formatTargets(resolvedTargets),
		"to_release":             resolvedRelease,
		"vault_file":             vaultGuardrail.File,
		"vault_recipients":       intString(vaultGuardrail.RecipientCount),
		"vault_trust":            boolString(vaultGuardrail.Trusted),
		"wait_timeout":           strings.TrimSpace(*waitTimeout),
	}
	if !vaultGuardrail.Trusted {
		fields["vault_trust_warning"] = vaultGuardrail.TrustWarning
	}
	if eventPath := recordPaasDeployEvent("rollback", "succeeded", fields, nil); strings.TrimSpace(eventPath) != "" {
		fields["event_log"] = eventPath
	}
	printPaasScaffold("rollback", fields, jsonOut)
}

func failPaasDeploy(jsonOut bool, err error, fields map[string]string) {
	_ = recordPaasDeployEvent("deploy", "failed", fields, err)
	failPaasCommand("deploy", jsonOut, err, fields)
}

func failPaasRollback(jsonOut bool, err error, fields map[string]string) {
	_ = recordPaasDeployEvent("rollback", "failed", fields, err)
	failPaasCommand("rollback", jsonOut, err, fields)
}

func cmdPaasLogs(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas logs", flag.ExitOnError)
	app := fs.String("app", "", "app slug")
	target := fs.String("target", "", "target id")
	service := fs.String("service", "", "service name")
	tail := fs.Int("tail", 200, "number of lines")
	follow := fs.Bool("follow", false, "follow logs")
	since := fs.String("since", "", "relative duration")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasLogsUsageText)
		return
	}
	if *tail < 1 {
		fmt.Fprintln(os.Stderr, "--tail must be >= 1")
		printUsage(paasLogsUsageText)
		return
	}
	printPaasScaffold("logs", map[string]string{
		"app":     strings.TrimSpace(*app),
		"follow":  boolString(*follow),
		"service": strings.TrimSpace(*service),
		"since":   strings.TrimSpace(*since),
		"tail":    intString(*tail),
		"target":  strings.TrimSpace(*target),
	}, jsonOut)
}

func isValidDeployStrategy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "serial", "rolling", "canary", "parallel":
		return true
	default:
		return false
	}
}
