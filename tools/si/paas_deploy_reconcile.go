package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path"
	"strings"
	"time"
)

const paasDeployReconcileUsageText = "usage: si paas deploy reconcile --app <slug> [--target <id>] [--targets <id1,id2|all>] [--remote-dir <path>] [--timeout <duration>] [--json]"

type paasReconcileResult struct {
	Target         string   `json:"target"`
	DesiredRelease string   `json:"desired_release,omitempty"`
	Status         string   `json:"status"`
	RemotePresent  bool     `json:"remote_present"`
	RuntimeHealthy bool     `json:"runtime_healthy"`
	Orphaned       []string `json:"orphaned,omitempty"`
	Plan           []string `json:"plan,omitempty"`
	Error          string   `json:"error,omitempty"`
}

func cmdPaasDeployReconcile(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas deploy reconcile", flag.ExitOnError)
	app := fs.String("app", "", "app slug")
	target := fs.String("target", "", "single target id")
	targets := fs.String("targets", "", "target ids csv or all")
	remoteDir := fs.String("remote-dir", "/opt/si/paas/releases", "remote release root directory")
	timeout := fs.String("timeout", "45s", "per-target reconcile timeout")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasDeployReconcileUsageText)
		return
	}
	if !requirePaasValue(strings.TrimSpace(*app), "app", paasDeployReconcileUsageText) {
		return
	}
	timeoutValue, err := time.ParseDuration(strings.TrimSpace(*timeout))
	if err != nil || timeoutValue <= 0 {
		failPaasCommand("deploy reconcile", jsonOut, newPaasOperationFailure(
			paasFailureInvalidArgument,
			"flag_validation",
			"",
			"pass a positive --timeout duration (for example 45s)",
			fmt.Errorf("invalid --timeout %q", strings.TrimSpace(*timeout)),
		), nil)
	}
	selectedTargets := normalizeTargets(*target, *targets)
	resolvedTargets, err := resolvePaasDeployTargets(selectedTargets)
	if err != nil {
		failPaasCommand("deploy reconcile", jsonOut, err, nil)
	}
	desiredRelease, err := resolvePaasCurrentRelease(strings.TrimSpace(*app))
	if err != nil {
		failPaasCommand("deploy reconcile", jsonOut, newPaasOperationFailure(
			paasFailureUnknown,
			"desired_state_resolve",
			"",
			"verify deployment state file permissions and rerun reconcile",
			err,
		), nil)
	}
	if strings.TrimSpace(desiredRelease) == "" {
		desiredRelease, _ = resolveLatestPaasReleaseID("", strings.TrimSpace(*app), "")
	}

	results := make([]paasReconcileResult, 0, len(resolvedTargets))
	for _, t := range resolvedTargets {
		ctx, cancel := context.WithTimeout(context.Background(), timeoutValue)
		row := runPaasTargetReconcile(ctx, t, strings.TrimSpace(*remoteDir), strings.TrimSpace(desiredRelease))
		cancel()
		results = append(results, row)
	}

	okCount := 0
	driftCount := 0
	errorCount := 0
	for _, row := range results {
		switch row.Status {
		case "ok":
			okCount++
		case "error":
			errorCount++
		default:
			driftCount++
		}
	}
	for _, row := range results {
		emitAlert := false
		severity := "warning"
		switch row.Status {
		case "error":
			emitAlert = true
			severity = "critical"
		case "drifted":
			emitAlert = true
		default:
			if !row.RuntimeHealthy && row.RemotePresent {
				emitAlert = true
			}
		}
		if !emitAlert {
			continue
		}
		message := strings.TrimSpace(row.Error)
		if message == "" {
			message = "health degradation detected during reconcile"
		}
		guidance := ""
		if len(row.Plan) > 0 {
			guidance = row.Plan[0]
		}
		_ = emitPaasOperationalAlert(
			"deploy reconcile",
			severity,
			row.Target,
			message,
			guidance,
			map[string]string{
				"app":             strings.TrimSpace(*app),
				"status":          row.Status,
				"desired_release": row.DesiredRelease,
			},
		)
	}

	if jsonOut {
		payload := map[string]any{
			"ok":              errorCount == 0,
			"command":         "deploy reconcile",
			"context":         currentPaasContext(),
			"mode":            "live",
			"app":             strings.TrimSpace(*app),
			"desired_release": desiredRelease,
			"target_count":    len(results),
			"ok_count":        okCount,
			"drift_count":     driftCount,
			"error_count":     errorCount,
			"data":            results,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		if errorCount > 0 {
			os.Exit(1)
		}
		return
	}

	fmt.Printf("%s %d\n", styleHeading("paas deploy reconcile:"), len(results))
	fmt.Printf("  app=%s desired_release=%s\n", strings.TrimSpace(*app), desiredRelease)
	for _, row := range results {
		fmt.Printf("  [%s] %s\n", row.Status, row.Target)
		for _, step := range row.Plan {
			fmt.Printf("    - %s\n", step)
		}
		if strings.TrimSpace(row.Error) != "" {
			fmt.Printf("    %s\n", styleDim(row.Error))
		}
	}
	if errorCount > 0 {
		os.Exit(1)
	}
}

