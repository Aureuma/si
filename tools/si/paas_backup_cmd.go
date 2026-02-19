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

var paasBackupActions = []subcommandAction{
	{Name: "contract", Description: "show supported backup profiles"},
	{Name: "run", Description: "trigger a WAL-G backup run"},
	{Name: "restore", Description: "trigger a WAL-G restore run"},
	{Name: "status", Description: "check backup service status"},
}

type paasBackupContract struct {
	Profile               string   `json:"profile"`
	Description           string   `json:"description"`
	RecommendedAddonPacks []string `json:"recommended_addon_packs"`
	DefaultRunService     string   `json:"default_run_service"`
	DefaultRestoreService string   `json:"default_restore_service"`
	RequiredEnv           []string `json:"required_env"`
}

type paasBackupResult struct {
	Target     string `json:"target"`
	App        string `json:"app"`
	Release    string `json:"release"`
	Service    string `json:"service"`
	Operation  string `json:"operation"`
	Command    string `json:"command"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
	Output     string `json:"output,omitempty"`
	DurationMs int64  `json:"duration_ms"`
}

func cmdPaasBackup(args []string) {
	resolved, showUsage, ok := resolveSubcommandDispatchArgs(args, isInteractiveTerminal(), selectPaasBackupAction)
	if showUsage {
		printUsage(paasBackupUsageText)
		return
	}
	if !ok {
		return
	}
	args = resolved
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "help", "-h", "--help":
		printUsage(paasBackupUsageText)
	case "contract":
		cmdPaasBackupContract(rest)
	case "run":
		cmdPaasBackupRun(rest)
	case "restore":
		cmdPaasBackupRestore(rest)
	case "status":
		cmdPaasBackupStatus(rest)
	default:
		printUnknown("paas backup", sub)
		printUsage(paasBackupUsageText)
		os.Exit(1)
	}
}

func selectPaasBackupAction() (string, bool) {
	return selectSubcommandAction("PaaS backup commands:", paasBackupActions)
}

func cmdPaasBackupContract(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas backup contract", flag.ExitOnError)
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si paas backup contract [--json]")
		return
	}
	rows := []paasBackupContract{
		{
			Profile:     "supabase-self-hosted",
			Description: "WAL-G object-storage backups plus private Databasus metadata service without host web exposure",
			RecommendedAddonPacks: []string{
				"supabase-walg",
				"databasus",
			},
			DefaultRunService:     "supabase-walg-backup",
			DefaultRestoreService: "supabase-walg-restore",
			RequiredEnv: []string{
				"WALG_S3_PREFIX",
				"WALG_AWS_ACCESS_KEY_ID",
				"WALG_AWS_SECRET_ACCESS_KEY",
				"WALG_AWS_ENDPOINT",
			},
		},
	}
	if jsonOut {
		payload := map[string]any{
			"ok":      true,
			"command": "backup contract",
			"context": currentPaasContext(),
			"mode":    "live",
			"count":   len(rows),
			"data":    rows,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		_ = recordPaasAuditEvent("backup contract", "succeeded", "live", map[string]string{
			"count": intString(len(rows)),
		}, nil)
		return
	}
	fmt.Printf("%s %d\n", styleHeading("paas backup contract:"), len(rows))
	for _, row := range rows {
		fmt.Printf("  profile=%s run_service=%s restore_service=%s\n", row.Profile, row.DefaultRunService, row.DefaultRestoreService)
		fmt.Printf("    addons=%s\n", strings.Join(row.RecommendedAddonPacks, ","))
		fmt.Printf("    required_env=%s\n", strings.Join(row.RequiredEnv, ","))
		fmt.Printf("    %s\n", styleDim(row.Description))
	}
	_ = recordPaasAuditEvent("backup contract", "succeeded", "live", map[string]string{
		"count": intString(len(rows)),
	}, nil)
}

func cmdPaasBackupRun(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas backup run", flag.ExitOnError)
	app := fs.String("app", "", "app slug")
	target := fs.String("target", "", "target id (defaults to current target)")
	service := fs.String("service", "supabase-walg-backup", "compose service name")
	releaseID := fs.String("release", "", "release id (defaults to current release)")
	remoteRoot := fs.String("remote-dir", "", "remote release root")
	timeout := fs.String("timeout", "2m", "execution timeout")
	command := fs.String("command", "wal-g backup-push \"${WALG_PGDATA:-/var/lib/postgresql/data}\"", "service shell command")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si paas backup run --app <slug> [--target <id>] [--service <name>] [--release <id>] [--remote-dir <path>] [--timeout <duration>] [--command <shell>] [--json]")
		return
	}
	if !requirePaasValue(*app, "app", "usage: si paas backup run --app <slug> [--target <id>] [--service <name>] [--release <id>] [--remote-dir <path>] [--timeout <duration>] [--command <shell>] [--json]") {
		return
	}
	if !requirePaasValue(*service, "service", "usage: si paas backup run --app <slug> [--target <id>] [--service <name>] [--release <id>] [--remote-dir <path>] [--timeout <duration>] [--command <shell>] [--json]") {
		return
	}
	results := runPaasBackupRemoteAction(paasBackupRemoteActionOptions{
		Operation:  "run",
		App:        strings.TrimSpace(*app),
		Target:     strings.TrimSpace(*target),
		Service:    strings.TrimSpace(*service),
		ReleaseID:  strings.TrimSpace(*releaseID),
		RemoteRoot: strings.TrimSpace(*remoteRoot),
		Timeout:    strings.TrimSpace(*timeout),
		Command:    strings.TrimSpace(*command),
	}, jsonOut)
	printPaasBackupResults("backup run", jsonOut, results)
}

func cmdPaasBackupRestore(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas backup restore", flag.ExitOnError)
	app := fs.String("app", "", "app slug")
	target := fs.String("target", "", "target id (defaults to current target)")
	service := fs.String("service", "supabase-walg-restore", "compose service name")
	releaseID := fs.String("release", "", "release id (defaults to current release)")
	remoteRoot := fs.String("remote-dir", "", "remote release root")
	timeout := fs.String("timeout", "3m", "execution timeout")
	restoreFrom := fs.String("from", "LATEST", "WAL-G backup id to restore")
	force := fs.Bool("force", false, "wipe PGDATA before restore")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si paas backup restore --app <slug> [--target <id>] [--service <name>] [--release <id>] [--remote-dir <path>] [--timeout <duration>] [--from <backup-id>] [--force] [--json]")
		return
	}
	if !requirePaasValue(*app, "app", "usage: si paas backup restore --app <slug> [--target <id>] [--service <name>] [--release <id>] [--remote-dir <path>] [--timeout <duration>] [--from <backup-id>] [--force] [--json]") {
		return
	}
	if !requirePaasValue(*service, "service", "usage: si paas backup restore --app <slug> [--target <id>] [--service <name>] [--release <id>] [--remote-dir <path>] [--timeout <duration>] [--from <backup-id>] [--force] [--json]") {
		return
	}
	restoreScript := strings.Join([]string{
		"set -eu",
		"restore_from=" + quoteSingle(strings.TrimSpace(*restoreFrom)),
		"force_mode=" + quoteSingle(boolString(*force)),
		"pgdata=\"${WALG_PGDATA:-/var/lib/postgresql/data}\"",
		"if [ \"$force_mode\" = \"true\" ]; then rm -rf \"${pgdata:?}\"/*; fi",
		"wal-g backup-fetch \"$pgdata\" \"$restore_from\"",
		"printf \"%s\\n\" \"restore_command = 'wal-g wal-fetch %f %p'\" >> \"$pgdata/postgresql.auto.conf\"",
		": > \"$pgdata/recovery.signal\"",
	}, "; ")
	results := runPaasBackupRemoteAction(paasBackupRemoteActionOptions{
		Operation:  "restore",
		App:        strings.TrimSpace(*app),
		Target:     strings.TrimSpace(*target),
		Service:    strings.TrimSpace(*service),
		ReleaseID:  strings.TrimSpace(*releaseID),
		RemoteRoot: strings.TrimSpace(*remoteRoot),
		Timeout:    strings.TrimSpace(*timeout),
		Command:    restoreScript,
	}, jsonOut)
	printPaasBackupResults("backup restore", jsonOut, results)
}

func cmdPaasBackupStatus(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas backup status", flag.ExitOnError)
	app := fs.String("app", "", "app slug")
	target := fs.String("target", "", "target id (defaults to current target)")
	service := fs.String("service", "supabase-walg-backup", "compose service name")
	releaseID := fs.String("release", "", "release id (defaults to current release)")
	remoteRoot := fs.String("remote-dir", "", "remote release root")
	timeout := fs.String("timeout", "1m", "execution timeout")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si paas backup status --app <slug> [--target <id>] [--service <name>] [--release <id>] [--remote-dir <path>] [--timeout <duration>] [--json]")
		return
	}
	if !requirePaasValue(*app, "app", "usage: si paas backup status --app <slug> [--target <id>] [--service <name>] [--release <id>] [--remote-dir <path>] [--timeout <duration>] [--json]") {
		return
	}
	if !requirePaasValue(*service, "service", "usage: si paas backup status --app <slug> [--target <id>] [--service <name>] [--release <id>] [--remote-dir <path>] [--timeout <duration>] [--json]") {
		return
	}
	results := runPaasBackupRemoteAction(paasBackupRemoteActionOptions{
		Operation:  "status",
		App:        strings.TrimSpace(*app),
		Target:     strings.TrimSpace(*target),
		Service:    strings.TrimSpace(*service),
		ReleaseID:  strings.TrimSpace(*releaseID),
		RemoteRoot: strings.TrimSpace(*remoteRoot),
		Timeout:    strings.TrimSpace(*timeout),
	}, jsonOut)
	printPaasBackupResults("backup status", jsonOut, results)
}

type paasBackupRemoteActionOptions struct {
	Operation  string
	App        string
	Target     string
	Service    string
	ReleaseID  string
	RemoteRoot string
	Timeout    string
	Command    string
}

func runPaasBackupRemoteAction(opts paasBackupRemoteActionOptions, jsonOut bool) []paasBackupResult {
	timeoutValue, err := time.ParseDuration(firstNonEmptyString(opts.Timeout, "2m"))
	if err != nil || timeoutValue <= 0 {
		failPaasCommand("backup "+strings.TrimSpace(opts.Operation), jsonOut, newPaasOperationFailure(
			paasFailureInvalidArgument,
			"flag_validation",
			"",
			"pass a positive --timeout duration, for example --timeout 2m",
			err,
		), map[string]string{"timeout": opts.Timeout})
	}
	selectedTargets := normalizeTargets(opts.Target, "")
	targetRows, err := resolvePaasDeployTargets(selectedTargets)
	if err != nil {
		failPaasCommand("backup "+strings.TrimSpace(opts.Operation), jsonOut, newPaasOperationFailure(
			paasFailureTargetResolution,
			"target_resolve",
			"",
			"verify --target value or set a default target via `si paas target use --target <id>`",
			err,
		), map[string]string{"target": strings.TrimSpace(opts.Target)})
	}
	releaseID, err := resolvePaasBackupReleaseID(opts.App, opts.ReleaseID)
	if err != nil {
		failPaasCommand("backup "+strings.TrimSpace(opts.Operation), jsonOut, newPaasOperationFailure(
			paasFailureRollbackResolve,
			"release_resolve",
			"",
			"deploy app once or pass --release <id>",
			err,
		), map[string]string{"app": opts.App, "release": opts.ReleaseID})
	}
	resolvedRemoteRoot, err := resolvePaasRemoteDirForApp(opts.RemoteRoot, opts.App)
	if err != nil {
		failPaasCommand("backup "+strings.TrimSpace(opts.Operation), jsonOut, newPaasOperationFailure(
			paasFailureInvalidArgument,
			"flag_validation",
			"",
			"pass an absolute --remote-dir path (for example /opt/si/paas/releases)",
			err,
		), map[string]string{"remote_dir": strings.TrimSpace(opts.RemoteRoot)})
	}
	releaseDir := path.Join(resolvedRemoteRoot, sanitizePaasReleasePathSegment(releaseID))
	results := make([]paasBackupResult, 0, len(targetRows))
	for _, target := range targetRows {
		start := time.Now()
		item := paasBackupResult{
			Target:    target.Name,
			App:       opts.App,
			Release:   releaseID,
			Service:   opts.Service,
			Operation: opts.Operation,
			Status:    "failed",
		}
		cmd := buildPaasBackupRemoteCommand(opts, releaseDir)
		item.Command = cmd
		ctx, cancel := context.WithTimeout(context.Background(), timeoutValue)
		out, err := runPaasSSHCommand(ctx, target, cmd)
		cancel()
		if err != nil {
			item.Error = err.Error()
			item.DurationMs = time.Since(start).Milliseconds()
			results = append(results, item)
			continue
		}
		item.Status = "ok"
		item.Output = out
		item.DurationMs = time.Since(start).Milliseconds()
		results = append(results, item)
	}
	return results
}

func buildPaasBackupRemoteCommand(opts paasBackupRemoteActionOptions, releaseDir string) string {
	service := quoteSingle(strings.TrimSpace(opts.Service))
	command := strings.TrimSpace(opts.Command)
	composeSubcommand := ""
	switch strings.ToLower(strings.TrimSpace(opts.Operation)) {
	case "status":
		composeSubcommand = "ps --all " + service
	default:
		composeSubcommand = "run --rm " + service + " sh -lc " + quoteSingle(command)
	}
	inner := strings.Join([]string{
		"set -eu",
		"extra_files=''",
		"for f in compose.addon.*.yaml; do [ -f \"$f\" ] || continue; extra_files=\"$extra_files -f $f\"; done",
		"docker compose -f compose.yaml $extra_files " + composeSubcommand,
	}, "; ")
	return "cd " + quoteSingle(releaseDir) + " && sh -lc " + quoteSingle(inner)
}

func resolvePaasBackupReleaseID(app, assigned string) (string, error) {
	if strings.TrimSpace(assigned) != "" {
		return strings.TrimSpace(assigned), nil
	}
	current, err := resolvePaasCurrentRelease(app)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(current) != "" {
		return strings.TrimSpace(current), nil
	}
	latest, err := resolveLatestPaasReleaseID("", app, "")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(latest) == "" {
		return "", fmt.Errorf("no release history found for app %q", strings.TrimSpace(app))
	}
	return strings.TrimSpace(latest), nil
}

func printPaasBackupResults(command string, jsonOut bool, results []paasBackupResult) {
	failed := 0
	for _, row := range results {
		if row.Status != "ok" {
			failed++
		}
	}
	if jsonOut {
		payload := map[string]any{
			"ok":      failed == 0,
			"command": command,
			"context": currentPaasContext(),
			"mode":    "live",
			"count":   len(results),
			"data":    results,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		status := "succeeded"
		if failed > 0 {
			status = "failed"
		}
		_ = recordPaasAuditEvent(command, status, "live", map[string]string{
			"count": intString(len(results)),
		}, nil)
		if failed > 0 {
			os.Exit(1)
		}
		return
	}
	fmt.Printf("%s %d\n", styleHeading(command+":"), len(results))
	for _, row := range results {
		fmt.Printf("  [%s] target=%s app=%s release=%s service=%s duration_ms=%d\n", row.Status, row.Target, row.App, row.Release, row.Service, row.DurationMs)
		if strings.TrimSpace(row.Error) != "" {
			fmt.Printf("    %s\n", styleDim(row.Error))
		}
		if strings.TrimSpace(row.Output) != "" {
			for _, line := range strings.Split(row.Output, "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				fmt.Printf("    %s\n", line)
			}
		}
	}
	status := "succeeded"
	if failed > 0 {
		status = "failed"
	}
	_ = recordPaasAuditEvent(command, status, "live", map[string]string{
		"count": intString(len(results)),
	}, nil)
	if failed > 0 {
		os.Exit(1)
	}
}
