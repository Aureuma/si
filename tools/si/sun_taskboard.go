package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	heliaTaskboardUsageText = "usage: si sun taskboard <use|show|list|add|claim|release|done> ..."
	heliaTaskboardKind      = "dyad_taskboard"

	heliaTaskStatusTodo  = "todo"
	heliaTaskStatusDoing = "doing"
	heliaTaskStatusDone  = "done"

	heliaTaskPriorityP1 = "P1"
	heliaTaskPriorityP2 = "P2"
	heliaTaskPriorityP3 = "P3"
)

type heliaTaskboard struct {
	Version   int                            `json:"version"`
	Name      string                         `json:"name"`
	UpdatedAt string                         `json:"updated_at,omitempty"`
	Tasks     []heliaTaskboardTask           `json:"tasks"`
	Agents    map[string]heliaTaskboardAgent `json:"agents,omitempty"`
}

type heliaTaskboardTask struct {
	ID          string              `json:"id"`
	Title       string              `json:"title"`
	Prompt      string              `json:"prompt"`
	Status      string              `json:"status"`
	Priority    string              `json:"priority"`
	Tags        []string            `json:"tags,omitempty"`
	CreatedAt   string              `json:"created_at,omitempty"`
	UpdatedAt   string              `json:"updated_at,omitempty"`
	CompletedAt string              `json:"completed_at,omitempty"`
	Result      string              `json:"result,omitempty"`
	Assignment  *heliaTaskboardLock `json:"assignment,omitempty"`
}

type heliaTaskboardLock struct {
	AgentID        string `json:"agent_id"`
	Dyad           string `json:"dyad,omitempty"`
	Machine        string `json:"machine,omitempty"`
	User           string `json:"user,omitempty"`
	LockToken      string `json:"lock_token,omitempty"`
	ClaimedAt      string `json:"claimed_at,omitempty"`
	LeaseSeconds   int    `json:"lease_seconds,omitempty"`
	LeaseExpiresAt string `json:"lease_expires_at,omitempty"`
}

type heliaTaskboardAgent struct {
	ID            string `json:"id"`
	Dyad          string `json:"dyad,omitempty"`
	Machine       string `json:"machine,omitempty"`
	User          string `json:"user,omitempty"`
	Status        string `json:"status,omitempty"`
	CurrentTaskID string `json:"current_task_id,omitempty"`
	LastSeenAt    string `json:"last_seen_at,omitempty"`
}

type heliaTaskboardAgentIdentity struct {
	AgentID string
	Dyad    string
	Machine string
	User    string
}

type heliaTaskboardClaimRequest struct {
	TaskID       string
	Agent        heliaTaskboardAgentIdentity
	LeaseSeconds int
}

type heliaTaskboardClaimResult struct {
	BoardName string             `json:"board_name"`
	Task      heliaTaskboardTask `json:"task"`
}

func cmdHeliaTaskboard(args []string) {
	if len(args) == 0 {
		printUsage(heliaTaskboardUsageText)
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "help", "-h", "--help":
		printUsage(heliaTaskboardUsageText)
	case "use":
		cmdHeliaTaskboardUse(rest)
	case "show":
		cmdHeliaTaskboardShow(rest)
	case "list":
		cmdHeliaTaskboardList(rest)
	case "add":
		cmdHeliaTaskboardAdd(rest)
	case "claim":
		cmdHeliaTaskboardClaim(rest)
	case "release":
		cmdHeliaTaskboardRelease(rest)
	case "done":
		cmdHeliaTaskboardDone(rest)
	default:
		printUnknown("sun taskboard", sub)
		printUsage(heliaTaskboardUsageText)
		os.Exit(1)
	}
}

func cmdHeliaTaskboardUse(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("sun taskboard use", flag.ExitOnError)
	name := fs.String("name", strings.TrimSpace(settings.Helia.Taskboard), "default taskboard object name")
	agent := fs.String("agent", strings.TrimSpace(settings.Helia.TaskboardAgent), "default agent id")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		*name = strings.TrimSpace(fs.Arg(0))
	}
	if strings.TrimSpace(*name) == "" {
		fatal(fmt.Errorf("taskboard name required (--name or helia.taskboard)"))
	}
	current, err := loadSettings()
	if err != nil {
		fatal(err)
	}
	current.Helia.Taskboard = strings.TrimSpace(*name)
	if strings.TrimSpace(*agent) != "" {
		current.Helia.TaskboardAgent = strings.TrimSpace(*agent)
	}
	if err := saveSettings(current); err != nil {
		fatal(err)
	}
	successf("sun taskboard default set to %s", strings.TrimSpace(*name))
	if strings.TrimSpace(*agent) != "" {
		successf("sun taskboard default agent set to %s", strings.TrimSpace(*agent))
	}
}

