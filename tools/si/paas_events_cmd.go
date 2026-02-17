package main

import (
	"encoding/json"
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
	rows, paths, err := loadPaasEventRecords(*limit, *severity, *status)
	if err != nil {
		failPaasCommand("events list", jsonOut, newPaasOperationFailure(
			paasFailureUnknown,
			"event_query",
			"",
			"verify context state permissions and event file integrity, then retry",
			err,
		), nil)
	}
	filterSeverity := strings.ToLower(strings.TrimSpace(*severity))
	filterStatus := strings.ToLower(strings.TrimSpace(*status))
	pathValue := strings.Join(paths, ",")
	if jsonOut {
		payload := map[string]any{
			"ok":       true,
			"command":  "events list",
			"context":  currentPaasContext(),
			"mode":     "live",
			"limit":    *limit,
			"severity": filterSeverity,
			"status":   filterStatus,
			"count":    len(rows),
			"paths":    paths,
			"data":     rows,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		_ = recordPaasAuditEvent("events list", "succeeded", "live", map[string]string{
			"count":    intString(len(rows)),
			"limit":    intString(*limit),
			"severity": filterSeverity,
			"status":   filterStatus,
		}, nil)
		return
	}
	printPaasScaffold("events list", map[string]string{
		"count":    intString(len(rows)),
		"limit":    intString(*limit),
		"paths":    pathValue,
		"severity": filterSeverity,
		"status":   filterStatus,
	}, false)
	for _, row := range rows {
		fmt.Printf("  - %s [%s] %s source=%s target=%s message=%s\n",
			row.Timestamp,
			row.Severity,
			row.Status,
			row.Source,
			row.Target,
			row.Message,
		)
	}
}
