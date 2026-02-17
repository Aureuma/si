package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	paasAgentEnableUsageText  = "usage: si paas agent enable --name <agent> [--targets <id|all>] [--profile <codex_profile>] [--json]"
	paasAgentDisableUsageText = "usage: si paas agent disable --name <agent> [--json]"
	paasAgentStatusUsageText  = "usage: si paas agent status [--name <agent>] [--json]"
	paasAgentLogsUsageText    = "usage: si paas agent logs --name <agent> [--tail <n>] [--follow] [--json]"
	paasAgentRunOnceUsageText = "usage: si paas agent run-once --name <agent> [--incident <id>] [--json]"
	paasAgentApproveUsageText = "usage: si paas agent approve --run <id> [--json]"
	paasAgentDenyUsageText    = "usage: si paas agent deny --run <id> [--json]"
)

var paasAgentActions = []subcommandAction{
	{Name: "enable", Description: "enable agent worker"},
	{Name: "disable", Description: "disable agent worker"},
	{Name: "status", Description: "show agent status"},
	{Name: "logs", Description: "show agent logs"},
	{Name: "run-once", Description: "run a single agent cycle"},
	{Name: "approve", Description: "approve pending run"},
	{Name: "deny", Description: "deny pending run"},
}

func cmdPaasAgent(args []string) {
	resolved, showUsage, ok := resolveSubcommandDispatchArgs(args, isInteractiveTerminal(), selectPaasAgentAction)
	if showUsage {
		printUsage(paasAgentUsageText)
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
		printUsage(paasAgentUsageText)
	case "enable":
		cmdPaasAgentEnable(rest)
	case "disable":
		cmdPaasAgentDisable(rest)
	case "status":
		cmdPaasAgentStatus(rest)
	case "logs":
		cmdPaasAgentLogs(rest)
	case "run-once", "runonce":
		cmdPaasAgentRunOnce(rest)
	case "approve":
		cmdPaasAgentApprove(rest)
	case "deny":
		cmdPaasAgentDeny(rest)
	default:
		printUnknown("paas agent", sub)
		printUsage(paasAgentUsageText)
		os.Exit(1)
	}
}

func selectPaasAgentAction() (string, bool) {
	return selectSubcommandAction("PaaS agent commands:", paasAgentActions)
}

func cmdPaasAgentEnable(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas agent enable", flag.ExitOnError)
	name := fs.String("name", "", "agent name")
	targets := fs.String("targets", "all", "target selection (id,csv,all)")
	profile := fs.String("profile", "", "codex profile")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasAgentEnableUsageText)
		return
	}
	if !requirePaasValue(*name, "name", paasAgentEnableUsageText) {
		return
	}
	store, _, err := loadPaasAgentStore(currentPaasContext())
	if err != nil {
		failPaasCommand("agent enable", jsonOut, err, nil)
	}
	selectedName := strings.TrimSpace(*name)
	selectedTargets := normalizeTargets("", *targets)
	if len(selectedTargets) == 0 {
		selectedTargets = []string{"all"}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	index := findPaasAgentIndex(store, selectedName)
	if index < 0 {
		store.Agents = append(store.Agents, paasAgentConfig{
			Name:      selectedName,
			Enabled:   true,
			Targets:   selectedTargets,
			Profile:   strings.TrimSpace(*profile),
			CreatedAt: now,
			UpdatedAt: now,
		})
		index = len(store.Agents) - 1
	} else {
		store.Agents[index].Enabled = true
		store.Agents[index].Targets = selectedTargets
		store.Agents[index].Profile = strings.TrimSpace(*profile)
		if strings.TrimSpace(store.Agents[index].CreatedAt) == "" {
			store.Agents[index].CreatedAt = now
		}
		store.Agents[index].UpdatedAt = now
	}
	path, err := savePaasAgentStore(currentPaasContext(), store)
	if err != nil {
		failPaasCommand("agent enable", jsonOut, err, nil)
	}
	row := store.Agents[index]
	printPaasAgentEnvelope("agent enable", row, path, jsonOut)
}

