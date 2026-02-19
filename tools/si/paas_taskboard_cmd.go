package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	paasTaskboardPathEnvKey      = "SI_PAAS_TASKBOARD_PATH"
	defaultTaskboardJSONRelPath  = "tickets/taskboard/shared-taskboard.json"
	defaultTaskboardMarkdownPath = "tickets/taskboard/SHARED_TASKBOARD.md"
	paasTaskboardShowUsageText   = "usage: si paas taskboard show [--json]"
	paasTaskboardListUsageText   = "usage: si paas taskboard list [--status <column>] [--priority <P1|P2|P3>] [--limit <n>] [--json]"
	paasTaskboardAddUsageText    = "usage: si paas taskboard add --title <text> [--status <column>] [--priority <P1|P2|P3>] [--owner <name>] [--workstream <name>] [--ticket <path>] [--tags <csv>] [--json]"
	paasTaskboardMoveUsageText   = "usage: si paas taskboard move --id <task-id> --status <column> [--json]"
)

var paasTaskboardActions = []subcommandAction{
	{Name: "show", Description: "show taskboard summary"},
	{Name: "list", Description: "list taskboard tasks"},
	{Name: "add", Description: "add taskboard task"},
	{Name: "move", Description: "move task to another column"},
}

type paasTaskboard struct {
	Version   int                      `json:"version"`
	UpdatedAt string                   `json:"updated_at"`
	Columns   []paasTaskboardColumn    `json:"columns"`
	Tasks     []paasTaskboardTask      `json:"tasks"`
	History   []paasTaskboardRunRecord `json:"history,omitempty"`
}

type paasTaskboardColumn struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type paasTaskboardTask struct {
	ID          string                 `json:"id"`
	Title       string                 `json:"title"`
	Status      string                 `json:"status"`
	Priority    string                 `json:"priority"`
	Workstream  string                 `json:"workstream,omitempty"`
	Owner       string                 `json:"owner,omitempty"`
	Score       int                    `json:"score,omitempty"`
	Matched     []string               `json:"matched_terms,omitempty"`
	ActionPlan  paasTaskboardAction    `json:"action_plan,omitempty"`
	Source      *paasTaskboardSource   `json:"source,omitempty"`
	TicketPath  string                 `json:"ticket_path,omitempty"`
	CreatedAt   string                 `json:"created_at,omitempty"`
	UpdatedAt   string                 `json:"updated_at,omitempty"`
	Tags        []string               `json:"tags,omitempty"`
	Extensions  map[string]interface{} `json:"-"`
}

type paasTaskboardAction struct {
	Opportunity string `json:"opportunity,omitempty"`
	Plan        string `json:"plan,omitempty"`
	Experiment  string `json:"experiment,omitempty"`
}

type paasTaskboardSource struct {
	Link        string `json:"link,omitempty"`
	Feed        string `json:"feed,omitempty"`
	PublishedAt string `json:"published_at,omitempty"`
}

type paasTaskboardRunRecord struct {
	RanAt           string `json:"ran_at,omitempty"`
	SignalsScanned  int    `json:"signals_scanned,omitempty"`
	TopOpportunities int   `json:"top_opportunities,omitempty"`
	NewTasks        int    `json:"new_tasks,omitempty"`
}

func cmdPaasTaskboard(args []string) {
	resolved, showUsage, ok := resolveSubcommandDispatchArgs(args, isInteractiveTerminal(), selectPaasTaskboardAction)
	if showUsage {
		printUsage(paasTaskboardUsageText)
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
		printUsage(paasTaskboardUsageText)
	case "show":
		cmdPaasTaskboardShow(rest)
	case "list":
		cmdPaasTaskboardList(rest)
	case "add":
		cmdPaasTaskboardAdd(rest)
	case "move":
		cmdPaasTaskboardMove(rest)
	default:
		printUnknown("paas taskboard", sub)
		printUsage(paasTaskboardUsageText)
		os.Exit(1)
	}
}

func selectPaasTaskboardAction() (string, bool) {
	return selectSubcommandAction("PaaS taskboard commands:", paasTaskboardActions)
}

