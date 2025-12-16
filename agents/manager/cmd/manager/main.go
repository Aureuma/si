package main

import (
    "bytes"
    "encoding/json"
    "log"
    "net/http"
    "os"
    "path/filepath"
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

type feedback struct {
    ID        int       `json:"id"`
    Source    string    `json:"source"`
    Severity  string    `json:"severity"` // info|warn|error
    Message   string    `json:"message"`
    Context   string    `json:"context"`
    CreatedAt time.Time `json:"created_at"`
}

type accessRequest struct {
    ID         int       `json:"id"`
    Requester  string    `json:"requester"`  // dyad or actor/critic
    Department string    `json:"department"`
    Resource   string    `json:"resource"`
    Action     string    `json:"action"`
    Reason     string    `json:"reason"`
    Status     string    `json:"status"` // pending|approved|denied
    CreatedAt  time.Time `json:"created_at"`
    ResolvedAt *time.Time `json:"resolved_at,omitempty"`
    ResolvedBy string    `json:"resolved_by,omitempty"`
    Notes      string    `json:"notes,omitempty"`
}

type store struct {
    mu    sync.Mutex
    beats []heartbeat
    tasks []humanTask
    nextTaskID int
    feedbacks []feedback
    nextFeedbackID int
    access []accessRequest
    nextAccessID int
    dataPath string
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
    s.nextTaskID++
    t.ID = s.nextTaskID
    t.Status = "open"
    t.CreatedAt = time.Now().UTC()
    s.tasks = append(s.tasks, t)
    s.persistLocked()
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
            s.persistLocked()
            return true
        }
    }
    return false
}

func (s *store) addFeedback(f feedback) feedback {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.nextFeedbackID++
    f.ID = s.nextFeedbackID
    if f.Severity == "" {
        f.Severity = "info"
    }
    f.CreatedAt = time.Now().UTC()
    s.feedbacks = append(s.feedbacks, f)
    s.persistLocked()
    return f
}

func (s *store) listFeedback() []feedback {
    s.mu.Lock()
    defer s.mu.Unlock()
    out := make([]feedback, len(s.feedbacks))
    copy(out, s.feedbacks)
    return out
}

func (s *store) addAccess(r accessRequest) accessRequest {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.nextAccessID++
    r.ID = s.nextAccessID
    if r.Status == "" {
        r.Status = "pending"
    }
    r.CreatedAt = time.Now().UTC()
    s.access = append(s.access, r)
    s.persistLocked()
    return r
}

func (s *store) listAccess() []accessRequest {
    s.mu.Lock()
    defer s.mu.Unlock()
    out := make([]accessRequest, len(s.access))
    copy(out, s.access)
    return out
}

func (s *store) resolveAccess(id int, status, by, notes string) bool {
    s.mu.Lock()
    defer s.mu.Unlock()
    for i := range s.access {
        if s.access[i].ID == id {
            s.access[i].Status = status
            now := time.Now().UTC()
            s.access[i].ResolvedAt = &now
            s.access[i].ResolvedBy = by
            s.access[i].Notes = notes
            s.persistLocked()
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
    dataDir := envOr("DATA_DIR", "/data")
    st := newStore(dataDir, logger)
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

    http.HandleFunc("/feedback", func(w http.ResponseWriter, r *http.Request) {
        switch r.Method {
        case http.MethodGet:
            f := st.listFeedback()
            w.Header().Set("Content-Type", "application/json")
            json.NewEncoder(w).Encode(f)
        case http.MethodPost:
            var fb feedback
            if err := json.NewDecoder(r.Body).Decode(&fb); err != nil || fb.Message == "" {
                http.Error(w, "invalid payload", http.StatusBadRequest)
                return
            }
            created := st.addFeedback(fb)
            w.Header().Set("Content-Type", "application/json")
            json.NewEncoder(w).Encode(created)
        default:
            w.WriteHeader(http.StatusMethodNotAllowed)
        }
    })

    http.HandleFunc("/access-requests", func(w http.ResponseWriter, r *http.Request) {
        switch r.Method {
        case http.MethodGet:
            list := st.listAccess()
            w.Header().Set("Content-Type", "application/json")
            json.NewEncoder(w).Encode(list)
        case http.MethodPost:
            var ar accessRequest
            if err := json.NewDecoder(r.Body).Decode(&ar); err != nil || ar.Requester == "" || ar.Resource == "" || ar.Action == "" {
                http.Error(w, "invalid payload", http.StatusBadRequest)
                return
            }
            created := st.addAccess(ar)
            w.Header().Set("Content-Type", "application/json")
            json.NewEncoder(w).Encode(created)
        default:
            w.WriteHeader(http.StatusMethodNotAllowed)
        }
    })

    http.HandleFunc("/access-requests/resolve", func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
            w.WriteHeader(http.StatusMethodNotAllowed)
            return
        }
        idStr := r.URL.Query().Get("id")
        status := r.URL.Query().Get("status")
        if idStr == "" || status == "" {
            http.Error(w, "id and status required", http.StatusBadRequest)
            return
        }
        if status != "approved" && status != "denied" {
            http.Error(w, "status must be approved or denied", http.StatusBadRequest)
            return
        }
        id, err := strconv.Atoi(idStr)
        if err != nil || id <= 0 {
            http.Error(w, "invalid id", http.StatusBadRequest)
            return
        }
        by := r.URL.Query().Get("by")
        notes := r.URL.Query().Get("notes")
        if st.resolveAccess(id, status, by, notes) {
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

func envOr(key, def string) string {
    if v := os.Getenv(key); v != "" {
        return v
    }
    return def
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

func newStore(dataDir string, logger *log.Logger) *store {
    _ = os.MkdirAll(dataDir, 0o755)
    s := &store{dataPath: filepath.Join(dataDir, "tasks.json")}
    s.load(logger)
    return s
}

func (s *store) load(logger *log.Logger) {
    b, err := os.ReadFile(s.dataPath)
    if err != nil {
        if os.IsNotExist(err) {
            return
        }
        logger.Printf("tasks load error: %v", err)
        return
    }
    var payload struct {
        Tasks     []humanTask `json:"tasks"`
        Feedbacks []feedback  `json:"feedbacks"`
        NextTask  int         `json:"next_id"`
        NextFeedback int      `json:"next_feedback_id"`
    }
    if err := json.Unmarshal(b, &payload); err != nil {
        logger.Printf("tasks decode error: %v", err)
        return
    }
    s.tasks = payload.Tasks
    s.feedbacks = payload.Feedbacks
    s.nextTaskID = payload.NextTask
    s.nextFeedbackID = payload.NextFeedback
}

func (s *store) persistLocked() {
    if s.dataPath == "" {
        return
    }
    payload := struct {
        Tasks        []humanTask `json:"tasks"`
        Feedbacks    []feedback  `json:"feedbacks"`
        NextTask     int         `json:"next_id"`
        NextFeedback int         `json:"next_feedback_id"`
    }{Tasks: s.tasks, Feedbacks: s.feedbacks, NextTask: s.nextTaskID, NextFeedback: s.nextFeedbackID}
    b, err := json.MarshalIndent(payload, "", "  ")
    if err != nil {
        return
    }
    tmp := s.dataPath + ".tmp"
    if err := os.WriteFile(tmp, b, 0o644); err == nil {
        _ = os.Rename(tmp, s.dataPath)
    }
}
