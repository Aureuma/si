package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type notifier struct {
	bot             *tgbotapi.BotAPI
	chatID          *int64
	logger          *log.Logger
	managerURL      string
	codexMonitorURL string
	httpClient      *http.Client
	statusMu        sync.Mutex
	statusCache     string
	statusCacheAt   time.Time
}

type button struct {
	Text string `json:"text"`
	URL  string `json:"url,omitempty"`
}

type notifyPayload struct {
	Message               string   `json:"message"`
	ChatID                *int64   `json:"chat_id,omitempty"`
	MessageID             int      `json:"message_id,omitempty"` // if set, edits that message instead of sending a new one
	Emoji                 string   `json:"emoji,omitempty"`
	Buttons               []button `json:"buttons,omitempty"`
	ParseMode             string   `json:"parse_mode,omitempty"` // "HTML" or "MarkdownV2" or "Markdown"
	DisableWebPagePreview *bool    `json:"disable_web_page_preview,omitempty"`
	DisableNotification   *bool    `json:"disable_notification,omitempty"`
}

type taskPayload struct {
	Title       string `json:"title"`
	Commands    string `json:"commands"`
	RequestedBy string `json:"requested_by"`
	Notes       string `json:"notes"`
}

type managerHealth struct {
	Status        string     `json:"status"`
	TasksOpen     int        `json:"tasks_open"`
	AccessPending int        `json:"access_pending"`
	MetricsCount  int        `json:"metrics_count"`
	BeatsRecent   int        `json:"beats_recent"`
	UptimeSeconds int64      `json:"uptime_seconds"`
	LastBeat      *time.Time `json:"last_beat,omitempty"`
}

type managerTask struct {
	ID       int    `json:"id"`
	Title    string `json:"title"`
	Commands string `json:"commands"`
	Status   string `json:"status"`
}

type managerDyadTask struct {
	ID        int    `json:"id"`
	Title     string `json:"title"`
	Kind      string `json:"kind"`
	Status    string `json:"status"`
	Priority  string `json:"priority"`
	Dyad      string `json:"dyad"`
	ClaimedBy string `json:"claimed_by"`
}

const (
	managerTimeout = 4 * time.Second
	codexTimeout   = 3 * time.Second
	tasksTimeout   = 4 * time.Second
)

func main() {
	logger := log.New(os.Stdout, "telegram-bot ", log.LstdFlags|log.LUTC)
	token := readToken()
	chatID := readChatID()
	managerURL := os.Getenv("MANAGER_URL")
	if managerURL == "" {
		managerURL = "http://manager:9090"
	}
	codexMonitorURL := os.Getenv("CODEX_MONITOR_URL")
	if codexMonitorURL == "" {
		codexMonitorURL = "http://codex-monitor:8086/status"
	}

	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		logger.Fatalf("bot init: %v", err)
	}
	bot.Debug = false
	bot.Client = &http.Client{Timeout: 70 * time.Second}

	setCommands(bot, logger)

	n := &notifier{
		bot:             bot,
		chatID:          chatID,
		logger:          logger,
		managerURL:      managerURL,
		codexMonitorURL: codexMonitorURL,
		httpClient:      &http.Client{Timeout: 6 * time.Second},
	}
	go n.pollUpdates()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/notify", n.handleNotify)
	mux.HandleFunc("/human-task", n.handleHumanTask)

	srv := &http.Server{Addr: ":8081", Handler: mux}
	logger.Printf("telegram notifier listening on :8081 for chat %v", chatID)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Fatalf("server error: %v", err)
	}
}

func setCommands(bot *tgbotapi.BotAPI, logger *log.Logger) {
	cmds := []tgbotapi.BotCommand{
		{Command: "status", Description: "System health and counts"},
		{Command: "tasks", Description: "List open human tasks"},
		{Command: "task", Description: "Create a human task: /task Title | command | notes"},
		{Command: "help", Description: "Show available commands and format"},
	}
	req := tgbotapi.NewSetMyCommands(cmds...)
	if _, err := bot.Request(req); err != nil {
		logger.Printf("set commands error: %v", err)
	}
}