func cmdPaasTaskboardShow(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas taskboard show", flag.ExitOnError)
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasTaskboardShowUsageText)
		return
	}
	board, jsonPath, mdPath, err := loadPaasTaskboard()
	if err != nil {
		failPaasCommand("taskboard show", jsonOut, err, nil)
	}
	if jsonOut {
		payload := map[string]any{
			"ok":       true,
			"command":  "taskboard show",
			"context":  currentPaasContext(),
			"mode":     "live",
			"count":    len(board.Tasks),
			"json_path": jsonPath,
			"md_path":   mdPath,
			"data":     board,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		_ = recordPaasAuditEvent("taskboard show", "succeeded", "live", map[string]string{
			"count": intString(len(board.Tasks)),
		}, nil)
		return
	}
	printPaasScaffold("taskboard show", map[string]string{
		"count":    intString(len(board.Tasks)),
		"json_path": jsonPath,
		"md_path":   mdPath,
	}, false)
	for _, column := range board.Columns {
		count := 0
		for _, task := range board.Tasks {
			if task.Status == column.ID {
				count++
			}
		}
		fmt.Printf("  - %s (%s): %d\n", column.Name, column.ID, count)
	}
}

func cmdPaasTaskboardList(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas taskboard list", flag.ExitOnError)
	status := fs.String("status", "", "column id filter")
	priority := fs.String("priority", "", "priority filter (P1|P2|P3)")
	limit := fs.Int("limit", 50, "max tasks")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasTaskboardListUsageText)
		return
	}
	if *limit < 1 {
		fmt.Fprintln(os.Stderr, "--limit must be >= 1")
		printUsage(paasTaskboardListUsageText)
		return
	}
	board, jsonPath, _, err := loadPaasTaskboard()
	if err != nil {
		failPaasCommand("taskboard list", jsonOut, err, nil)
	}
	filterStatus := strings.ToLower(strings.TrimSpace(*status))
	filterPriority := normalizeTaskboardPriority(*priority)
	rows := filterPaasTaskboardTasks(board.Tasks, filterStatus, filterPriority, *limit)
	if jsonOut {
		payload := map[string]any{
			"ok":       true,
			"command":  "taskboard list",
			"context":  currentPaasContext(),
			"mode":     "live",
			"count":    len(rows),
			"limit":    *limit,
			"status":   filterStatus,
			"priority": filterPriority,
			"path":     jsonPath,
			"data":     rows,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		_ = recordPaasAuditEvent("taskboard list", "succeeded", "live", map[string]string{
			"count":    intString(len(rows)),
			"status":   filterStatus,
			"priority": filterPriority,
		}, nil)
		return
	}
	printPaasScaffold("taskboard list", map[string]string{
		"count":    intString(len(rows)),
		"status":   filterStatus,
		"priority": filterPriority,
		"path":     jsonPath,
	}, false)
	for _, row := range rows {
		fmt.Printf("  - %s [%s] %s owner=%s status=%s ticket=%s\n",
			row.ID,
			row.Priority,
			row.Title,
			row.Owner,
			row.Status,
			row.TicketPath,
		)
	}
}

