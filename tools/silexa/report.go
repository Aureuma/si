package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

func cmdReport(args []string) {
	if len(args) == 0 {
		fmt.Println("usage: silexa report <status|escalate|review|dyad>")
		return
	}
	switch args[0] {
	case "status":
		cmdReportStatus(args[1:])
	case "escalate":
		cmdReportEscalate(args[1:])
	case "review":
		cmdReportReview(args[1:])
	case "dyad":
		cmdReportDyad(args[1:])
	default:
		fmt.Println("unknown report command:", args[0])
	}
}

type humanTask struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	RequestedBy string `json:"requested_by"`
	Status      string `json:"status"`
}

type accessRequest struct {
	ID       int    `json:"id"`
	Requester string `json:"requester"`
	Resource string `json:"resource"`
	Action   string `json:"action"`
	Status   string `json:"status"`
}

type feedback struct {
	Severity string `json:"severity"`
	Source   string `json:"source"`
	Message  string `json:"message"`
}

func cmdReportStatus(args []string) {
	managerURL := envOr("MANAGER_URL", "http://localhost:9090")
	tasks := []humanTask{}
	access := []accessRequest{}
	feedbacks := []feedback{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := getJSON(ctx, strings.TrimRight(managerURL, "/")+"/human-tasks", &tasks); err != nil {
		fatal(err)
	}
	if err := getJSON(ctx, strings.TrimRight(managerURL, "/")+"/access-requests", &access); err != nil {
		fatal(err)
	}
	if err := getJSON(ctx, strings.TrimRight(managerURL, "/")+"/feedback", &feedbacks); err != nil {
		fatal(err)
	}

	openTasks := make([]humanTask, 0)
	doneTasks := 0
	for _, t := range tasks {
		if strings.ToLower(strings.TrimSpace(t.Status)) == "done" {
			doneTasks++
		} else {
			openTasks = append(openTasks, t)
		}
	}
	pendingAccess := make([]accessRequest, 0)
	for _, a := range access {
		if strings.ToLower(strings.TrimSpace(a.Status)) == "pending" {
			pendingAccess = append(pendingAccess, a)
		}
	}
	recentFeedback := feedbacks
	if len(recentFeedback) > 5 {
		recentFeedback = recentFeedback[len(recentFeedback)-5:]
	}

	lines := []string{
		"Silexa Status",
		fmt.Sprintf("Tasks: %d/%d %s", doneTasks, len(tasks), bar(doneTasks, len(tasks), 10)),
	}
	if len(openTasks) > 0 {
		lines = append(lines, "Open tasks (top 5):")
		for i, t := range openTasks {
			if i >= 5 {
				break
			}
			lines = append(lines, fmt.Sprintf("  - #%d %s (%s)", t.ID, t.Title, t.RequestedBy))
		}
	}
	if len(pendingAccess) > 0 {
		lines = append(lines, fmt.Sprintf("Pending access: %d", len(pendingAccess)))
		for i, a := range pendingAccess {
			if i >= 5 {
				break
			}
			lines = append(lines, fmt.Sprintf("  - #%d %s -> %s (%s)", a.ID, a.Requester, a.Resource, a.Action))
		}
	}
	if len(recentFeedback) > 0 {
		lines = append(lines, "Recent feedback:")
		for _, f := range recentFeedback {
			lines = append(lines, fmt.Sprintf("  - [%s] %s: %s", f.Severity, f.Source, f.Message))
		}
	}
	report := strings.Join(lines, "\n")
	fmt.Println(report)

	if chatID := strings.TrimSpace(os.Getenv("TELEGRAM_CHAT_ID")); chatID != "" {
		notifyURL := envOr("NOTIFY_URL", "http://localhost:8081/notify")
		if err := sendNotify(notifyURL, report, chatID); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "notify failed:", err)
		}
	}
	_ = args
}

