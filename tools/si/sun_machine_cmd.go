package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	heliaMachineUsageText = "usage: si sun machine <register|status|list|allow|deny|run|jobs|serve> ..."

	heliaMachineKind    = "si_machine"
	heliaMachineJobKind = "si_machine_job"

	heliaMachineJobStatusQueued    = "queued"
	heliaMachineJobStatusRunning   = "running"
	heliaMachineJobStatusSucceeded = "succeeded"
	heliaMachineJobStatusFailed    = "failed"
	heliaMachineJobStatusDenied    = "denied"

	heliaMachineOutputMaxBytes = 64 * 1024
)

type heliaMachineRecord struct {
	Version       int                       `json:"version"`
	MachineID     string                    `json:"machine_id"`
	DisplayName   string                    `json:"display_name,omitempty"`
	OwnerOperator string                    `json:"owner_operator"`
	UpdatedAt     string                    `json:"updated_at,omitempty"`
	RegisteredAt  string                    `json:"registered_at,omitempty"`
	Capabilities  heliaMachineCapabilities  `json:"capabilities"`
	ACL           heliaMachineAccessControl `json:"acl"`
	Heartbeat     heliaMachineHeartbeat     `json:"heartbeat"`
}

type heliaMachineCapabilities struct {
	CanControlOthers bool `json:"can_control_others"`
	CanBeControlled  bool `json:"can_be_controlled"`
}

type heliaMachineAccessControl struct {
	AllowedOperators []string `json:"allowed_operators,omitempty"`
}

type heliaMachineHeartbeat struct {
	LastSeenAt string `json:"last_seen_at,omitempty"`
	LastState  string `json:"last_state,omitempty"`
}

type heliaMachineJob struct {
	Version        int      `json:"version"`
	JobID          string   `json:"job_id"`
	MachineID      string   `json:"machine_id"`
	RequestedBy    string   `json:"requested_by"`
	SourceMachine  string   `json:"source_machine,omitempty"`
	Command        []string `json:"command"`
	TimeoutSeconds int      `json:"timeout_seconds"`
	Status         string   `json:"status"`
	RequestedAt    string   `json:"requested_at,omitempty"`
	UpdatedAt      string   `json:"updated_at,omitempty"`
	ClaimedBy      string   `json:"claimed_by,omitempty"`
	ClaimedAt      string   `json:"claimed_at,omitempty"`
	StartedAt      string   `json:"started_at,omitempty"`
	CompletedAt    string   `json:"completed_at,omitempty"`
	ExitCode       int      `json:"exit_code,omitempty"`
	Stdout         string   `json:"stdout,omitempty"`
	Stderr         string   `json:"stderr,omitempty"`
	Error          string   `json:"error,omitempty"`
}

type heliaMachineServeSummary struct {
	MachineID string   `json:"machine_id"`
	Processed int      `json:"processed"`
	JobIDs    []string `json:"job_ids,omitempty"`
}

func cmdHeliaMachine(args []string) {
	if len(args) == 0 {
		printUsage(heliaMachineUsageText)
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "help", "-h", "--help":
		printUsage(heliaMachineUsageText)
	case "register":
		cmdHeliaMachineRegister(rest)
	case "status":
		cmdHeliaMachineStatus(rest)
	case "list":
		cmdHeliaMachineList(rest)
	case "allow":
		cmdHeliaMachineAllow(rest)
	case "deny":
		cmdHeliaMachineDeny(rest)
	case "run", "exec":
		cmdHeliaMachineRun(rest)
	case "jobs":
		cmdHeliaMachineJobs(rest)
	case "serve":
		cmdHeliaMachineServe(rest)
	default:
		printUnknown("sun machine", sub)
		printUsage(heliaMachineUsageText)
		os.Exit(1)
	}
}