func cmdPaasTaskboardAdd(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas taskboard add", flag.ExitOnError)
	title := fs.String("title", "", "task title")
	status := fs.String("status", "paas-backlog", "column id")
	priority := fs.String("priority", "P2", "priority (P1|P2|P3)")
	owner := fs.String("owner", "automation-agent", "owner")
	workstream := fs.String("workstream", "paas", "workstream")
	ticketPath := fs.String("ticket", "", "ticket path")
	tags := fs.String("tags", "", "csv tags")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasTaskboardAddUsageText)
		return
	}
	if !requirePaasValue(*title, "title", paasTaskboardAddUsageText) {
		return
	}
	board, jsonPath, mdPath, err := loadPaasTaskboard()
	if err != nil {
		failPaasCommand("taskboard add", jsonOut, err, nil)
	}
	normalizedStatus := strings.ToLower(strings.TrimSpace(*status))
	if !taskboardStatusExists(board, normalizedStatus) {
		failPaasCommand("taskboard add", jsonOut, fmt.Errorf("status %q is not defined in board columns", normalizedStatus), nil)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	task := paasTaskboardTask{
		ID:         nextPaasTaskboardTaskID(board.Tasks),
		Title:      strings.TrimSpace(*title),
		Status:     normalizedStatus,
		Priority:   normalizeTaskboardPriority(*priority),
		Owner:      strings.TrimSpace(*owner),
		Workstream: strings.TrimSpace(*workstream),
		TicketPath: strings.TrimSpace(*ticketPath),
		Tags:       parseCSV(*tags),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if task.Priority == "" {
		task.Priority = "P2"
	}
	board.Tasks = append(board.Tasks, task)
	board.UpdatedAt = now
	if err := savePaasTaskboard(board, jsonPath, mdPath); err != nil {
		failPaasCommand("taskboard add", jsonOut, err, nil)
	}
	if jsonOut {
		payload := map[string]any{
			"ok":      true,
			"command": "taskboard add",
			"context": currentPaasContext(),
			"mode":    "live",
			"path":    jsonPath,
			"data":    task,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		_ = recordPaasAuditEvent("taskboard add", "succeeded", "live", map[string]string{
			"id":     task.ID,
			"status": task.Status,
		}, nil)
		return
	}
	printPaasScaffold("taskboard add", map[string]string{
		"id":       task.ID,
		"status":   task.Status,
		"priority": task.Priority,
		"title":    task.Title,
		"path":     jsonPath,
	}, false)
}

func cmdPaasTaskboardMove(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas taskboard move", flag.ExitOnError)
	id := fs.String("id", "", "task id")
	status := fs.String("status", "", "new column id")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasTaskboardMoveUsageText)
		return
	}
	if !requirePaasValue(*id, "id", paasTaskboardMoveUsageText) {
		return
	}
	if !requirePaasValue(*status, "status", paasTaskboardMoveUsageText) {
		return
	}
	board, jsonPath, mdPath, err := loadPaasTaskboard()
	if err != nil {
		failPaasCommand("taskboard move", jsonOut, err, nil)
	}
	normalizedStatus := strings.ToLower(strings.TrimSpace(*status))
	if !taskboardStatusExists(board, normalizedStatus) {
		failPaasCommand("taskboard move", jsonOut, fmt.Errorf("status %q is not defined in board columns", normalizedStatus), nil)
	}
	index := findPaasTaskboardTaskIndex(board.Tasks, *id)
	if index < 0 {
		failPaasCommand("taskboard move", jsonOut, fmt.Errorf("task %q not found", strings.TrimSpace(*id)), nil)
	}
	board.Tasks[index].Status = normalizedStatus
	board.Tasks[index].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	board.UpdatedAt = board.Tasks[index].UpdatedAt
	if err := savePaasTaskboard(board, jsonPath, mdPath); err != nil {
		failPaasCommand("taskboard move", jsonOut, err, nil)
	}
	if jsonOut {
		payload := map[string]any{
			"ok":      true,
			"command": "taskboard move",
			"context": currentPaasContext(),
			"mode":    "live",
			"path":    jsonPath,
			"data":    board.Tasks[index],
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		_ = recordPaasAuditEvent("taskboard move", "succeeded", "live", map[string]string{
			"id":     board.Tasks[index].ID,
			"status": board.Tasks[index].Status,
		}, nil)
		return
	}
	printPaasScaffold("taskboard move", map[string]string{
		"id":     board.Tasks[index].ID,
		"status": board.Tasks[index].Status,
		"path":   jsonPath,
	}, false)
}

func loadPaasTaskboard() (paasTaskboard, string, string, error) {
	jsonPath, mdPath, err := resolvePaasTaskboardPaths(currentPaasContext())
	if err != nil {
		return paasTaskboard{}, "", "", err
	}
	raw, err := os.ReadFile(jsonPath) // #nosec G304 -- local config path from trusted resolver.
	if err != nil {
		if os.IsNotExist(err) {
			board := defaultPaasTaskboard()
			board.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			return board, jsonPath, mdPath, nil
		}
		return paasTaskboard{}, jsonPath, mdPath, err
	}
	var board paasTaskboard
	if err := json.Unmarshal(raw, &board); err != nil {
		return paasTaskboard{}, jsonPath, mdPath, fmt.Errorf("invalid taskboard file: %w", err)
	}
	board = normalizePaasTaskboard(board)
	return board, jsonPath, mdPath, nil
}

func resolvePaasTaskboardPaths(contextName string) (string, string, error) {
	if assigned := strings.TrimSpace(os.Getenv(paasTaskboardPathEnvKey)); assigned != "" {
		jsonPath := filepath.Clean(assigned)
		mdPath := filepath.Join(filepath.Dir(jsonPath), filepath.Base(defaultTaskboardMarkdownPath))
		return jsonPath, mdPath, nil
	}
	if root, err := repoRoot(); err == nil {
		jsonPath := filepath.Join(root, defaultTaskboardJSONRelPath)
		mdPath := filepath.Join(root, defaultTaskboardMarkdownPath)
		return jsonPath, mdPath, nil
	}
	contextDir, err := resolvePaasContextDir(contextName)
	if err != nil {
		return "", "", err
	}
	jsonPath := filepath.Join(contextDir, "taskboard", "shared-taskboard.json")
	mdPath := filepath.Join(contextDir, "taskboard", "SHARED_TASKBOARD.md")
	return jsonPath, mdPath, nil
}

func savePaasTaskboard(board paasTaskboard, jsonPath, mdPath string) error {
	board = normalizePaasTaskboard(board)
	board.UpdatedAt = strings.TrimSpace(board.UpdatedAt)
	if board.UpdatedAt == "" {
		board.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	raw, err := json.MarshalIndent(board, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(filepath.Dir(jsonPath), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(jsonPath, raw, 0o600); err != nil {
		return err
	}
	markdown := renderPaasTaskboardMarkdown(board)
	if err := os.MkdirAll(filepath.Dir(mdPath), 0o700); err != nil {
		return err
	}
	return os.WriteFile(mdPath, []byte(markdown), 0o600)
}

func defaultPaasTaskboard() paasTaskboard {
	return paasTaskboard{
		Version: 1,
		Columns: defaultPaasTaskboardColumns(),
		Tasks:   []paasTaskboardTask{},
		History: []paasTaskboardRunRecord{},
	}
}

func defaultPaasTaskboardColumns() []paasTaskboardColumn {
	return []paasTaskboardColumn{
		{ID: "market-intel", Name: "Market Intel"},
		{ID: "paas-backlog", Name: "PaaS Backlog"},
		{ID: "paas-build", Name: "PaaS Build"},
		{ID: "validate", Name: "Validate"},
		{ID: "done", Name: "Done"},
	}
}

func normalizePaasTaskboard(board paasTaskboard) paasTaskboard {
	if board.Version <= 0 {
		board.Version = 1
	}
	if len(board.Columns) == 0 {
		board.Columns = defaultPaasTaskboardColumns()
	}
	validStatus := make(map[string]struct{}, len(board.Columns))
	for i := range board.Columns {
		board.Columns[i].ID = strings.ToLower(strings.TrimSpace(board.Columns[i].ID))
		board.Columns[i].Name = strings.TrimSpace(board.Columns[i].Name)
		if board.Columns[i].Name == "" {
			board.Columns[i].Name = board.Columns[i].ID
		}
		if board.Columns[i].ID != "" {
			validStatus[board.Columns[i].ID] = struct{}{}
		}
	}
	for i := range board.Tasks {
		board.Tasks[i].ID = strings.TrimSpace(board.Tasks[i].ID)
		board.Tasks[i].Title = strings.TrimSpace(board.Tasks[i].Title)
		board.Tasks[i].Status = strings.ToLower(strings.TrimSpace(board.Tasks[i].Status))
		if _, ok := validStatus[board.Tasks[i].Status]; !ok {
			board.Tasks[i].Status = "market-intel"
		}
		board.Tasks[i].Priority = normalizeTaskboardPriority(board.Tasks[i].Priority)
		if board.Tasks[i].Priority == "" {
			board.Tasks[i].Priority = "P2"
		}
		board.Tasks[i].Workstream = strings.TrimSpace(board.Tasks[i].Workstream)
		board.Tasks[i].Owner = strings.TrimSpace(board.Tasks[i].Owner)
		board.Tasks[i].TicketPath = strings.TrimSpace(board.Tasks[i].TicketPath)
		board.Tasks[i].CreatedAt = strings.TrimSpace(board.Tasks[i].CreatedAt)
		board.Tasks[i].UpdatedAt = strings.TrimSpace(board.Tasks[i].UpdatedAt)
		board.Tasks[i].Tags = parseCSV(strings.Join(board.Tasks[i].Tags, ","))
		board.Tasks[i].Matched = parseCSV(strings.Join(board.Tasks[i].Matched, ","))
	}
	sort.SliceStable(board.Tasks, func(i, j int) bool {
		leftPriority := taskboardPriorityRank(board.Tasks[i].Priority)
		rightPriority := taskboardPriorityRank(board.Tasks[j].Priority)
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		leftTime := parsePaasIncidentQueueTimestamp(board.Tasks[i].UpdatedAt)
		rightTime := parsePaasIncidentQueueTimestamp(board.Tasks[j].UpdatedAt)
		if !leftTime.Equal(rightTime) {
			return leftTime.After(rightTime)
		}
		return strings.ToLower(board.Tasks[i].ID) < strings.ToLower(board.Tasks[j].ID)
	})
	return board
}

func normalizeTaskboardPriority(value string) string {
	priority := strings.ToUpper(strings.TrimSpace(value))
	switch priority {
	case "P1", "P2", "P3":
		return priority
	default:
		return ""
	}
}

func taskboardPriorityRank(priority string) int {
	switch strings.ToUpper(strings.TrimSpace(priority)) {
	case "P1":
		return 1
	case "P2":
		return 2
	case "P3":
		return 3
	default:
		return 9
	}
}

func taskboardStatusExists(board paasTaskboard, status string) bool {
	needle := strings.ToLower(strings.TrimSpace(status))
	if needle == "" {
		return false
	}
	for _, column := range board.Columns {
		if strings.EqualFold(strings.TrimSpace(column.ID), needle) {
			return true
		}
	}
	return false
}

func filterPaasTaskboardTasks(tasks []paasTaskboardTask, status, priority string, limit int) []paasTaskboardTask {
	rows := make([]paasTaskboardTask, 0, len(tasks))
	for _, row := range tasks {
		if status != "" && !strings.EqualFold(strings.TrimSpace(row.Status), strings.TrimSpace(status)) {
			continue
		}
		if priority != "" && !strings.EqualFold(strings.TrimSpace(row.Priority), strings.TrimSpace(priority)) {
			continue
		}
		rows = append(rows, row)
		if len(rows) == limit {
			break
		}
	}
	return rows
}

func findPaasTaskboardTaskIndex(tasks []paasTaskboardTask, id string) int {
	needle := strings.ToLower(strings.TrimSpace(id))
	if needle == "" {
		return -1
	}
	for i, row := range tasks {
		if strings.ToLower(strings.TrimSpace(row.ID)) == needle {
			return i
		}
	}
	return -1
}

func nextPaasTaskboardTaskID(tasks []paasTaskboardTask) string {
	prefix := "tb-" + time.Now().UTC().Format("20060102t150405")
	for i := 1; i < 10000; i++ {
		candidate := fmt.Sprintf("%s-%03d", prefix, i)
		if findPaasTaskboardTaskIndex(tasks, candidate) == -1 {
			return candidate
		}
	}
	return fmt.Sprintf("tb-%d", time.Now().UTC().UnixNano())
}

func renderPaasTaskboardMarkdown(board paasTaskboard) string {
	lines := []string{
		"# Shared Market Taskboard",
		"",
		"Updated: " + strings.TrimSpace(board.UpdatedAt),
		"",
	}
	for _, column := range board.Columns {
		lines = append(lines, "## "+column.Name)
		colTasks := make([]paasTaskboardTask, 0)
		for _, task := range board.Tasks {
			if strings.EqualFold(task.Status, column.ID) {
				colTasks = append(colTasks, task)
			}
		}
		if len(colTasks) == 0 {
			lines = append(lines, "- _(none)_", "")
			continue
		}
		for _, task := range colTasks {
			lines = append(lines, fmt.Sprintf("- **[%s] %s**", task.Priority, task.Title))
			lines = append(lines, fmt.Sprintf("  - Owner: `%s`", task.Owner))
			lines = append(lines, fmt.Sprintf("  - Workstream: `%s`", task.Workstream))
			if strings.TrimSpace(task.TicketPath) != "" {
				lines = append(lines, fmt.Sprintf("  - Ticket: `%s`", task.TicketPath))
			}
			if strings.TrimSpace(task.ActionPlan.Plan) != "" {
				lines = append(lines, fmt.Sprintf("  - Plan: %s", strings.TrimSpace(task.ActionPlan.Plan)))
			}
		}
		lines = append(lines, "")
	}
	return strings.TrimSpace(strings.Join(lines, "\n")) + "\n"
}
