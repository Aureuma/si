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
	sunTaskboardUsageText = "usage: si sun taskboard <use|show|list|add|claim|release|done> ..."
	sunTaskboardKind      = "dyad_taskboard"

	sunTaskStatusTodo  = "todo"
	sunTaskStatusDoing = "doing"
	sunTaskStatusDone  = "done"

	sunTaskPriorityP1 = "P1"
	sunTaskPriorityP2 = "P2"
	sunTaskPriorityP3 = "P3"
)

type sunTaskboard struct {
	Version   int                          `json:"version"`
	Name      string                       `json:"name"`
	UpdatedAt string                       `json:"updated_at,omitempty"`
	Tasks     []sunTaskboardTask           `json:"tasks"`
	Agents    map[string]sunTaskboardAgent `json:"agents,omitempty"`
}

type sunTaskboardTask struct {
	ID          string            `json:"id"`
	Title       string            `json:"title"`
	Prompt      string            `json:"prompt"`
	Status      string            `json:"status"`
	Priority    string            `json:"priority"`
	Tags        []string          `json:"tags,omitempty"`
	CreatedAt   string            `json:"created_at,omitempty"`
	UpdatedAt   string            `json:"updated_at,omitempty"`
	CompletedAt string            `json:"completed_at,omitempty"`
	Result      string            `json:"result,omitempty"`
	Assignment  *sunTaskboardLock `json:"assignment,omitempty"`
}

type sunTaskboardLock struct {
	AgentID        string `json:"agent_id"`
	Dyad           string `json:"dyad,omitempty"`
	Machine        string `json:"machine,omitempty"`
	User           string `json:"user,omitempty"`
	LockToken      string `json:"lock_token,omitempty"`
	ClaimedAt      string `json:"claimed_at,omitempty"`
	LeaseSeconds   int    `json:"lease_seconds,omitempty"`
	LeaseExpiresAt string `json:"lease_expires_at,omitempty"`
}

type sunTaskboardAgent struct {
	ID            string `json:"id"`
	Dyad          string `json:"dyad,omitempty"`
	Machine       string `json:"machine,omitempty"`
	User          string `json:"user,omitempty"`
	Status        string `json:"status,omitempty"`
	CurrentTaskID string `json:"current_task_id,omitempty"`
	LastSeenAt    string `json:"last_seen_at,omitempty"`
}

type sunTaskboardAgentIdentity struct {
	AgentID string
	Dyad    string
	Machine string
	User    string
}

type sunTaskboardClaimRequest struct {
	TaskID       string
	Agent        sunTaskboardAgentIdentity
	LeaseSeconds int
}

type sunTaskboardClaimResult struct {
	BoardName string           `json:"board_name"`
	Task      sunTaskboardTask `json:"task"`
}

func cmdSunTaskboard(args []string) {
	if len(args) == 0 {
		printUsage(sunTaskboardUsageText)
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "help", "-h", "--help":
		printUsage(sunTaskboardUsageText)
	case "use":
		cmdSunTaskboardUse(rest)
	case "show":
		cmdSunTaskboardShow(rest)
	case "list":
		cmdSunTaskboardList(rest)
	case "add":
		cmdSunTaskboardAdd(rest)
	case "claim":
		cmdSunTaskboardClaim(rest)
	case "release":
		cmdSunTaskboardRelease(rest)
	case "done":
		cmdSunTaskboardDone(rest)
	default:
		printUnknown("sun taskboard", sub)
		printUsage(sunTaskboardUsageText)
		os.Exit(1)
	}
}

func cmdSunTaskboardUse(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("sun taskboard use", flag.ExitOnError)
	name := fs.String("name", strings.TrimSpace(settings.Sun.Taskboard), "default taskboard object name")
	agent := fs.String("agent", strings.TrimSpace(settings.Sun.TaskboardAgent), "default agent id")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		*name = strings.TrimSpace(fs.Arg(0))
	}
	if strings.TrimSpace(*name) == "" {
		fatal(fmt.Errorf("taskboard name required (--name or sun.taskboard)"))
	}
	current, err := loadSettings()
	if err != nil {
		fatal(err)
	}
	current.Sun.Taskboard = strings.TrimSpace(*name)
	if strings.TrimSpace(*agent) != "" {
		current.Sun.TaskboardAgent = strings.TrimSpace(*agent)
	}
	if err := saveSettings(current); err != nil {
		fatal(err)
	}
	successf("sun taskboard default set to %s", strings.TrimSpace(*name))
	if strings.TrimSpace(*agent) != "" {
		successf("sun taskboard default agent set to %s", strings.TrimSpace(*agent))
	}
}