func cmdHeliaMachineRegister(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("sun machine register", flag.ExitOnError)
	machineID := fs.String("machine", "", "machine id")
	operatorID := fs.String("operator", "", "operator id")
	displayName := fs.String("display-name", "", "display name")
	allowOperators := fs.String("allow-operators", "", "comma-separated allowed operator IDs")
	canControlOthers := fs.Bool("can-control-others", false, "allow this machine to dispatch jobs to other machines")
	canBeControlled := fs.Bool("can-be-controlled", true, "allow this machine to execute remote jobs")
	setDefaults := fs.Bool("set-defaults", true, "persist machine/operator defaults to settings")
	jsonOut := fs.Bool("json", false, "json output")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si sun machine register [--machine <id>] [--operator <id>] [--display-name <name>] [--allow-operators <csv>] [--can-control-others] [--can-be-controlled=false] [--set-defaults] [--json]")
		return
	}
	client, err := heliaClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	ctx := heliaContext(settings)
	resolvedMachine := heliaMachineResolveID(settings, strings.TrimSpace(*machineID))
	resolvedOperator := heliaMachineResolveOperatorID(settings, strings.TrimSpace(*operatorID), resolvedMachine)
	if resolvedMachine == "" {
		fatal(fmt.Errorf("machine id is required"))
	}
	if resolvedOperator == "" {
		fatal(fmt.Errorf("operator id is required"))
	}

	record, revision, exists, err := heliaMachineLoad(ctx, client, resolvedMachine)
	if err != nil {
		fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if !exists {
		record = heliaMachineRecord{
			Version:       1,
			MachineID:     resolvedMachine,
			OwnerOperator: resolvedOperator,
			RegisteredAt:  now,
		}
	}
	if record.Version <= 0 {
		record.Version = 1
	}
	record.MachineID = resolvedMachine
	if strings.TrimSpace(record.OwnerOperator) == "" {
		record.OwnerOperator = resolvedOperator
	}
	if strings.TrimSpace(*displayName) != "" {
		record.DisplayName = strings.TrimSpace(*displayName)
	}
	controlProvided := flagProvided(args, "can-control-others")
	controlTargetProvided := flagProvided(args, "can-be-controlled")
	if !exists {
		record.Capabilities.CanControlOthers = *canControlOthers
		record.Capabilities.CanBeControlled = *canBeControlled
	} else {
		if controlProvided {
			record.Capabilities.CanControlOthers = *canControlOthers
		}
		if controlTargetProvided {
			record.Capabilities.CanBeControlled = *canBeControlled
		}
	}
	allowList := make([]string, 0, len(record.ACL.AllowedOperators)+4)
	allowList = append(allowList, record.ACL.AllowedOperators...)
	allowList = append(allowList, resolvedOperator)
	allowList = append(allowList, record.OwnerOperator)
	allowList = append(allowList, splitCSVScopes(*allowOperators)...)
	record.ACL.AllowedOperators = normalizeMachineOperatorIDs(allowList)
	record.UpdatedAt = now
	record.Heartbeat.LastSeenAt = now
	record.Heartbeat.LastState = "registered"

	newRevision, err := heliaMachinePersist(ctx, client, record, exists, revision)
	if err != nil {
		fatal(err)
	}
	if *setDefaults {
		current, loadErr := loadSettings()
		if loadErr != nil {
			fatal(loadErr)
		}
		current.Helia.MachineID = resolvedMachine
		current.Helia.OperatorID = resolvedOperator
		if saveErr := saveSettings(current); saveErr != nil {
			fatal(saveErr)
		}
	}
	if *jsonOut {
		printJSON(map[string]interface{}{
			"machine":  record,
			"revision": newRevision,
		})
		return
	}
	successf("registered machine %s (revision %d)", record.MachineID, newRevision)
}

func cmdHeliaMachineStatus(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("sun machine status", flag.ExitOnError)
	machineID := fs.String("machine", "", "machine id")
	jsonOut := fs.Bool("json", false, "json output")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si sun machine status [--machine <id>] [--json]")
		return
	}
	client, err := heliaClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	resolvedMachine := heliaMachineResolveID(settings, strings.TrimSpace(*machineID))
	record, _, exists, err := heliaMachineLoad(heliaContext(settings), client, resolvedMachine)
	if err != nil {
		fatal(err)
	}
	if !exists {
		fatal(fmt.Errorf("machine %q is not registered", resolvedMachine))
	}
	if *jsonOut {
		printJSON(record)
		return
	}
	printHeliaMachineRecord(record)
}

func cmdHeliaMachineList(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("sun machine list", flag.ExitOnError)
	limit := fs.Int("limit", 200, "max rows")
	jsonOut := fs.Bool("json", false, "json output")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si sun machine list [--limit <n>] [--json]")
		return
	}
	client, err := heliaClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	items, err := client.listObjects(heliaContext(settings), heliaMachineKind, "", *limit)
	if err != nil {
		fatal(err)
	}
	rows := make([]heliaMachineRecord, 0, len(items))
	for i := range items {
		record, _, exists, loadErr := heliaMachineLoad(heliaContext(settings), client, strings.TrimSpace(items[i].Name))
		if loadErr != nil || !exists {
			continue
		}
		rows = append(rows, record)
	}
	sort.Slice(rows, func(i, j int) bool {
		return strings.Compare(rows[i].MachineID, rows[j].MachineID) < 0
	})
	if *jsonOut {
		printJSON(rows)
		return
	}
	if len(rows) == 0 {
		infof("no machines registered")
		return
	}
	table := make([][]string, 0, len(rows))
	for _, row := range rows {
		table = append(table, []string{
			row.MachineID,
			boolString(row.Capabilities.CanControlOthers),
			boolString(row.Capabilities.CanBeControlled),
			row.OwnerOperator,
			row.Heartbeat.LastSeenAt,
		})
	}
	printAlignedTable([]string{
		styleHeading("MACHINE"),
		styleHeading("CAN_CONTROL"),
		styleHeading("CAN_EXECUTE"),
		styleHeading("OWNER"),
		styleHeading("LAST_SEEN"),
	}, table, 2)
}

func cmdHeliaMachineAllow(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("sun machine allow", flag.ExitOnError)
	machineID := fs.String("machine", "", "target machine id")
	grantOperator := fs.String("grant", "", "operator id to allow")
	asOperator := fs.String("as", "", "requesting operator id")
	jsonOut := fs.Bool("json", false, "json output")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si sun machine allow --machine <id> --grant <operator> [--as <operator>] [--json]")
		return
	}
	targetMachine := heliaMachineResolveID(settings, strings.TrimSpace(*machineID))
	if strings.TrimSpace(*grantOperator) == "" {
		fatal(fmt.Errorf("--grant is required"))
	}
	caller := heliaMachineResolveOperatorID(settings, strings.TrimSpace(*asOperator), heliaMachineResolveID(settings, ""))
	client, err := heliaClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	record, revision, exists, err := heliaMachineLoad(heliaContext(settings), client, targetMachine)
	if err != nil {
		fatal(err)
	}
	if !exists {
		fatal(fmt.Errorf("machine %q is not registered", targetMachine))
	}
	if !strings.EqualFold(strings.TrimSpace(record.OwnerOperator), caller) {
		fatal(fmt.Errorf("only machine owner %q can grant operators", strings.TrimSpace(record.OwnerOperator)))
	}
	updated := append([]string{}, record.ACL.AllowedOperators...)
	updated = append(updated, strings.TrimSpace(*grantOperator))
	record.ACL.AllowedOperators = normalizeMachineOperatorIDs(updated)
	record.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	newRevision, err := heliaMachinePersist(heliaContext(settings), client, record, true, revision)
	if err != nil {
		fatal(err)
	}
	if *jsonOut {
		printJSON(map[string]interface{}{
			"machine":  record,
			"revision": newRevision,
		})
		return
	}
	successf("granted operator %s on machine %s", strings.TrimSpace(*grantOperator), targetMachine)
}

