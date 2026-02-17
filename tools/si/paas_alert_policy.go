package main

import (
	"encoding/json"
	"flag"
	"os"
	"strings"
)

const (
	paasAlertPolicyUsageText    = "usage: si paas alert policy <show|set> [args...]"
	paasAlertPolicyShowUsage    = "usage: si paas alert policy show [--json]"
	paasAlertPolicySetUsageText = "usage: si paas alert policy set [--default <telegram|disabled>] [--info <telegram|disabled>] [--warning <telegram|disabled>] [--critical <telegram|disabled>] [--json]"
)

func cmdPaasAlertPolicy(args []string) {
	resolved, showUsage, ok := resolveSubcommandDispatchArgs(args, isInteractiveTerminal(), func() (string, bool) {
		return selectSubcommandAction("PaaS alert policy commands:", []subcommandAction{
			{Name: "show", Description: "show alert routing policy"},
			{Name: "set", Description: "set alert routing policy"},
		})
	})
	if showUsage {
		printUsage(paasAlertPolicyUsageText)
		return
	}
	if !ok {
		return
	}
	sub := strings.ToLower(strings.TrimSpace(resolved[0]))
	rest := resolved[1:]
	switch sub {
	case "help", "-h", "--help":
		printUsage(paasAlertPolicyUsageText)
	case "show":
		cmdPaasAlertPolicyShow(rest)
	case "set":
		cmdPaasAlertPolicySet(rest)
	default:
		printUnknown("paas alert policy", sub)
		printUsage(paasAlertPolicyUsageText)
	}
}

func cmdPaasAlertPolicyShow(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas alert policy show", flag.ExitOnError)
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasAlertPolicyShowUsage)
		return
	}
	policy, path, err := loadPaasAlertRoutingPolicy(currentPaasContext())
	if err != nil {
		failPaasCommand("alert policy show", jsonOut, err, nil)
	}
	if jsonOut {
		payload := map[string]any{
			"ok":       true,
			"command":  "alert policy show",
			"context":  currentPaasContext(),
			"mode":     "live",
			"path":     path,
			"data":     policy,
			"channels": []string{"telegram", "disabled"},
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		_ = recordPaasAuditEvent("alert policy show", "succeeded", "live", map[string]string{
			"path":            path,
			"default_channel": policy.DefaultChannel,
			"info":            policy.Severity["info"],
			"warning":         policy.Severity["warning"],
			"critical":        policy.Severity["critical"],
		}, nil)
		return
	}
	printPaasScaffold("alert policy show", map[string]string{
		"default_channel": policy.DefaultChannel,
		"info":            policy.Severity["info"],
		"warning":         policy.Severity["warning"],
		"critical":        policy.Severity["critical"],
		"path":            path,
	}, false)
}

func cmdPaasAlertPolicySet(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas alert policy set", flag.ExitOnError)
	defaultChannel := fs.String("default", "", "default channel")
	info := fs.String("info", "", "info severity channel")
	warning := fs.String("warning", "", "warning severity channel")
	critical := fs.String("critical", "", "critical severity channel")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasAlertPolicySetUsageText)
		return
	}
	current, _, err := loadPaasAlertRoutingPolicy(currentPaasContext())
	if err != nil {
		failPaasCommand("alert policy set", jsonOut, err, nil)
	}
	updated := current
	if updated.Severity == nil {
		updated.Severity = map[string]string{}
	}
	if value := strings.TrimSpace(*defaultChannel); value != "" {
		updated.DefaultChannel = value
	}
	if value := strings.TrimSpace(*info); value != "" {
		updated.Severity["info"] = value
	}
	if value := strings.TrimSpace(*warning); value != "" {
		updated.Severity["warning"] = value
	}
	if value := strings.TrimSpace(*critical); value != "" {
		updated.Severity["critical"] = value
	}
	path, err := savePaasAlertRoutingPolicy(currentPaasContext(), updated)
	if err != nil {
		failPaasCommand("alert policy set", jsonOut, err, nil)
	}
	policy, _, err := loadPaasAlertRoutingPolicy(currentPaasContext())
	if err != nil {
		failPaasCommand("alert policy set", jsonOut, err, nil)
	}
	printPaasScaffold("alert policy set", map[string]string{
		"default_channel": policy.DefaultChannel,
		"info":            policy.Severity["info"],
		"warning":         policy.Severity["warning"],
		"critical":        policy.Severity["critical"],
		"path":            path,
	}, jsonOut)
}

func resolvePaasAlertRoute(severity string) (string, string, error) {
	policy, path, err := loadPaasAlertRoutingPolicy(currentPaasContext())
	if err != nil {
		return "", "", err
	}
	level := strings.ToLower(strings.TrimSpace(severity))
	channel := strings.TrimSpace(policy.DefaultChannel)
	if mapped := strings.TrimSpace(policy.Severity[level]); mapped != "" {
		channel = mapped
	}
	if channel == "" {
		channel = "telegram"
	}
	return channel, path, nil
}
