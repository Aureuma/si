package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"silexa/agents/manager/internal/state"
)

type notifier struct {
	url    string
	chatID *int64
	logger *log.Logger
}

type notifyResult struct {
	OK        bool `json:"ok"`
	Edited    bool `json:"edited"`
	MessageID int  `json:"message_id"`
}

func buildNotifier(logger *log.Logger) *notifier {
	url := os.Getenv("TELEGRAM_NOTIFY_URL")
	if url == "" {
		return nil
	}
	var chatID *int64
	if raw := os.Getenv("TELEGRAM_CHAT_ID"); raw != "" {
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil {
			chatID = &v
		} else {
			logger.Printf("invalid TELEGRAM_CHAT_ID: %v", err)
		}
	}
	return &notifier{url: url, chatID: chatID, logger: logger}
}

func (n *notifier) maybeSend(msg string, chatID *int64) {
	if n == nil || n.url == "" {
		return
	}
	targetChat := chatID
	if targetChat == nil {
		targetChat = n.chatID
	}
	if targetChat == nil {
		n.logger.Printf("skip notify: no chat id provided")
		return
	}
	payload := map[string]interface{}{
		"message":                  msg,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
		"chat_id":                  *targetChat,
	}
	b, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, n.url, bytes.NewReader(b))
	if err != nil {
		n.logger.Printf("notify build error: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		n.logger.Printf("notify send error: %v", err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		n.logger.Printf("notify non-2xx: %s", resp.Status)
	}
}

func (n *notifier) upsertDyadTaskMessage(t state.DyadTask) (int, bool) {
	if n == nil || n.url == "" {
		return 0, false
	}
	if n.chatID == nil {
		n.logger.Printf("skip notify: no chat id provided")
		return 0, false
	}
	payload := map[string]interface{}{
		"chat_id":                  *n.chatID,
		"message":                  formatDyadTaskMessage(t),
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	}
	if t.TelegramMessageID != 0 {
		payload["message_id"] = t.TelegramMessageID
	}
	b, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, n.url, bytes.NewReader(b))
	if err != nil {
		n.logger.Printf("notify build error: %v", err)
		return 0, false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		n.logger.Printf("notify send error: %v", err)
		return 0, false
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 300 {
		n.logger.Printf("notify non-2xx: %s", resp.Status)
		return 0, false
	}
	var out notifyResult
	if err := json.Unmarshal(body, &out); err != nil {
		return 0, false
	}
	return out.MessageID, out.Edited
}

func (n *notifier) upsertMessageHTML(message string, messageID int) (int, bool) {
	if n == nil || n.url == "" {
		return 0, false
	}
	if n.chatID == nil {
		n.logger.Printf("skip notify: no chat id provided")
		return 0, false
	}
	payload := map[string]interface{}{
		"chat_id":                  *n.chatID,
		"message":                  message,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	}
	if messageID > 0 {
		payload["message_id"] = messageID
	}
	b, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, n.url, bytes.NewReader(b))
	if err != nil {
		n.logger.Printf("notify build error: %v", err)
		return 0, false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		n.logger.Printf("notify send error: %v", err)
		return 0, false
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 300 {
		n.logger.Printf("notify non-2xx: %s", resp.Status)
		return 0, false
	}
	var out notifyResult
	if err := json.Unmarshal(body, &out); err != nil {
		return 0, false
	}
	return out.MessageID, out.Edited
}

func formatTaskMessage(t state.HumanTask) string {
	title := strings.TrimSpace(t.Title)
	if title == "" {
		title = fmt.Sprintf("Task #%d", t.ID)
	}

	var b strings.Builder
	b.WriteString("\U0001F9D1\u200D\U0001F4BB <b>Human Task</b>\n")
	b.WriteString("<b>Title:</b> " + html.EscapeString(title) + "\n")
	if strings.TrimSpace(t.Commands) != "" {
		b.WriteString("<b>Command:</b>\n<pre><code>" + html.EscapeString(strings.TrimSpace(t.Commands)) + "</code></pre>\n")
	}
	if strings.TrimSpace(t.URL) != "" {
		b.WriteString("<b>URL:</b> " + html.EscapeString(strings.TrimSpace(t.URL)) + "\n")
	}
	if strings.TrimSpace(t.Timeout) != "" {
		b.WriteString("<b>Timeout:</b> " + html.EscapeString(strings.TrimSpace(t.Timeout)) + "\n")
	}
	if strings.TrimSpace(t.RequestedBy) != "" {
		b.WriteString("<b>Requested By:</b> " + html.EscapeString(strings.TrimSpace(t.RequestedBy)) + "\n")
	}
	b.WriteString("<b>Status:</b> " + html.EscapeString(strings.TrimSpace(t.Status)) + "\n")
	b.WriteString("<b>Created:</b> " + formatUTCWhen(t.CreatedAt) + "\n")
	if strings.TrimSpace(t.Notes) != "" {
		b.WriteString("<b>Notes:</b>\n<pre><code>" + html.EscapeString(truncate(t.Notes, 900)) + "</code></pre>\n")
	}
	return strings.TrimSpace(b.String())
}

