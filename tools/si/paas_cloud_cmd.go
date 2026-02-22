package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
)

var paasCloudActions = []subcommandAction{
	{Name: "status", Description: "show paas cloud backend configuration"},
	{Name: "use", Description: "set paas cloud backend mode"},
	{Name: "push", Description: "push paas control-plane snapshot to helia"},
	{Name: "pull", Description: "pull paas control-plane snapshot from helia"},
}

func cmdPaasCloud(args []string) {
	resolved, showUsage, ok := resolveSubcommandDispatchArgs(args, isInteractiveTerminal(), selectPaasCloudAction)
	if showUsage {
		printUsage(paasCloudUsageText)
		return
	}
	if !ok {
		return
	}
	if len(resolved) == 0 {
		printUsage(paasCloudUsageText)
		return
	}
	sub := strings.ToLower(strings.TrimSpace(resolved[0]))
	rest := resolved[1:]
	switch sub {
	case "help", "-h", "--help":
		printUsage(paasCloudUsageText)
	case "status":
		cmdPaasCloudStatus(rest)
	case "use", "set":
		cmdPaasCloudUse(rest)
	case "push":
		cmdPaasCloudPush(rest)
	case "pull":
		cmdPaasCloudPull(rest)
	default:
		printUnknown("paas cloud", sub)
		printUsage(paasCloudUsageText)
		os.Exit(1)
	}
}

func selectPaasCloudAction() (string, bool) {
	return selectSubcommandAction("PaaS cloud commands:", paasCloudActions)
}

func cmdPaasCloudStatus(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("paas cloud status", flag.ExitOnError)
	contextName := fs.String("context", currentPaasContext(), "paas context to inspect")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si paas cloud status [--context <name>] [--json]")
		return
	}
	resolution, err := resolvePaasSyncBackend(settings)
	if err != nil {
		failPaasCommand("cloud status", jsonOut, newPaasOperationFailure(
			paasFailureInvalidArgument,
			"backend_resolve",
			"",
			"pass git, helia, or dual for paas sync backend",
			err,
		), nil)
	}
	resolvedContext := resolvePaasContextName(strings.TrimSpace(*contextName))
	objectName := resolvePaasSnapshotObjectName(settings, resolvedContext)
	report := map[string]any{
		"mode":                  resolution.Mode,
		"source":                resolution.Source,
		"context":               resolvedContext,
		"object_name":           objectName,
		"helia_sync_enabled":    resolution.Mode == paasSyncBackendHelia || resolution.Mode == paasSyncBackendDual,
		"helia_sync_strict":     resolution.Mode == paasSyncBackendHelia,
		"helia_auth_configured": strings.TrimSpace(firstNonEmpty(envSunToken(), settings.Helia.Token)) != "",
	}
	if client, clientErr := heliaClientFromSettings(settings); clientErr == nil {
		items, listErr := client.listObjects(heliaContext(settings), heliaPaasControlPlaneSnapshotKind, objectName, 1)
		if listErr != nil {
			report["remote_check_error"] = listErr.Error()
		} else if len(items) > 0 {
			report["remote_exists"] = true
			report["remote_revision"] = items[0].LatestRevision
			report["remote_updated_at"] = items[0].UpdatedAt
		} else {
			report["remote_exists"] = false
		}
	} else {
		report["remote_check_error"] = clientErr.Error()
	}
	if jsonOut {
		printJSON(report)
		return
	}
	fmt.Printf("%s %s\n", styleHeading("mode:"), report["mode"])
	fmt.Printf("%s %s\n", styleHeading("source:"), report["source"])
	fmt.Printf("%s %s\n", styleHeading("context:"), report["context"])
	fmt.Printf("%s %s\n", styleHeading("object_name:"), report["object_name"])
	fmt.Printf("%s %s\n", styleHeading("helia_sync_enabled:"), boolString(report["helia_sync_enabled"].(bool)))
	fmt.Printf("%s %s\n", styleHeading("helia_sync_strict:"), boolString(report["helia_sync_strict"].(bool)))
	fmt.Printf("%s %s\n", styleHeading("helia_auth_configured:"), boolString(report["helia_auth_configured"].(bool)))
	if exists, ok := report["remote_exists"].(bool); ok {
		fmt.Printf("%s %s\n", styleHeading("remote_exists:"), boolString(exists))
		if exists {
			fmt.Printf("%s %v\n", styleHeading("remote_revision:"), report["remote_revision"])
			fmt.Printf("%s %v\n", styleHeading("remote_updated_at:"), report["remote_updated_at"])
		}
	}
	if remoteErr := strings.TrimSpace(fmt.Sprintf("%v", report["remote_check_error"])); remoteErr != "" && remoteErr != "<nil>" {
		fmt.Printf("%s %s\n", styleHeading("remote_check_error:"), remoteErr)
	}
}