func cmdReportEscalate(args []string) {
	managerURL := envOr("MANAGER_URL", "http://localhost:9090")
	tasks := []humanTask{}
	access := []accessRequest{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := getJSON(ctx, strings.TrimRight(managerURL, "/")+"/human-tasks", &tasks); err != nil {
		fatal(err)
	}
	if err := getJSON(ctx, strings.TrimRight(managerURL, "/")+"/access-requests", &access); err != nil {
		fatal(err)
	}
	openTasks := make([]humanTask, 0)
	for _, t := range tasks {
		if strings.ToLower(strings.TrimSpace(t.Status)) != "done" {
			openTasks = append(openTasks, t)
		}
	}
	pendingAccess := make([]accessRequest, 0)
	for _, a := range access {
		if strings.ToLower(strings.TrimSpace(a.Status)) == "pending" {
			pendingAccess = append(pendingAccess, a)
		}
	}
	lines := []string{"Escalation"}
	if len(openTasks) > 0 {
		lines = append(lines, "Open tasks:")
		for i, t := range openTasks {
			if i >= 10 {
				break
			}
			lines = append(lines, fmt.Sprintf("- #%d %s (by %s)", t.ID, t.Title, t.RequestedBy))
		}
	}
	if len(pendingAccess) > 0 {
		lines = append(lines, "Pending access:")
		for i, a := range pendingAccess {
			if i >= 10 {
				break
			}
			lines = append(lines, fmt.Sprintf("- #%d %s -> %s (%s)", a.ID, a.Requester, a.Resource, a.Action))
		}
	}
	report := strings.Join(lines, "\n")
	fmt.Println(report)

	_ = postJSON(context.Background(), strings.TrimRight(managerURL, "/")+"/feedback", map[string]string{
		"severity": "info",
		"message":  report,
		"source":   "escalate-blockers",
		"context":  "open tasks/pending access",
	}, nil)

	if chatID := strings.TrimSpace(os.Getenv("TELEGRAM_CHAT_ID")); chatID != "" {
		notifyURL := envOr("NOTIFY_URL", "http://localhost:8081/notify")
		if err := sendNotify(notifyURL, report, chatID); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "notify failed:", err)
		}
	}
	_ = args
}

func cmdReportReview(args []string) {
	managerURL := envOr("MANAGER_URL", "http://localhost:9090")
	tasks := []humanTask{}
	access := []accessRequest{}
	feedbacks := []feedback{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := getJSON(ctx, strings.TrimRight(managerURL, "/")+"/human-tasks", &tasks); err != nil {
		fatal(err)
	}
	if err := getJSON(ctx, strings.TrimRight(managerURL, "/")+"/access-requests", &access); err != nil {
		fatal(err)
	}
	if err := getJSON(ctx, strings.TrimRight(managerURL, "/")+"/feedback", &feedbacks); err != nil {
		fatal(err)
	}
	openTasks := make([]humanTask, 0)
	for _, t := range tasks {
		if strings.ToLower(strings.TrimSpace(t.Status)) != "done" {
			openTasks = append(openTasks, t)
		}
	}
	pendingAccess := make([]accessRequest, 0)
	for _, a := range access {
		if strings.ToLower(strings.TrimSpace(a.Status)) == "pending" {
			pendingAccess = append(pendingAccess, a)
		}
	}
	recentFeedback := feedbacks
	if len(recentFeedback) > 5 {
		recentFeedback = recentFeedback[len(recentFeedback)-5:]
	}
	lines := []string{"High-stakes review"}
	if len(openTasks) > 0 {
		lines = append(lines, "Open tasks:")
		for i, t := range openTasks {
			if i >= 5 {
				break
			}
			lines = append(lines, fmt.Sprintf("- #%d %s (%s)", t.ID, t.Title, t.RequestedBy))
		}
	}
	if len(pendingAccess) > 0 {
		lines = append(lines, "Pending access:")
		for i, a := range pendingAccess {
			if i >= 5 {
				break
			}
			lines = append(lines, fmt.Sprintf("- #%d %s -> %s (%s)", a.ID, a.Requester, a.Resource, a.Action))
		}
	}
	if len(recentFeedback) > 0 {
		lines = append(lines, "Feedback:")
		for _, f := range recentFeedback {
			lines = append(lines, fmt.Sprintf("- [%s] %s: %s", f.Severity, f.Source, f.Message))
		}
	}
	report := strings.Join(lines, "\n")
	fmt.Println(report)

	if chatID := strings.TrimSpace(os.Getenv("TELEGRAM_CHAT_ID")); chatID != "" {
		notifyURL := envOr("NOTIFY_URL", "http://localhost:8081/notify")
		if err := sendNotify(notifyURL, report, chatID); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "notify failed:", err)
		}
	}
	_ = args
}