func formatDyadTaskMessage(t state.DyadTask) string {
	title := strings.TrimSpace(t.Title)
	if title == "" {
		title = fmt.Sprintf("Task #%d", t.ID)
	}
	status := strings.TrimSpace(t.Status)
	priority := strings.TrimSpace(t.Priority)
	kind := strings.TrimSpace(t.Kind)

	var b strings.Builder
	b.WriteString(statusEmoji(status) + " " + kindEmoji(kind) + " <b>Dyad Task</b>\n")
	b.WriteString("<b>Title:</b> " + html.EscapeString(title) + "\n")
	b.WriteString("<b>Status:</b> " + statusEmoji(status) + " " + html.EscapeString(status) + "\n")
	if priority != "" {
		b.WriteString("<b>Priority:</b> " + priorityEmoji(priority) + " " + html.EscapeString(priority) + "\n")
	}
	if kind != "" {
		b.WriteString("<b>Kind:</b> " + kindEmoji(kind) + " " + html.EscapeString(kind) + "\n")
	}
	if strings.TrimSpace(t.Dyad) != "" {
		b.WriteString("<b>Dyad:</b> " + html.EscapeString(strings.TrimSpace(t.Dyad)) + "\n")
	}
	if strings.TrimSpace(t.ClaimedBy) != "" {
		b.WriteString("<b>Owner:</b> " + html.EscapeString(strings.TrimSpace(t.ClaimedBy)) + "\n")
	}
	if strings.TrimSpace(t.Link) != "" {
		b.WriteString("<b>Link:</b> " + html.EscapeString(strings.TrimSpace(t.Link)) + "\n")
	}
	if strings.TrimSpace(t.Description) != "" {
		b.WriteString("<b>Details:</b>\n<pre><code>" + html.EscapeString(truncate(t.Description, 900)) + "</code></pre>\n")
	}
	if strings.TrimSpace(t.Notes) != "" {
		b.WriteString("<b>Notes:</b>\n<pre><code>" + html.EscapeString(truncate(t.Notes, 900)) + "</code></pre>\n")
	}

	when := t.UpdatedAt
	if when.IsZero() {
		when = t.CreatedAt
	}
	if !when.IsZero() {
		b.WriteString("<b>When (UTC):</b> " + formatUTCWhen(when) + "\n")
	}
	return strings.TrimSpace(b.String())
}

func shouldNotifyDyadTask(t state.DyadTask) bool {
	kind := strings.ToLower(strings.TrimSpace(t.Kind))
	status := strings.ToLower(strings.TrimSpace(t.Status))
	requestedBy := strings.ToLower(strings.TrimSpace(t.RequestedBy))
	priority := strings.ToLower(strings.TrimSpace(t.Priority))

	if strings.HasPrefix(requestedBy, "human") {
		return true
	}
	switch status {
	case "blocked", "review", "done":
		return true
	}
	switch priority {
	case "high", "p0", "urgent":
		return true
	}
	return false
}

func statusEmoji(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "todo", "open":
		return "\U0001F4DD"
	case "in_progress":
		return "\U0001F6A7"
	case "review":
		return "\U0001F50E"
	case "blocked":
		return "\u26D4"
	case "done":
		return "\u2705"
	default:
		return "\U0001F4CC"
	}
}

func priorityEmoji(priority string) string {
	switch strings.ToLower(strings.TrimSpace(priority)) {
	case "high", "p0", "urgent":
		return "\U0001F525"
	case "low", "p2":
		return "\U0001F7E9"
	case "normal", "medium", "p1", "":
		return "\U0001F7E6"
	default:
		return "\U0001F7EA"
	}
}

func kindEmoji(kind string) string {
	k := strings.ToLower(strings.TrimSpace(kind))
	switch {
	case strings.Contains(k, "stripe") || strings.Contains(k, "billing") || strings.Contains(k, "payments"):
		return "\U0001F4B3"
	case strings.Contains(k, "github"):
		return "\U0001F419"
	case strings.Contains(k, "mcp"):
		return "\U0001F50C"
	case strings.Contains(k, "codex"):
		return "\U0001F9E0"
	case strings.HasPrefix(k, "test."):
		return "\U0001F9EA"
	case strings.Contains(k, "docs"):
		return "\U0001F4DA"
	case strings.Contains(k, "infra"):
		return "\U0001F3D7"
	default:
		return "\U0001F9E9"
	}
}

func statusRank(status string) int {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "blocked":
		return 0
	case "review":
		return 1
	case "in_progress":
		return 2
	case "todo":
		return 3
	default:
		return 9
	}
}

func priorityRank(priority string) int {
	switch strings.ToLower(strings.TrimSpace(priority)) {
	case "high", "p0", "urgent":
		return 300
	case "normal", "medium", "p1", "":
		return 200
	case "low", "p2":
		return 100
	default:
		return 150
	}
}

func formatUTCWhen(t time.Time) string {
	if t.IsZero() {
		return "n/a"
	}
	return t.UTC().Format("Mon 2006-01-02 15:04 UTC")
}

func truncate(s string, max int) string {
	trimmed := strings.TrimSpace(s)
	if max <= 0 || len(trimmed) <= max {
		return trimmed
	}
	return strings.TrimSpace(trimmed[:max]) + "..."
}
