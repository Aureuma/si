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
	sunMachineUsageText = "usage: si sun machine <register|status|list|allow|deny|run|jobs|serve> ..."

	sunMachineKind    = "si_machine"
	sunMachineJobKind = "si_machine_job"

	sunMachineJobStatusQueued    = "queued"
	sunMachineJobStatusRunning   = "running"
	sunMachineJobStatusSucceeded = "succeeded"
	sunMachineJobStatusFailed    = "failed"
	sunMachineJobStatusDenied    = "denied"

	sunMachineOutputMaxBytes = 64 * 1024
)

type sunMachineRecord struct {
	Version       int                     `json:"version"`
	MachineID     string                  `json:"machine_id"`
	DisplayName   string                  `json:"display_name,omitempty"`
	OwnerOperator string                  `json:"owner_operator"`
	UpdatedAt     string                  `json:"updated_at,omitempty"`
	RegisteredAt  string                  `json:"registered_at,omitempty"`
	Capabilities  sunMachineCapabilities  `json:"capabilities"`
	ACL           sunMachineAccessControl `json:"acl"`
	Heartbeat     sunMachineHeartbeat     `json:"heartbeat"`
}

type sunMachineCapabilities struct {
	CanControlOthers bool `json:"can_control_others"`
	CanBeControlled  bool `json:"can_be_controlled"`
}

type sunMachineAccessControl struct {
	AllowedOperators []string `json:"allowed_operators,omitempty"`
}

type sunMachineHeartbeat struct {
	LastSeenAt string `json:"last_seen_at,omitempty"`
	LastState  string `json:"last_state,omitempty"`
}

