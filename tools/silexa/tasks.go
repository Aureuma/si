package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func cmdTask(args []string) {
	if len(args) == 0 {
		fmt.Println("usage: si task <add|add-dyad|update>")
		return
	}
	switch args[0] {
	case "add":
		cmdTaskAdd(args[1:])
	case "add-dyad":
		cmdTaskAddDyad(args[1:])
	case "update":
		cmdTaskUpdate(args[1:])
	default:
		fmt.Println("unknown task command:", args[0])
	}
}

func cmdTaskAdd(args []string) {
	if len(args) < 1 {
		fmt.Println("usage: si task add <title> [kind] [priority] [description] [link] [notes] [complexity]")
		return
	}
	title := args[0]
	kind := argOr(args, 1, "")
	priority := argOr(args, 2, "normal")
	desc := argOr(args, 3, "")
	link := argOr(args, 4, "")
	notes := argOr(args, 5, "")
	complexity := argOr(args, 6, envOr("DYAD_TASK_COMPLEXITY", ""))
	requestedBy := envOr("REQUESTED_BY", "router")
	managerURL := envOr("MANAGER_URL", "http://localhost:9090")

	payload := dyadTaskPayload{
		Title:       title,
		Kind:        kind,
		Priority:    priority,
		Description: desc,
		Link:        link,
		Notes:       notes,
		Complexity:  complexity,
		RequestedBy: requestedBy,
	}
	if err := postDyadTask(managerURL, payload); err != nil {
		fatal(err)
	}
	fmt.Println("task created")
}

func cmdTaskAddDyad(args []string) {
	if len(args) < 2 {
		fmt.Println("usage: si task add-dyad <title> <dyad> [actor] [critic] [priority] [description] [link] [notes] [complexity]")
		return
	}
	title := args[0]
	dyad := args[1]
	actor := argOr(args, 2, "")
	critic := argOr(args, 3, "")
	priority := argOr(args, 4, "normal")
	desc := argOr(args, 5, "")
	link := argOr(args, 6, "")
	notes := argOr(args, 7, "")
	complexity := argOr(args, 8, envOr("DYAD_TASK_COMPLEXITY", ""))
	kind := envOr("DYAD_TASK_KIND", "")
	requestedBy := envOr("REQUESTED_BY", "router")
	managerURL := envOr("MANAGER_URL", "http://localhost:9090")

	payload := dyadTaskPayload{
		Title:       title,
		Kind:        kind,
		Description: desc,
		Dyad:        dyad,
		Actor:       actor,
		Critic:      critic,
		Priority:    priority,
		Complexity:  complexity,
		RequestedBy: requestedBy,
		Notes:       notes,
		Link:        link,
	}
	if err := postDyadTask(managerURL, payload); err != nil {
		fatal(err)
	}
	fmt.Println("dyad task created")
}

func cmdTaskUpdate(args []string) {
	if len(args) < 2 {
		fmt.Println("usage: si task update <id> <status> [notes] [actor] [critic] [complexity]")
		return
	}
	id, err := strconv.Atoi(args[0])
	if err != nil || id <= 0 {
		fatal(fmt.Errorf("invalid id"))
	}
	status := args[1]
	notes := argOr(args, 2, "")
	actor := argOr(args, 3, "")
	critic := argOr(args, 4, "")
	complexity := argOr(args, 5, envOr("DYAD_TASK_COMPLEXITY", ""))
	managerURL := envOr("MANAGER_URL", "http://localhost:9090")

	payload := dyadTaskPayload{
		ID:         id,
		Status:     status,
		Notes:      notes,
		Actor:      actor,
		Critic:     critic,
		Complexity: complexity,
	}
	if err := postDyadTaskUpdate(managerURL, payload); err != nil {
		fatal(err)
	}
	fmt.Println("dyad task updated")
}