func cmdPaasCloudUse(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("paas cloud use", flag.ExitOnError)
	modeFlag := fs.String("mode", "", "paas cloud sync backend mode: git, helia, or dual")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si paas cloud use --mode <git|helia|dual>")
		return
	}
	mode := normalizePaasSyncBackend(*modeFlag)
	if mode == "" {
		fatal(fmt.Errorf("invalid --mode %q (expected git, helia, or dual)", strings.TrimSpace(*modeFlag)))
	}
	settings.Paas.SyncBackend = mode
	if err := saveSettings(settings); err != nil {
		fatal(err)
	}
	successf("paas cloud sync backend set to %s", mode)
	if mode == paasSyncBackendHelia || mode == paasSyncBackendDual {
		token := firstNonEmpty(envSunToken(), strings.TrimSpace(settings.Helia.Token))
		if token == "" {
			warnf("sun token not configured; run `si sun auth login --url <sun-url> --token <token> --account <slug>`")
		}
	}
}

func cmdPaasCloudPush(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas cloud push", flag.ExitOnError)
	contextName := fs.String("context", currentPaasContext(), "paas context to push")
	objectName := fs.String("name", "", "helia object name override")
	revision := fs.String("expected-revision", "", "optional optimistic-lock revision")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si paas cloud push [--context <name>] [--name <object>] [--expected-revision <n>] [--json]")
		return
	}
	var expectedRevision *int64
	if strings.TrimSpace(*revision) != "" {
		value, err := strconv.ParseInt(strings.TrimSpace(*revision), 10, 64)
		if err != nil || value < 0 {
			failPaasCommand("cloud push", jsonOut, newPaasOperationFailure(
				paasFailureInvalidArgument,
				"flag_validation",
				"",
				"pass a non-negative integer for --expected-revision",
				fmt.Errorf("invalid --expected-revision %q", strings.TrimSpace(*revision)),
			), nil)
		}
		expectedRevision = &value
	}
	summary, err := pushPaasControlPlaneSnapshotToHelia(strings.TrimSpace(*contextName), strings.TrimSpace(*objectName), expectedRevision)
	if err != nil {
		failPaasCommand("cloud push", jsonOut, newPaasOperationFailure(
			paasFailureUnknown,
			"cloud_push",
			"",
			"verify helia auth and retry",
			err,
		), nil)
	}
	printPaasScaffold("cloud push", map[string]string{
		"context":          summary.Context,
		"object_name":      summary.ObjectName,
		"revision":         strconv.FormatInt(summary.Revision, 10),
		"targets":          intString(summary.Targets),
		"deploy_apps":      intString(summary.DeployApps),
		"webhook_mappings": intString(summary.WebhookMappings),
		"addon_apps":       intString(summary.AddonApps),
		"bluegreen_apps":   intString(summary.BlueGreenApps),
		"agents":           intString(summary.Agents),
		"approvals":        intString(summary.Approvals),
		"incidents":        intString(summary.Incidents),
	}, jsonOut)
}

func cmdPaasCloudPull(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas cloud pull", flag.ExitOnError)
	contextName := fs.String("context", currentPaasContext(), "paas context to restore")
	objectName := fs.String("name", "", "helia object name override")
	replace := fs.Bool("replace", false, "replace local stores instead of merging")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si paas cloud pull [--context <name>] [--name <object>] [--replace] [--json]")
		return
	}
	summary, err := pullPaasControlPlaneSnapshotFromHelia(strings.TrimSpace(*contextName), strings.TrimSpace(*objectName), *replace)
	if err != nil {
		failPaasCommand("cloud pull", jsonOut, newPaasOperationFailure(
			paasFailureUnknown,
			"cloud_pull",
			"",
			"verify helia auth/object name and retry",
			err,
		), nil)
	}
	printPaasScaffold("cloud pull", map[string]string{
		"context":          summary.Context,
		"object_name":      summary.ObjectName,
		"replace":          boolString(*replace),
		"targets":          intString(summary.Targets),
		"deploy_apps":      intString(summary.DeployApps),
		"webhook_mappings": intString(summary.WebhookMappings),
		"addon_apps":       intString(summary.AddonApps),
		"bluegreen_apps":   intString(summary.BlueGreenApps),
		"agents":           intString(summary.Agents),
		"approvals":        intString(summary.Approvals),
		"incidents":        intString(summary.Incidents),
	}, jsonOut)
}