type sunMachineJob struct {
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

type sunMachineServeSummary struct {
	MachineID string   `json:"machine_id"`
	Processed int      `json:"processed"`
	JobIDs    []string `json:"job_ids,omitempty"`
}

func cmdSunMachine(args []string) {
	if len(args) == 0 {
		printUsage(sunMachineUsageText)
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "help", "-h", "--help":
		printUsage(sunMachineUsageText)
	case "register":
		cmdSunMachineRegister(rest)
	case "status":
		cmdSunMachineStatus(rest)
	case "list":
		cmdSunMachineList(rest)
	case "allow":
		cmdSunMachineAllow(rest)
	case "deny":
		cmdSunMachineDeny(rest)
	case "run", "exec":
		cmdSunMachineRun(rest)
	case "jobs":
		cmdSunMachineJobs(rest)
	case "serve":
		cmdSunMachineServe(rest)
	default:
		printUnknown("sun machine", sub)
		printUsage(sunMachineUsageText)
		os.Exit(1)
	}
}

func cmdSunMachineRegister(args []string) {
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
	client, err := sunClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	ctx := sunContext(settings)
	resolvedMachine := sunMachineResolveID(settings, strings.TrimSpace(*machineID))
	resolvedOperator := sunMachineResolveOperatorID(settings, strings.TrimSpace(*operatorID), resolvedMachine)
	if resolvedMachine == "" {
		fatal(fmt.Errorf("machine id is required"))
	}
	if resolvedOperator == "" {
		fatal(fmt.Errorf("operator id is required"))
	}

	record, revision, exists, err := sunMachineLoad(ctx, client, resolvedMachine)
	if err != nil {
		fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if !exists {
		record = sunMachineRecord{
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

	newRevision, err := sunMachinePersist(ctx, client, record, exists, revision)
	if err != nil {
		fatal(err)
	}
	if *setDefaults {
		current, loadErr := loadSettings()
		if loadErr != nil {
			fatal(loadErr)
		}
		current.Sun.MachineID = resolvedMachine
		current.Sun.OperatorID = resolvedOperator
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

func cmdSunMachineStatus(args []string) {
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
	client, err := sunClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	resolvedMachine := sunMachineResolveID(settings, strings.TrimSpace(*machineID))
	record, _, exists, err := sunMachineLoad(sunContext(settings), client, resolvedMachine)
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
	printSunMachineRecord(record)
}

func cmdSunMachineList(args []string) {
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
	client, err := sunClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	items, err := client.listObjects(sunContext(settings), sunMachineKind, "", *limit)
	if err != nil {
		fatal(err)
	}
	rows := make([]sunMachineRecord, 0, len(items))
	for i := range items {
		record, _, exists, loadErr := sunMachineLoad(sunContext(settings), client, strings.TrimSpace(items[i].Name))
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

func cmdSunMachineAllow(args []string) {
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
	targetMachine := sunMachineResolveID(settings, strings.TrimSpace(*machineID))
	if strings.TrimSpace(*grantOperator) == "" {
		fatal(fmt.Errorf("--grant is required"))
	}
	caller := sunMachineResolveOperatorID(settings, strings.TrimSpace(*asOperator), sunMachineResolveID(settings, ""))
	client, err := sunClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	record, revision, exists, err := sunMachineLoad(sunContext(settings), client, targetMachine)
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
	newRevision, err := sunMachinePersist(sunContext(settings), client, record, true, revision)
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

func cmdSunMachineDeny(args []string) {
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
	targetMachine := sunMachineResolveID(settings, strings.TrimSpace(*machineID))
	revoke := strings.TrimSpace(*revokeOperator)
	if revoke == "" {
		fatal(fmt.Errorf("--revoke is required"))
	}
	caller := sunMachineResolveOperatorID(settings, strings.TrimSpace(*asOperator), sunMachineResolveID(settings, ""))
	client, err := sunClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	record, revision, exists, err := sunMachineLoad(sunContext(settings), client, targetMachine)
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
	newRevision, err := sunMachinePersist(sunContext(settings), client, record, true, revision)
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

func cmdSunMachineRun(args []string) {
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
	targetMachine := sunMachineResolveID(settings, strings.TrimSpace(*targetMachineFlag))
	sourceMachine := sunMachineResolveID(settings, strings.TrimSpace(*sourceMachineFlag))
	operatorID := sunMachineResolveOperatorID(settings, strings.TrimSpace(*operatorFlag), sourceMachine)

	client, err := sunClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	ctx := sunContext(settings)
	targetRecord, _, exists, err := sunMachineLoad(ctx, client, targetMachine)
	if err != nil {
		fatal(err)
	}
	if !exists {
		fatal(fmt.Errorf("target machine %q is not registered", targetMachine))
	}
	if !targetRecord.Capabilities.CanBeControlled {
		fatal(fmt.Errorf("target machine %q does not accept remote control (can_be_controlled=false)", targetMachine))
	}
	if !sunMachineOperatorAllowed(targetRecord, operatorID) {
		fatal(fmt.Errorf("operator %q is not allowed to control machine %q", operatorID, targetMachine))
	}
	sourceRecord, _, sourceExists, err := sunMachineLoad(ctx, client, sourceMachine)
	if err != nil {
		fatal(err)
	}
	if !sourceExists {
		fatal(fmt.Errorf("source machine %q is not registered; run `si sun machine register --machine %s --can-control-others` first", sourceMachine, sourceMachine))
	}
	if !sunMachineOperatorAllowed(sourceRecord, operatorID) {
		fatal(fmt.Errorf("operator %q is not allowed on source machine %q", operatorID, sourceMachine))
	}
	if !sourceRecord.Capabilities.CanControlOthers {
		fatal(fmt.Errorf("source machine %q cannot control other machines (can_control_others=false)", sourceMachine))
	}

	now := time.Now().UTC()
	jobID := sunMachineJobID(now)
	job := sunMachineJob{
		Version:        1,
		JobID:          jobID,
		MachineID:      targetMachine,
		RequestedBy:    operatorID,
		SourceMachine:  sourceMachine,
		Command:        append([]string(nil), commandArgs...),
		TimeoutSeconds: max(10, *timeoutSeconds),
		Status:         sunMachineJobStatusQueued,
		RequestedAt:    now.Format(time.RFC3339),
		UpdatedAt:      now.Format(time.RFC3339),
	}
	jobName := sunMachineJobObjectName(targetMachine, jobID)
	if _, err := sunMachinePersistJob(ctx, client, jobName, job, false, 0); err != nil {
		fatal(err)
	}

	if *wait {
		waitCtx, cancel := context.WithTimeout(ctx, time.Duration(max(10, *waitTimeoutSeconds))*time.Second)
		defer cancel()
		doneJob, _, waitErr := sunMachineWaitForJob(waitCtx, client, jobName, max(1, *pollSeconds))
		if waitErr != nil {
			fatal(waitErr)
		}
		if *jsonOut {
			printJSON(doneJob)
		} else {
			printSunMachineJob(doneJob)
		}
		if statusErr := sunMachineJobFailureError(doneJob); statusErr != nil {
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

func cmdSunMachineJobs(args []string) {
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
	client, err := sunClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	items, err := client.listObjects(sunContext(settings), sunMachineJobKind, "", *limit)
	if err != nil {
		fatal(err)
	}
	rows := make([]sunMachineJob, 0, len(items))
	for i := range items {
		job, _, exists, loadErr := sunMachineLoadJob(sunContext(settings), client, strings.TrimSpace(items[i].Name))
		if loadErr != nil || !exists {
			continue
		}
		if strings.TrimSpace(*machineID) != "" && !strings.EqualFold(strings.TrimSpace(job.MachineID), sunMachineResolveID(settings, strings.TrimSpace(*machineID))) {
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

func cmdSunMachineServe(args []string) {
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
	client, err := sunClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	ctx := sunContext(settings)
	resolvedMachine := sunMachineResolveID(settings, strings.TrimSpace(*machineID))
	localOperator := sunMachineResolveOperatorID(settings, strings.TrimSpace(*operatorID), resolvedMachine)

	record, machineRevision, exists, err := sunMachineLoad(ctx, client, resolvedMachine)
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
	if _, err := sunMachinePersist(ctx, client, record, true, machineRevision); err != nil {
		fatal(err)
	}

	summary := sunMachineServeSummary{MachineID: resolvedMachine}
	for {
		jobName, job, jobRevision, found, claimErr := sunMachineClaimNextJob(ctx, client, resolvedMachine)
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
		finished := sunMachineRunClaimedJob(job, record, localOperator)
		if _, err := sunMachinePersistJob(ctx, client, jobName, finished, true, jobRevision); err != nil {
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

func sunMachineRunClaimedJob(job sunMachineJob, machine sunMachineRecord, localOperator string) sunMachineJob {
	now := time.Now().UTC()
	job.Status = sunMachineJobStatusRunning
	job.UpdatedAt = now.Format(time.RFC3339)
	if strings.TrimSpace(job.StartedAt) == "" {
		job.StartedAt = now.Format(time.RFC3339)
	}
	if !sunMachineOperatorAllowed(machine, job.RequestedBy) {
		job.Status = sunMachineJobStatusDenied
		job.CompletedAt = time.Now().UTC().Format(time.RFC3339)
		job.UpdatedAt = job.CompletedAt
		job.ExitCode = 1
		job.Error = fmt.Sprintf("operator %q is not allowed by machine %q ACL", strings.TrimSpace(job.RequestedBy), strings.TrimSpace(machine.MachineID))
		return job
	}
	if !machine.Capabilities.CanBeControlled {
		job.Status = sunMachineJobStatusDenied
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
		job.Status = sunMachineJobStatusFailed
		if runErr != nil {
			job.Error = strings.TrimSpace(runErr.Error())
		} else {
			job.Error = fmt.Sprintf("command exited with code %d", exitCode)
		}
		return job
	}
	job.Status = sunMachineJobStatusSucceeded
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

func sunMachineClaimNextJob(ctx context.Context, client *sunClient, machineID string) (string, sunMachineJob, int64, bool, error) {
	items, err := client.listObjects(ctx, sunMachineJobKind, "", 200)
	if err != nil {
		return "", sunMachineJob{}, 0, false, err
	}
	type candidate struct {
		name string
		job  sunMachineJob
	}
	candidates := make([]candidate, 0, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		if !strings.HasPrefix(strings.ToLower(name), strings.ToLower(sunMachineJobNamePrefix(machineID))) {
			continue
		}
		job, _, exists, loadErr := sunMachineLoadJob(ctx, client, name)
		if loadErr != nil || !exists {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(job.Status), sunMachineJobStatusQueued) {
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
		job, revision, exists, loadErr := sunMachineLoadJob(ctx, client, cand.name)
		if loadErr != nil || !exists {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(job.Status), sunMachineJobStatusQueued) {
			continue
		}
		now := time.Now().UTC().Format(time.RFC3339)
		job.Status = sunMachineJobStatusRunning
		job.ClaimedBy = machineID
		job.ClaimedAt = now
		job.StartedAt = now
		job.UpdatedAt = now
		newRevision, persistErr := sunMachinePersistJob(ctx, client, cand.name, job, true, revision)
		if persistErr != nil {
			if isSunStatus(persistErr, 409) {
				continue
			}
			return "", sunMachineJob{}, 0, false, persistErr
		}
		return cand.name, job, newRevision, true, nil
	}
	return "", sunMachineJob{}, 0, false, nil
}

func sunMachineWaitForJob(ctx context.Context, client *sunClient, jobName string, pollSeconds int) (sunMachineJob, int64, error) {
	interval := time.Duration(max(1, pollSeconds)) * time.Second
	for {
		job, revision, exists, err := sunMachineLoadJob(ctx, client, jobName)
		if err != nil {
			return sunMachineJob{}, 0, err
		}
		if !exists {
			return sunMachineJob{}, 0, fmt.Errorf("remote job %q not found", strings.TrimSpace(jobName))
		}
		if sunMachineIsTerminalStatus(job.Status) {
			return job, revision, nil
		}
		select {
		case <-ctx.Done():
			return sunMachineJob{}, 0, fmt.Errorf("timed out waiting for remote job %q", strings.TrimSpace(job.JobID))
		case <-time.After(interval):
		}
	}
}

func sunMachineLoad(ctx context.Context, client *sunClient, machineID string) (sunMachineRecord, int64, bool, error) {
	id := strings.TrimSpace(machineID)
	if id == "" {
		return sunMachineRecord{}, 0, false, fmt.Errorf("machine id is required")
	}
	meta, err := sunLookupObjectMeta(ctx, client, sunMachineKind, id)
	if err != nil {
		return sunMachineRecord{}, 0, false, err
	}
	if meta == nil {
		return sunMachineRecord{}, 0, false, nil
	}
	payload, err := client.getPayload(ctx, sunMachineKind, id)
	if err != nil {
		return sunMachineRecord{}, 0, true, err
	}
	var out sunMachineRecord
	if err := json.Unmarshal(payload, &out); err != nil {
		return sunMachineRecord{}, 0, true, fmt.Errorf("machine payload invalid: %w", err)
	}
	out = normalizeSunMachineRecord(out, id)
	return out, meta.LatestRevision, true, nil
}

func sunMachinePersist(ctx context.Context, client *sunClient, record sunMachineRecord, exists bool, revision int64) (int64, error) {
	record = normalizeSunMachineRecord(record, record.MachineID)
	payload, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return 0, err
	}
	var expected *int64
	if exists {
		expected = &revision
	}
	put, err := client.putObject(ctx, sunMachineKind, record.MachineID, payload, "application/json", map[string]interface{}{
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

func sunMachineLoadJob(ctx context.Context, client *sunClient, jobName string) (sunMachineJob, int64, bool, error) {
	name := strings.TrimSpace(jobName)
	if name == "" {
		return sunMachineJob{}, 0, false, fmt.Errorf("job name is required")
	}
	meta, err := sunLookupObjectMeta(ctx, client, sunMachineJobKind, name)
	if err != nil {
		return sunMachineJob{}, 0, false, err
	}
	if meta == nil {
		return sunMachineJob{}, 0, false, nil
	}
	payload, err := client.getPayload(ctx, sunMachineJobKind, name)
	if err != nil {
		return sunMachineJob{}, 0, true, err
	}
	var out sunMachineJob
	if err := json.Unmarshal(payload, &out); err != nil {
		return sunMachineJob{}, 0, true, fmt.Errorf("machine job payload invalid: %w", err)
	}
	out = normalizeSunMachineJob(out, "")
	return out, meta.LatestRevision, true, nil
}

func sunMachinePersistJob(ctx context.Context, client *sunClient, jobName string, job sunMachineJob, exists bool, revision int64) (int64, error) {
	job = normalizeSunMachineJob(job, "")
	payload, err := json.MarshalIndent(job, "", "  ")
	if err != nil {
		return 0, err
	}
	var expected *int64
	if exists {
		expected = &revision
	}
	put, err := client.putObject(ctx, sunMachineJobKind, strings.TrimSpace(jobName), payload, "application/json", map[string]interface{}{
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

func normalizeSunMachineRecord(record sunMachineRecord, fallbackID string) sunMachineRecord {
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

func normalizeSunMachineJob(job sunMachineJob, fallbackMachine string) sunMachineJob {
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

func sunMachineResolveID(settings Settings, explicit string) string {
	if trimmed := sanitizeMachineID(strings.TrimSpace(explicit)); trimmed != "" {
		return trimmed
	}
	if trimmed := sanitizeMachineID(envSunMachineID()); trimmed != "" {
		return trimmed
	}
	if trimmed := sanitizeMachineID(strings.TrimSpace(settings.Sun.MachineID)); trimmed != "" {
		return trimmed
	}
	host, _ := os.Hostname()
	if trimmed := sanitizeMachineID(strings.TrimSpace(host)); trimmed != "" {
		return trimmed
	}
	return "machine-unknown"
}

func sunMachineResolveOperatorID(settings Settings, explicit string, machineID string) string {
	if trimmed := sanitizeOperatorID(strings.TrimSpace(explicit)); trimmed != "" {
		return trimmed
	}
	if trimmed := sanitizeOperatorID(envSunOperatorID()); trimmed != "" {
		return trimmed
	}
	if trimmed := sanitizeOperatorID(strings.TrimSpace(settings.Sun.OperatorID)); trimmed != "" {
		return trimmed
	}
	userName := strings.TrimSpace(firstNonEmpty(os.Getenv("USER"), os.Getenv("USERNAME")))
	if userName == "" {
		userName = "user"
	}
	userName = sanitizeMachineID(userName)
	return sanitizeOperatorID("op:" + userName + "@" + sanitizeMachineID(machineID))
}

func sunMachineOperatorAllowed(record sunMachineRecord, operatorID string) bool {
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

func sunMachineJobID(now time.Time) string {
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

func sunMachineJobObjectName(machineID string, jobID string) string {
	return sunMachineJobNamePrefix(machineID) + strings.TrimSpace(jobID)
}

func sunMachineJobNamePrefix(machineID string) string {
	return strings.TrimSpace(sanitizeMachineID(machineID)) + "--"
}

func sunMachineIsTerminalStatus(status string) bool {
	switch normalizeMachineJobStatus(status) {
	case sunMachineJobStatusSucceeded, sunMachineJobStatusFailed, sunMachineJobStatusDenied:
		return true
	default:
		return false
	}
}

func sunMachineJobFailureError(job sunMachineJob) error {
	status := normalizeMachineJobStatus(job.Status)
	if status == sunMachineJobStatusSucceeded {
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
		return sunMachineJobStatusQueued
	case "running", "claimed":
		return sunMachineJobStatusRunning
	case "succeeded", "success", "ok":
		return sunMachineJobStatusSucceeded
	case "failed", "error":
		return sunMachineJobStatusFailed
	case "denied", "forbidden":
		return sunMachineJobStatusDenied
	default:
		return ""
	}
}

func truncateMachineOutput(raw string) string {
	if len(raw) <= sunMachineOutputMaxBytes {
		return raw
	}
	return raw[:sunMachineOutputMaxBytes] + "\n[truncated]"
}

func printSunMachineRecord(record sunMachineRecord) {
	fmt.Printf("%s %s\n", styleHeading("machine:"), record.MachineID)
	fmt.Printf("%s %s\n", styleHeading("owner:"), record.OwnerOperator)
	fmt.Printf("%s %s\n", styleHeading("display_name:"), firstNonEmpty(record.DisplayName, "-"))
	fmt.Printf("%s %s\n", styleHeading("can_control_others:"), boolString(record.Capabilities.CanControlOthers))
	fmt.Printf("%s %s\n", styleHeading("can_be_controlled:"), boolString(record.Capabilities.CanBeControlled))
	fmt.Printf("%s %s\n", styleHeading("allowed_operators:"), strings.Join(record.ACL.AllowedOperators, ","))
	fmt.Printf("%s %s\n", styleHeading("registered_at:"), firstNonEmpty(record.RegisteredAt, "-"))
	fmt.Printf("%s %s\n", styleHeading("last_seen:"), firstNonEmpty(record.Heartbeat.LastSeenAt, "-"))
}

func printSunMachineJob(job sunMachineJob) {
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