func cmdHeliaMachineDeny(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("sun machine deny", flag.ExitOnError)
	machineID := fs.String("machine", "", "target machine id")
	revokeOperator := fs.String("revoke", "", "operator id to revoke")
	asOperator := fs.String("as", "", "requesting operator id")
	jsonOut := fs.Bool("json", false, "json output")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si sun machine deny --machine <id> --revoke <operator> [--as <operator>] [--json]")
		return
	}
	targetMachine := heliaMachineResolveID(settings, strings.TrimSpace(*machineID))
	revoke := strings.TrimSpace(*revokeOperator)
	if revoke == "" {
		fatal(fmt.Errorf("--revoke is required"))
	}
	caller := heliaMachineResolveOperatorID(settings, strings.TrimSpace(*asOperator), heliaMachineResolveID(settings, ""))
	client, err := heliaClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	record, revision, exists, err := heliaMachineLoad(heliaContext(settings), client, targetMachine)
	if err != nil {
		fatal(err)
	}
	if !exists {
		fatal(fmt.Errorf("machine %q is not registered", targetMachine))
	}
	if strings.EqualFold(revoke, strings.TrimSpace(record.OwnerOperator)) {
		fatal(fmt.Errorf("cannot revoke owner operator %q", strings.TrimSpace(record.OwnerOperator)))
	}
	if !strings.EqualFold(strings.TrimSpace(record.OwnerOperator), caller) {
		fatal(fmt.Errorf("only machine owner %q can revoke operators", strings.TrimSpace(record.OwnerOperator)))
	}
	filtered := make([]string, 0, len(record.ACL.AllowedOperators))
	for _, op := range record.ACL.AllowedOperators {
		if strings.EqualFold(strings.TrimSpace(op), revoke) {
			continue
		}
		filtered = append(filtered, strings.TrimSpace(op))
	}
	record.ACL.AllowedOperators = normalizeMachineOperatorIDs(filtered)
	record.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	newRevision, err := heliaMachinePersist(heliaContext(settings), client, record, true, revision)
	if err != nil {
		fatal(err)
	}
	if *jsonOut {
		printJSON(map[string]interface{}{
			"machine":  record,
			"revision": newRevision,
		})
		return
	}
	successf("revoked operator %s on machine %s", revoke, targetMachine)
}

func cmdHeliaMachineRun(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("sun machine run", flag.ExitOnError)
	targetMachineFlag := fs.String("machine", "", "target machine id")
	sourceMachineFlag := fs.String("source-machine", "", "requesting machine id")
	operatorFlag := fs.String("operator", "", "requesting operator id")
	timeoutSeconds := fs.Int("timeout-seconds", 900, "remote si command timeout")
	wait := fs.Bool("wait", false, "wait for completion")
	waitTimeoutSeconds := fs.Int("wait-timeout-seconds", 1200, "max wait time when --wait is used")
	pollSeconds := fs.Int("poll-seconds", 2, "poll interval when waiting")
	jsonOut := fs.Bool("json", false, "json output")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	commandArgs := fs.Args()
	if len(commandArgs) > 0 && strings.TrimSpace(commandArgs[0]) == "--" {
		commandArgs = commandArgs[1:]
	}
	if len(commandArgs) > 0 && strings.EqualFold(strings.TrimSpace(commandArgs[0]), "si") {
		commandArgs = commandArgs[1:]
	}
	if len(commandArgs) == 0 {
		printUsage("usage: si sun machine run --machine <id> [--source-machine <id>] [--operator <id>] [--timeout-seconds <n>] [--wait] [--wait-timeout-seconds <n>] [--poll-seconds <n>] [--json] -- <si args...>")
		return
	}
	targetMachine := heliaMachineResolveID(settings, strings.TrimSpace(*targetMachineFlag))
	sourceMachine := heliaMachineResolveID(settings, strings.TrimSpace(*sourceMachineFlag))
	operatorID := heliaMachineResolveOperatorID(settings, strings.TrimSpace(*operatorFlag), sourceMachine)

	client, err := heliaClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	ctx := heliaContext(settings)
	targetRecord, _, exists, err := heliaMachineLoad(ctx, client, targetMachine)
	if err != nil {
		fatal(err)
	}
	if !exists {
		fatal(fmt.Errorf("target machine %q is not registered", targetMachine))
	}
	if !targetRecord.Capabilities.CanBeControlled {
		fatal(fmt.Errorf("target machine %q does not accept remote control (can_be_controlled=false)", targetMachine))
	}
	if !heliaMachineOperatorAllowed(targetRecord, operatorID) {
		fatal(fmt.Errorf("operator %q is not allowed to control machine %q", operatorID, targetMachine))
	}
	sourceRecord, _, sourceExists, err := heliaMachineLoad(ctx, client, sourceMachine)
	if err != nil {
		fatal(err)
	}
	if !sourceExists {
		fatal(fmt.Errorf("source machine %q is not registered; run `si sun machine register --machine %s --can-control-others` first", sourceMachine, sourceMachine))
	}
	if !heliaMachineOperatorAllowed(sourceRecord, operatorID) {
		fatal(fmt.Errorf("operator %q is not allowed on source machine %q", operatorID, sourceMachine))
	}
	if !sourceRecord.Capabilities.CanControlOthers {
		fatal(fmt.Errorf("source machine %q cannot control other machines (can_control_others=false)", sourceMachine))
	}

	now := time.Now().UTC()
	jobID := heliaMachineJobID(now)
	job := heliaMachineJob{
		Version:        1,
		JobID:          jobID,
		MachineID:      targetMachine,
		RequestedBy:    operatorID,
		SourceMachine:  sourceMachine,
		Command:        append([]string(nil), commandArgs...),
		TimeoutSeconds: max(10, *timeoutSeconds),
		Status:         heliaMachineJobStatusQueued,
		RequestedAt:    now.Format(time.RFC3339),
		UpdatedAt:      now.Format(time.RFC3339),
	}
	jobName := heliaMachineJobObjectName(targetMachine, jobID)
	if _, err := heliaMachinePersistJob(ctx, client, jobName, job, false, 0); err != nil {
		fatal(err)
	}

	if *wait {
		waitCtx, cancel := context.WithTimeout(ctx, time.Duration(max(10, *waitTimeoutSeconds))*time.Second)
		defer cancel()
		doneJob, _, waitErr := heliaMachineWaitForJob(waitCtx, client, jobName, max(1, *pollSeconds))
		if waitErr != nil {
			fatal(waitErr)
		}
		if *jsonOut {
			printJSON(doneJob)
		} else {
			printHeliaMachineJob(doneJob)
		}
		if statusErr := heliaMachineJobFailureError(doneJob); statusErr != nil {
			fatal(statusErr)
		}
		return
	}
	if *jsonOut {
		printJSON(job)
		return
	}
	successf("queued remote job %s on machine %s", job.JobID, targetMachine)
}