func cmdPaasAgentDisable(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas agent disable", flag.ExitOnError)
	name := fs.String("name", "", "agent name")
	_ = fs.Parse(args)
	if fs.NArg() > 1 {
		printUsage(paasAgentDisableUsageText)
		return
	}
	selected := strings.TrimSpace(*name)
	if selected == "" && fs.NArg() == 1 {
		selected = strings.TrimSpace(fs.Arg(0))
	}
	if !requirePaasValue(selected, "name", paasAgentDisableUsageText) {
		return
	}
	store, _, err := loadPaasAgentStore(currentPaasContext())
	if err != nil {
		failPaasCommand("agent disable", jsonOut, err, nil)
	}
	index := findPaasAgentIndex(store, selected)
	if index < 0 {
		failPaasCommand("agent disable", jsonOut, fmt.Errorf("agent %q was not found", selected), nil)
	}
	store.Agents[index].Enabled = false
	store.Agents[index].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	path, err := savePaasAgentStore(currentPaasContext(), store)
	if err != nil {
		failPaasCommand("agent disable", jsonOut, err, nil)
	}
	printPaasAgentEnvelope("agent disable", store.Agents[index], path, jsonOut)
}

func cmdPaasAgentStatus(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas agent status", flag.ExitOnError)
	name := fs.String("name", "", "agent name")
	_ = fs.Parse(args)
	if fs.NArg() > 1 {
		printUsage(paasAgentStatusUsageText)
		return
	}
	selected := strings.TrimSpace(*name)
	if selected == "" && fs.NArg() == 1 {
		selected = strings.TrimSpace(fs.Arg(0))
	}
	store, path, err := loadPaasAgentStore(currentPaasContext())
	if err != nil {
		failPaasCommand("agent status", jsonOut, err, nil)
	}
	rows := store.Agents
	if selected != "" {
		index := findPaasAgentIndex(store, selected)
		if index < 0 {
			rows = []paasAgentConfig{}
		} else {
			rows = []paasAgentConfig{store.Agents[index]}
		}
	}
	if jsonOut {
		payload := map[string]any{
			"ok":      true,
			"command": "agent status",
			"context": currentPaasContext(),
			"mode":    "live",
			"count":   len(rows),
			"path":    path,
			"data":    rows,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		_ = recordPaasAuditEvent("agent status", "succeeded", "live", map[string]string{
			"count": intString(len(rows)),
			"path":  path,
		}, nil)
		return
	}
	fmt.Printf("%s %d\n", styleHeading("paas agent status:"), len(rows))
	for _, row := range rows {
		fmt.Printf("  - %s enabled=%s targets=%s profile=%s last_run=%s state=%s\n",
			row.Name,
			boolString(row.Enabled),
			formatTargets(row.Targets),
			row.Profile,
			row.LastRunAt,
			row.LastRunState,
		)
	}
	_ = recordPaasAuditEvent("agent status", "succeeded", "live", map[string]string{
		"count": intString(len(rows)),
		"path":  path,
	}, nil)
}

func cmdPaasAgentLogs(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas agent logs", flag.ExitOnError)
	name := fs.String("name", "", "agent name")
	tail := fs.Int("tail", 200, "number of log lines")
	follow := fs.Bool("follow", false, "follow logs")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasAgentLogsUsageText)
		return
	}
	if !requirePaasValue(*name, "name", paasAgentLogsUsageText) {
		return
	}
	rows, path, err := loadPaasAgentRunRecords(strings.TrimSpace(*name), *tail)
	if err != nil {
		failPaasCommand("agent logs", jsonOut, err, nil)
	}
	if jsonOut {
		payload := map[string]any{
			"ok":      true,
			"command": "agent logs",
			"context": currentPaasContext(),
			"mode":    "live",
			"name":    strings.TrimSpace(*name),
			"tail":    *tail,
			"follow":  *follow,
			"path":    path,
			"count":   len(rows),
			"data":    rows,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		_ = recordPaasAuditEvent("agent logs", "succeeded", "live", map[string]string{
			"count":  intString(len(rows)),
			"name":   strings.TrimSpace(*name),
			"tail":   intString(*tail),
			"follow": boolString(*follow),
			"path":   path,
		}, nil)
		return
	}
	fmt.Printf("%s %d\n", styleHeading("paas agent logs:"), len(rows))
	for _, row := range rows {
		fmt.Printf("  - %s run=%s status=%s incident=%s runtime=%s profile=%s message=%s\n",
			row.Timestamp,
			row.RunID,
			row.Status,
			row.IncidentID,
			row.RuntimeMode,
			row.RuntimeProfile,
			row.Message,
		)
	}
	_ = recordPaasAuditEvent("agent logs", "succeeded", "live", map[string]string{
		"count":  intString(len(rows)),
		"name":   strings.TrimSpace(*name),
		"tail":   intString(*tail),
		"follow": boolString(*follow),
		"path":   path,
	}, nil)
}