func cmdHeliaTaskboardShow(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("sun taskboard show", flag.ExitOnError)
	boardName := fs.String("name", "", "taskboard object name")
	jsonOut := fs.Bool("json", false, "json output")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si sun taskboard show [--name <name>] [--json]")
		return
	}
	client, err := heliaClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	targetBoard := heliaTaskboardName(settings, strings.TrimSpace(*boardName))
	board, _, _, err := heliaTaskboardLoad(heliaContext(settings), client, targetBoard)
	if err != nil {
		fatal(err)
	}
	if *jsonOut {
		printJSON(board)
		return
	}
	todo, doing, done := heliaTaskboardStatusCounts(board.Tasks)
	fmt.Printf("%s %s\n", styleHeading("board:"), board.Name)
	fmt.Printf("%s %d (todo=%d doing=%d done=%d)\n", styleHeading("tasks:"), len(board.Tasks), todo, doing, done)
	if strings.TrimSpace(board.UpdatedAt) != "" {
		fmt.Printf("%s %s\n", styleHeading("updated_at:"), board.UpdatedAt)
	}
	if len(board.Tasks) == 0 {
		infof("no tasks on board")
		return
	}
	tasks := append([]heliaTaskboardTask(nil), board.Tasks...)
	heliaSortTasks(tasks)
	printHeliaTaskRows(tasks)
}

func cmdHeliaTaskboardList(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("sun taskboard list", flag.ExitOnError)
	boardName := fs.String("name", "", "taskboard object name")
	status := fs.String("status", "", "filter status: todo|doing|done")
	owner := fs.String("owner", "", "filter assigned agent id")
	limit := fs.Int("limit", 50, "max tasks")
	jsonOut := fs.Bool("json", false, "json output")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si sun taskboard list [--name <name>] [--status <todo|doing|done>] [--owner <agent>] [--limit <n>] [--json]")
		return
	}
	client, err := heliaClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	targetBoard := heliaTaskboardName(settings, strings.TrimSpace(*boardName))
	board, _, _, err := heliaTaskboardLoad(heliaContext(settings), client, targetBoard)
	if err != nil {
		fatal(err)
	}
	statusFilter, err := parseHeliaTaskStatusFilter(*status)
	if err != nil {
		fatal(err)
	}
	ownerFilter := strings.TrimSpace(*owner)
	filtered := make([]heliaTaskboardTask, 0, len(board.Tasks))
	for i := range board.Tasks {
		task := board.Tasks[i]
		if statusFilter != "" && task.Status != statusFilter {
			continue
		}
		if ownerFilter != "" {
			if task.Assignment == nil || !strings.EqualFold(strings.TrimSpace(task.Assignment.AgentID), ownerFilter) {
				continue
			}
		}
		filtered = append(filtered, task)
	}
	heliaSortTasks(filtered)
	if *limit > 0 && len(filtered) > *limit {
		filtered = filtered[:*limit]
	}
	if *jsonOut {
		printJSON(filtered)
		return
	}
	if len(filtered) == 0 {
		infof("no matching tasks")
		return
	}
	printHeliaTaskRows(filtered)
}

func cmdHeliaTaskboardAdd(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("sun taskboard add", flag.ExitOnError)
	boardName := fs.String("name", "", "taskboard object name")
	title := fs.String("title", "", "task title")
	prompt := fs.String("prompt", "", "task prompt used by dyad autopilot")
	priority := fs.String("priority", heliaTaskPriorityP2, "priority: P1|P2|P3")
	tagsCSV := fs.String("tags", "", "comma-separated tags")
	jsonOut := fs.Bool("json", false, "json output")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si sun taskboard add --title <text> [--prompt <text>] [--priority <P1|P2|P3>] [--tags <csv>] [--name <name>] [--json]")
		return
	}
	taskTitle := strings.TrimSpace(*title)
	if taskTitle == "" {
		fatal(fmt.Errorf("--title is required"))
	}
	taskPrompt := strings.TrimSpace(*prompt)
	if taskPrompt == "" {
		taskPrompt = taskTitle
	}
	client, err := heliaClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	targetBoard := heliaTaskboardName(settings, strings.TrimSpace(*boardName))
	var created heliaTaskboardTask
	_, _, err = heliaTaskboardMutateWithRetry(heliaContext(settings), client, targetBoard, func(board *heliaTaskboard, now time.Time) error {
		id := heliaTaskID(now, board.Tasks)
		created = heliaTaskboardTask{
			ID:        id,
			Title:     taskTitle,
			Prompt:    taskPrompt,
			Status:    heliaTaskStatusTodo,
			Priority:  normalizeHeliaTaskPriority(*priority),
			Tags:      splitCSVScopes(*tagsCSV),
			CreatedAt: now.Format(time.RFC3339),
			UpdatedAt: now.Format(time.RFC3339),
		}
		board.Tasks = append(board.Tasks, created)
		return nil
	})
	if err != nil {
		fatal(err)
	}
	if *jsonOut {
		printJSON(created)
		return
	}
	successf("added task %s (%s)", created.ID, created.Title)
}

