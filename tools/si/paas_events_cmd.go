package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

const paasEventsListUsageText = "usage: si paas events list [--severity <level>] [--status <state>] [--limit <n>] [--json]"

var paasEventsActions = []subcommandAction{
	{Name: "list", Description: "list recorded events"},
}

func cmdPaasEvents(args []string) {
	resolved, showUsage, ok := resolveSubcommandDispatchArgs(args, isInteractiveTerminal(), selectPaasEventsAction)
	if showUsage {
		printUsage(paasEventsUsageText)
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
		printUsage(paasEventsUsageText)
	case "list":
		cmdPaasEventsList(rest)
	default:
		printUnknown("paas events", sub)
		printUsage(paasEventsUsageText)
		os.Exit(1)
	}
}

func selectPaasEventsAction() (string, bool) {
	return selectSubcommandAction("PaaS events commands:", paasEventsActions)
}

func cmdPaasEventsList(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas events list", flag.ExitOnError)
	severity := fs.String("severity", "", "severity filter")
	status := fs.String("status", "", "status filter")
	limit := fs.Int("limit", 50, "max events")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasEventsListUsageText)
		return
	}
	if *limit < 1 {
		fmt.Fprintln(os.Stderr, "--limit must be >= 1")
		printUsage(paasEventsListUsageText)
		return
	}
	printPaasScaffold("events list", map[string]string{
		"limit":    intString(*limit),
		"severity": strings.ToLower(strings.TrimSpace(*severity)),
		"status":   strings.ToLower(strings.TrimSpace(*status)),
	}, jsonOut)
}