func cmdSunTaskboardShow(args []string) {
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
	client, err := sunClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	targetBoard := sunTaskboardName(settings, strings.TrimSpace(*boardName))
	board, _, _, err := sunTaskboardLoad(sunContext(settings), client, targetBoard)
	if err != nil {
		fatal(err)
	}
	if *jsonOut {
		printJSON(board)
		return
	}
	todo, doing, done := sunTaskboardStatusCounts(board.Tasks)
	fmt.Printf("%s %s\n", styleHeading("board:"), board.Name)
	fmt.Printf("%s %d (todo=%d doing=%d done=%d)\n", styleHeading("tasks:"), len(board.Tasks), todo, doing, done)
	if strings.TrimSpace(board.UpdatedAt) != "" {
		fmt.Printf("%s %s\n", styleHeading("updated_at:"), board.UpdatedAt)
	}
	if len(board.Tasks) == 0 {
		infof("no tasks on board")
		return
	}
	tasks := append([]sunTaskboardTask(nil), board.Tasks...)
	sunSortTasks(tasks)
	printSunTaskRows(tasks)
}

func cmdSunTaskboardList(args []string) {
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
	client, err := sunClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	targetBoard := sunTaskboardName(settings, strings.TrimSpace(*boardName))
	board, _, _, err := sunTaskboardLoad(sunContext(settings), client, targetBoard)
	if err != nil {
		fatal(err)
	}
	statusFilter, err := parseSunTaskStatusFilter(*status)
	if err != nil {
		fatal(err)
	}
	ownerFilter := strings.TrimSpace(*owner)
	filtered := make([]sunTaskboardTask, 0, len(board.Tasks))
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
	sunSortTasks(filtered)
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
	printSunTaskRows(filtered)
}