func cmdHeliaTaskboardClaim(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("sun taskboard claim", flag.ExitOnError)
	boardName := fs.String("name", "", "taskboard object name")
	id := fs.String("id", "", "specific task id")
	agent := fs.String("agent", "", "agent id (defaults to dyad@machine identity)")
	dyad := fs.String("dyad", "", "dyad name for agent identity")
	machine := fs.String("machine", "", "machine id for agent identity")
	leaseSeconds := fs.Int("lease-seconds", 0, "assignment lease in seconds")
	jsonOut := fs.Bool("json", false, "json output")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si sun taskboard claim [--id <task-id>] [--name <name>] [--agent <agent-id>] [--dyad <dyad>] [--lease-seconds <n>] [--json]")
		return
	}
	client, err := heliaClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	targetBoard := heliaTaskboardName(settings, strings.TrimSpace(*boardName))
	identity := heliaTaskboardResolveAgent(settings, strings.TrimSpace(*agent), strings.TrimSpace(*dyad), strings.TrimSpace(*machine))
	result, err := heliaTaskboardClaim(heliaContext(settings), client, targetBoard, heliaTaskboardClaimRequest{
		TaskID:       strings.TrimSpace(*id),
		Agent:        identity,
		LeaseSeconds: heliaTaskboardLeaseSeconds(settings, *leaseSeconds),
	})
	if err != nil {
		fatal(err)
	}
	if *jsonOut {
		printJSON(result)
		return
	}
	successf("claimed %s on board %s as %s", result.Task.ID, result.BoardName, identity.AgentID)
}

func cmdHeliaTaskboardRelease(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("sun taskboard release", flag.ExitOnError)
	boardName := fs.String("name", "", "taskboard object name")
	id := fs.String("id", "", "task id")
	agent := fs.String("agent", "", "agent id (defaults to dyad@machine identity)")
	dyad := fs.String("dyad", "", "dyad name for agent identity")
	machine := fs.String("machine", "", "machine id for agent identity")
	jsonOut := fs.Bool("json", false, "json output")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si sun taskboard release --id <task-id> [--name <name>] [--agent <agent-id>] [--dyad <dyad>] [--json]")
		return
	}
	taskID := strings.TrimSpace(*id)
	if taskID == "" {
		fatal(fmt.Errorf("--id is required"))
	}
	client, err := heliaClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	targetBoard := heliaTaskboardName(settings, strings.TrimSpace(*boardName))
	identity := heliaTaskboardResolveAgent(settings, strings.TrimSpace(*agent), strings.TrimSpace(*dyad), strings.TrimSpace(*machine))
	task, err := heliaTaskboardRelease(heliaContext(settings), client, targetBoard, taskID, identity)
	if err != nil {
		fatal(err)
	}
	if *jsonOut {
		printJSON(task)
		return
	}
	successf("released %s on board %s", task.ID, targetBoard)
}

func cmdHeliaTaskboardDone(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("sun taskboard done", flag.ExitOnError)
	boardName := fs.String("name", "", "taskboard object name")
	id := fs.String("id", "", "task id")
	resultText := fs.String("result", "", "completion notes")
	agent := fs.String("agent", "", "agent id (defaults to dyad@machine identity)")
	dyad := fs.String("dyad", "", "dyad name for agent identity")
	machine := fs.String("machine", "", "machine id for agent identity")
	jsonOut := fs.Bool("json", false, "json output")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si sun taskboard done --id <task-id> [--result <text>] [--name <name>] [--agent <agent-id>] [--dyad <dyad>] [--json]")
		return
	}
	taskID := strings.TrimSpace(*id)
	if taskID == "" {
		fatal(fmt.Errorf("--id is required"))
	}
	client, err := heliaClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	targetBoard := heliaTaskboardName(settings, strings.TrimSpace(*boardName))
	identity := heliaTaskboardResolveAgent(settings, strings.TrimSpace(*agent), strings.TrimSpace(*dyad), strings.TrimSpace(*machine))
	task, err := heliaTaskboardMarkDone(heliaContext(settings), client, targetBoard, taskID, identity, strings.TrimSpace(*resultText))
	if err != nil {
		fatal(err)
	}
	if *jsonOut {
		printJSON(task)
		return
	}
	successf("completed %s on board %s", task.ID, targetBoard)
}