func (n *notifier) pollUpdates() {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := n.bot.GetUpdatesChan(u)
	for update := range updates {
		if update.Message == nil {
			continue
		}
		from := "unknown"
		if update.Message.From != nil {
			if strings.TrimSpace(update.Message.From.UserName) != "" {
				from = update.Message.From.UserName
			} else if strings.TrimSpace(update.Message.From.FirstName) != "" {
				from = update.Message.From.FirstName
			}
		}
		chatID := update.Message.Chat.ID
		text := strings.TrimSpace(update.Message.Text)
		if len(text) > 200 {
			text = text[:200] + "…"
		}
		n.logger.Printf("incoming chat_id=%d from=%s text=%q", chatID, from, text)
		if update.Message.ReplyToMessage != nil {
			n.handleReply(chatID, update.Message)
			continue
		}
		if update.Message.IsCommand() {
			n.handleCommand(chatID, update.Message)
			continue
		}
		n.handlePlain(chatID, update.Message.Text)
	}
}

func (n *notifier) handleHumanTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	b, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}
	var p taskPayload
	if err := json.Unmarshal(b, &p); err != nil || p.Title == "" || p.Commands == "" {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	payload := map[string]string{
		"title":        p.Title,
		"commands":     p.Commands,
		"requested_by": p.RequestedBy,
		"notes":        p.Notes,
	}
	body, _ := json.Marshal(payload)
	resp, err := n.postJSON(tasksTimeout, n.managerURL+"/human-tasks", body)
	if err == nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	msgLines := []string{"✅ Human task created", "📝 " + p.Title, "💻 " + p.Commands}
	if p.RequestedBy != "" {
		msgLines = append(msgLines, "🙋 "+p.RequestedBy)
	}
	if p.Notes != "" {
		msgLines = append(msgLines, "🗒 "+p.Notes)
	}
	n.send(strings.Join(msgLines, "\n"), nil, "", nil, "", nil, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (n *notifier) handleNotify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}
	var payload notifyPayload
	if err := json.Unmarshal(body, &payload); err != nil || payload.Message == "" {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	msg, edited, err := n.sendOrEdit(payload)
	if err != nil {
		preview := strings.TrimSpace(payload.Message)
		if len(preview) > 200 {
			preview = preview[:200] + "…"
		}
		chatID := int64(0)
		if payload.ChatID != nil {
			chatID = *payload.ChatID
		} else if n.chatID != nil {
			chatID = *n.chatID
		}
		n.logger.Printf("notify error chat_id=%d message_id=%d parse_mode=%q preview=%q err=%v",
			chatID, payload.MessageID, payload.ParseMode, preview, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":         true,
		"edited":     edited,
		"message_id": msg.MessageID,
	})
}

func (n *notifier) handleCommand(chatID int64, msg *tgbotapi.Message) {
	cmd := msg.Command()
	args := strings.TrimSpace(msg.CommandArguments())
	switch cmd {
	case "help":
		n.send(helpMessage(), &chatID, "ℹ️", nil, "", nil, nil)
	case "status":
		n.send(n.statusMessage(), &chatID, "📊", nil, "", nil, nil)
	case "tasks":
		n.send(n.tasksMessage(), &chatID, "🧭", nil, "", nil, nil)
	case "board", "dyads":
		n.send(n.dyadBoardMessage(), &chatID, "🧭", nil, "", nil, nil)
	case "chatid", "whereami":
		n.send(fmt.Sprintf("Chat ID: %d", chatID), &chatID, "🧷", nil, "", nil, nil)
	case "task":
		if args == "" {
			n.send("Usage: /task Title | command to run | optional notes", &chatID, "✍️", nil, "", nil, nil)
			return
		}
		parts := strings.SplitN(args, "|", 3)
		if len(parts) < 2 {
			n.send("Need at least title and command separated by '|'", &chatID, "⚠️", nil, "", nil, nil)
			return
		}
		payload := map[string]interface{}{
			"title":        strings.TrimSpace(parts[0]),
			"commands":     strings.TrimSpace(parts[1]),
			"requested_by": "human",
			"notes":        "",
			"chat_id":      chatID,
		}
		if len(parts) == 3 {
			payload["notes"] = strings.TrimSpace(parts[2])
		}
		body, _ := json.Marshal(payload)
		resp, err := n.postJSON(tasksTimeout, n.managerURL+"/human-tasks", body)
		if err != nil {
			n.send("Failed to create task: "+err.Error(), &chatID, "⚠️", nil, "", nil, nil)
			return
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			n.send(fmt.Sprintf("Task not accepted (status %d)", resp.StatusCode), &chatID, "⚠️", nil, "", nil, nil)
			return
		}
		n.send("Task recorded and queued for agents.", &chatID, "✅", nil, "", nil, nil)
	default:
		n.send("Unknown command. Try /help", &chatID, "ℹ️", nil, "", nil, nil)
	}
}

func (n *notifier) handlePlain(chatID int64, text string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return
	}
	payload := map[string]string{
		"source":   fmt.Sprintf("human:%d", chatID),
		"severity": "info",
		"message":  trimmed,
		"context":  "telegram",
	}
	body, _ := json.Marshal(payload)
	resp, err := n.postJSON(tasksTimeout, n.managerURL+"/feedback", body)
	if err == nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	n.send("Noted. For actionable work, use /task Title | command | notes", &chatID, "📥", nil, "", nil, nil)
}

