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
		fatal(fmt.Errorf("invalid --apply-timeout %q", strings.TrimSpace(*applyTimeout)))
	}
	composeGuardrail, err := enforcePaasPlaintextSecretGuardrail(strings.TrimSpace(*composeFile), *allowPlaintextSecrets)
	if err != nil {
		fatal(err)
	}
	vaultGuardrail, err := runPaasVaultDeployGuardrail(strings.TrimSpace(*vaultFile), *allowUntrustedVault)
	if err != nil {
		fatal(err)
	}
	bundleDir, bundleMetaPath, err := ensurePaasReleaseBundle(
		strings.TrimSpace(*app),
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
		fatal(err)
	}
	releaseID := filepath.Base(bundleDir)
	appliedTargets, err := applyPaasReleaseToTargets(*applyRemote, resolvedTargets, bundleDir, releaseID, strings.TrimSpace(*remoteDir), applyTimeoutValue)
	if err != nil {
		fatal(err)
	}
	fields := map[string]string{
		"app":                      strings.TrimSpace(*app),
		"apply":                    boolString(*applyRemote),
		"apply_timeout":            applyTimeoutValue.String(),
		"applied_targets":          formatTargets(appliedTargets),
		"bundle_dir":               bundleDir,
		"bundle_metadata":          bundleMetaPath,
		"compose_secret_guardrail": composeGuardrail["compose_secret_guardrail"],
		"compose_secret_findings":  composeGuardrail["compose_secret_findings"],
		"compose_file":             strings.TrimSpace(*composeFile),
		"continue_on_error":        boolString(*continueOnError),
		"max_parallel":             intString(*maxParallel),
		"release":                  releaseID,
		"remote_dir":               strings.TrimSpace(*remoteDir),
		"strategy":                 resolvedStrategy,
		"targets":                  formatTargets(resolvedTargets),
		"vault_file":               vaultGuardrail.File,
		"vault_recipients":         intString(vaultGuardrail.RecipientCount),
		"vault_trust":              boolString(vaultGuardrail.Trusted),
		"wait_timeout":             strings.TrimSpace(*waitTimeout),
	}
	if !vaultGuardrail.Trusted {
		fields["vault_trust_warning"] = vaultGuardrail.TrustWarning
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
	vaultGuardrail, err := runPaasVaultDeployGuardrail(strings.TrimSpace(*vaultFile), *allowUntrustedVault)
	if err != nil {
		fatal(err)
	}
	fields := map[string]string{
		"app":               strings.TrimSpace(*app),
		"continue_on_error": boolString(*continueOnError),
		"max_parallel":      intString(*maxParallel),
		"strategy":          resolvedStrategy,
		"targets":           formatTargets(resolvedTargets),
		"to_release":        strings.TrimSpace(*toRelease),
		"vault_file":        vaultGuardrail.File,
		"vault_recipients":  intString(vaultGuardrail.RecipientCount),
		"vault_trust":       boolString(vaultGuardrail.Trusted),
		"wait_timeout":      strings.TrimSpace(*waitTimeout),
	}
	if !vaultGuardrail.Trusted {
		fields["vault_trust_warning"] = vaultGuardrail.TrustWarning
	}
	printPaasScaffold("rollback", fields, jsonOut)
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