func heliaAutopilotClaimTask(settings Settings, dyadName string) (heliaTaskboardClaimResult, error) {
	client, err := heliaClientFromSettings(settings)
	if err != nil {
		return heliaTaskboardClaimResult{}, err
	}
	identity := heliaTaskboardResolveAgent(settings, "", dyadName, "")
	return heliaTaskboardClaim(heliaContext(settings), client, heliaTaskboardName(settings, ""), heliaTaskboardClaimRequest{
		Agent:        identity,
		LeaseSeconds: heliaTaskboardLeaseSeconds(settings, 0),
	})
}

func heliaTaskboardClaim(ctx context.Context, client *heliaClient, boardName string, req heliaTaskboardClaimRequest) (heliaTaskboardClaimResult, error) {
	result := heliaTaskboardClaimResult{BoardName: strings.TrimSpace(boardName)}
	if strings.TrimSpace(req.Agent.AgentID) == "" {
		return result, fmt.Errorf("agent id required")
	}
	leaseSeconds := req.LeaseSeconds
	if leaseSeconds <= 0 {
		leaseSeconds = 1800
	}
	var claimed heliaTaskboardTask
	_, _, err := heliaTaskboardMutateWithRetry(ctx, client, boardName, func(board *heliaTaskboard, now time.Time) error {
		index := -1
		if strings.TrimSpace(req.TaskID) != "" {
			for i := range board.Tasks {
				if strings.EqualFold(strings.TrimSpace(board.Tasks[i].ID), strings.TrimSpace(req.TaskID)) {
					index = i
					break
				}
			}
			if index < 0 {
				return fmt.Errorf("task %q not found", strings.TrimSpace(req.TaskID))
			}
		} else {
			index = heliaTaskboardSelectNextClaimable(board.Tasks, now)
			if index < 0 {
				return fmt.Errorf("no claimable tasks available")
			}
		}
		task := board.Tasks[index]
		if task.Status == heliaTaskStatusDone {
			return fmt.Errorf("task %s is already done", task.ID)
		}
		if task.Assignment != nil {
			assignedTo := strings.TrimSpace(task.Assignment.AgentID)
			expired := heliaTaskboardLockExpired(*task.Assignment, now)
			if assignedTo != "" && !expired && !strings.EqualFold(assignedTo, req.Agent.AgentID) {
				return fmt.Errorf("task %s is locked by %s", task.ID, assignedTo)
			}
		}
		lockToken := heliaTaskboardLockToken(now)
		expires := now.Add(time.Duration(leaseSeconds) * time.Second).UTC().Format(time.RFC3339)
		task.Status = heliaTaskStatusDoing
		task.UpdatedAt = now.UTC().Format(time.RFC3339)
		task.Assignment = &heliaTaskboardLock{
			AgentID:        req.Agent.AgentID,
			Dyad:           req.Agent.Dyad,
			Machine:        req.Agent.Machine,
			User:           req.Agent.User,
			LockToken:      lockToken,
			ClaimedAt:      now.UTC().Format(time.RFC3339),
			LeaseSeconds:   leaseSeconds,
			LeaseExpiresAt: expires,
		}
		board.Tasks[index] = task
		heliaTaskboardTouchAgent(board, req.Agent, now, "working", task.ID)
		heliaTaskboardClearOtherAgentsForTask(board, req.Agent.AgentID, task.ID, now)
		claimed = task
		return nil
	})
	if err != nil {
		return result, err
	}
	result.Task = claimed
	return result, nil
}

func heliaTaskboardRelease(ctx context.Context, client *heliaClient, boardName string, taskID string, agent heliaTaskboardAgentIdentity) (heliaTaskboardTask, error) {
	var updated heliaTaskboardTask
	_, _, err := heliaTaskboardMutateWithRetry(ctx, client, boardName, func(board *heliaTaskboard, now time.Time) error {
		index := heliaTaskboardFindTask(board.Tasks, taskID)
		if index < 0 {
			return fmt.Errorf("task %q not found", taskID)
		}
		task := board.Tasks[index]
		if task.Assignment == nil {
			return fmt.Errorf("task %s is not currently assigned", task.ID)
		}
		assignedTo := strings.TrimSpace(task.Assignment.AgentID)
		expired := heliaTaskboardLockExpired(*task.Assignment, now)
		if assignedTo != "" && !expired && !strings.EqualFold(assignedTo, agent.AgentID) {
			return fmt.Errorf("task %s is locked by %s", task.ID, assignedTo)
		}
		task.Assignment = nil
		if task.Status != heliaTaskStatusDone {
			task.Status = heliaTaskStatusTodo
		}
		task.UpdatedAt = now.UTC().Format(time.RFC3339)
		board.Tasks[index] = task
		heliaTaskboardTouchAgent(board, agent, now, "idle", "")
		if assignedTo != "" && !strings.EqualFold(assignedTo, agent.AgentID) {
			heliaTaskboardSetAgentState(board, assignedTo, now, "idle", "")
		}
		updated = task
		return nil
	})
	return updated, err
}

