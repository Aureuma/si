package main

import (
    "bytes"
    "encoding/json"
    "io"
    "log"
    "net/http"
    "os"
    "strconv"
    "strings"

    tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type notifier struct {
    bot    *tgbotapi.BotAPI
    chatID *int64
    logger *log.Logger
    managerURL string
}

type notifyPayload struct {
    Message string `json:"message"`
    ChatID  *int64  `json:"chat_id,omitempty"`
}

type taskPayload struct {
    Title   string `json:"title"`
    Commands string `json:"commands"`
    RequestedBy string `json:"requested_by"`
    Notes   string `json:"notes"`
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

    n := &notifier{bot: bot, chatID: chatID, logger: logger, managerURL: managerURL}

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
        "title": p.Title,
        "commands": p.Commands,
        "requested_by": p.RequestedBy,
        "notes": p.Notes,
    }
    body, _ := json.Marshal(payload)
    resp, err := http.Post(n.managerURL+"/human-tasks", "application/json", bytes.NewReader(body))
    if err == nil {
        io.Copy(io.Discard, resp.Body)
        resp.Body.Close()
    }
    msgLines := []string{"Human task created", "Title: " + p.Title, "Commands: " + p.Commands}
    if p.RequestedBy != "" {
        msgLines = append(msgLines, "Requested by: "+p.RequestedBy)
    }
    if p.Notes != "" {
        msgLines = append(msgLines, "Notes: "+p.Notes)
    }
    n.send(strings.Join(msgLines, "\n"), nil)
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
    n.send(payload.Message, payload.ChatID)
    w.WriteHeader(http.StatusNoContent)
}

func (n *notifier) send(msg string, chatID *int64) {
    targetChat := chatID
    if targetChat == nil {
        targetChat = n.chatID
    }
    if targetChat == nil {
        n.logger.Printf("skip notify: no chat id provided")
        return
    }
    m := tgbotapi.NewMessage(*targetChat, msg)
    if _, err := n.bot.Send(m); err != nil {
        n.logger.Printf("send error: %v", err)
    } else {
        n.logger.Printf("sent notification")
    }
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
