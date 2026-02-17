package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

const paasAlertIngressTLSUsageText = "usage: si paas alert ingress-tls [--target <id>] [--targets <id1,id2|all>] [--timeout <duration>] [--log-tail <n>] [--json]"

type paasIngressTLSResult struct {
	Target     string `json:"target"`
	Status     string `json:"status"`
	Severity   string `json:"severity"`
	RetryCount int    `json:"retry_count"`
	Message    string `json:"message"`
	Guidance   string `json:"guidance"`
}

func cmdPaasAlertIngressTLS(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas alert ingress-tls", flag.ExitOnError)
	target := fs.String("target", "", "single target id")
	targets := fs.String("targets", "", "target ids csv or all")
	timeout := fs.String("timeout", "20s", "per-target check timeout")
	logTail := fs.Int("log-tail", 200, "number of traefik log lines to inspect")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasAlertIngressTLSUsageText)
		return
	}
	if *logTail < 1 {
		fmt.Fprintln(os.Stderr, "--log-tail must be >= 1")
		printUsage(paasAlertIngressTLSUsageText)
		return
	}
	timeoutValue, err := time.ParseDuration(strings.TrimSpace(*timeout))
	if err != nil || timeoutValue <= 0 {
		fmt.Fprintln(os.Stderr, "invalid --timeout")
		printUsage(paasAlertIngressTLSUsageText)
		return
	}
	resolvedTargets := normalizeTargets(*target, *targets)
	rows, err := resolvePaasDeployTargets(resolvedTargets)
	if err != nil {
		fatal(err)
	}
	results := make([]paasIngressTLSResult, 0, len(rows))
	alertsEmitted := 0
	retryingTargets := []string{}
	degradedTargets := []string{}
	missingTargets := []string{}
	for _, row := range rows {
		res := checkPaasIngressTLSStatus(row, timeoutValue, *logTail)
		results = append(results, res)
		if res.Status == "retrying" {
			retryingTargets = append(retryingTargets, row.Name)
		}
		if res.Status == "degraded" {
			degradedTargets = append(degradedTargets, row.Name)
		}
		if res.Status == "missing" || res.Status == "unmanaged" {
			missingTargets = append(missingTargets, row.Name)
		}
		if res.Severity != "info" {
			alertsEmitted++
			recordPaasAlertEntry(paasAlertEntry{
				Command:  "alert ingress-tls",
				Severity: res.Severity,
				Status:   res.Status,
				Target:   res.Target,
				Message:  res.Message,
				Guidance: res.Guidance,
				Fields: map[string]string{
					"retry_count": intString(res.RetryCount),
				},
			})
		}
	}
	fields := map[string]string{
		"targets_checked":   intString(len(results)),
		"retrying_targets":  formatTargets(retryingTargets),
		"degraded_targets":  formatTargets(degradedTargets),
		"missing_targets":   formatTargets(missingTargets),
		"alerts_emitted":    intString(alertsEmitted),
		"timeout":           timeoutValue.String(),
		"log_tail":          intString(*logTail),
		"recovery_guidance": "run `si paas target ingress-baseline` and verify DNS + ports 80/443 + Traefik logs on affected targets",
	}
	if historyPath, _ := resolvePaasAlertHistoryPath(currentPaasContext()); strings.TrimSpace(historyPath) != "" {
		fields["alert_history"] = historyPath
	}
	if jsonOut {
		payload := map[string]any{
			"ok":      true,
			"command": "alert ingress-tls",
			"context": currentPaasContext(),
			"mode":    "live",
			"fields":  fields,
			"count":   len(results),
			"data":    results,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	printPaasScaffold("alert ingress-tls", fields, false)
	for _, row := range results {
		fmt.Printf("  - target=%s status=%s severity=%s retries=%d\n", row.Target, row.Status, row.Severity, row.RetryCount)
		if strings.TrimSpace(row.Message) != "" {
			fmt.Printf("    %s\n", row.Message)
		}
	}
}

func checkPaasIngressTLSStatus(target paasTarget, timeout time.Duration, logTail int) paasIngressTLSResult {
	result := paasIngressTLSResult{
		Target:   target.Name,
		Status:   "ok",
		Severity: "info",
		Message:  "Traefik ACME signals are healthy.",
		Guidance: "No action required.",
	}
	if strings.ToLower(strings.TrimSpace(target.IngressProvider)) != paasIngressProviderTraefik {
		result.Status = "unmanaged"
		result.Message = "Target is not configured with Traefik ingress baseline."
		result.Guidance = "Run `si paas target ingress-baseline` to configure Traefik for this target."
		return result
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	containerCmd := "docker ps --format '{{.Names}}' | grep '^si-traefik-' | head -n 1 || true"
	container, err := runPaasSSHCommand(ctx, target, "sh -lc "+quoteSingle(containerCmd))
	if err != nil {
		result.Status = "degraded"
		result.Severity = "critical"
		result.Message = "Failed to inspect Traefik container state."
		result.Guidance = "Verify SSH reachability and Docker runtime health, then rerun ingress checks."
		return result
	}
	container = strings.TrimSpace(container)
	if container == "" {
		result.Status = "missing"
		result.Severity = "critical"
		result.Message = "Traefik container is not running."
		result.Guidance = "Run `si paas target ingress-baseline`, deploy Traefik compose, and verify ports 80/443 are reachable."
		return result
	}

	acmeCmd := "docker exec " + quoteSingle(container) + " sh -lc " + quoteSingle("test -s /var/lib/traefik/acme.json && echo ready || echo empty")
	acmeState, err := runPaasSSHCommand(ctx, target, acmeCmd)
	if err != nil || strings.TrimSpace(acmeState) != "ready" {
		result.Status = "degraded"
		result.Severity = "warning"
		result.Message = "Traefik ACME store is empty or inaccessible."
		result.Guidance = "Check DNS records and ensure ACME challenge traffic reaches this target on port 80."
	}

	logCmd := fmt.Sprintf("docker logs --tail %d %s 2>&1 || true", logTail, quoteSingle(container))
	logs, err := runPaasSSHCommand(ctx, target, "sh -lc "+quoteSingle(logCmd))
	if err != nil {
		return result
	}
	retryCount, hasFailure := analyzePaasTraefikACMELogs(logs)
	result.RetryCount = retryCount
	if hasFailure {
		result.Status = "degraded"
		result.Severity = "critical"
		result.Message = "Traefik logs show persistent ACME/TLS failures."
		result.Guidance = "Check DNS propagation, verify 80/443 reachability, and inspect `docker logs` for challenge failures."
		return result
	}
	if retryCount > 0 {
		result.Status = "retrying"
		if result.Severity == "info" {
			result.Severity = "warning"
		}
		result.Message = "Traefik ACME retries detected."
		result.Guidance = "Monitor DNS/ingress reachability and rerun `si paas alert ingress-tls` until retries clear."
	}
	return result
}

func analyzePaasTraefikACMELogs(raw string) (int, bool) {
	text := strings.ToLower(raw)
	retryPatterns := []string{
		"retrying in",
		"acme challenge",
		"timeout during connect",
		"connection refused",
	}
	failurePatterns := []string{
		"unable to obtain acme certificate",
		"error while renewing certificate",
		"cannot obtain certificates",
		"tls challenge failed",
	}
	retries := 0
	for _, pattern := range retryPatterns {
		retries += strings.Count(text, pattern)
	}
	for _, pattern := range failurePatterns {
		if strings.Contains(text, pattern) {
			return retries, true
		}
	}
	return retries, false
}