func cmdPaasAgentRunOnce(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas agent run-once", flag.ExitOnError)
	name := fs.String("name", "", "agent name")
	incident := fs.String("incident", "", "incident id")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasAgentRunOnceUsageText)
		return
	}
	if !requirePaasValue(*name, "name", paasAgentRunOnceUsageText) {
		return
	}
	selectedName := strings.TrimSpace(*name)
	store, _, err := loadPaasAgentStore(currentPaasContext())
	if err != nil {
		failPaasCommand("agent run-once", jsonOut, err, nil)
	}
	index := findPaasAgentIndex(store, selectedName)
	if index < 0 {
		failPaasCommand("agent run-once", jsonOut, fmt.Errorf("agent %q was not found", selectedName), nil)
	}

	syncResult, err := syncPaasIncidentQueueFromCollectors(paasIncidentQueueDefaultCollectLimit, paasIncidentQueueDefaultMaxEntries, paasIncidentQueueDefaultMaxAge)
	if err != nil {
		failPaasCommand("agent run-once", jsonOut, err, nil)
	}
	queueRows, _, err := loadPaasIncidentQueueSummary(paasIncidentQueueDefaultCollectLimit)
	if err != nil {
		failPaasCommand("agent run-once", jsonOut, err, nil)
	}
	requestedIncident := strings.TrimSpace(*incident)
	var selectedEntry *paasIncidentQueueEntry
	status := "noop"
	message := "no queued incidents available"
	for i := range queueRows {
		row := queueRows[i]
		candidateID := strings.TrimSpace(row.Incident.ID)
		candidateKey := strings.TrimSpace(row.Key)
		if requestedIncident != "" {
			if requestedIncident != candidateID && requestedIncident != candidateKey {
				continue
			}
		}
		selectedEntry = &queueRows[i]
		break
	}
	if requestedIncident != "" && selectedEntry == nil {
		status = "blocked"
		message = "requested incident was not found in queue"
	}
	plan, planErr := buildPaasAgentRuntimeAdapterPlan(store.Agents[index], selectedEntry)
	if planErr != nil {
		status = "blocked"
		message = strings.TrimSpace(planErr.Error())
	}
	if planErr == nil && !plan.Ready {
		status = "blocked"
		if strings.TrimSpace(plan.Reason) != "" {
			message = strings.TrimSpace(plan.Reason)
		} else {
			message = "runtime adapter is not ready"
		}
	}
	if selectedEntry != nil && planErr == nil && plan.Ready {
		status = "queued"
		message = "incident queued for remediation policy evaluation"
	}
	runID := "run-" + time.Now().UTC().Format("20060102T150405.000000000Z07:00")
	_, err = appendPaasAgentRunRecord(paasAgentRunRecord{
		Agent:          selectedName,
		RunID:          runID,
		Status:         status,
		IncidentID:     plan.IncidentID,
		RuntimeMode:    plan.Mode,
		RuntimeProfile: plan.ProfileID,
		RuntimeAuth:    plan.AuthPath,
		RuntimeReady:   plan.Ready,
		Collected:      syncResult.Collected,
		Inserted:       syncResult.Inserted,
		Updated:        syncResult.Updated,
		Pruned:         syncResult.Pruned,
		QueueTotal:     syncResult.Total,
		QueuePath:      syncResult.Path,
		CollectorCount: len(syncResult.CollectorStats),
		Message:        message,
	})
	if err != nil {
		failPaasCommand("agent run-once", jsonOut, err, nil)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	store.Agents[index].LastRunAt = now
	store.Agents[index].LastRunID = runID
	store.Agents[index].LastRunState = status
	store.Agents[index].UpdatedAt = now
	storePath, err := savePaasAgentStore(currentPaasContext(), store)
	if err != nil {
		failPaasCommand("agent run-once", jsonOut, err, nil)
	}

	fields := map[string]string{
		"name":            selectedName,
		"run_id":          runID,
		"incident":        requestedIncident,
		"selected":        plan.IncidentID,
		"status":          status,
		"message":         message,
		"runtime_mode":    plan.Mode,
		"runtime_profile": plan.ProfileID,
		"runtime_auth":    plan.AuthPath,
		"runtime_ready":   boolString(plan.Ready),
		"runtime_reason":  plan.Reason,
		"collected":       intString(syncResult.Collected),
		"queue_inserted":  intString(syncResult.Inserted),
		"queue_updated":   intString(syncResult.Updated),
		"queue_pruned":    intString(syncResult.Pruned),
		"queue_total":     intString(syncResult.Total),
		"collector_count": intString(len(syncResult.CollectorStats)),
		"queue_path":      syncResult.Path,
		"store_path":      storePath,
	}
	printPaasLiveAgentScaffold("agent run-once", fields, jsonOut)
}