func (n *notifier) handleReply(chatID int64, msg *tgbotapi.Message) {
	trimmed := strings.TrimSpace(msg.Text)
	if trimmed == "" {
		return
	}
	orig := strings.TrimSpace(msg.ReplyToMessage.Text)
	if len(orig) > 200 {
		orig = orig[:200] + "…"
	}
	payload := map[string]string{
		"source":   fmt.Sprintf("human-reply:%d", chatID),
		"severity": "info",
		"message":  trimmed,
		"context":  fmt.Sprintf("reply_to:%d %s", msg.ReplyToMessage.MessageID, orig),
	}
	body, _ := json.Marshal(payload)
	resp, err := n.postJSON(tasksTimeout, n.managerURL+"/feedback", body)
	if err == nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	ack := "Captured reply"
	if orig != "" {
		ack += fmt.Sprintf(" (re: %s)", orig)
	}
	n.send(ack, &chatID, "📬", nil, "", nil, nil)
}

func (n *notifier) sendOrEdit(payload notifyPayload) (*tgbotapi.Message, bool, error) {
	targetChat := payload.ChatID
	if targetChat == nil {
		targetChat = n.chatID
	}
	if targetChat == nil {
		return nil, false, fmt.Errorf("skip notify: no chat id provided")
	}

	msgText := payload.Message
	if payload.Emoji != "" {
		msgText = payload.Emoji + " " + msgText
	}

	parseMode := strings.TrimSpace(payload.ParseMode)

	disableWebPreview := false
	if payload.DisableWebPagePreview != nil {
		disableWebPreview = *payload.DisableWebPagePreview
	}
	disableNotification := false
	if payload.DisableNotification != nil {
		disableNotification = *payload.DisableNotification
	}

	replyMarkup := buttonsMarkup(payload.Buttons)

	if payload.MessageID > 0 {
		cfg := tgbotapi.NewEditMessageText(*targetChat, payload.MessageID, msgText)
		if parseMode != "" {
			cfg.ParseMode = parseMode
		}
		cfg.DisableWebPagePreview = disableWebPreview
		if replyMarkup != nil {
			cfg.ReplyMarkup = replyMarkup
		}
		m, err := n.bot.Send(cfg)
		if err == nil {
			return &m, true, nil
		}

		// If the message was deleted (e.g., user cleared chat history) or cannot be edited,
		// fall back to sending a new message and return its message_id so callers can re-anchor.
		// Typical Telegram errors:
		// - "Bad Request: message to edit not found"
		// - "Bad Request: message can't be edited"
		// - "Bad Request: message is not modified: ..."
		errText := strings.ToLower(err.Error())
		if strings.Contains(errText, "message is not modified") {
			// Treat as success to avoid spurious 500s when callers upsert the same content.
			return &tgbotapi.Message{MessageID: payload.MessageID}, true, nil
		}
		if strings.Contains(errText, "message to edit not found") ||
			strings.Contains(errText, "message can't be edited") {
			n.logger.Printf("edit failed (will send new): %v", err)
			// Continue below to send a new message.
		} else {
			return nil, false, err
		}
	}

	cfg := tgbotapi.NewMessage(*targetChat, msgText)
	if parseMode != "" {
		cfg.ParseMode = parseMode
	}
	cfg.DisableWebPagePreview = disableWebPreview
	cfg.DisableNotification = disableNotification
	if replyMarkup != nil {
		cfg.ReplyMarkup = replyMarkup
	}
	m, err := n.bot.Send(cfg)
	if err != nil {
		return nil, false, err
	}
	return &m, false, nil
}