func cmdSunTaskboardAdd(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("sun taskboard add", flag.ExitOnError)
	boardName := fs.String("name", "", "taskboard object name")
	title := fs.String("title", "", "task title")
	prompt := fs.String("prompt", "", "task prompt used by dyad autopilot")
	priority := fs.String("priority", sunTaskPriorityP2, "priority: P1|P2|P3")
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
	client, err := sunClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	targetBoard := sunTaskboardName(settings, strings.TrimSpace(*boardName))
	var created sunTaskboardTask
	_, _, err = sunTaskboardMutateWithRetry(sunContext(settings), client, targetBoard, func(board *sunTaskboard, now time.Time) error {
		id := sunTaskID(now, board.Tasks)
		created = sunTaskboardTask{
			ID:        id,
			Title:     taskTitle,
			Prompt:    taskPrompt,
			Status:    sunTaskStatusTodo,
			Priority:  normalizeSunTaskPriority(*priority),
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

func cmdSunTaskboardClaim(args []string) {
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
	client, err := sunClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	targetBoard := sunTaskboardName(settings, strings.TrimSpace(*boardName))
	identity := sunTaskboardResolveAgent(settings, strings.TrimSpace(*agent), strings.TrimSpace(*dyad), strings.TrimSpace(*machine))
	result, err := sunTaskboardClaim(sunContext(settings), client, targetBoard, sunTaskboardClaimRequest{
		TaskID:       strings.TrimSpace(*id),
		Agent:        identity,
		LeaseSeconds: sunTaskboardLeaseSeconds(settings, *leaseSeconds),
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

func cmdSunTaskboardRelease(args []string) {
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
	client, err := sunClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	targetBoard := sunTaskboardName(settings, strings.TrimSpace(*boardName))
	identity := sunTaskboardResolveAgent(settings, strings.TrimSpace(*agent), strings.TrimSpace(*dyad), strings.TrimSpace(*machine))
	task, err := sunTaskboardRelease(sunContext(settings), client, targetBoard, taskID, identity)
	if err != nil {
		fatal(err)
	}
	if *jsonOut {
		printJSON(task)
		return
	}
	successf("released %s on board %s", task.ID, targetBoard)
}

func cmdSunTaskboardDone(args []string) {
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
	client, err := sunClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	targetBoard := sunTaskboardName(settings, strings.TrimSpace(*boardName))
	identity := sunTaskboardResolveAgent(settings, strings.TrimSpace(*agent), strings.TrimSpace(*dyad), strings.TrimSpace(*machine))
	task, err := sunTaskboardMarkDone(sunContext(settings), client, targetBoard, taskID, identity, strings.TrimSpace(*resultText))
	if err != nil {
		fatal(err)
	}
	if *jsonOut {
		printJSON(task)
		return
	}
	successf("completed %s on board %s", task.ID, targetBoard)
}

func sunAutopilotClaimTask(settings Settings, dyadName string) (sunTaskboardClaimResult, error) {
	client, err := sunClientFromSettings(settings)
	if err != nil {
		return sunTaskboardClaimResult{}, err
	}
	identity := sunTaskboardResolveAgent(settings, "", dyadName, "")
	return sunTaskboardClaim(sunContext(settings), client, sunTaskboardName(settings, ""), sunTaskboardClaimRequest{
		Agent:        identity,
		LeaseSeconds: sunTaskboardLeaseSeconds(settings, 0),
	})
}

func sunTaskboardClaim(ctx context.Context, client *sunClient, boardName string, req sunTaskboardClaimRequest) (sunTaskboardClaimResult, error) {
	result := sunTaskboardClaimResult{BoardName: strings.TrimSpace(boardName)}
	if strings.TrimSpace(req.Agent.AgentID) == "" {
		return result, fmt.Errorf("agent id required")
	}
	leaseSeconds := req.LeaseSeconds
	if leaseSeconds <= 0 {
		leaseSeconds = 1800
	}
	var claimed sunTaskboardTask
	_, _, err := sunTaskboardMutateWithRetry(ctx, client, boardName, func(board *sunTaskboard, now time.Time) error {
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
			index = sunTaskboardSelectNextClaimable(board.Tasks, now)
			if index < 0 {
				return fmt.Errorf("no claimable tasks available")
			}
		}
		task := board.Tasks[index]
		if task.Status == sunTaskStatusDone {
			return fmt.Errorf("task %s is already done", task.ID)
		}
		if task.Assignment != nil {
			assignedTo := strings.TrimSpace(task.Assignment.AgentID)
			expired := sunTaskboardLockExpired(*task.Assignment, now)
			if assignedTo != "" && !expired && !strings.EqualFold(assignedTo, req.Agent.AgentID) {
				return fmt.Errorf("task %s is locked by %s", task.ID, assignedTo)
			}
		}
		lockToken := sunTaskboardLockToken(now)
		expires := now.Add(time.Duration(leaseSeconds) * time.Second).UTC().Format(time.RFC3339)
		task.Status = sunTaskStatusDoing
		task.UpdatedAt = now.UTC().Format(time.RFC3339)
		task.Assignment = &sunTaskboardLock{
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
		sunTaskboardTouchAgent(board, req.Agent, now, "working", task.ID)
		sunTaskboardClearOtherAgentsForTask(board, req.Agent.AgentID, task.ID, now)
		claimed = task
		return nil
	})
	if err != nil {
		return result, err
	}
	result.Task = claimed
	return result, nil
}

func sunTaskboardRelease(ctx context.Context, client *sunClient, boardName string, taskID string, agent sunTaskboardAgentIdentity) (sunTaskboardTask, error) {
	var updated sunTaskboardTask
	_, _, err := sunTaskboardMutateWithRetry(ctx, client, boardName, func(board *sunTaskboard, now time.Time) error {
		index := sunTaskboardFindTask(board.Tasks, taskID)
		if index < 0 {
			return fmt.Errorf("task %q not found", taskID)
		}
		task := board.Tasks[index]
		if task.Assignment == nil {
			return fmt.Errorf("task %s is not currently assigned", task.ID)
		}
		assignedTo := strings.TrimSpace(task.Assignment.AgentID)
		expired := sunTaskboardLockExpired(*task.Assignment, now)
		if assignedTo != "" && !expired && !strings.EqualFold(assignedTo, agent.AgentID) {
			return fmt.Errorf("task %s is locked by %s", task.ID, assignedTo)
		}
		task.Assignment = nil
		if task.Status != sunTaskStatusDone {
			task.Status = sunTaskStatusTodo
		}
		task.UpdatedAt = now.UTC().Format(time.RFC3339)
		board.Tasks[index] = task
		sunTaskboardTouchAgent(board, agent, now, "idle", "")
		if assignedTo != "" && !strings.EqualFold(assignedTo, agent.AgentID) {
			sunTaskboardSetAgentState(board, assignedTo, now, "idle", "")
		}
		updated = task
		return nil
	})
	return updated, err
}

func sunTaskboardMarkDone(ctx context.Context, client *sunClient, boardName string, taskID string, agent sunTaskboardAgentIdentity, resultText string) (sunTaskboardTask, error) {
	var updated sunTaskboardTask
	_, _, err := sunTaskboardMutateWithRetry(ctx, client, boardName, func(board *sunTaskboard, now time.Time) error {
		index := sunTaskboardFindTask(board.Tasks, taskID)
		if index < 0 {
			return fmt.Errorf("task %q not found", taskID)
		}
		task := board.Tasks[index]
		if task.Assignment != nil {
			assignedTo := strings.TrimSpace(task.Assignment.AgentID)
			expired := sunTaskboardLockExpired(*task.Assignment, now)
			if assignedTo != "" && !expired && !strings.EqualFold(assignedTo, agent.AgentID) {
				return fmt.Errorf("task %s is locked by %s", task.ID, assignedTo)
			}
			if assignedTo != "" && !strings.EqualFold(assignedTo, agent.AgentID) {
				sunTaskboardSetAgentState(board, assignedTo, now, "idle", "")
			}
		}
		task.Status = sunTaskStatusDone
		task.Assignment = nil
		task.UpdatedAt = now.UTC().Format(time.RFC3339)
		task.CompletedAt = now.UTC().Format(time.RFC3339)
		if strings.TrimSpace(resultText) != "" {
			task.Result = strings.TrimSpace(resultText)
		}
		board.Tasks[index] = task
		sunTaskboardTouchAgent(board, agent, now, "idle", "")
		updated = task
		return nil
	})
	return updated, err
}

func sunTaskboardMutateWithRetry(ctx context.Context, client *sunClient, boardName string, mutate func(board *sunTaskboard, now time.Time) error) (sunTaskboard, int64, error) {
	name := strings.TrimSpace(boardName)
	if name == "" {
		return sunTaskboard{}, 0, fmt.Errorf("taskboard name required")
	}
	var lastErr error
	for attempt := 0; attempt < 8; attempt++ {
		board, revision, exists, err := sunTaskboardLoad(ctx, client, name)
		if err != nil {
			return sunTaskboard{}, 0, err
		}
		now := time.Now().UTC()
		if err := mutate(&board, now); err != nil {
			return sunTaskboard{}, 0, err
		}
		board.UpdatedAt = now.Format(time.RFC3339)
		newRevision, err := sunTaskboardPersist(ctx, client, board, exists, revision)
		if err == nil {
			return board, newRevision, nil
		}
		if !isSunStatus(err, 409) {
			return sunTaskboard{}, 0, err
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("taskboard update conflict")
	}
	return sunTaskboard{}, 0, fmt.Errorf("taskboard update failed after retries: %w", lastErr)
}

func sunTaskboardLoad(ctx context.Context, client *sunClient, boardName string) (sunTaskboard, int64, bool, error) {
	name := strings.TrimSpace(boardName)
	if name == "" {
		return sunTaskboard{}, 0, false, fmt.Errorf("taskboard name required")
	}
	meta, err := sunLookupObjectMeta(ctx, client, sunTaskboardKind, name)
	if err != nil {
		return sunTaskboard{}, 0, false, err
	}
	if meta == nil {
		return sunNewTaskboard(name), 0, false, nil
	}
	payload, err := client.getPayload(ctx, sunTaskboardKind, name)
	if err != nil {
		return sunTaskboard{}, 0, true, err
	}
	board := sunNewTaskboard(name)
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &board); err != nil {
			return sunTaskboard{}, 0, true, fmt.Errorf("taskboard %s payload invalid: %w", name, err)
		}
	}
	board = sunNormalizeTaskboard(board, name)
	return board, meta.LatestRevision, true, nil
}

func sunTaskboardPersist(ctx context.Context, client *sunClient, board sunTaskboard, exists bool, revision int64) (int64, error) {
	board = sunNormalizeTaskboard(board, board.Name)
	payload, err := json.MarshalIndent(board, "", "  ")
	if err != nil {
		return 0, err
	}
	metadata := sunTaskboardObjectMetadata(board)
	var expected *int64
	if exists {
		expected = &revision
	}
	result, err := client.putObject(ctx, sunTaskboardKind, board.Name, payload, "application/json", metadata, expected)
	if err != nil {
		return 0, err
	}
	if result.Result.Object.LatestRevision > 0 {
		return result.Result.Object.LatestRevision, nil
	}
	return result.Result.Revision.Revision, nil
}

func sunTaskboardObjectMetadata(board sunTaskboard) map[string]interface{} {
	todo, doing, done := sunTaskboardStatusCounts(board.Tasks)
	return map[string]interface{}{
		"tasks_total": len(board.Tasks),
		"tasks_todo":  todo,
		"tasks_doing": doing,
		"tasks_done":  done,
	}
}

func sunNewTaskboard(name string) sunTaskboard {
	return sunTaskboard{
		Version: 1,
		Name:    strings.TrimSpace(name),
		Tasks:   []sunTaskboardTask{},
		Agents:  map[string]sunTaskboardAgent{},
	}
}

func sunNormalizeTaskboard(board sunTaskboard, fallbackName string) sunTaskboard {
	if board.Version <= 0 {
		board.Version = 1
	}
	board.Name = strings.TrimSpace(board.Name)
	if board.Name == "" {
		board.Name = strings.TrimSpace(fallbackName)
	}
	if board.Tasks == nil {
		board.Tasks = []sunTaskboardTask{}
	}
	if board.Agents == nil {
		board.Agents = map[string]sunTaskboardAgent{}
	}
	for i := range board.Tasks {
		board.Tasks[i].ID = strings.TrimSpace(board.Tasks[i].ID)
		board.Tasks[i].Title = strings.TrimSpace(board.Tasks[i].Title)
		board.Tasks[i].Prompt = strings.TrimSpace(board.Tasks[i].Prompt)
		if board.Tasks[i].Prompt == "" {
			board.Tasks[i].Prompt = board.Tasks[i].Title
		}
		board.Tasks[i].Status = normalizeSunTaskStatus(board.Tasks[i].Status)
		board.Tasks[i].Priority = normalizeSunTaskPriority(board.Tasks[i].Priority)
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

func sunTaskboardStatusCounts(tasks []sunTaskboardTask) (todo int, doing int, done int) {
	for i := range tasks {
		switch normalizeSunTaskStatus(tasks[i].Status) {
		case sunTaskStatusDone:
			done++
		case sunTaskStatusDoing:
			doing++
		default:
			todo++
		}
	}
	return todo, doing, done
}

func normalizeSunTaskStatus(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "todo", "open", "queued", "backlog":
		return sunTaskStatusTodo
	case "doing", "in-progress", "in_progress", "claimed", "active":
		return sunTaskStatusDoing
	case "done", "closed", "complete", "completed":
		return sunTaskStatusDone
	default:
		return sunTaskStatusTodo
	}
}

func parseSunTaskStatusFilter(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	switch strings.ToLower(trimmed) {
	case "todo", "open", "queued", "backlog":
		return sunTaskStatusTodo, nil
	case "doing", "in-progress", "in_progress", "claimed", "active":
		return sunTaskStatusDoing, nil
	case "done", "closed", "complete", "completed":
		return sunTaskStatusDone, nil
	default:
		return "", fmt.Errorf("invalid --status %q (expected todo|doing|done)", trimmed)
	}
}

func normalizeSunTaskPriority(raw string) string {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case sunTaskPriorityP1:
		return sunTaskPriorityP1
	case sunTaskPriorityP3:
		return sunTaskPriorityP3
	default:
		return sunTaskPriorityP2
	}
}

func sunTaskPriorityRank(priority string) int {
	switch normalizeSunTaskPriority(priority) {
	case sunTaskPriorityP1:
		return 1
	case sunTaskPriorityP2:
		return 2
	default:
		return 3
	}
}

func sunTaskboardFindTask(tasks []sunTaskboardTask, id string) int {
	needle := strings.TrimSpace(id)
	for i := range tasks {
		if strings.EqualFold(strings.TrimSpace(tasks[i].ID), needle) {
			return i
		}
	}
	return -1
}

func sunTaskboardSelectNextClaimable(tasks []sunTaskboardTask, now time.Time) int {
	candidates := make([]int, 0, len(tasks))
	for i := range tasks {
		status := normalizeSunTaskStatus(tasks[i].Status)
		if status == sunTaskStatusDone {
			continue
		}
		if tasks[i].Assignment == nil || sunTaskboardLockExpired(*tasks[i].Assignment, now) {
			candidates = append(candidates, i)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left := tasks[candidates[i]]
		right := tasks[candidates[j]]
		if sunTaskPriorityRank(left.Priority) != sunTaskPriorityRank(right.Priority) {
			return sunTaskPriorityRank(left.Priority) < sunTaskPriorityRank(right.Priority)
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

func sunTaskboardLockExpired(lock sunTaskboardLock, now time.Time) bool {
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

func sunTaskboardTouchAgent(board *sunTaskboard, identity sunTaskboardAgentIdentity, now time.Time, status string, currentTaskID string) {
	if board.Agents == nil {
		board.Agents = map[string]sunTaskboardAgent{}
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

func sunTaskboardSetAgentState(board *sunTaskboard, agentID string, now time.Time, status string, currentTaskID string) {
	id := strings.TrimSpace(agentID)
	if id == "" {
		return
	}
	if board.Agents == nil {
		board.Agents = map[string]sunTaskboardAgent{}
	}
	agent := board.Agents[id]
	agent.ID = id
	agent.Status = strings.TrimSpace(status)
	agent.CurrentTaskID = strings.TrimSpace(currentTaskID)
	agent.LastSeenAt = now.UTC().Format(time.RFC3339)
	board.Agents[id] = agent
}

func sunTaskboardClearOtherAgentsForTask(board *sunTaskboard, keepAgent string, taskID string, now time.Time) {
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

func sunTaskboardResolveAgent(settings Settings, explicitAgent string, dyad string, machine string) sunTaskboardAgentIdentity {
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
		agentID = strings.TrimSpace(settings.Sun.TaskboardAgent)
	}
	if agentID == "" {
		base := userName
		if dyad != "" {
			base = dyad
		}
		agentID = "dyad:" + base + "@" + host
	}
	return sunTaskboardAgentIdentity{
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

func sunTaskID(now time.Time, existing []sunTaskboardTask) string {
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

func sunTaskboardLockToken(now time.Time) string {
	n, err := secureIntn(1_000_000)
	if err != nil {
		n = int(now.UnixNano() % 1_000_000)
	}
	return fmt.Sprintf("lock-%d-%06d", now.UTC().Unix(), n)
}

func sunTaskboardName(settings Settings, explicit string) string {
	if strings.TrimSpace(explicit) != "" {
		return strings.TrimSpace(explicit)
	}
	if env := envSunTaskboard(); env != "" {
		return env
	}
	if strings.TrimSpace(settings.Sun.Taskboard) != "" {
		return strings.TrimSpace(settings.Sun.Taskboard)
	}
	return "default"
}

func sunTaskboardLeaseSeconds(settings Settings, explicit int) int {
	if explicit > 0 {
		return explicit
	}
	if env := envSunTaskboardLeaseSeconds(); env != "" {
		if parsed, err := strconv.Atoi(env); err == nil && parsed > 0 {
			return parsed
		}
	}
	if settings.Sun.TaskboardLeaseSeconds > 0 {
		return settings.Sun.TaskboardLeaseSeconds
	}
	return 1800
}

func isSunStatus(err error, statusCode int) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), fmt.Sprintf("status %d", statusCode))
}

func sunSortTasks(tasks []sunTaskboardTask) {
	sort.SliceStable(tasks, func(i, j int) bool {
		leftStatus := normalizeSunTaskStatus(tasks[i].Status)
		rightStatus := normalizeSunTaskStatus(tasks[j].Status)
		if leftStatus != rightStatus {
			return sunTaskStatusSortRank(leftStatus) < sunTaskStatusSortRank(rightStatus)
		}
		if sunTaskPriorityRank(tasks[i].Priority) != sunTaskPriorityRank(tasks[j].Priority) {
			return sunTaskPriorityRank(tasks[i].Priority) < sunTaskPriorityRank(tasks[j].Priority)
		}
		leftCreated := parseRFC3339(tasks[i].CreatedAt)
		rightCreated := parseRFC3339(tasks[j].CreatedAt)
		if !leftCreated.Equal(rightCreated) {
			return leftCreated.Before(rightCreated)
		}
		return strings.Compare(tasks[i].ID, tasks[j].ID) < 0
	})
}

func sunTaskStatusSortRank(status string) int {
	switch normalizeSunTaskStatus(status) {
	case sunTaskStatusDoing:
		return 1
	case sunTaskStatusTodo:
		return 2
	default:
		return 3
	}
}

func printSunTaskRows(tasks []sunTaskboardTask) {
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
