package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

var paasTargetActions = []subcommandAction{
	{Name: "add", Description: "add a target node"},
	{Name: "list", Description: "list configured targets"},
	{Name: "check", Description: "run target preflight checks"},
	{Name: "use", Description: "set default target"},
	{Name: "remove", Description: "remove a target"},
}

func cmdPaasTarget(args []string) {
	resolved, showUsage, ok := resolveSubcommandDispatchArgs(args, isInteractiveTerminal(), selectPaasTargetAction)
	if showUsage {
		printUsage(paasTargetUsageText)
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
		printUsage(paasTargetUsageText)
	case "add":
		cmdPaasTargetAdd(rest)
	case "list":
		cmdPaasTargetList(rest)
	case "check":
		cmdPaasTargetCheck(rest)
	case "use":
		cmdPaasTargetUse(rest)
	case "remove", "rm", "delete":
		cmdPaasTargetRemove(rest)
	default:
		printUnknown("paas target", sub)
		printUsage(paasTargetUsageText)
		os.Exit(1)
	}
}

func selectPaasTargetAction() (string, bool) {
	return selectSubcommandAction("PaaS target commands:", paasTargetActions)
}

func cmdPaasTargetAdd(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas target add", flag.ExitOnError)
	name := fs.String("name", "", "target identifier")
	host := fs.String("host", "", "target host or IP")
	port := fs.Int("port", 22, "ssh port")
	user := fs.String("user", "", "ssh username")
	authMethod := fs.String("auth-method", "key", "ssh auth method (key|password)")
	labels := fs.String("labels", "", "comma-separated labels")
	setDefault := fs.Bool("default", false, "set target as default")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si paas target add --name <id> --host <host> --user <user> [--port <n>] [--auth-method <key|password>] [--labels <k:v,...>] [--default] [--json]")
		return
	}
	if !requirePaasValue(*name, "name", "usage: si paas target add --name <id> --host <host> --user <user> [--port <n>] [--auth-method <key|password>] [--labels <k:v,...>] [--default] [--json]") {
		return
	}
	if !requirePaasValue(*host, "host", "usage: si paas target add --name <id> --host <host> --user <user> [--port <n>] [--auth-method <key|password>] [--labels <k:v,...>] [--default] [--json]") {
		return
	}
	if !requirePaasValue(*user, "user", "usage: si paas target add --name <id> --host <host> --user <user> [--port <n>] [--auth-method <key|password>] [--labels <k:v,...>] [--default] [--json]") {
		return
	}
	printPaasScaffold("target add", map[string]string{
		"auth_method": strings.ToLower(strings.TrimSpace(*authMethod)),
		"default":     boolString(*setDefault),
		"host":        strings.TrimSpace(*host),
		"labels":      strings.Join(parseCSV(*labels), ","),
		"name":        strings.TrimSpace(*name),
		"port":        intString(*port),
		"user":        strings.TrimSpace(*user),
	}, jsonOut)
}

func cmdPaasTargetList(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas target list", flag.ExitOnError)
	all := fs.Bool("all", false, "include archived targets")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si paas target list [--all] [--json]")
		return
	}
	printPaasScaffold("target list", map[string]string{"all": boolString(*all)}, jsonOut)
}

func cmdPaasTargetCheck(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas target check", flag.ExitOnError)
	target := fs.String("target", "", "target id")
	checkAll := fs.Bool("all", false, "check all targets")
	timeout := fs.String("timeout", "30s", "preflight timeout")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si paas target check [--target <id> | --all] [--timeout <duration>] [--json]")
		return
	}
	selected := strings.TrimSpace(*target)
	if !*checkAll && selected == "" {
		fmt.Fprintln(os.Stderr, "either --target or --all is required")
		printUsage("usage: si paas target check [--target <id> | --all] [--timeout <duration>] [--json]")
		return
	}
	if *checkAll {
		selected = "all"
	}
	printPaasScaffold("target check", map[string]string{
		"target":  selected,
		"timeout": strings.TrimSpace(*timeout),
	}, jsonOut)
}

func cmdPaasTargetUse(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas target use", flag.ExitOnError)
	target := fs.String("target", "", "target id")
	_ = fs.Parse(args)
	if fs.NArg() > 1 {
		printUsage("usage: si paas target use --target <id> [--json]")
		return
	}
	selected := strings.TrimSpace(*target)
	if selected == "" && fs.NArg() == 1 {
		selected = strings.TrimSpace(fs.Arg(0))
	}
	if !requirePaasValue(selected, "target", "usage: si paas target use --target <id> [--json]") {
		return
	}
	printPaasScaffold("target use", map[string]string{"target": selected}, jsonOut)
}

func cmdPaasTargetRemove(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas target remove", flag.ExitOnError)
	target := fs.String("target", "", "target id")
	force := fs.Bool("force", false, "force removal even if referenced")
	_ = fs.Parse(args)
	if fs.NArg() > 1 {
		printUsage("usage: si paas target remove --target <id> [--force] [--json]")
		return
	}
	selected := strings.TrimSpace(*target)
	if selected == "" && fs.NArg() == 1 {
		selected = strings.TrimSpace(fs.Arg(0))
	}
	if !requirePaasValue(selected, "target", "usage: si paas target remove --target <id> [--force] [--json]") {
		return
	}
	printPaasScaffold("target remove", map[string]string{
		"force":  boolString(*force),
		"target": selected,
	}, jsonOut)
}