func cmdHeliaMachineJobs(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("sun machine jobs", flag.ExitOnError)
	machineID := fs.String("machine", "", "machine id filter")
	requestedBy := fs.String("requested-by", "", "requesting operator filter")
	status := fs.String("status", "", "status filter")
	limit := fs.Int("limit", 200, "max jobs")
	jsonOut := fs.Bool("json", false, "json output")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si sun machine jobs [--machine <id>] [--requested-by <operator>] [--status <queued|running|succeeded|failed|denied>] [--limit <n>] [--json]")
		return
	}
	statusFilter := normalizeMachineJobStatus(*status)
	if strings.TrimSpace(*status) != "" && statusFilter == "" {
		fatal(fmt.Errorf("invalid --status %q", strings.TrimSpace(*status)))
	}
	client, err := heliaClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	items, err := client.listObjects(heliaContext(settings), heliaMachineJobKind, "", *limit)
	if err != nil {
		fatal(err)
	}
	rows := make([]heliaMachineJob, 0, len(items))
	for i := range items {
		job, _, exists, loadErr := heliaMachineLoadJob(heliaContext(settings), client, strings.TrimSpace(items[i].Name))
		if loadErr != nil || !exists {
			continue
		}
		if strings.TrimSpace(*machineID) != "" && !strings.EqualFold(strings.TrimSpace(job.MachineID), heliaMachineResolveID(settings, strings.TrimSpace(*machineID))) {
			continue
		}
		if strings.TrimSpace(*requestedBy) != "" && !strings.EqualFold(strings.TrimSpace(job.RequestedBy), strings.TrimSpace(*requestedBy)) {
			continue
		}
		if statusFilter != "" && !strings.EqualFold(strings.TrimSpace(job.Status), statusFilter) {
			continue
		}
		rows = append(rows, job)
	}
	sort.Slice(rows, func(i, j int) bool {
		left := parseRFC3339(rows[i].RequestedAt)
		right := parseRFC3339(rows[j].RequestedAt)
		if !left.Equal(right) {
			return left.Before(right)
		}
		return strings.Compare(rows[i].JobID, rows[j].JobID) < 0
	})
	if *jsonOut {
		printJSON(rows)
		return
	}
	if len(rows) == 0 {
		infof("no machine jobs found")
		return
	}
	table := make([][]string, 0, len(rows))
	for _, row := range rows {
		table = append(table, []string{
			row.JobID,
			row.MachineID,
			row.Status,
			row.RequestedBy,
			row.RequestedAt,
			strings.Join(row.Command, " "),
		})
	}
	printAlignedTable([]string{
		styleHeading("JOB"),
		styleHeading("MACHINE"),
		styleHeading("STATUS"),
		styleHeading("REQUESTED_BY"),
		styleHeading("AT"),
		styleHeading("COMMAND"),
	}, table, 2)
}