type heartbeat struct {
	Dyad   string    `json:"dyad"`
	Actor  string    `json:"actor"`
	Critic string    `json:"critic"`
	When   time.Time `json:"when"`
}

type dyadTask struct {
	ID       int    `json:"id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Priority string `json:"priority"`
	Actor    string `json:"actor"`
	Critic   string `json:"critic"`
	Dyad     string `json:"dyad"`
}

func cmdReportDyad(args []string) {
	if len(args) < 1 {
		fmt.Println("usage: silexa report dyad <name>")
		return
	}
	dyad := args[0]
	managerURL := envOr("MANAGER_URL", "http://localhost:9090")
	beats := []heartbeat{}
	tasks := []dyadTask{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := getJSON(ctx, strings.TrimRight(managerURL, "/")+"/beats", &beats); err != nil {
		fatal(err)
	}
	if err := getJSON(ctx, strings.TrimRight(managerURL, "/")+"/dyad-tasks", &tasks); err != nil {
		fatal(err)
	}
	var last heartbeat
	found := false
	for _, b := range beats {
		if b.Dyad != dyad {
			continue
		}
		if !found || b.When.After(last.When) {
			last = b
			found = true
		}
	}
	openTasks := make([]dyadTask, 0)
	for _, t := range tasks {
		if t.Dyad == dyad && strings.ToLower(strings.TrimSpace(t.Status)) != "done" {
			openTasks = append(openTasks, t)
		}
	}
	sort.Slice(openTasks, func(i, j int) bool {
		return openTasks[i].ID < openTasks[j].ID
	})
	lines := []string{
		fmt.Sprintf("Dyad report: %s", dyad),
	}
	if found {
		lines = append(lines, fmt.Sprintf("Actor: %s, last beat: %s", valueOr(last.Actor, "n/a"), last.When.Format(time.RFC3339)))
		lines = append(lines, fmt.Sprintf("Critic: %s, last beat: %s", valueOr(last.Critic, "n/a"), last.When.Format(time.RFC3339)))
	} else {
		lines = append(lines, "No beats recorded.")
	}
	if len(openTasks) > 0 {
		lines = append(lines, "Tasks:")
		for _, t := range openTasks {
			lines = append(lines, fmt.Sprintf("- #%d %s [%s] prio=%s actor=%s critic=%s", t.ID, t.Title, t.Status, t.Priority, t.Actor, t.Critic))
		}
	} else {
		lines = append(lines, "Tasks: none open")
	}
	report := strings.Join(lines, "\n")
	fmt.Println(report)

	_ = postJSON(context.Background(), strings.TrimRight(managerURL, "/")+"/feedback", map[string]string{
		"source":   "critic-router",
		"severity": "info",
		"message":  report,
		"context":  "dyad-report:" + dyad,
	}, nil)
}

func bar(done, total, length int) string {
	if length <= 0 {
		length = 10
	}
	if total <= 0 {
		return strings.Repeat("-", length)
	}
	filled := int(float64(length) * float64(done) / float64(total))
	if filled > length {
		filled = length
	}
	return strings.Repeat("#", filled) + strings.Repeat("-", length-filled)
}

func valueOr(val, def string) string {
	if strings.TrimSpace(val) == "" {
		return def
	}
	return val
}

