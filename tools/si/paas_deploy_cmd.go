package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
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
	waitTimeout := fs.String("wait-timeout", "5m", "deployment wait timeout")
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
	printPaasScaffold("deploy", map[string]string{
		"app":               strings.TrimSpace(*app),
		"compose_file":      strings.TrimSpace(*composeFile),
		"continue_on_error": boolString(*continueOnError),
		"max_parallel":      intString(*maxParallel),
		"release":           strings.TrimSpace(*release),
		"strategy":          resolvedStrategy,
		"targets":           formatTargets(resolvedTargets),
		"wait_timeout":      strings.TrimSpace(*waitTimeout),
	}, jsonOut)
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
	printPaasScaffold("rollback", map[string]string{
		"app":               strings.TrimSpace(*app),
		"continue_on_error": boolString(*continueOnError),
		"max_parallel":      intString(*maxParallel),
		"strategy":          resolvedStrategy,
		"targets":           formatTargets(resolvedTargets),
		"to_release":        strings.TrimSpace(*toRelease),
		"wait_timeout":      strings.TrimSpace(*waitTimeout),
	}, jsonOut)
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
