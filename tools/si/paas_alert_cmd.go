package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
)

const (
	paasAlertSetupTelegramUsageText = "usage: si paas alert setup-telegram --bot-token <token> --chat-id <id> [--dry-run] [--json]"
	paasAlertTestUsageText          = "usage: si paas alert test [--severity <info|warning|critical>] [--message <text>] [--json]"
	paasAlertHistoryUsageText       = "usage: si paas alert history [--limit <n>] [--severity <info|warning|critical>] [--json]"
)

var paasAlertActions = []subcommandAction{
	{Name: "setup-telegram", Description: "configure telegram notifier"},
	{Name: "test", Description: "send test alert"},
	{Name: "history", Description: "show recent alerts"},
	{Name: "ingress-tls", Description: "inspect Traefik/ACME retry signals and emit alerts"},
}

func cmdPaasAlert(args []string) {
	resolved, showUsage, ok := resolveSubcommandDispatchArgs(args, isInteractiveTerminal(), selectPaasAlertAction)
	if showUsage {
		printUsage(paasAlertUsageText)
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
		printUsage(paasAlertUsageText)
	case "setup-telegram":
		cmdPaasAlertSetupTelegram(rest)
	case "test":
		cmdPaasAlertTest(rest)
	case "history":
		cmdPaasAlertHistory(rest)
	case "ingress-tls":
		cmdPaasAlertIngressTLS(rest)
	default:
		printUnknown("paas alert", sub)
		printUsage(paasAlertUsageText)
		os.Exit(1)
	}
}

func selectPaasAlertAction() (string, bool) {
	return selectSubcommandAction("PaaS alert commands:", paasAlertActions)
}

func cmdPaasAlertSetupTelegram(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas alert setup-telegram", flag.ExitOnError)
	botToken := fs.String("bot-token", "", "telegram bot token")
	chatID := fs.String("chat-id", "", "telegram chat id")
	dryRun := fs.Bool("dry-run", false, "validate config only")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasAlertSetupTelegramUsageText)
		return
	}
	if !requirePaasValue(*botToken, "bot-token", paasAlertSetupTelegramUsageText) {
		return
	}
	if !requirePaasValue(*chatID, "chat-id", paasAlertSetupTelegramUsageText) {
		return
	}
	printPaasScaffold("alert setup-telegram", map[string]string{
		"bot_token_set": boolString(strings.TrimSpace(*botToken) != ""),
		"chat_id":       strings.TrimSpace(*chatID),
		"dry_run":       boolString(*dryRun),
	}, jsonOut)
}

func cmdPaasAlertTest(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas alert test", flag.ExitOnError)
	severity := fs.String("severity", "info", "severity level")
	message := fs.String("message", "si paas alert test", "alert message")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasAlertTestUsageText)
		return
	}
	fields := map[string]string{
		"message":  strings.TrimSpace(*message),
		"severity": strings.ToLower(strings.TrimSpace(*severity)),
	}
	if historyPath := recordPaasAlertEntry(paasAlertEntry{
		Command:  "alert test",
		Severity: fields["severity"],
		Status:   "sent",
		Message:  fields["message"],
		Guidance: "Verify notifier routing and escalation paths.",
	}); strings.TrimSpace(historyPath) != "" {
		fields["alert_history"] = historyPath
	}
	printPaasScaffold("alert test", fields, jsonOut)
}

func cmdPaasAlertHistory(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas alert history", flag.ExitOnError)
	limit := fs.Int("limit", 20, "max rows")
	severity := fs.String("severity", "", "severity filter")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasAlertHistoryUsageText)
		return
	}
	if *limit < 1 {
		fatal(fmt.Errorf("--limit must be >= 1"))
	}
	rows, path, err := loadPaasAlertHistory(*limit, *severity)
	if err != nil {
		fatal(err)
	}
	if jsonOut {
		payload := map[string]any{
			"ok":       true,
			"command":  "alert history",
			"context":  currentPaasContext(),
			"mode":     "live",
			"count":    len(rows),
			"severity": strings.ToLower(strings.TrimSpace(*severity)),
			"path":     path,
			"data":     rows,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	printPaasScaffold("alert history", map[string]string{
		"limit":    intString(*limit),
		"severity": strings.ToLower(strings.TrimSpace(*severity)),
		"count":    intString(len(rows)),
		"path":     path,
	}, false)
	for _, row := range rows {
		fmt.Printf("  - %s [%s] %s target=%s message=%s\n",
			row.Timestamp,
			row.Severity,
			row.Status,
			row.Target,
			row.Message,
		)
	}
}
