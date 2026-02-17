package main

import (
	"flag"
	"os"
	"strings"
)

const (
	paasAgentEnableUsageText  = "usage: si paas agent enable --name <agent> [--targets <id|all>] [--profile <codex_profile>] [--json]"
	paasAgentDisableUsageText = "usage: si paas agent disable --name <agent> [--json]"
	paasAgentStatusUsageText  = "usage: si paas agent status [--name <agent>] [--json]"
	paasAgentLogsUsageText    = "usage: si paas agent logs --name <agent> [--tail <n>] [--follow] [--json]"
	paasAgentRunOnceUsageText = "usage: si paas agent run-once --name <agent> [--incident <id>] [--json]"
	paasAgentApproveUsageText = "usage: si paas agent approve --run <id> [--json]"
	paasAgentDenyUsageText    = "usage: si paas agent deny --run <id> [--json]"
)

func cmdPaasAgent(args []string) {
	if len(args) == 0 {
		printUsage(paasAgentUsageText)
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "help", "-h", "--help":
		printUsage(paasAgentUsageText)
	case "enable":
		cmdPaasAgentEnable(rest)
	case "disable":
		cmdPaasAgentDisable(rest)
	case "status":
		cmdPaasAgentStatus(rest)
	case "logs":
		cmdPaasAgentLogs(rest)
	case "run-once", "runonce":
		cmdPaasAgentRunOnce(rest)
	case "approve":
		cmdPaasAgentApprove(rest)
	case "deny":
		cmdPaasAgentDeny(rest)
	default:
		printUnknown("paas agent", sub)
		printUsage(paasAgentUsageText)
		os.Exit(1)
	}
}

func cmdPaasAgentEnable(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas agent enable", flag.ExitOnError)
	name := fs.String("name", "", "agent name")
	targets := fs.String("targets", "all", "target selection (id,csv,all)")
	profile := fs.String("profile", "", "codex profile")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasAgentEnableUsageText)
		return
	}
	if !requirePaasValue(*name, "name", paasAgentEnableUsageText) {
		return
	}
	printPaasScaffold("agent enable", map[string]string{
		"name":    strings.TrimSpace(*name),
		"profile": strings.TrimSpace(*profile),
		"targets": formatTargets(normalizeTargets("", *targets)),
	}, jsonOut)
}

func cmdPaasAgentDisable(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas agent disable", flag.ExitOnError)
	name := fs.String("name", "", "agent name")
	_ = fs.Parse(args)
	if fs.NArg() > 1 {
		printUsage(paasAgentDisableUsageText)
		return
	}
	selected := strings.TrimSpace(*name)
	if selected == "" && fs.NArg() == 1 {
		selected = strings.TrimSpace(fs.Arg(0))
	}
	if !requirePaasValue(selected, "name", paasAgentDisableUsageText) {
		return
	}
	printPaasScaffold("agent disable", map[string]string{"name": selected}, jsonOut)
}

func cmdPaasAgentStatus(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas agent status", flag.ExitOnError)
	name := fs.String("name", "", "agent name")
	_ = fs.Parse(args)
	if fs.NArg() > 1 {
		printUsage(paasAgentStatusUsageText)
		return
	}
	selected := strings.TrimSpace(*name)
	if selected == "" && fs.NArg() == 1 {
		selected = strings.TrimSpace(fs.Arg(0))
	}
	printPaasScaffold("agent status", map[string]string{"name": selected}, jsonOut)
}

func cmdPaasAgentLogs(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas agent logs", flag.ExitOnError)
	name := fs.String("name", "", "agent name")
	tail := fs.Int("tail", 200, "number of log lines")
	follow := fs.Bool("follow", false, "follow logs")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasAgentLogsUsageText)
		return
	}
	if !requirePaasValue(*name, "name", paasAgentLogsUsageText) {
		return
	}
	printPaasScaffold("agent logs", map[string]string{
		"follow": boolString(*follow),
		"name":   strings.TrimSpace(*name),
		"tail":   intString(*tail),
	}, jsonOut)
}

func cmdPaasAgentRunOnce(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas agent run-once", flag.ExitOnError)
	name := fs.String("name", "", "agent name")
	incident := fs.String("incident", "", "incident id")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasAgentRunOnceUsageText)
		return
	}
	if !requirePaasValue(*name, "name", paasAgentRunOnceUsageText) {
		return
	}
	printPaasScaffold("agent run-once", map[string]string{
		"incident": strings.TrimSpace(*incident),
		"name":     strings.TrimSpace(*name),
	}, jsonOut)
}

func cmdPaasAgentApprove(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas agent approve", flag.ExitOnError)
	runID := fs.String("run", "", "agent run identifier")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasAgentApproveUsageText)
		return
	}
	if !requirePaasValue(*runID, "run", paasAgentApproveUsageText) {
		return
	}
	printPaasScaffold("agent approve", map[string]string{"run": strings.TrimSpace(*runID)}, jsonOut)
}

func cmdPaasAgentDeny(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas agent deny", flag.ExitOnError)
	runID := fs.String("run", "", "agent run identifier")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasAgentDenyUsageText)
		return
	}
	if !requirePaasValue(*runID, "run", paasAgentDenyUsageText) {
		return
	}
	printPaasScaffold("agent deny", map[string]string{"run": strings.TrimSpace(*runID)}, jsonOut)
}
