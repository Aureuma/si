package main

import (
    "bytes"
    "encoding/json"
    "io"
    "log"
    "net/http"
    "os"
    "strconv"

    tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type notifier struct {
    bot    *tgbotapi.BotAPI
    chatID int64
    logger *log.Logger
}

type notifyPayload struct {
    Message string `json:"message"`
}

func main() {
    logger := log.New(os.Stdout, "telegram-bot ", log.LstdFlags|log.LUTC)
    token := readToken()
    chatID := mustChatID()

    bot, err := tgbotapi.NewBotAPI(token)
    if err != nil {
        logger.Fatalf("bot init: %v", err)
    }
    bot.Debug = false

    n := &notifier{bot: bot, chatID: chatID, logger: logger}

    mux := http.NewServeMux()
    mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
        w.WriteHeader(http.StatusOK)
        _, _ = w.Write([]byte("ok"))
    })
    mux.HandleFunc("/notify", n.handleNotify)

    srv := &http.Server{Addr: ":8081", Handler: mux}
    logger.Printf("telegram notifier listening on :8081 for chat %d", chatID)
    if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
        logger.Fatalf("server error: %v", err)
    }
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
    msg := tgbotapi.NewMessage(n.chatID, payload.Message)
    if _, err := n.bot.Send(msg); err != nil {
        n.logger.Printf("send error: %v", err)
        http.Error(w, "send error", http.StatusBadGateway)
        return
    }
    n.logger.Printf("sent notification")
    w.WriteHeader(http.StatusNoContent)
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

func mustChatID() int64 {
    raw := os.Getenv("TELEGRAM_CHAT_ID")
    if raw == "" {
        log.Fatalf("TELEGRAM_CHAT_ID required")
    }
    id, err := strconv.ParseInt(raw, 10, 64)
    if err != nil {
        log.Fatalf("invalid TELEGRAM_CHAT_ID: %v", err)
    }
    return id
}

func trimBytes(b []byte) []byte {
    return []byte(string(b))
}