type dyadTaskPayload struct {
	ID          int    `json:"id,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Kind        string `json:"kind,omitempty"`
	Status      string `json:"status,omitempty"`
	Priority    string `json:"priority,omitempty"`
	Complexity  string `json:"complexity,omitempty"`
	Dyad        string `json:"dyad,omitempty"`
	Actor       string `json:"actor,omitempty"`
	Critic      string `json:"critic,omitempty"`
	RequestedBy string `json:"requested_by,omitempty"`
	Notes       string `json:"notes,omitempty"`
	Link        string `json:"link,omitempty"`
}

func postDyadTask(managerURL string, payload dyadTaskPayload) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return postJSON(ctx, strings.TrimRight(managerURL, "/")+"/dyad-tasks", payload, nil)
}

func postDyadTaskUpdate(managerURL string, payload dyadTaskPayload) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return postJSON(ctx, strings.TrimRight(managerURL, "/")+"/dyad-tasks/update", payload, nil)
}

func cmdHuman(args []string) {
	if len(args) == 0 {
		fmt.Println("usage: si human <add|complete>")
		return
	}
	switch args[0] {
	case "add":
		cmdHumanAdd(args[1:])
	case "complete":
		cmdHumanComplete(args[1:])
	default:
		fmt.Println("unknown human command:", args[0])
	}
}

func cmdHumanAdd(args []string) {
	if len(args) < 2 {
		fmt.Println("usage: si human add <title> <commands> [url] [timeout] [requested_by] [notes]")
		return
	}
	title := args[0]
	commands := args[1]
	urlVal := argOr(args, 2, "")
	timeoutVal := argOr(args, 3, "")
	requestedBy := argOr(args, 4, "")
	notes := argOr(args, 5, "")
	managerURL := envOr("MANAGER_URL", "http://localhost:9090")
	var chatID *int64
	if raw := strings.TrimSpace(os.Getenv("TELEGRAM_CHAT_ID")); raw != "" {
		if id, err := strconv.ParseInt(raw, 10, 64); err == nil {
			chatID = &id
		}
	}
	payload := map[string]interface{}{
		"title":        title,
		"commands":     commands,
		"url":          urlVal,
		"timeout":      timeoutVal,
		"requested_by": requestedBy,
		"notes":        notes,
	}
	if chatID != nil {
		payload["chat_id"] = *chatID
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := postJSON(ctx, strings.TrimRight(managerURL, "/")+"/human-tasks", payload, nil); err != nil {
		fatal(err)
	}
	fmt.Println("human task created")
}

func cmdHumanComplete(args []string) {
	if len(args) < 1 {
		fmt.Println("usage: si human complete <id>")
		return
	}
	id := args[0]
	managerURL := envOr("MANAGER_URL", "http://localhost:9090")
	u := strings.TrimRight(managerURL, "/") + "/human-tasks/complete?id=" + url.QueryEscape(id)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := postJSON(ctx, u, nil, nil); err != nil {
		fatal(err)
	}
	fmt.Println("human task completed")
}

func cmdFeedback(args []string) {
	if len(args) == 0 {
		fmt.Println("usage: si feedback <add|broadcast>")
		return
	}
	switch args[0] {
	case "add":
		cmdFeedbackAdd(args[1:])
	case "broadcast":
		cmdFeedbackBroadcast(args[1:])
	default:
		fmt.Println("unknown feedback command:", args[0])
	}
}

func cmdFeedbackAdd(args []string) {
	if len(args) < 2 {
		fmt.Println("usage: si feedback add <severity> <message> [source] [context]")
		return
	}
	sev := args[0]
	message := args[1]
	source := argOr(args, 2, "")
	contextVal := argOr(args, 3, "")
	managerURL := envOr("MANAGER_URL", "http://localhost:9090")
	payload := map[string]string{
		"severity": sev,
		"message":  message,
		"source":   source,
		"context":  contextVal,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := postJSON(ctx, strings.TrimRight(managerURL, "/")+"/feedback", payload, nil); err != nil {
		fatal(err)
	}
	fmt.Println("feedback posted")
}

func cmdFeedbackBroadcast(args []string) {
	if len(args) < 1 {
		fmt.Println("usage: si feedback broadcast <message> [severity]")
		return
	}
	message := args[0]
	sev := argOr(args, 1, "info")
	managerURL := envOr("MANAGER_URL", "http://localhost:9090")

	payload := map[string]string{
		"source":   "management",
		"severity": sev,
		"message":  message,
		"context":  "management-bridge",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := postJSON(ctx, strings.TrimRight(managerURL, "/")+"/feedback", payload, nil); err != nil {
		fatal(err)
	}

	if notifyURL := envOr("TELEGRAM_NOTIFY_URL", ""); notifyURL != "" {
		if err := sendNotify(notifyURL, message, os.Getenv("TELEGRAM_CHAT_ID")); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "notify failed:", err)
		}
	}
	fmt.Println("broadcast sent")
}

func cmdAccess(args []string) {
	if len(args) == 0 {
		fmt.Println("usage: si access <request|resolve>")
		return
	}
	switch args[0] {
	case "request":
		cmdAccessRequest(args[1:])
	case "resolve":
		cmdAccessResolve(args[1:])
	default:
		fmt.Println("unknown access command:", args[0])
	}
}

func cmdAccessRequest(args []string) {
	if len(args) < 3 {
		fmt.Println("usage: si access request <requester> <resource> <action> [reason] [department]")
		return
	}
	managerURL := envOr("MANAGER_URL", "http://localhost:9090")
	payload := map[string]string{
		"requester":  args[0],
		"resource":   args[1],
		"action":     args[2],
		"reason":     argOr(args, 3, ""),
		"department": argOr(args, 4, ""),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := postJSON(ctx, strings.TrimRight(managerURL, "/")+"/access-requests", payload, nil); err != nil {
		fatal(err)
	}
	fmt.Println("access request created")
}

func cmdAccessResolve(args []string) {
	if len(args) < 2 {
		fmt.Println("usage: si access resolve <id> <approved|denied> [resolved_by] [notes]")
		return
	}
	managerURL := envOr("MANAGER_URL", "http://localhost:9090")
	id := args[0]
	status := args[1]
	by := argOr(args, 2, "")
	notes := argOr(args, 3, "")
	u := fmt.Sprintf("%s/access-requests/resolve?id=%s&status=%s&by=%s&notes=%s",
		strings.TrimRight(managerURL, "/"),
		url.QueryEscape(id),
		url.QueryEscape(status),
		url.QueryEscape(by),
		url.QueryEscape(notes),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := postJSON(ctx, u, nil, nil); err != nil {
		fatal(err)
	}
	fmt.Println("access request resolved")
}

func cmdResource(args []string) {
	if len(args) == 0 {
		fmt.Println("usage: si resource <request>")
		return
	}
	switch args[0] {
	case "request":
		cmdResourceRequest(args[1:])
	default:
		fmt.Println("unknown resource command:", args[0])
	}
}

func cmdResourceRequest(args []string) {
	if len(args) < 3 {
		fmt.Println("usage: si resource request <resource> <action> <payload> [requested_by] [notes]")
		return
	}
	brokerURL := envOr("BROKER_URL", "http://localhost:9091")
	payload := map[string]string{
		"resource":     args[0],
		"action":       args[1],
		"payload":      args[2],
		"requested_by": argOr(args, 3, ""),
		"notes":        argOr(args, 4, ""),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := postJSON(ctx, strings.TrimRight(brokerURL, "/")+"/requests", payload, nil); err != nil {
		fatal(err)
	}
	fmt.Println("resource request created")
}

func cmdMetric(args []string) {
	if len(args) == 0 {
		fmt.Println("usage: si metric <post>")
		return
	}
	switch args[0] {
	case "post":
		cmdMetricPost(args[1:])
	default:
		fmt.Println("unknown metric command:", args[0])
	}
}

func cmdMetricPost(args []string) {
	if len(args) < 4 {
		fmt.Println("usage: si metric post <dyad> <department> <name> <value> [unit] [recorded_by]")
		return
	}
	value, err := strconv.ParseFloat(args[3], 64)
	if err != nil {
		fatal(fmt.Errorf("invalid value"))
	}
	payload := map[string]interface{}{
		"dyad":        args[0],
		"department":  args[1],
		"name":        args[2],
		"value":       value,
		"unit":        argOr(args, 4, "count"),
		"recorded_by": argOr(args, 5, "manual"),
	}
	managerURL := envOr("MANAGER_URL", "http://localhost:9090")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := postJSON(ctx, strings.TrimRight(managerURL, "/")+"/metrics", payload, nil); err != nil {
		fatal(err)
	}
	fmt.Println("metric posted")
}

func cmdNotify(args []string) {
	if len(args) < 1 {
		fmt.Println("usage: si notify <message>")
		return
	}
	msg := strings.Join(args, " ")
	notifyURL := envOr("NOTIFY_URL", "http://localhost:8081/notify")
	if err := sendNotify(notifyURL, msg, os.Getenv("TELEGRAM_CHAT_ID")); err != nil {
		fatal(err)
	}
	fmt.Println("notification sent")
}

func sendNotify(url, message, chatID string) error {
	payload := map[string]interface{}{
		"message": message,
	}
	if strings.TrimSpace(chatID) != "" {
		if parsed, err := strconv.ParseInt(strings.TrimSpace(chatID), 10, 64); err == nil {
			payload["chat_id"] = parsed
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return postJSON(ctx, strings.TrimRight(url, "/"), payload, nil)
}

func argOr(args []string, idx int, def string) string {
	if idx < len(args) {
		return args[idx]
	}
	return def
}

func cmdProfile(args []string) {
	if len(args) < 1 {
		fmt.Println("usage: si profile <name>")
		return
	}
	root := mustRepoRoot()
	path := filepath.Join(root, "profiles", args[0]+".md")
	data, ok, err := readFileTrim(path)
	if err != nil {
		fatal(err)
	}
	if !ok {
		fatal(fmt.Errorf("profile not found: %s", path))
	}
	fmt.Println(data)
}

func cmdCapability(args []string) {
	if len(args) < 1 {
		fmt.Println("usage: si capability <role>")
		return
	}
	role := args[0]
	if text, ok := capabilityText(role); ok {
		fmt.Println(text)
		return
	}
	fatal(fmt.Errorf("unknown role: %s", role))
}
