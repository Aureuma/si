package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type notifier struct {
	bot        *tgbotapi.BotAPI
	chatID     *int64
	logger     *log.Logger
	managerURL string
}

type button struct {
	Text string `json:"text"`
	URL  string `json:"url,omitempty"`
}

type notifyPayload struct {
	Message string   `json:"message"`
	ChatID  *int64   `json:"chat_id,omitempty"`
	Emoji   string   `json:"emoji,omitempty"`
	Buttons []button `json:"buttons,omitempty"`
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

func main() {
	logger := log.New(os.Stdout, "telegram-bot ", log.LstdFlags|log.LUTC)
	token := readToken()
	chatID := readChatID()
	managerURL := os.Getenv("MANAGER_URL")
	if managerURL == "" {
		managerURL = "http://manager:9090"
	}

	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		logger.Fatalf("bot init: %v", err)
	}
	bot.Debug = false

	setCommands(bot, logger)

	n := &notifier{bot: bot, chatID: chatID, logger: logger, managerURL: managerURL}
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
		chatID := update.Message.Chat.ID
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
	resp, err := http.Post(n.managerURL+"/human-tasks", "application/json", bytes.NewReader(body))
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
	n.send(strings.Join(msgLines, "\n"), nil, "", nil)
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
	n.send(payload.Message, payload.ChatID, payload.Emoji, payload.Buttons)
	w.WriteHeader(http.StatusNoContent)
}

func (n *notifier) handleCommand(chatID int64, msg *tgbotapi.Message) {
	cmd := msg.Command()
	args := strings.TrimSpace(msg.CommandArguments())
	switch cmd {
	case "help":
		n.send(helpMessage(), &chatID, "ℹ️", nil)
	case "status":
		n.send(n.statusMessage(), &chatID, "📊", nil)
	case "tasks":
		n.send(n.tasksMessage(), &chatID, "🧭", nil)
	case "task":
		if args == "" {
			n.send("Usage: /task Title | command to run | optional notes", &chatID, "✍️", nil)
			return
		}
		parts := strings.SplitN(args, "|", 3)
		if len(parts) < 2 {
			n.send("Need at least title and command separated by '|'", &chatID, "⚠️", nil)
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
		resp, err := http.Post(n.managerURL+"/human-tasks", "application/json", bytes.NewReader(body))
		if err != nil {
			n.send("Failed to create task: "+err.Error(), &chatID, "⚠️", nil)
			return
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			n.send(fmt.Sprintf("Task not accepted (status %d)", resp.StatusCode), &chatID, "⚠️", nil)
			return
		}
		n.send("Task recorded and queued for agents.", &chatID, "✅", nil)
	default:
		n.send("Unknown command. Try /help", &chatID, "ℹ️", nil)
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
	resp, err := http.Post(n.managerURL+"/feedback", "application/json", bytes.NewReader(body))
	if err == nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	n.send("Noted. For actionable work, use /task Title | command | notes", &chatID, "📥", nil)
}

func (n *notifier) send(msg string, chatID *int64, emoji string, buttons []button) {
	targetChat := chatID
	if targetChat == nil {
		targetChat = n.chatID
	}
	if targetChat == nil {
		n.logger.Printf("skip notify: no chat id provided")
		return
	}
	if emoji != "" {
		msg = emoji + " " + msg
	}
	m := tgbotapi.NewMessage(*targetChat, msg)
	m.ParseMode = "Markdown"
	if len(buttons) > 0 {
		rows := make([][]tgbotapi.InlineKeyboardButton, 0, len(buttons))
		for _, b := range buttons {
			btn := tgbotapi.NewInlineKeyboardButtonURL(b.Text, b.URL)
			rows = append(rows, []tgbotapi.InlineKeyboardButton{btn})
		}
		m.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
	}
	if _, err := n.bot.Send(m); err != nil {
		n.logger.Printf("send error: %v", err)
	} else {
		n.logger.Printf("sent notification")
	}
}

func (n *notifier) statusMessage() string {
	h, err := n.fetchHealth()
	if err != nil {
		return "Status unavailable: " + err.Error()
	}
	lastBeat := "n/a"
	if h.LastBeat != nil {
		lastBeat = h.LastBeat.UTC().Format(time.RFC3339)
	}
	return fmt.Sprintf("Status: %s\nOpen tasks: %d\nAccess pending: %d\nRecent beats: %d\nMetrics: %d\nUptime: %ds\nLast beat: %s",
		h.Status, h.TasksOpen, h.AccessPending, h.BeatsRecent, h.MetricsCount, h.UptimeSeconds, lastBeat)
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

func (n *notifier) fetchHealth() (*managerHealth, error) {
	resp, err := http.Get(n.managerURL + "/healthz")
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

func (n *notifier) fetchTasks() ([]managerTask, error) {
	resp, err := http.Get(n.managerURL + "/human-tasks")
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

func helpMessage() string {
	return strings.TrimSpace(`
/status - System health, counts, last beat
/tasks - Open human tasks
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
