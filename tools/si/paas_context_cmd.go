package main

import (
	"flag"
	"os"
	"strings"
)

const (
	paasContextCreateUsageText = "usage: si paas context create --name <name> [--type <internal-dogfood|oss-demo|customer>] [--state-root <path>] [--vault-file <path>] [--json]"
	paasContextListUsageText   = "usage: si paas context list [--json]"
	paasContextUseUsageText    = "usage: si paas context use --name <name> [--json]"
	paasContextShowUsageText   = "usage: si paas context show [--name <name>] [--json]"
	paasContextRemoveUsageText = "usage: si paas context remove --name <name> [--force] [--json]"
)

func cmdPaasContext(args []string) {
	if len(args) == 0 {
		printUsage(paasContextUsageText)
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "help", "-h", "--help":
		printUsage(paasContextUsageText)
	case "create":
		cmdPaasContextCreate(rest)
	case "list":
		cmdPaasContextList(rest)
	case "use":
		cmdPaasContextUse(rest)
	case "show":
		cmdPaasContextShow(rest)
	case "remove", "rm", "delete":
		cmdPaasContextRemove(rest)
	default:
		printUnknown("paas context", sub)
		printUsage(paasContextUsageText)
		os.Exit(1)
	}
}

func cmdPaasContextCreate(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas context create", flag.ExitOnError)
	name := fs.String("name", "", "context name")
	contextType := fs.String("type", "internal-dogfood", "context type")
	stateRoot := fs.String("state-root", "", "context state root path")
	vaultFile := fs.String("vault-file", "", "default vault file")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasContextCreateUsageText)
		return
	}
	if !requirePaasValue(*name, "name", paasContextCreateUsageText) {
		return
	}
	printPaasScaffold("context create", map[string]string{
		"name":       strings.TrimSpace(*name),
		"state_root": strings.TrimSpace(*stateRoot),
		"type":       strings.ToLower(strings.TrimSpace(*contextType)),
		"vault_file": strings.TrimSpace(*vaultFile),
	}, jsonOut)
}

func cmdPaasContextList(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas context list", flag.ExitOnError)
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasContextListUsageText)
		return
	}
	printPaasScaffold("context list", nil, jsonOut)
}

func cmdPaasContextUse(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas context use", flag.ExitOnError)
	name := fs.String("name", "", "context name")
	_ = fs.Parse(args)
	if fs.NArg() > 1 {
		printUsage(paasContextUseUsageText)
		return
	}
	selected := strings.TrimSpace(*name)
	if selected == "" && fs.NArg() == 1 {
		selected = strings.TrimSpace(fs.Arg(0))
	}
	if !requirePaasValue(selected, "name", paasContextUseUsageText) {
		return
	}
	printPaasScaffold("context use", map[string]string{"name": selected}, jsonOut)
}

func cmdPaasContextShow(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas context show", flag.ExitOnError)
	name := fs.String("name", "", "context name")
	_ = fs.Parse(args)
	if fs.NArg() > 1 {
		printUsage(paasContextShowUsageText)
		return
	}
	selected := strings.TrimSpace(*name)
	if selected == "" && fs.NArg() == 1 {
		selected = strings.TrimSpace(fs.Arg(0))
	}
	printPaasScaffold("context show", map[string]string{"name": selected}, jsonOut)
}

func cmdPaasContextRemove(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas context remove", flag.ExitOnError)
	name := fs.String("name", "", "context name")
	force := fs.Bool("force", false, "force removal")
	_ = fs.Parse(args)
	if fs.NArg() > 1 {
		printUsage(paasContextRemoveUsageText)
		return
	}
	selected := strings.TrimSpace(*name)
	if selected == "" && fs.NArg() == 1 {
		selected = strings.TrimSpace(fs.Arg(0))
	}
	if !requirePaasValue(selected, "name", paasContextRemoveUsageText) {
		return
	}
	printPaasScaffold("context remove", map[string]string{
		"force": boolString(*force),
		"name":  selected,
	}, jsonOut)
}