func heliaTaskboardMarkDone(ctx context.Context, client *heliaClient, boardName string, taskID string, agent heliaTaskboardAgentIdentity, resultText string) (heliaTaskboardTask, error) {
	var updated heliaTaskboardTask
	_, _, err := heliaTaskboardMutateWithRetry(ctx, client, boardName, func(board *heliaTaskboard, now time.Time) error {
		index := heliaTaskboardFindTask(board.Tasks, taskID)
		if index < 0 {
			return fmt.Errorf("task %q not found", taskID)
		}
		task := board.Tasks[index]
		if task.Assignment != nil {
			assignedTo := strings.TrimSpace(task.Assignment.AgentID)
			expired := heliaTaskboardLockExpired(*task.Assignment, now)
			if assignedTo != "" && !expired && !strings.EqualFold(assignedTo, agent.AgentID) {
				return fmt.Errorf("task %s is locked by %s", task.ID, assignedTo)
			}
			if assignedTo != "" && !strings.EqualFold(assignedTo, agent.AgentID) {
				heliaTaskboardSetAgentState(board, assignedTo, now, "idle", "")
			}
		}
		task.Status = heliaTaskStatusDone
		task.Assignment = nil
		task.UpdatedAt = now.UTC().Format(time.RFC3339)
		task.CompletedAt = now.UTC().Format(time.RFC3339)
		if strings.TrimSpace(resultText) != "" {
			task.Result = strings.TrimSpace(resultText)
		}
		board.Tasks[index] = task
		heliaTaskboardTouchAgent(board, agent, now, "idle", "")
		updated = task
		return nil
	})
	return updated, err
}

func heliaTaskboardMutateWithRetry(ctx context.Context, client *heliaClient, boardName string, mutate func(board *heliaTaskboard, now time.Time) error) (heliaTaskboard, int64, error) {
	name := strings.TrimSpace(boardName)
	if name == "" {
		return heliaTaskboard{}, 0, fmt.Errorf("taskboard name required")
	}
	var lastErr error
	for attempt := 0; attempt < 8; attempt++ {
		board, revision, exists, err := heliaTaskboardLoad(ctx, client, name)
		if err != nil {
			return heliaTaskboard{}, 0, err
		}
		now := time.Now().UTC()
		if err := mutate(&board, now); err != nil {
			return heliaTaskboard{}, 0, err
		}
		board.UpdatedAt = now.Format(time.RFC3339)
		newRevision, err := heliaTaskboardPersist(ctx, client, board, exists, revision)
		if err == nil {
			return board, newRevision, nil
		}
		if !isHeliaStatus(err, 409) {
			return heliaTaskboard{}, 0, err
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("taskboard update conflict")
	}
	return heliaTaskboard{}, 0, fmt.Errorf("taskboard update failed after retries: %w", lastErr)
}

func heliaTaskboardLoad(ctx context.Context, client *heliaClient, boardName string) (heliaTaskboard, int64, bool, error) {
	name := strings.TrimSpace(boardName)
	if name == "" {
		return heliaTaskboard{}, 0, false, fmt.Errorf("taskboard name required")
	}
	meta, err := heliaLookupObjectMeta(ctx, client, heliaTaskboardKind, name)
	if err != nil {
		return heliaTaskboard{}, 0, false, err
	}
	if meta == nil {
		return heliaNewTaskboard(name), 0, false, nil
	}
	payload, err := client.getPayload(ctx, heliaTaskboardKind, name)
	if err != nil {
		return heliaTaskboard{}, 0, true, err
	}
	board := heliaNewTaskboard(name)
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &board); err != nil {
			return heliaTaskboard{}, 0, true, fmt.Errorf("taskboard %s payload invalid: %w", name, err)
		}
	}
	board = heliaNormalizeTaskboard(board, name)
	return board, meta.LatestRevision, true, nil
}

func heliaTaskboardPersist(ctx context.Context, client *heliaClient, board heliaTaskboard, exists bool, revision int64) (int64, error) {
	board = heliaNormalizeTaskboard(board, board.Name)
	payload, err := json.MarshalIndent(board, "", "  ")
	if err != nil {
		return 0, err
	}
	metadata := heliaTaskboardObjectMetadata(board)
	var expected *int64
	if exists {
		expected = &revision
	}
	result, err := client.putObject(ctx, heliaTaskboardKind, board.Name, payload, "application/json", metadata, expected)
	if err != nil {
		return 0, err
	}
	if result.Result.Object.LatestRevision > 0 {
		return result.Result.Object.LatestRevision, nil
	}
	return result.Result.Revision.Revision, nil
}