func cmdHeliaMachineServe(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("sun machine serve", flag.ExitOnError)
	machineID := fs.String("machine", "", "machine id")
	operatorID := fs.String("operator", "", "local operator id")
	pollSeconds := fs.Int("poll-seconds", 2, "queue poll interval seconds")
	once := fs.Bool("once", false, "process at most one job then exit")
	maxJobs := fs.Int("max-jobs", 0, "max jobs to process before exit (0 = unlimited)")
	jsonOut := fs.Bool("json", false, "json output")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si sun machine serve [--machine <id>] [--operator <id>] [--poll-seconds <n>] [--once] [--max-jobs <n>] [--json]")
		return
	}
	client, err := heliaClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	ctx := heliaContext(settings)
	resolvedMachine := heliaMachineResolveID(settings, strings.TrimSpace(*machineID))
	localOperator := heliaMachineResolveOperatorID(settings, strings.TrimSpace(*operatorID), resolvedMachine)

	record, machineRevision, exists, err := heliaMachineLoad(ctx, client, resolvedMachine)
	if err != nil {
		fatal(err)
	}
	if !exists {
		fatal(fmt.Errorf("machine %q is not registered; run `si sun machine register --machine %s` first", resolvedMachine, resolvedMachine))
	}
	if !record.Capabilities.CanBeControlled {
		fatal(fmt.Errorf("machine %q is not accepting remote jobs (can_be_controlled=false)", resolvedMachine))
	}
	now := time.Now().UTC().Format(time.RFC3339)
	record.Heartbeat.LastSeenAt = now
	record.Heartbeat.LastState = "serving"
	record.UpdatedAt = now
	if _, err := heliaMachinePersist(ctx, client, record, true, machineRevision); err != nil {
		fatal(err)
	}

	summary := heliaMachineServeSummary{MachineID: resolvedMachine}
	for {
		jobName, job, jobRevision, found, claimErr := heliaMachineClaimNextJob(ctx, client, resolvedMachine)
		if claimErr != nil {
			fatal(claimErr)
		}
		if !found {
			if *once {
				break
			}
			if *maxJobs > 0 && summary.Processed >= *maxJobs {
				break
			}
			time.Sleep(time.Duration(max(1, *pollSeconds)) * time.Second)
			continue
		}
		finished := heliaMachineRunClaimedJob(job, record, localOperator)
		if _, err := heliaMachinePersistJob(ctx, client, jobName, finished, true, jobRevision); err != nil {
			fatal(err)
		}
		summary.Processed++
		summary.JobIDs = append(summary.JobIDs, finished.JobID)
		if *once {
			break
		}
		if *maxJobs > 0 && summary.Processed >= *maxJobs {
			break
		}
	}
	if *jsonOut {
		printJSON(summary)
		return
	}
	successf("machine serve processed %d jobs on %s", summary.Processed, resolvedMachine)
}

func heliaMachineRunClaimedJob(job heliaMachineJob, machine heliaMachineRecord, localOperator string) heliaMachineJob {
	now := time.Now().UTC()
	job.Status = heliaMachineJobStatusRunning
	job.UpdatedAt = now.Format(time.RFC3339)
	if strings.TrimSpace(job.StartedAt) == "" {
		job.StartedAt = now.Format(time.RFC3339)
	}
	if !heliaMachineOperatorAllowed(machine, job.RequestedBy) {
		job.Status = heliaMachineJobStatusDenied
		job.CompletedAt = time.Now().UTC().Format(time.RFC3339)
		job.UpdatedAt = job.CompletedAt
		job.ExitCode = 1
		job.Error = fmt.Sprintf("operator %q is not allowed by machine %q ACL", strings.TrimSpace(job.RequestedBy), strings.TrimSpace(machine.MachineID))
		return job
	}
	if !machine.Capabilities.CanBeControlled {
		job.Status = heliaMachineJobStatusDenied
		job.CompletedAt = time.Now().UTC().Format(time.RFC3339)
		job.UpdatedAt = job.CompletedAt
		job.ExitCode = 1
		job.Error = fmt.Sprintf("machine %q refuses remote control", strings.TrimSpace(machine.MachineID))
		return job
	}
	timeout := time.Duration(max(10, job.TimeoutSeconds)) * time.Second
	runCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	stdout, stderr, exitCode, runErr := runLocalSICommand(runCtx, job.Command)
	job.Stdout = truncateMachineOutput(stdout)
	job.Stderr = truncateMachineOutput(stderr)
	job.ExitCode = exitCode
	completed := time.Now().UTC().Format(time.RFC3339)
	job.CompletedAt = completed
	job.UpdatedAt = completed
	if runErr != nil || exitCode != 0 {
		job.Status = heliaMachineJobStatusFailed
		if runErr != nil {
			job.Error = strings.TrimSpace(runErr.Error())
		} else {
			job.Error = fmt.Sprintf("command exited with code %d", exitCode)
		}
		return job
	}
	job.Status = heliaMachineJobStatusSucceeded
	job.Error = ""
	job.ClaimedBy = firstNonEmpty(strings.TrimSpace(job.ClaimedBy), strings.TrimSpace(machine.MachineID))
	job.ClaimedAt = firstNonEmpty(strings.TrimSpace(job.ClaimedAt), now.Format(time.RFC3339))
	_ = localOperator
	return job
}

func runLocalSICommand(ctx context.Context, args []string) (stdout string, stderr string, exitCode int, err error) {
	exe, err := os.Executable()
	if err != nil {
		return "", "", 1, err
	}
	safeArgs := make([]string, 0, len(args))
	for _, arg := range args {
		trimmed := strings.TrimSpace(arg)
		if trimmed == "" {
			continue
		}
		safeArgs = append(safeArgs, trimmed)
	}
	if len(safeArgs) == 0 {
		return "", "", 1, fmt.Errorf("empty si command")
	}
	cmd := exec.CommandContext(ctx, exe, safeArgs...)
	cmd.Env = append([]string{}, os.Environ()...)
	cmd.Env = append(cmd.Env, "NO_COLOR=1")
	var outBuf bytes.Buffer
	var errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	runErr := cmd.Run()
	exit := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exit = exitErr.ExitCode()
		} else {
			exit = 1
		}
	}
	return outBuf.String(), errBuf.String(), exit, runErr
}