func (n *notifier) send(msg string, chatID *int64, emoji string, buttons []button, parseMode string, disableWebPreview *bool, disableNotification *bool) {
	p := notifyPayload{
		Message:               msg,
		ChatID:                chatID,
		Emoji:                 emoji,
		Buttons:               buttons,
		ParseMode:             parseMode,
		DisableWebPagePreview: disableWebPreview,
		DisableNotification:   disableNotification,
	}
	if _, _, err := n.sendOrEdit(p); err != nil {
		n.logger.Printf("send error: %v", err)
		return
	}
	n.logger.Printf("sent notification")
}

func buttonsMarkup(buttons []button) *tgbotapi.InlineKeyboardMarkup {
	if len(buttons) == 0 {
		return nil
	}
	rows := make([][]tgbotapi.InlineKeyboardButton, 0, len(buttons))
	for _, b := range buttons {
		if strings.TrimSpace(b.Text) == "" || strings.TrimSpace(b.URL) == "" {
			continue
		}
		btn := tgbotapi.NewInlineKeyboardButtonURL(b.Text, b.URL)
		rows = append(rows, []tgbotapi.InlineKeyboardButton{btn})
	}
	if len(rows) == 0 {
		return nil
	}
	m := tgbotapi.NewInlineKeyboardMarkup(rows...)
	return &m
}

func (n *notifier) statusMessage() string {
	msg, err := n.buildStatusMessage()
	if err == nil {
		n.setStatusCache(msg)
		return msg
	}
	if cached, age := n.getStatusCache(); cached != "" {
		return fmt.Sprintf("Status (stale %s):\n%s\n\nLast error: %s", age, cached, err.Error())
	}
	return "Status unavailable: " + err.Error()
}

func (n *notifier) buildStatusMessage() (string, error) {
	h, err := n.fetchHealth()
	if err != nil {
		return "", err
	}
	lastBeat := "n/a"
	if h.LastBeat != nil {
		lastBeat = h.LastBeat.UTC().Format(time.RFC3339)
	}
	msg := fmt.Sprintf("Status: %s\nOpen tasks: %d\nAccess pending: %d\nRecent beats: %d\nMetrics: %d\nUptime: %ds\nLast beat: %s",
		h.Status, h.TasksOpen, h.AccessPending, h.BeatsRecent, h.MetricsCount, h.UptimeSeconds, lastBeat)
	if codex, err := n.fetchCodexStatus(); err == nil {
		if codex != "" {
			msg += "\n\n" + codex
		}
	} else {
		msg += "\n\nCodex usage: unavailable"
	}
	return msg, nil
}

func (n *notifier) tasksMessage() string {
	tasks, err := n.fetchTasks()
	if err != nil {
		return "Cannot load tasks: " + err.Error()
	}
	if len(tasks) == 0 {
		return "No open human tasks."
	}
	var b strings.Builder
	b.WriteString("Open human tasks:\n")
	for i, t := range tasks {
		if i >= 5 {
			b.WriteString("… and more\n")
			break
		}
		b.WriteString(fmt.Sprintf("#%d %s\n↳ %s\n", t.ID, t.Title, t.Commands))
	}
	return b.String()
}

