package main

import (
	"flag"
	"os"
	"strings"
)

var paasAppActions = []subcommandAction{
	{Name: "init", Description: "initialize app metadata"},
	{Name: "list", Description: "list apps"},
	{Name: "status", Description: "show app status"},
	{Name: "remove", Description: "remove app metadata"},
	{Name: "addon", Description: "manage add-on/service-pack lifecycle"},
}

func cmdPaasApp(args []string) {
	resolved, showUsage, ok := resolveSubcommandDispatchArgs(args, isInteractiveTerminal(), selectPaasAppAction)
	if showUsage {
		printUsage(paasAppUsageText)
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
		printUsage(paasAppUsageText)
	case "init":
		cmdPaasAppInit(rest)
	case "list":
		cmdPaasAppList(rest)
	case "status":
		cmdPaasAppStatus(rest)
	case "remove", "rm", "delete":
		cmdPaasAppRemove(rest)
	case "addon":
		cmdPaasAppAddon(rest)
	default:
		printUnknown("paas app", sub)
		printUsage(paasAppUsageText)
		os.Exit(1)
	}
}

func selectPaasAppAction() (string, bool) {
	return selectSubcommandAction("PaaS app commands:", paasAppActions)
}

func cmdPaasAppInit(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas app init", flag.ExitOnError)
	name := fs.String("name", "", "app slug")
	repo := fs.String("repo", "", "source repo")
	composeFile := fs.String("compose-file", "compose.yaml", "compose file path")
	targetGroup := fs.String("target-group", "", "default target group")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si paas app init --name <slug> [--repo <url>] [--compose-file <path>] [--target-group <name>] [--json]")
		return
	}
	if !requirePaasValue(*name, "name", "usage: si paas app init --name <slug> [--repo <url>] [--compose-file <path>] [--target-group <name>] [--json]") {
		return
	}
	printPaasScaffold("app init", map[string]string{
		"compose_file": strings.TrimSpace(*composeFile),
		"name":         strings.TrimSpace(*name),
		"repo":         strings.TrimSpace(*repo),
		"target_group": strings.TrimSpace(*targetGroup),
	}, jsonOut)
}

func cmdPaasAppList(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas app list", flag.ExitOnError)
	all := fs.Bool("all", false, "include archived apps")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si paas app list [--all] [--json]")
		return
	}
	printPaasScaffold("app list", map[string]string{"all": boolString(*all)}, jsonOut)
}

func cmdPaasAppStatus(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas app status", flag.ExitOnError)
	app := fs.String("app", "", "app slug")
	target := fs.String("target", "", "target id")
	watch := fs.Bool("watch", false, "watch status changes")
	_ = fs.Parse(args)
	if fs.NArg() > 1 {
		printUsage("usage: si paas app status [--app <slug>] [--target <id>] [--watch] [--json]")
		return
	}
	selectedApp := strings.TrimSpace(*app)
	if selectedApp == "" && fs.NArg() == 1 {
		selectedApp = strings.TrimSpace(fs.Arg(0))
	}
	printPaasScaffold("app status", map[string]string{
		"app":    selectedApp,
		"target": strings.TrimSpace(*target),
		"watch":  boolString(*watch),
	}, jsonOut)
}

func cmdPaasAppRemove(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas app remove", flag.ExitOnError)
	app := fs.String("app", "", "app slug")
	force := fs.Bool("force", false, "force removal")
	_ = fs.Parse(args)
	if fs.NArg() > 1 {
		printUsage("usage: si paas app remove --app <slug> [--force] [--json]")
		return
	}
	selectedApp := strings.TrimSpace(*app)
	if selectedApp == "" && fs.NArg() == 1 {
		selectedApp = strings.TrimSpace(fs.Arg(0))
	}
	if !requirePaasValue(selectedApp, "app", "usage: si paas app remove --app <slug> [--force] [--json]") {
		return
	}
	printPaasScaffold("app remove", map[string]string{
		"app":   selectedApp,
		"force": boolString(*force),
	}, jsonOut)
}