func heliaTaskboardObjectMetadata(board heliaTaskboard) map[string]interface{} {
	todo, doing, done := heliaTaskboardStatusCounts(board.Tasks)
	return map[string]interface{}{
		"tasks_total": len(board.Tasks),
		"tasks_todo":  todo,
		"tasks_doing": doing,
		"tasks_done":  done,
	}
}

func heliaNewTaskboard(name string) heliaTaskboard {
	return heliaTaskboard{
		Version: 1,
		Name:    strings.TrimSpace(name),
		Tasks:   []heliaTaskboardTask{},
		Agents:  map[string]heliaTaskboardAgent{},
	}
}

func heliaNormalizeTaskboard(board heliaTaskboard, fallbackName string) heliaTaskboard {
	if board.Version <= 0 {
		board.Version = 1
	}
	board.Name = strings.TrimSpace(board.Name)
	if board.Name == "" {
		board.Name = strings.TrimSpace(fallbackName)
	}
	if board.Tasks == nil {
		board.Tasks = []heliaTaskboardTask{}
	}
	if board.Agents == nil {
		board.Agents = map[string]heliaTaskboardAgent{}
	}
	for i := range board.Tasks {
		board.Tasks[i].ID = strings.TrimSpace(board.Tasks[i].ID)
		board.Tasks[i].Title = strings.TrimSpace(board.Tasks[i].Title)
		board.Tasks[i].Prompt = strings.TrimSpace(board.Tasks[i].Prompt)
		if board.Tasks[i].Prompt == "" {
			board.Tasks[i].Prompt = board.Tasks[i].Title
		}
		board.Tasks[i].Status = normalizeHeliaTaskStatus(board.Tasks[i].Status)
		board.Tasks[i].Priority = normalizeHeliaTaskPriority(board.Tasks[i].Priority)
		if board.Tasks[i].Tags == nil {
			board.Tasks[i].Tags = []string{}
		}
		if board.Tasks[i].Assignment != nil {
			board.Tasks[i].Assignment.AgentID = strings.TrimSpace(board.Tasks[i].Assignment.AgentID)
			if board.Tasks[i].Assignment.AgentID == "" {
				board.Tasks[i].Assignment = nil
			}
		}
	}
	return board
}

func heliaTaskboardStatusCounts(tasks []heliaTaskboardTask) (todo int, doing int, done int) {
	for i := range tasks {
		switch normalizeHeliaTaskStatus(tasks[i].Status) {
		case heliaTaskStatusDone:
			done++
		case heliaTaskStatusDoing:
			doing++
		default:
			todo++
		}
	}
	return todo, doing, done
}

func normalizeHeliaTaskStatus(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "todo", "open", "queued", "backlog":
		return heliaTaskStatusTodo
	case "doing", "in-progress", "in_progress", "claimed", "active":
		return heliaTaskStatusDoing
	case "done", "closed", "complete", "completed":
		return heliaTaskStatusDone
	default:
		return heliaTaskStatusTodo
	}
}

func parseHeliaTaskStatusFilter(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	switch strings.ToLower(trimmed) {
	case "todo", "open", "queued", "backlog":
		return heliaTaskStatusTodo, nil
	case "doing", "in-progress", "in_progress", "claimed", "active":
		return heliaTaskStatusDoing, nil
	case "done", "closed", "complete", "completed":
		return heliaTaskStatusDone, nil
	default:
		return "", fmt.Errorf("invalid --status %q (expected todo|doing|done)", trimmed)
	}
}

func normalizeHeliaTaskPriority(raw string) string {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case heliaTaskPriorityP1:
		return heliaTaskPriorityP1
	case heliaTaskPriorityP3:
		return heliaTaskPriorityP3
	default:
		return heliaTaskPriorityP2
	}
}

func heliaTaskPriorityRank(priority string) int {
	switch normalizeHeliaTaskPriority(priority) {
	case heliaTaskPriorityP1:
		return 1
	case heliaTaskPriorityP2:
		return 2
	default:
		return 3
	}
}

func heliaTaskboardFindTask(tasks []heliaTaskboardTask, id string) int {
	needle := strings.TrimSpace(id)
	for i := range tasks {
		if strings.EqualFold(strings.TrimSpace(tasks[i].ID), needle) {
			return i
		}
	}
	return -1
}