func (n *notifier) dyadBoardMessage() string {
	tasks, err := n.fetchDyadTasks()
	if err != nil {
		return "Cannot load dyad tasks: " + err.Error()
	}
	open := make([]managerDyadTask, 0, len(tasks))
	for _, t := range tasks {
		if strings.TrimSpace(strings.ToLower(t.Status)) == "done" {
			continue
		}
		open = append(open, t)
	}
	if len(open) == 0 {
		return "✅ No open dyad tasks."
	}
	var b strings.Builder
	b.WriteString("Dyad tasks (open):\n")
	for i, t := range open {
		if i >= 15 {
			b.WriteString("…\n")
			break
		}
		line := fmt.Sprintf("#%d [%s] %s", t.ID, t.Status, t.Title)
		if strings.TrimSpace(t.Dyad) != "" {
			line += fmt.Sprintf(" (%s)", t.Dyad)
		}
		if strings.TrimSpace(t.ClaimedBy) != "" {
			line += fmt.Sprintf(" — %s", t.ClaimedBy)
		}
		b.WriteString(line + "\n")
	}
	return strings.TrimSpace(b.String())
}

func (n *notifier) fetchHealth() (*managerHealth, error) {
	resp, err := n.get(managerTimeout, n.managerURL+"/healthz")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	var h managerHealth
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
		return nil, err
	}
	return &h, nil
}

func (n *notifier) fetchCodexStatus() (string, error) {
	if strings.TrimSpace(n.codexMonitorURL) == "" {
		return "", nil
	}
	resp, err := n.get(codexTimeout, n.codexMonitorURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("codex status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(body)), nil
}

func (n *notifier) fetchTasks() ([]managerTask, error) {
	resp, err := n.get(tasksTimeout, n.managerURL+"/human-tasks")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	var list []managerTask
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, err
	}
	out := make([]managerTask, 0, len(list))
	for _, t := range list {
		if strings.ToLower(t.Status) != "done" {
			out = append(out, t)
		}
	}
	return out, nil
}

func (n *notifier) fetchDyadTasks() ([]managerDyadTask, error) {
	resp, err := n.get(tasksTimeout, n.managerURL+"/dyad-tasks")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("manager returned %s", resp.Status)
	}
	var tasks []managerDyadTask
	if err := json.NewDecoder(resp.Body).Decode(&tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

func (n *notifier) get(timeout time.Duration, url string) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	client := n.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	return client.Do(req)
}

func (n *notifier) postJSON(timeout time.Duration, url string, body []byte) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := n.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	return client.Do(req)
}

func (n *notifier) setStatusCache(msg string) {
	n.statusMu.Lock()
	n.statusCache = msg
	n.statusCacheAt = time.Now().UTC()
	n.statusMu.Unlock()
}

func (n *notifier) getStatusCache() (string, string) {
	n.statusMu.Lock()
	defer n.statusMu.Unlock()
	if n.statusCache == "" || n.statusCacheAt.IsZero() {
		return "", ""
	}
	age := time.Since(n.statusCacheAt).Truncate(time.Second)
	if age < 0 {
		age = 0
	}
	return n.statusCache, age.String()
}

func helpMessage() string {
	return strings.TrimSpace(`
/status - System health, counts, last beat
/tasks - Open human tasks
/board - Open dyad tasks
/chatid - Show this chat id
/task Title | command | notes - Log a human task for agents
/help - This menu

Any non-command message is recorded as feedback for the manager. Use /task for actionable requests.
`)
}

func readToken() string {
	if path := os.Getenv("TELEGRAM_BOT_TOKEN_FILE"); path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			log.Fatalf("read token file: %v", err)
		}
		tok := string(bytes.TrimSpace(b))
		if tok != "" {
			return tok
		}
	}
	tok := os.Getenv("TELEGRAM_BOT_TOKEN")
	if tok == "" {
		log.Fatalf("TELEGRAM_BOT_TOKEN or TELEGRAM_BOT_TOKEN_FILE required")
	}
	return tok
}

func readChatID() *int64 {
	raw := os.Getenv("TELEGRAM_CHAT_ID")
	if raw == "" {
		return nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		log.Fatalf("invalid TELEGRAM_CHAT_ID: %v", err)
	}
	return &id
}
