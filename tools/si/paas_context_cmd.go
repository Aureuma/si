package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
)

const (
	paasContextCreateUsageText = "usage: si paas context create --name <name> [--type <internal-dogfood|oss-demo|customer>] [--state-root <path>] [--vault-file <scope>] [--json]"
	paasContextInitUsageText   = "usage: si paas context init [--name <name>] [--type <internal-dogfood|oss-demo|customer>] [--state-root <path>] [--vault-file <scope>] [--json]"
	paasContextListUsageText   = "usage: si paas context list [--json]"
	paasContextUseUsageText    = "usage: si paas context use --name <name> [--json]"
	paasContextShowUsageText   = "usage: si paas context show [--name <name>] [--json]"
	paasContextRemoveUsageText = "usage: si paas context remove --name <name> [--force] [--json]"
	paasContextExportUsageText = "usage: si paas context export --output <path> [--name <name>] [--force] [--json]"
	paasContextImportUsageText = "usage: si paas context import --input <path> [--name <name>] [--replace] [--json]"
)

var paasContextActions = []subcommandAction{
	{Name: "create", Description: "create a context"},
	{Name: "init", Description: "initialize context layout"},
	{Name: "list", Description: "list contexts"},
	{Name: "use", Description: "set active context"},
	{Name: "show", Description: "show context settings"},
	{Name: "remove", Description: "remove a context"},
	{Name: "export", Description: "export non-secret metadata"},
	{Name: "import", Description: "import non-secret metadata"},
}

func cmdPaasContext(args []string) {
	resolved, showUsage, ok := resolveSubcommandDispatchArgs(args, isInteractiveTerminal(), selectPaasContextAction)
	if showUsage {
		printUsage(paasContextUsageText)
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
		printUsage(paasContextUsageText)
	case "create":
		cmdPaasContextCreate(rest)
	case "init":
		cmdPaasContextInit(rest)
	case "list":
		cmdPaasContextList(rest)
	case "use":
		cmdPaasContextUse(rest)
	case "show":
		cmdPaasContextShow(rest)
	case "remove", "rm", "delete":
		cmdPaasContextRemove(rest)
	case "export":
		cmdPaasContextExport(rest)
	case "import":
		cmdPaasContextImport(rest)
	default:
		printUnknown("paas context", sub)
		printUsage(paasContextUsageText)
		os.Exit(1)
	}
}

func selectPaasContextAction() (string, bool) {
	return selectSubcommandAction("PaaS context commands:", paasContextActions)
}

func cmdPaasContextCreate(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas context create", flag.ExitOnError)
	name := fs.String("name", "", "context name")
	contextType := fs.String("type", "internal-dogfood", "context type")
	stateRoot := fs.String("state-root", "", "context state root path")
	vaultFile := fs.String("vault-file", "", "default vault scope (compat alias)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasContextCreateUsageText)
		return
	}
	if !requirePaasValue(*name, "name", paasContextCreateUsageText) {
		return
	}
	config, err := initializePaasContextLayout(strings.TrimSpace(*name), strings.TrimSpace(*contextType), strings.TrimSpace(*stateRoot), strings.TrimSpace(*vaultFile))
	if err != nil {
		failPaasCommand("context create", jsonOut, err, nil)
	}
	printPaasScaffold("context create", map[string]string{
		"name":       config.Name,
		"state_root": config.StateRoot,
		"type":       config.Type,
		"vault_file": config.VaultFile,
		"created_at": config.CreatedAt,
		"updated_at": config.UpdatedAt,
	}, jsonOut)
}

func cmdPaasContextInit(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas context init", flag.ExitOnError)
	name := fs.String("name", "", "context name")
	contextType := fs.String("type", "internal-dogfood", "context type")
	stateRoot := fs.String("state-root", "", "context state root path")
	vaultFile := fs.String("vault-file", "", "default vault scope (compat alias)")
	_ = fs.Parse(args)
	if fs.NArg() > 1 {
		printUsage(paasContextInitUsageText)
		return
	}
	selected := strings.TrimSpace(*name)
	if selected == "" && fs.NArg() == 1 {
		selected = strings.TrimSpace(fs.Arg(0))
	}
	if selected == "" {
		selected = currentPaasContext()
	}
	config, err := initializePaasContextLayout(selected, strings.TrimSpace(*contextType), strings.TrimSpace(*stateRoot), strings.TrimSpace(*vaultFile))
	if err != nil {
		failPaasCommand("context init", jsonOut, err, nil)
	}
	printPaasScaffold("context init", map[string]string{
		"name":       config.Name,
		"state_root": config.StateRoot,
		"type":       config.Type,
		"vault_file": config.VaultFile,
		"created_at": config.CreatedAt,
		"updated_at": config.UpdatedAt,
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
	rows, err := listPaasContextConfigs()
	if err != nil {
		failPaasCommand("context list", jsonOut, err, nil)
	}
	current := ""
	if selected, err := loadPaasSelectedContext(); err == nil {
		current = selected
	}
	if jsonOut {
		payload := map[string]any{
			"ok":      true,
			"command": "context list",
			"context": currentPaasContext(),
			"mode":    "live",
			"current": current,
			"count":   len(rows),
			"data":    rows,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		_ = recordPaasAuditEvent("context list", "succeeded", "live", map[string]string{
			"count": intString(len(rows)),
		}, nil)
		return
	}
	fmt.Printf("%s %d\n", styleHeading("paas context list:"), len(rows))
	for _, row := range rows {
		marker := " "
		if strings.EqualFold(strings.TrimSpace(current), strings.TrimSpace(row.Name)) {
			marker = "*"
		}
		fmt.Printf("  [%s] %s type=%s vault=%s\n", marker, row.Name, row.Type, row.VaultFile)
	}
	_ = recordPaasAuditEvent("context list", "succeeded", "live", map[string]string{
		"count": intString(len(rows)),
	}, nil)
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
	contextDir, err := resolvePaasContextDir(selected)
	if err != nil {
		failPaasCommand("context use", jsonOut, err, nil)
	}
	if _, err := os.Stat(contextDir); err != nil {
		failPaasCommand("context use", jsonOut, err, nil)
	}
	if err := savePaasSelectedContext(selected); err != nil {
		failPaasCommand("context use", jsonOut, err, nil)
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
	if selected == "" {
		if current, err := loadPaasSelectedContext(); err == nil {
			selected = strings.TrimSpace(current)
		}
	}
	if selected == "" {
		selected = currentPaasContext()
	}
	config, err := loadPaasContextConfig(selected)
	if err != nil {
		failPaasCommand("context show", jsonOut, err, nil)
	}
	printPaasScaffold("context show", map[string]string{
		"name":       config.Name,
		"type":       config.Type,
		"state_root": config.StateRoot,
		"vault_file": config.VaultFile,
		"created_at": config.CreatedAt,
		"updated_at": config.UpdatedAt,
	}, jsonOut)
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
	if err := removePaasContextLayout(selected, *force); err != nil {
		failPaasCommand("context remove", jsonOut, err, nil)
	}
	if strings.EqualFold(strings.TrimSpace(currentPaasContext()), strings.TrimSpace(selected)) {
		paasCommandContext = defaultPaasContext
	}
	printPaasScaffold("context remove", map[string]string{
		"force": boolString(*force),
		"name":  selected,
	}, jsonOut)
}
