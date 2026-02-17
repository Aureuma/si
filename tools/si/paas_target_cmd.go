package main

import (
	"encoding/json"
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
	store, err := loadPaasTargetStore(currentPaasContext())
	if err != nil {
		fatal(err)
	}
	selectedName := strings.TrimSpace(*name)
	if findPaasTarget(store, selectedName) != -1 {
		fatal(fmt.Errorf("target %q already exists", selectedName))
	}
	target := normalizePaasTarget(paasTarget{
		Name:       selectedName,
		Host:       strings.TrimSpace(*host),
		Port:       *port,
		User:       strings.TrimSpace(*user),
		AuthMethod: strings.ToLower(strings.TrimSpace(*authMethod)),
		Labels:     parseCSV(*labels),
		CreatedAt:  utcNowRFC3339(),
		UpdatedAt:  utcNowRFC3339(),
	})
	store.Targets = append(store.Targets, target)
	if *setDefault || strings.TrimSpace(store.CurrentTarget) == "" {
		store.CurrentTarget = target.Name
	}
	if err := savePaasTargetStore(currentPaasContext(), store); err != nil {
		fatal(err)
	}
	printPaasScaffold("target add", map[string]string{
		"auth_method": target.AuthMethod,
		"default":     boolString(store.CurrentTarget == target.Name),
		"host":        target.Host,
		"labels":      strings.Join(target.Labels, ","),
		"name":        target.Name,
		"port":        intString(target.Port),
		"total":       intString(len(store.Targets)),
		"user":        target.User,
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
	store, err := loadPaasTargetStore(currentPaasContext())
	if err != nil {
		fatal(err)
	}
	if jsonOut {
		payload := map[string]any{
			"ok":             true,
			"command":        "target list",
			"context":        currentPaasContext(),
			"mode":           "live",
			"all":            *all,
			"current_target": store.CurrentTarget,
			"count":          len(store.Targets),
			"data":           store.Targets,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	fmt.Printf("%s %d\n", styleHeading("paas targets:"), len(store.Targets))
	if len(store.Targets) == 0 {
		fmt.Println(styleDim("  no targets configured"))
		return
	}
	for _, row := range store.Targets {
		marker := " "
		if strings.EqualFold(strings.TrimSpace(row.Name), strings.TrimSpace(store.CurrentTarget)) {
			marker = "*"
		}
		fmt.Printf("  %s %s %s@%s:%d\n", marker, row.Name, row.User, row.Host, row.Port)
	}
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
	} else {
		store, err := loadPaasTargetStore(currentPaasContext())
		if err != nil {
			fatal(err)
		}
		if findPaasTarget(store, selected) == -1 {
			fatal(fmt.Errorf("target %q not found", selected))
		}
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
	store, err := loadPaasTargetStore(currentPaasContext())
	if err != nil {
		fatal(err)
	}
	idx := findPaasTarget(store, selected)
	if idx == -1 {
		fatal(fmt.Errorf("target %q not found", selected))
	}
	store.CurrentTarget = store.Targets[idx].Name
	if err := savePaasTargetStore(currentPaasContext(), store); err != nil {
		fatal(err)
	}
	printPaasScaffold("target use", map[string]string{"target": store.CurrentTarget}, jsonOut)
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
	store, err := loadPaasTargetStore(currentPaasContext())
	if err != nil {
		fatal(err)
	}
	idx := findPaasTarget(store, selected)
	if idx == -1 {
		fatal(fmt.Errorf("target %q not found", selected))
	}
	normalizedCurrent := strings.TrimSpace(store.CurrentTarget)
	if !*force && normalizedCurrent != "" && strings.EqualFold(normalizedCurrent, strings.TrimSpace(selected)) {
		fatal(fmt.Errorf("cannot remove current target %q without --force", store.CurrentTarget))
	}
	removed := store.Targets[idx].Name
	store.Targets = append(store.Targets[:idx], store.Targets[idx+1:]...)
	if strings.EqualFold(strings.TrimSpace(store.CurrentTarget), strings.TrimSpace(removed)) {
		store.CurrentTarget = ""
		if len(store.Targets) > 0 {
			store.CurrentTarget = store.Targets[0].Name
		}
	}
	if err := savePaasTargetStore(currentPaasContext(), store); err != nil {
		fatal(err)
	}
	printPaasScaffold("target remove", map[string]string{
		"force":  boolString(*force),
		"target": removed,
		"total":  intString(len(store.Targets)),
	}, jsonOut)
}