func cmdPaasAgentApprove(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas agent approve", flag.ExitOnError)
	runID := fs.String("run", "", "agent run identifier")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasAgentApproveUsageText)
		return
	}
	if !requirePaasValue(*runID, "run", paasAgentApproveUsageText) {
		return
	}
	printPaasScaffold("agent approve", map[string]string{"run": strings.TrimSpace(*runID)}, jsonOut)
}

func cmdPaasAgentDeny(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas agent deny", flag.ExitOnError)
	runID := fs.String("run", "", "agent run identifier")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasAgentDenyUsageText)
		return
	}
	if !requirePaasValue(*runID, "run", paasAgentDenyUsageText) {
		return
	}
	printPaasScaffold("agent deny", map[string]string{"run": strings.TrimSpace(*runID)}, jsonOut)
}

func printPaasAgentEnvelope(command string, row paasAgentConfig, path string, jsonOut bool) {
	fields := map[string]string{
		"name":          row.Name,
		"enabled":       boolString(row.Enabled),
		"targets":       formatTargets(row.Targets),
		"profile":       row.Profile,
		"created_at":    row.CreatedAt,
		"updated_at":    row.UpdatedAt,
		"last_run_at":   row.LastRunAt,
		"last_run_id":   row.LastRunID,
		"last_run_state": row.LastRunState,
		"path":          path,
	}
	printPaasLiveAgentScaffold(command, fields, jsonOut)
}

func printPaasLiveAgentScaffold(command string, fields map[string]string, jsonOut bool) {
	if jsonOut {
		payload := map[string]any{
			"ok":      true,
			"command": command,
			"context": currentPaasContext(),
			"mode":    "live",
			"fields":  redactPaasSensitiveFields(fields),
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		_ = recordPaasAuditEvent(command, "succeeded", "live", fields, nil)
		return
	}
	fmt.Printf("%s %s\n", styleHeading("si paas:"), command)
	fmt.Printf("  context=%s\n", currentPaasContext())
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	redacted := redactPaasSensitiveFields(fields)
	for _, key := range keys {
		fmt.Printf("  %s=%s\n", key, redacted[key])
	}
	_ = recordPaasAuditEvent(command, "succeeded", "live", fields, nil)
}