func heliaTaskboardSelectNextClaimable(tasks []heliaTaskboardTask, now time.Time) int {
	candidates := make([]int, 0, len(tasks))
	for i := range tasks {
		status := normalizeHeliaTaskStatus(tasks[i].Status)
		if status == heliaTaskStatusDone {
			continue
		}
		if tasks[i].Assignment == nil || heliaTaskboardLockExpired(*tasks[i].Assignment, now) {
			candidates = append(candidates, i)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left := tasks[candidates[i]]
		right := tasks[candidates[j]]
		if heliaTaskPriorityRank(left.Priority) != heliaTaskPriorityRank(right.Priority) {
			return heliaTaskPriorityRank(left.Priority) < heliaTaskPriorityRank(right.Priority)
		}
		leftCreated := parseRFC3339(left.CreatedAt)
		rightCreated := parseRFC3339(right.CreatedAt)
		if !leftCreated.Equal(rightCreated) {
			return leftCreated.Before(rightCreated)
		}
		return strings.Compare(left.ID, right.ID) < 0
	})
	if len(candidates) == 0 {
		return -1
	}
	return candidates[0]
}

func heliaTaskboardLockExpired(lock heliaTaskboardLock, now time.Time) bool {
	if strings.TrimSpace(lock.LeaseExpiresAt) != "" {
		expires := parseRFC3339(lock.LeaseExpiresAt)
		if !expires.IsZero() {
			return !now.Before(expires)
		}
	}
	if lock.LeaseSeconds > 0 {
		claimed := parseRFC3339(lock.ClaimedAt)
		if !claimed.IsZero() {
			expires := claimed.Add(time.Duration(lock.LeaseSeconds) * time.Second)
			return !now.Before(expires)
		}
	}
	return false
}

func parseRFC3339(raw string) time.Time {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func heliaTaskboardTouchAgent(board *heliaTaskboard, identity heliaTaskboardAgentIdentity, now time.Time, status string, currentTaskID string) {
	if board.Agents == nil {
		board.Agents = map[string]heliaTaskboardAgent{}
	}
	id := strings.TrimSpace(identity.AgentID)
	if id == "" {
		return
	}
	agent := board.Agents[id]
	agent.ID = id
	agent.Dyad = firstNonEmpty(strings.TrimSpace(identity.Dyad), strings.TrimSpace(agent.Dyad))
	agent.Machine = firstNonEmpty(strings.TrimSpace(identity.Machine), strings.TrimSpace(agent.Machine))
	agent.User = firstNonEmpty(strings.TrimSpace(identity.User), strings.TrimSpace(agent.User))
	agent.Status = strings.TrimSpace(status)
	agent.CurrentTaskID = strings.TrimSpace(currentTaskID)
	agent.LastSeenAt = now.UTC().Format(time.RFC3339)
	board.Agents[id] = agent
}

func heliaTaskboardSetAgentState(board *heliaTaskboard, agentID string, now time.Time, status string, currentTaskID string) {
	id := strings.TrimSpace(agentID)
	if id == "" {
		return
	}
	if board.Agents == nil {
		board.Agents = map[string]heliaTaskboardAgent{}
	}
	agent := board.Agents[id]
	agent.ID = id
	agent.Status = strings.TrimSpace(status)
	agent.CurrentTaskID = strings.TrimSpace(currentTaskID)
	agent.LastSeenAt = now.UTC().Format(time.RFC3339)
	board.Agents[id] = agent
}

func heliaTaskboardClearOtherAgentsForTask(board *heliaTaskboard, keepAgent string, taskID string, now time.Time) {
	if board.Agents == nil {
		return
	}
	for id, agent := range board.Agents {
		if strings.EqualFold(strings.TrimSpace(id), strings.TrimSpace(keepAgent)) {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(agent.CurrentTaskID), strings.TrimSpace(taskID)) {
			continue
		}
		agent.CurrentTaskID = ""
		agent.Status = "idle"
		agent.LastSeenAt = now.UTC().Format(time.RFC3339)
		board.Agents[id] = agent
	}
}

func heliaTaskboardResolveAgent(settings Settings, explicitAgent string, dyad string, machine string) heliaTaskboardAgentIdentity {
	host := strings.TrimSpace(machine)
	if host == "" {
		host, _ = os.Hostname()
	}
	host = sanitizeAgentIDComponent(firstNonEmpty(host, "machine"))
	userName := strings.TrimSpace(firstNonEmpty(os.Getenv("USER"), os.Getenv("USERNAME")))
	if userName == "" {
		userName = "user"
	}
	userName = sanitizeAgentIDComponent(userName)
	dyad = sanitizeAgentIDComponent(strings.TrimSpace(dyad))

	agentID := strings.TrimSpace(explicitAgent)
	if agentID == "" {
		agentID = envSunTaskboardAgent()
	}
	if agentID == "" {
		agentID = strings.TrimSpace(settings.Helia.TaskboardAgent)
	}
	if agentID == "" {
		base := userName
		if dyad != "" {
			base = dyad
		}
		agentID = "dyad:" + base + "@" + host
	}
	return heliaTaskboardAgentIdentity{
		AgentID: strings.TrimSpace(agentID),
		Dyad:    dyad,
		Machine: host,
		User:    userName,
	}
}

func sanitizeAgentIDComponent(raw string) string {
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
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return ""
	}
	return out
}

