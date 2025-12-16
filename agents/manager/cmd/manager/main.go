package main

import (
    "bytes"
    "encoding/json"
    "log"
    "net/http"
    "os"
    "strconv"
    "strings"
    "sync"
    "time"
)

type heartbeat struct {
    Actor   string `json:"actor"`
    Critic  string `json:"critic"`
    Status  string `json:"status"`
    Message string `json:"message"`
    When    time.Time `json:"when"`
}

type humanTask struct {
    ID          int       `json:"id"`
    Title       string    `json:"title"`
    Commands    string    `json:"commands"`
    URL         string    `json:"url"`
    Timeout     string    `json:"timeout"`
    RequestedBy string    `json:"requested_by"`
    Notes       string    `json:"notes"`
    ChatID      *int64    `json:"chat_id,omitempty"`
    Status      string    `json:"status"`
    CreatedAt   time.Time `json:"created_at"`
    CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type store struct {
    mu    sync.Mutex
    beats []heartbeat
    tasks []humanTask
    nextID int
}

func (s *store) add(h heartbeat) {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.beats = append(s.beats, h)
    if len(s.beats) > 1000 {
        s.beats = s.beats[len(s.beats)-1000:]
    }
}

func (s *store) latest() []heartbeat {
    s.mu.Lock()
    defer s.mu.Unlock()
    out := make([]heartbeat, len(s.beats))
    copy(out, s.beats)
    return out
}

func (s *store) addTask(t humanTask) humanTask {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.nextID++
    t.ID = s.nextID
    t.Status = "open"
    t.CreatedAt = time.Now().UTC()
    s.tasks = append(s.tasks, t)
    return t
}

func (s *store) listTasks() []humanTask {
    s.mu.Lock()
    defer s.mu.Unlock()
    out := make([]humanTask, len(s.tasks))
    copy(out, s.tasks)
    return out
}

func (s *store) completeTask(id int) bool {
    s.mu.Lock()
    defer s.mu.Unlock()
    for i := range s.tasks {
        if s.tasks[i].ID == id {
            if s.tasks[i].Status == "done" {
                return true
            }
            now := time.Now().UTC()
            s.tasks[i].Status = "done"
            s.tasks[i].CompletedAt = &now
            return true
        }
    }
    return false
}

type notifier struct {
    url    string
    chatID *int64
    logger *log.Logger
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
    payload := map[string]interface{}{"message": msg}
    payload["chat_id"] = *targetChat
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

func main() {
    logger := log.New(os.Stdout, "manager ", log.LstdFlags|log.LUTC)
    st := &store{}
    notif := buildNotifier(logger)

    http.HandleFunc("/heartbeat", func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
            w.WriteHeader(http.StatusMethodNotAllowed)
            return
        }
        var hb heartbeat
        if err := json.NewDecoder(r.Body).Decode(&hb); err != nil {
            logger.Printf("decode heartbeat: %v", err)
            w.WriteHeader(http.StatusBadRequest)
            return
        }
        hb.When = time.Now().UTC()
        st.add(hb)
        logger.Printf("beat from actor=%s critic=%s", hb.Actor, hb.Critic)
        w.WriteHeader(http.StatusNoContent)
    })

    http.HandleFunc("/beats", func(w http.ResponseWriter, _ *http.Request) {
        beats := st.latest()
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(beats)
    })

    http.HandleFunc("/human-tasks", func(w http.ResponseWriter, r *http.Request) {
        switch r.Method {
        case http.MethodGet:
            tasks := st.listTasks()
            w.Header().Set("Content-Type", "application/json")
            json.NewEncoder(w).Encode(tasks)
        case http.MethodPost:
            var t humanTask
            if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
                logger.Printf("decode task: %v", err)
                w.WriteHeader(http.StatusBadRequest)
                return
            }
            if t.Title == "" || t.Commands == "" {
                http.Error(w, "title and commands required", http.StatusBadRequest)
                return
            }
            created := st.addTask(t)
            go notif.maybeSend(formatTaskMessage(created), created.ChatID)
            w.Header().Set("Content-Type", "application/json")
            json.NewEncoder(w).Encode(created)
        default:
            w.WriteHeader(http.StatusMethodNotAllowed)
        }
    })

    http.HandleFunc("/human-tasks/complete", func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
            w.WriteHeader(http.StatusMethodNotAllowed)
            return
        }
        idStr := r.URL.Query().Get("id")
        id, err := strconv.Atoi(idStr)
        if err != nil || id <= 0 {
            http.Error(w, "invalid id", http.StatusBadRequest)
            return
        }
        if st.completeTask(id) {
            w.WriteHeader(http.StatusNoContent)
            return
        }
        http.Error(w, "not found", http.StatusNotFound)
    })

    addr := ":9090"
    logger.Printf("manager listening on %s", addr)
    if err := http.ListenAndServe(addr, nil); err != nil {
        logger.Fatalf("server error: %v", err)
    }
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

func formatTaskMessage(t humanTask) string {
    return strings.TrimSpace(strings.Join([]string{
        "Human Action Required",
        "Task: " + t.Title,
        "Commands: " + t.Commands,
        "URL: " + t.URL,
        "Timeout: " + t.Timeout,
        "Requested by: " + t.RequestedBy,
        "Notes: " + t.Notes,
    }, "\n"))
}
