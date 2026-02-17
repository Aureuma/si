package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
)

const (
	defaultPaasLogsQueryTimeout = 45 * time.Second
	defaultPaasReleaseRemoteDir = "/opt/si/paas/releases"
)

type paasLogResult struct {
	Target     string `json:"target"`
	App        string `json:"app,omitempty"`
	Release    string `json:"release,omitempty"`
	Service    string `json:"service,omitempty"`
	Command    string `json:"command"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
	Output     string `json:"output,omitempty"`
	LineCount  int    `json:"line_count"`
	DurationMs int64  `json:"duration_ms"`
}

func runPaasLogs(app, target, service string, tail int, since string, follow bool) ([]paasLogResult, error) {
	selected := normalizeTargets(target, "")
	resolvedTargets, err := resolvePaasDeployTargets(selected)
	if err != nil {
		return nil, newPaasOperationFailure(
			paasFailureTargetResolution,
			"target_resolve",
			"",
			"verify --target value or set a default target via `si paas target use --target <id>`",
			err,
		)
	}

	resolvedApp := strings.TrimSpace(app)
	resolvedService := strings.TrimSpace(service)
	resolvedSince := strings.TrimSpace(since)
	results := make([]paasLogResult, 0, len(resolvedTargets))
	for _, row := range resolvedTargets {
		started := time.Now()
		item := paasLogResult{
			Target:  row.Name,
			App:     resolvedApp,
			Service: resolvedService,
			Status:  "failed",
		}

		cmd, releaseID, buildErr := buildPaasLogsRemoteCommand(resolvedApp, resolvedService, tail, resolvedSince, follow)
		item.Release = releaseID
		if buildErr != nil {
			item.Error = buildErr.Error()
			item.DurationMs = time.Since(started).Milliseconds()
			results = append(results, item)
			continue
		}
		item.Command = cmd

		ctx, cancel := context.WithCancel(context.Background())
		if !follow {
			ctx, cancel = context.WithTimeout(context.Background(), defaultPaasLogsQueryTimeout)
		}
		output, queryErr := runPaasSSHCommand(ctx, row, cmd)
		cancel()
		if queryErr != nil {
			item.Error = queryErr.Error()
			item.DurationMs = time.Since(started).Milliseconds()
			results = append(results, item)
			continue
		}
		item.Output = output
		item.LineCount = countPaasLogLines(output)
		item.Status = "ok"
		item.DurationMs = time.Since(started).Milliseconds()
		results = append(results, item)
	}
	return results, nil
}

func buildPaasLogsRemoteCommand(app, service string, tail int, since string, follow bool) (string, string, error) {
	args := []string{"docker", "logs", "--tail", strconv.Itoa(tail)}
	if since != "" {
		args = append(args, "--since", since)
	}
	if follow {
		args = append(args, "--follow")
	}

	if app == "" {
		if service == "" {
			return "", "", fmt.Errorf("--service is required when --app is not set")
		}
		args = append(args, service)
		return "sh -lc " + quoteSingle(joinPaasShellArgs(args)), "", nil
	}

	releaseID, err := resolvePaasCurrentRelease(app)
	if err != nil {
		return "", "", fmt.Errorf("resolve current release for %q: %w", app, err)
	}
	if strings.TrimSpace(releaseID) == "" {
		releaseID, err = resolveLatestPaasReleaseID("", app, "")
		if err != nil {
			return "", "", fmt.Errorf("resolve latest release for %q: %w", app, err)
		}
	}
	if strings.TrimSpace(releaseID) == "" {
		return "", "", fmt.Errorf("no release history found for app %q; deploy once first", app)
	}
	releaseDir := path.Join(defaultPaasReleaseRemoteDir, sanitizePaasReleasePathSegment(releaseID))

	composeArgs := []string{"docker", "compose", "-f", "compose.yaml", "logs", "--no-color", "--tail", strconv.Itoa(tail)}
	if since != "" {
		composeArgs = append(composeArgs, "--since", since)
	}
	if follow {
		composeArgs = append(composeArgs, "--follow")
	}
	if service != "" {
		composeArgs = append(composeArgs, service)
	}
	cmd := "cd " + quoteSingle(releaseDir) + " && " + joinPaasShellArgs(composeArgs)
	return "sh -lc " + quoteSingle(cmd), strings.TrimSpace(releaseID), nil
}

func joinPaasShellArgs(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, quoteSingle(strings.TrimSpace(arg)))
	}
	return strings.Join(quoted, " ")
}

func countPaasLogLines(value string) int {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0
	}
	return len(strings.Split(trimmed, "\n"))
}

func printPaasLogsResults(jsonOut bool, app, target, service, since string, tail int, follow bool, results []paasLogResult) {
	failed := 0
	for _, row := range results {
		if row.Status != "ok" {
			failed++
		}
	}
	if jsonOut {
		payload := map[string]any{
			"ok":      failed == 0,
			"command": "logs",
			"context": currentPaasContext(),
			"mode":    "live",
			"app":     strings.TrimSpace(app),
			"target":  strings.TrimSpace(target),
			"service": strings.TrimSpace(service),
			"tail":    tail,
			"since":   strings.TrimSpace(since),
			"follow":  follow,
			"count":   len(results),
			"data":    results,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		if failed > 0 {
			os.Exit(1)
		}
		return
	}

	fmt.Printf("%s %d\n", styleHeading("paas logs:"), len(results))
	fmt.Printf("  app=%s target=%s service=%s tail=%d since=%s follow=%s\n",
		strings.TrimSpace(app),
		strings.TrimSpace(target),
		strings.TrimSpace(service),
		tail,
		strings.TrimSpace(since),
		boolString(follow),
	)
	for _, row := range results {
		fmt.Printf("  [%s] %s lines=%d duration_ms=%d\n", row.Status, row.Target, row.LineCount, row.DurationMs)
		if strings.TrimSpace(row.Release) != "" {
			fmt.Printf("    release=%s\n", row.Release)
		}
		if strings.TrimSpace(row.Error) != "" {
			fmt.Printf("    %s\n", styleDim(row.Error))
		}
		if strings.TrimSpace(row.Output) != "" {
			lines := strings.Split(row.Output, "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				fmt.Printf("    %s\n", line)
			}
		}
	}
	if failed > 0 {
		os.Exit(1)
	}
}