func heliaMachineClaimNextJob(ctx context.Context, client *heliaClient, machineID string) (string, heliaMachineJob, int64, bool, error) {
	items, err := client.listObjects(ctx, heliaMachineJobKind, "", 200)
	if err != nil {
		return "", heliaMachineJob{}, 0, false, err
	}
	type candidate struct {
		name string
		job  heliaMachineJob
	}
	candidates := make([]candidate, 0, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		if !strings.HasPrefix(strings.ToLower(name), strings.ToLower(heliaMachineJobNamePrefix(machineID))) {
			continue
		}
		job, _, exists, loadErr := heliaMachineLoadJob(ctx, client, name)
		if loadErr != nil || !exists {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(job.Status), heliaMachineJobStatusQueued) {
			continue
		}
		candidates = append(candidates, candidate{name: name, job: job})
	}
	sort.Slice(candidates, func(i, j int) bool {
		left := parseRFC3339(candidates[i].job.RequestedAt)
		right := parseRFC3339(candidates[j].job.RequestedAt)
		if !left.Equal(right) {
			return left.Before(right)
		}
		return strings.Compare(candidates[i].job.JobID, candidates[j].job.JobID) < 0
	})
	for _, cand := range candidates {
		job, revision, exists, loadErr := heliaMachineLoadJob(ctx, client, cand.name)
		if loadErr != nil || !exists {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(job.Status), heliaMachineJobStatusQueued) {
			continue
		}
		now := time.Now().UTC().Format(time.RFC3339)
		job.Status = heliaMachineJobStatusRunning
		job.ClaimedBy = machineID
		job.ClaimedAt = now
		job.StartedAt = now
		job.UpdatedAt = now
		newRevision, persistErr := heliaMachinePersistJob(ctx, client, cand.name, job, true, revision)
		if persistErr != nil {
			if isHeliaStatus(persistErr, 409) {
				continue
			}
			return "", heliaMachineJob{}, 0, false, persistErr
		}
		return cand.name, job, newRevision, true, nil
	}
	return "", heliaMachineJob{}, 0, false, nil
}

func heliaMachineWaitForJob(ctx context.Context, client *heliaClient, jobName string, pollSeconds int) (heliaMachineJob, int64, error) {
	interval := time.Duration(max(1, pollSeconds)) * time.Second
	for {
		job, revision, exists, err := heliaMachineLoadJob(ctx, client, jobName)
		if err != nil {
			return heliaMachineJob{}, 0, err
		}
		if !exists {
			return heliaMachineJob{}, 0, fmt.Errorf("remote job %q not found", strings.TrimSpace(jobName))
		}
		if heliaMachineIsTerminalStatus(job.Status) {
			return job, revision, nil
		}
		select {
		case <-ctx.Done():
			return heliaMachineJob{}, 0, fmt.Errorf("timed out waiting for remote job %q", strings.TrimSpace(job.JobID))
		case <-time.After(interval):
		}
	}
}

func heliaMachineLoad(ctx context.Context, client *heliaClient, machineID string) (heliaMachineRecord, int64, bool, error) {
	id := strings.TrimSpace(machineID)
	if id == "" {
		return heliaMachineRecord{}, 0, false, fmt.Errorf("machine id is required")
	}
	meta, err := heliaLookupObjectMeta(ctx, client, heliaMachineKind, id)
	if err != nil {
		return heliaMachineRecord{}, 0, false, err
	}
	if meta == nil {
		return heliaMachineRecord{}, 0, false, nil
	}
	payload, err := client.getPayload(ctx, heliaMachineKind, id)
	if err != nil {
		return heliaMachineRecord{}, 0, true, err
	}
	var out heliaMachineRecord
	if err := json.Unmarshal(payload, &out); err != nil {
		return heliaMachineRecord{}, 0, true, fmt.Errorf("machine payload invalid: %w", err)
	}
	out = normalizeHeliaMachineRecord(out, id)
	return out, meta.LatestRevision, true, nil
}

func heliaMachinePersist(ctx context.Context, client *heliaClient, record heliaMachineRecord, exists bool, revision int64) (int64, error) {
	record = normalizeHeliaMachineRecord(record, record.MachineID)
	payload, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return 0, err
	}
	var expected *int64
	if exists {
		expected = &revision
	}
	put, err := client.putObject(ctx, heliaMachineKind, record.MachineID, payload, "application/json", map[string]interface{}{
		"machine_id":          record.MachineID,
		"owner_operator":      record.OwnerOperator,
		"can_control_others":  record.Capabilities.CanControlOthers,
		"can_be_controlled":   record.Capabilities.CanBeControlled,
		"allowed_operators_n": len(record.ACL.AllowedOperators),
	}, expected)
	if err != nil {
		return 0, err
	}
	if put.Result.Object.LatestRevision > 0 {
		return put.Result.Object.LatestRevision, nil
	}
	return put.Result.Revision.Revision, nil
}

