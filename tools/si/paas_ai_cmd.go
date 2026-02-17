package main

import (
	"flag"
	"os"
	"strings"
)

const (
	paasAIPlanUsageText    = "usage: si paas ai plan [--app <slug>] [--target <id>] [--profile <codex_profile>] [--prompt <text>] [--dry-run] [--json]"
	paasAIInspectUsageText = "usage: si paas ai inspect [--app <slug>] [--target <id>] [--incident <id>] [--profile <codex_profile>] [--prompt <text>] [--dry-run] [--json]"
	paasAIFixUsageText     = "usage: si paas ai fix [--app <slug>] [--target <id>] [--incident <id>] [--profile <codex_profile>] [--prompt <text>] [--dry-run] [--json]"
)

var paasAIActions = []subcommandAction{
	{Name: "plan", Description: "produce remediation plan"},
	{Name: "inspect", Description: "inspect incident context"},
	{Name: "fix", Description: "propose guarded fix"},
}

func cmdPaasAI(args []string) {
	resolved, showUsage, ok := resolveSubcommandDispatchArgs(args, isInteractiveTerminal(), selectPaasAIAction)
	if showUsage {
		printUsage(paasAIUsageText)
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
		printUsage(paasAIUsageText)
	case "plan":
		cmdPaasAIPlan(rest)
	case "inspect":
		cmdPaasAIInspect(rest)
	case "fix":
		cmdPaasAIFix(rest)
	default:
		printUnknown("paas ai", sub)
		printUsage(paasAIUsageText)
		os.Exit(1)
	}
}

func selectPaasAIAction() (string, bool) {
	return selectSubcommandAction("PaaS AI commands:", paasAIActions)
}

func cmdPaasAIPlan(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas ai plan", flag.ExitOnError)
	app := fs.String("app", "", "app slug")
	target := fs.String("target", "", "target id")
	profile := fs.String("profile", "", "codex profile")
	prompt := fs.String("prompt", "", "extra instructions")
	dryRun := fs.Bool("dry-run", false, "simulate only")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasAIPlanUsageText)
		return
	}
	printPaasScaffold("ai plan", map[string]string{
		"app":     strings.TrimSpace(*app),
		"dry_run": boolString(*dryRun),
		"profile": strings.TrimSpace(*profile),
		"prompt":  strings.TrimSpace(*prompt),
		"target":  strings.TrimSpace(*target),
	}, jsonOut)
}

func cmdPaasAIInspect(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas ai inspect", flag.ExitOnError)
	app := fs.String("app", "", "app slug")
	target := fs.String("target", "", "target id")
	incident := fs.String("incident", "", "incident id")
	profile := fs.String("profile", "", "codex profile")
	prompt := fs.String("prompt", "", "extra instructions")
	dryRun := fs.Bool("dry-run", false, "simulate only")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasAIInspectUsageText)
		return
	}
	printPaasScaffold("ai inspect", map[string]string{
		"app":      strings.TrimSpace(*app),
		"dry_run":  boolString(*dryRun),
		"incident": strings.TrimSpace(*incident),
		"profile":  strings.TrimSpace(*profile),
		"prompt":   strings.TrimSpace(*prompt),
		"target":   strings.TrimSpace(*target),
	}, jsonOut)
}

func cmdPaasAIFix(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas ai fix", flag.ExitOnError)
	app := fs.String("app", "", "app slug")
	target := fs.String("target", "", "target id")
	incident := fs.String("incident", "", "incident id")
	profile := fs.String("profile", "", "codex profile")
	prompt := fs.String("prompt", "", "extra instructions")
	dryRun := fs.Bool("dry-run", false, "simulate only")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasAIFixUsageText)
		return
	}
	printPaasScaffold("ai fix", map[string]string{
		"app":      strings.TrimSpace(*app),
		"dry_run":  boolString(*dryRun),
		"incident": strings.TrimSpace(*incident),
		"profile":  strings.TrimSpace(*profile),
		"prompt":   strings.TrimSpace(*prompt),
		"target":   strings.TrimSpace(*target),
	}, jsonOut)
}