func heliaTaskID(now time.Time, existing []heliaTaskboardTask) string {
	prefix := "tsk-" + now.UTC().Format("20060102-150405")
	used := map[string]struct{}{}
	for i := range existing {
		used[strings.TrimSpace(existing[i].ID)] = struct{}{}
	}
	for i := 0; i < 128; i++ {
		n, err := secureIntn(46656) // 36^3
		if err != nil {
			n = i
		}
		suffix := strings.ToLower(strconv.FormatInt(int64(n), 36))
		if len(suffix) < 3 {
			suffix = strings.Repeat("0", 3-len(suffix)) + suffix
		}
		id := strings.ToLower(prefix + "-" + suffix)
		if _, ok := used[id]; ok {
			continue
		}
		return id
	}
	return strings.ToLower(fmt.Sprintf("%s-%d", prefix, now.UTC().UnixNano()))
}

func heliaTaskboardLockToken(now time.Time) string {
	n, err := secureIntn(1_000_000)
	if err != nil {
		n = int(now.UnixNano() % 1_000_000)
	}
	return fmt.Sprintf("lock-%d-%06d", now.UTC().Unix(), n)
}

func heliaTaskboardName(settings Settings, explicit string) string {
	if strings.TrimSpace(explicit) != "" {
		return strings.TrimSpace(explicit)
	}
	if env := envSunTaskboard(); env != "" {
		return env
	}
	if strings.TrimSpace(settings.Helia.Taskboard) != "" {
		return strings.TrimSpace(settings.Helia.Taskboard)
	}
	return "default"
}

func heliaTaskboardLeaseSeconds(settings Settings, explicit int) int {
	if explicit > 0 {
		return explicit
	}
	if env := envSunTaskboardLeaseSeconds(); env != "" {
		if parsed, err := strconv.Atoi(env); err == nil && parsed > 0 {
			return parsed
		}
	}
	if settings.Helia.TaskboardLeaseSeconds > 0 {
		return settings.Helia.TaskboardLeaseSeconds
	}
	return 1800
}

func isHeliaStatus(err error, statusCode int) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), fmt.Sprintf("status %d", statusCode))
}

func heliaSortTasks(tasks []heliaTaskboardTask) {
	sort.SliceStable(tasks, func(i, j int) bool {
		leftStatus := normalizeHeliaTaskStatus(tasks[i].Status)
		rightStatus := normalizeHeliaTaskStatus(tasks[j].Status)
		if leftStatus != rightStatus {
			return heliaTaskStatusSortRank(leftStatus) < heliaTaskStatusSortRank(rightStatus)
		}
		if heliaTaskPriorityRank(tasks[i].Priority) != heliaTaskPriorityRank(tasks[j].Priority) {
			return heliaTaskPriorityRank(tasks[i].Priority) < heliaTaskPriorityRank(tasks[j].Priority)
		}
		leftCreated := parseRFC3339(tasks[i].CreatedAt)
		rightCreated := parseRFC3339(tasks[j].CreatedAt)
		if !leftCreated.Equal(rightCreated) {
			return leftCreated.Before(rightCreated)
		}
		return strings.Compare(tasks[i].ID, tasks[j].ID) < 0
	})
}

func heliaTaskStatusSortRank(status string) int {
	switch normalizeHeliaTaskStatus(status) {
	case heliaTaskStatusDoing:
		return 1
	case heliaTaskStatusTodo:
		return 2
	default:
		return 3
	}
}

func printHeliaTaskRows(tasks []heliaTaskboardTask) {
	rows := make([][]string, 0, len(tasks))
	for i := range tasks {
		owner := "-"
		lockUntil := "-"
		if tasks[i].Assignment != nil {
			owner = firstNonEmpty(strings.TrimSpace(tasks[i].Assignment.AgentID), "-")
			if strings.TrimSpace(tasks[i].Assignment.LeaseExpiresAt) != "" {
				lockUntil = strings.TrimSpace(tasks[i].Assignment.LeaseExpiresAt)
			}
		}
		rows = append(rows, []string{
			tasks[i].ID,
			tasks[i].Status,
			tasks[i].Priority,
			tasks[i].Title,
			owner,
			lockUntil,
		})
	}
	printAlignedTable([]string{
		styleHeading("ID"),
		styleHeading("STATUS"),
		styleHeading("PRI"),
		styleHeading("TITLE"),
		styleHeading("AGENT"),
		styleHeading("LOCK_UNTIL"),
	}, rows, 2)
}
