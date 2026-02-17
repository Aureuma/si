package main

import (
	"flag"
	"os"
	"strings"
)

const (
	paasAlertSetupTelegramUsageText = "usage: si paas alert setup-telegram --bot-token <token> --chat-id <id> [--dry-run] [--json]"
	paasAlertTestUsageText          = "usage: si paas alert test [--severity <info|warning|critical>] [--message <text>] [--json]"
	paasAlertHistoryUsageText       = "usage: si paas alert history [--limit <n>] [--json]"
)

func cmdPaasAlert(args []string) {
	if len(args) == 0 {
		printUsage(paasAlertUsageText)
		return
	}
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
	default:
		printUnknown("paas alert", sub)
		printUsage(paasAlertUsageText)
		os.Exit(1)
	}
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
	printPaasScaffold("alert test", map[string]string{
		"message":  strings.TrimSpace(*message),
		"severity": strings.ToLower(strings.TrimSpace(*severity)),
	}, jsonOut)
}

func cmdPaasAlertHistory(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas alert history", flag.ExitOnError)
	limit := fs.Int("limit", 20, "max rows")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasAlertHistoryUsageText)
		return
	}
	printPaasScaffold("alert history", map[string]string{"limit": intString(*limit)}, jsonOut)
}