func heliaMachineLoadJob(ctx context.Context, client *heliaClient, jobName string) (heliaMachineJob, int64, bool, error) {
	name := strings.TrimSpace(jobName)
	if name == "" {
		return heliaMachineJob{}, 0, false, fmt.Errorf("job name is required")
	}
	meta, err := heliaLookupObjectMeta(ctx, client, heliaMachineJobKind, name)
	if err != nil {
		return heliaMachineJob{}, 0, false, err
	}
	if meta == nil {
		return heliaMachineJob{}, 0, false, nil
	}
	payload, err := client.getPayload(ctx, heliaMachineJobKind, name)
	if err != nil {
		return heliaMachineJob{}, 0, true, err
	}
	var out heliaMachineJob
	if err := json.Unmarshal(payload, &out); err != nil {
		return heliaMachineJob{}, 0, true, fmt.Errorf("machine job payload invalid: %w", err)
	}
	out = normalizeHeliaMachineJob(out, "")
	return out, meta.LatestRevision, true, nil
}

func heliaMachinePersistJob(ctx context.Context, client *heliaClient, jobName string, job heliaMachineJob, exists bool, revision int64) (int64, error) {
	job = normalizeHeliaMachineJob(job, "")
	payload, err := json.MarshalIndent(job, "", "  ")
	if err != nil {
		return 0, err
	}
	var expected *int64
	if exists {
		expected = &revision
	}
	put, err := client.putObject(ctx, heliaMachineJobKind, strings.TrimSpace(jobName), payload, "application/json", map[string]interface{}{
		"machine_id":   job.MachineID,
		"job_id":       job.JobID,
		"status":       job.Status,
		"requested_by": job.RequestedBy,
	}, expected)
	if err != nil {
		return 0, err
	}
	if put.Result.Object.LatestRevision > 0 {
		return put.Result.Object.LatestRevision, nil
	}
	return put.Result.Revision.Revision, nil
}

func normalizeHeliaMachineRecord(record heliaMachineRecord, fallbackID string) heliaMachineRecord {
	record.Version = max(1, record.Version)
	record.MachineID = sanitizeMachineID(firstNonEmpty(strings.TrimSpace(record.MachineID), strings.TrimSpace(fallbackID)))
	record.OwnerOperator = sanitizeOperatorID(strings.TrimSpace(record.OwnerOperator))
	record.DisplayName = strings.TrimSpace(record.DisplayName)
	record.ACL.AllowedOperators = normalizeMachineOperatorIDs(record.ACL.AllowedOperators)
	if record.OwnerOperator != "" {
		record.ACL.AllowedOperators = normalizeMachineOperatorIDs(append(record.ACL.AllowedOperators, record.OwnerOperator))
	}
	return record
}

func normalizeHeliaMachineJob(job heliaMachineJob, fallbackMachine string) heliaMachineJob {
	job.Version = max(1, job.Version)
	job.JobID = strings.TrimSpace(job.JobID)
	job.MachineID = sanitizeMachineID(firstNonEmpty(strings.TrimSpace(job.MachineID), strings.TrimSpace(fallbackMachine)))
	job.RequestedBy = sanitizeOperatorID(strings.TrimSpace(job.RequestedBy))
	job.SourceMachine = sanitizeMachineID(strings.TrimSpace(job.SourceMachine))
	job.Status = normalizeMachineJobStatus(job.Status)
	job.TimeoutSeconds = max(10, job.TimeoutSeconds)
	cleanArgs := make([]string, 0, len(job.Command))
	for _, arg := range job.Command {
		trimmed := strings.TrimSpace(arg)
		if trimmed == "" {
			continue
		}
		cleanArgs = append(cleanArgs, trimmed)
	}
	job.Command = cleanArgs
	return job
}

func normalizeMachineOperatorIDs(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		item := sanitizeOperatorID(strings.TrimSpace(value))
		if item == "" {
			continue
		}
		key := strings.ToLower(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func heliaMachineResolveID(settings Settings, explicit string) string {
	if trimmed := sanitizeMachineID(strings.TrimSpace(explicit)); trimmed != "" {
		return trimmed
	}
	if trimmed := sanitizeMachineID(envSunMachineID()); trimmed != "" {
		return trimmed
	}
	if trimmed := sanitizeMachineID(strings.TrimSpace(settings.Helia.MachineID)); trimmed != "" {
		return trimmed
	}
	host, _ := os.Hostname()
	if trimmed := sanitizeMachineID(strings.TrimSpace(host)); trimmed != "" {
		return trimmed
	}
	return "machine-unknown"
}

func heliaMachineResolveOperatorID(settings Settings, explicit string, machineID string) string {
	if trimmed := sanitizeOperatorID(strings.TrimSpace(explicit)); trimmed != "" {
		return trimmed
	}
	if trimmed := sanitizeOperatorID(envSunOperatorID()); trimmed != "" {
		return trimmed
	}
	if trimmed := sanitizeOperatorID(strings.TrimSpace(settings.Helia.OperatorID)); trimmed != "" {
		return trimmed
	}
	userName := strings.TrimSpace(firstNonEmpty(os.Getenv("USER"), os.Getenv("USERNAME")))
	if userName == "" {
		userName = "user"
	}
	userName = sanitizeMachineID(userName)
	return sanitizeOperatorID("op:" + userName + "@" + sanitizeMachineID(machineID))
}

func heliaMachineOperatorAllowed(record heliaMachineRecord, operatorID string) bool {
	operatorID = strings.TrimSpace(operatorID)
	if operatorID == "" {
		return false
	}
	if strings.EqualFold(operatorID, strings.TrimSpace(record.OwnerOperator)) {
		return true
	}
	for _, allowed := range record.ACL.AllowedOperators {
		if strings.EqualFold(strings.TrimSpace(allowed), operatorID) {
			return true
		}
	}
	return false
}

func sanitizeMachineID(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}
	var b strings.Builder
	for _, ch := range raw {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
			b.WriteRune(ch)
			continue
		}
		if ch == '-' || ch == '_' || ch == '.' {
			b.WriteRune(ch)
			continue
		}
		b.WriteByte('-')
	}
	return strings.Trim(b.String(), "-")
}