func runPaasTargetReconcile(ctx context.Context, target paasTarget, remoteRoot, desiredRelease string) paasReconcileResult {
	result := paasReconcileResult{
		Target:         target.Name,
		DesiredRelease: desiredRelease,
		Status:         "unknown",
		Plan:           []string{},
	}
	remoteRoot = strings.TrimSpace(remoteRoot)
	if remoteRoot == "" {
		remoteRoot = "/opt/si/paas/releases"
	}

	remoteReleases, listErr := runPaasRemoteListReleaseDirs(ctx, target, remoteRoot)
	if listErr != nil {
		result.Status = "error"
		result.Error = listErr.Error()
		result.Plan = append(result.Plan, "verify SSH connectivity and remote directory access")
		return result
	}

	desired := strings.TrimSpace(desiredRelease)
	if desired == "" {
		if len(remoteReleases) > 0 {
			result.Status = "unmanaged"
			result.Orphaned = append(result.Orphaned, remoteReleases...)
			result.Plan = append(result.Plan,
				"no desired release recorded locally",
				"set desired state via deploy/rollback history before attempting repair",
			)
			return result
		}
		result.Status = "missing"
		result.Plan = append(result.Plan, "no desired release and no remote release found; deploy app first")
		return result
	}

	desiredDir := path.Join(remoteRoot, sanitizePaasReleasePathSegment(desired))
	result.RemotePresent = runPaasRemotePathExists(ctx, target, desiredDir)
	if !result.RemotePresent {
		result.Status = "missing"
		result.Plan = append(result.Plan,
			fmt.Sprintf("desired release %s missing on target", desired),
			"run `si paas deploy --apply` to restore desired release",
		)
		return result
	}

	healthErr := runPaasRemoteHealthCheck(ctx, target, desired, remoteRoot, defaultPaasHealthCheckCommand)
	if healthErr != nil {
		result.Status = "drifted"
		result.RuntimeHealthy = false
		result.Error = healthErr.Error()
		result.Plan = append(result.Plan,
			"runtime health check failed for desired release",
			"run `si paas deploy --apply` for repair or `si paas rollback --apply` to previous known-good release",
		)
	} else {
		result.RuntimeHealthy = true
		result.Status = "ok"
	}

	for _, candidate := range remoteReleases {
		if strings.EqualFold(strings.TrimSpace(candidate), sanitizePaasReleasePathSegment(desired)) {
			continue
		}
		result.Orphaned = append(result.Orphaned, candidate)
	}
	if len(result.Orphaned) > 0 {
		result.Plan = append(result.Plan, "prune orphaned releases with `si paas deploy prune`")
		if result.Status == "ok" {
			result.Status = "orphaned"
		}
	}
	if len(result.Plan) == 0 {
		result.Plan = append(result.Plan, "state matches desired release")
	}
	return result
}

func runPaasRemoteListReleaseDirs(ctx context.Context, target paasTarget, remoteRoot string) ([]string, error) {
	remoteCmd := fmt.Sprintf("ls -1 %s 2>/dev/null || true", quoteSingle(strings.TrimSpace(remoteRoot)))
	out, err := runPaasSSHCommand(ctx, target, remoteCmd)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	outList := make([]string, 0, len(lines))
	seen := map[string]struct{}{}
	for _, line := range lines {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		outList = append(outList, name)
	}
	return outList, nil
}

func runPaasRemotePathExists(ctx context.Context, target paasTarget, pathValue string) bool {
	cmd := fmt.Sprintf("test -d %s && echo present", quoteSingle(strings.TrimSpace(pathValue)))
	out, err := runPaasSSHCommand(ctx, target, cmd)
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(out), "present")
}
