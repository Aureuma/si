package main

import (
    "bytes"
    "encoding/json"
    "log"
    "net/http"
    "os"
    "path/filepath"
    "strconv"
    "sync"
    "time"
)

type request struct {
    ID          int       `json:"id"`
    Resource    string    `json:"resource"`
    Action      string    `json:"action"`
    Payload     string    `json:"payload"`
    RequestedBy string    `json:"requested_by"`
    Notes       string    `json:"notes"`
    Status      string    `json:"status"`
    CreatedAt   time.Time `json:"created_at"`
    CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type store struct {
    mu       sync.Mutex
    filePath string
    nextID   int
    items    []request
}

func newStore(path string, logger *log.Logger) *store {
    _ = os.MkdirAll(filepath.Dir(path), 0o755)
    s := &store{filePath: path}
    s.load(logger)
    return s
}

func (s *store) load(logger *log.Logger) {
    b, err := os.ReadFile(s.filePath)
    if err != nil {
        if os.IsNotExist(err) {
            return
        }
        logger.Printf("load error: %v", err)
        return
    }
    var payload struct {
        Items  []request `json:"items"`
        NextID int       `json:"next_id"`
    }
    if err := json.Unmarshal(b, &payload); err != nil {
        logger.Printf("decode error: %v", err)
        return
    }
    s.items = payload.Items
    s.nextID = payload.NextID
}

func (s *store) persist() {
    payload := struct {
        Items  []request `json:"items"`
        NextID int       `json:"next_id"`
    }{Items: s.items, NextID: s.nextID}
    b, err := json.MarshalIndent(payload, "", "  ")
    if err != nil {
        return
    }
    tmp := s.filePath + ".tmp"
    if err := os.WriteFile(tmp, b, 0o644); err == nil {
        _ = os.Rename(tmp, s.filePath)
    }
}

func (s *store) add(r request) request {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.nextID++
    r.ID = s.nextID
    r.Status = "pending"
    r.CreatedAt = time.Now().UTC()
    s.items = append(s.items, r)
    s.persist()
    return r
}

func (s *store) list() []request {
    s.mu.Lock()
    defer s.mu.Unlock()
    out := make([]request, len(s.items))
    copy(out, s.items)
    return out
}

func (s *store) complete(id int, status string) bool {
    s.mu.Lock()
    defer s.mu.Unlock()
    for i := range s.items {
        if s.items[i].ID == id {
            s.items[i].Status = status
            now := time.Now().UTC()
            s.items[i].CompletedAt = &now
            s.persist()
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

func (n *notifier) send(msg string) {
    if n == nil || n.url == "" {
        return
    }
    payload := map[string]interface{}{"message": msg}
    if n.chatID != nil {
        payload["chat_id"] = *n.chatID
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

func main() {
    logger := log.New(os.Stdout, "resource-broker ", log.LstdFlags|log.LUTC)
    dataDir := env("DATA_DIR", "/data")
    chat := readChatID()
    notif := &notifier{url: os.Getenv("TELEGRAM_NOTIFY_URL"), chatID: chat, logger: logger}
    st := newStore(filepath.Join(dataDir, "requests.json"), logger)

    mux := http.NewServeMux()
    mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK); w.Write([]byte("ok")) })
    mux.HandleFunc("/requests", func(w http.ResponseWriter, r *http.Request) {
        switch r.Method {
        case http.MethodGet:
            w.Header().Set("Content-Type", "application/json")
            json.NewEncoder(w).Encode(st.list())
        case http.MethodPost:
            var req request
            if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Resource == "" || req.Action == "" {
                http.Error(w, "invalid payload", http.StatusBadRequest)
                return
            }
            created := st.add(req)
            notif.send("📦 Resource request " + strconv.Itoa(created.ID) + " (" + created.Resource + ":" + created.Action + ")")
            w.Header().Set("Content-Type", "application/json")
            json.NewEncoder(w).Encode(created)
        default:
            w.WriteHeader(http.StatusMethodNotAllowed)
        }
    })
    mux.HandleFunc("/requests/resolve", func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
            w.WriteHeader(http.StatusMethodNotAllowed)
            return
        }
        idStr := r.URL.Query().Get("id")
        status := r.URL.Query().Get("status")
        id, _ := strconv.Atoi(idStr)
        if id <= 0 || status == "" {
            http.Error(w, "id and status required", http.StatusBadRequest)
            return
        }
        if st.complete(id, status) {
            notif.send("✅ Resource request " + strconv.Itoa(id) + " resolved: " + status)
            w.WriteHeader(http.StatusNoContent)
        } else {
            http.Error(w, "not found", http.StatusNotFound)
        }
    })

    addr := ":9091"
    logger.Printf("resource-broker listening on %s", addr)
    if err := http.ListenAndServe(addr, mux); err != nil && err != http.ErrServerClosed {
        logger.Fatalf("server error: %v", err)
    }
}

func env(k, def string) string {
    if v := os.Getenv(k); v != "" {
        return v
    }
    return def
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