func sanitizeOperatorID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var b strings.Builder
	for _, ch := range raw {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') {
			b.WriteRune(ch)
			continue
		}
		switch ch {
		case '-', '_', '.', ':', '@':
			b.WriteRune(ch)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.TrimSpace(strings.Trim(b.String(), "-"))
	return out
}

func heliaMachineJobID(now time.Time) string {
	prefix := "job-" + now.UTC().Format("20060102-150405")
	n, err := secureIntn(36 * 36 * 36)
	if err != nil {
		n = int(now.UnixNano() % (36 * 36 * 36))
	}
	suffix := strings.ToLower(strconv.FormatInt(int64(n), 36))
	if len(suffix) < 3 {
		suffix = strings.Repeat("0", 3-len(suffix)) + suffix
	}
	return prefix + "-" + suffix
}

func heliaMachineJobObjectName(machineID string, jobID string) string {
	return heliaMachineJobNamePrefix(machineID) + strings.TrimSpace(jobID)
}

func heliaMachineJobNamePrefix(machineID string) string {
	return strings.TrimSpace(sanitizeMachineID(machineID)) + "--"
}

func heliaMachineIsTerminalStatus(status string) bool {
	switch normalizeMachineJobStatus(status) {
	case heliaMachineJobStatusSucceeded, heliaMachineJobStatusFailed, heliaMachineJobStatusDenied:
		return true
	default:
		return false
	}
}

func heliaMachineJobFailureError(job heliaMachineJob) error {
	status := normalizeMachineJobStatus(job.Status)
	if status == heliaMachineJobStatusSucceeded {
		return nil
	}
	if status == "" {
		status = strings.ToLower(strings.TrimSpace(job.Status))
	}
	if status == "" {
		status = "unknown"
	}
	jobID := strings.TrimSpace(job.JobID)
	if jobID == "" {
		jobID = "unknown-job"
	}
	if details := strings.TrimSpace(job.Error); details != "" {
		return fmt.Errorf("remote job %s finished with status %s: %s", jobID, status, details)
	}
	if job.ExitCode != 0 {
		return fmt.Errorf("remote job %s finished with status %s (exit code %d)", jobID, status, job.ExitCode)
	}
	return fmt.Errorf("remote job %s finished with status %s", jobID, status)
}

func normalizeMachineJobStatus(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "queued", "pending":
		return heliaMachineJobStatusQueued
	case "running", "claimed":
		return heliaMachineJobStatusRunning
	case "succeeded", "success", "ok":
		return heliaMachineJobStatusSucceeded
	case "failed", "error":
		return heliaMachineJobStatusFailed
	case "denied", "forbidden":
		return heliaMachineJobStatusDenied
	default:
		return ""
	}
}

func truncateMachineOutput(raw string) string {
	if len(raw) <= heliaMachineOutputMaxBytes {
		return raw
	}
	return raw[:heliaMachineOutputMaxBytes] + "\n[truncated]"
}

func printHeliaMachineRecord(record heliaMachineRecord) {
	fmt.Printf("%s %s\n", styleHeading("machine:"), record.MachineID)
	fmt.Printf("%s %s\n", styleHeading("owner:"), record.OwnerOperator)
	fmt.Printf("%s %s\n", styleHeading("display_name:"), firstNonEmpty(record.DisplayName, "-"))
	fmt.Printf("%s %s\n", styleHeading("can_control_others:"), boolString(record.Capabilities.CanControlOthers))
	fmt.Printf("%s %s\n", styleHeading("can_be_controlled:"), boolString(record.Capabilities.CanBeControlled))
	fmt.Printf("%s %s\n", styleHeading("allowed_operators:"), strings.Join(record.ACL.AllowedOperators, ","))
	fmt.Printf("%s %s\n", styleHeading("registered_at:"), firstNonEmpty(record.RegisteredAt, "-"))
	fmt.Printf("%s %s\n", styleHeading("last_seen:"), firstNonEmpty(record.Heartbeat.LastSeenAt, "-"))
}

func printHeliaMachineJob(job heliaMachineJob) {
	fmt.Printf("%s %s\n", styleHeading("job_id:"), job.JobID)
	fmt.Printf("%s %s\n", styleHeading("machine:"), job.MachineID)
	fmt.Printf("%s %s\n", styleHeading("status:"), job.Status)
	fmt.Printf("%s %s\n", styleHeading("requested_by:"), job.RequestedBy)
	fmt.Printf("%s %s\n", styleHeading("command:"), strings.Join(job.Command, " "))
	fmt.Printf("%s %d\n", styleHeading("exit_code:"), job.ExitCode)
	if strings.TrimSpace(job.Error) != "" {
		fmt.Printf("%s %s\n", styleHeading("error:"), job.Error)
	}
	if strings.TrimSpace(job.Stdout) != "" {
		fmt.Printf("%s\n%s\n", styleHeading("stdout:"), strings.TrimSpace(job.Stdout))
	}
	if strings.TrimSpace(job.Stderr) != "" {
		fmt.Printf("%s\n%s\n", styleHeading("stderr:"), strings.TrimSpace(job.Stderr))
	}
}
