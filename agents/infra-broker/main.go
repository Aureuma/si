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

type infraRequest struct {
    ID          int       `json:"id"`
    Category    string    `json:"category"` // e.g., network, dns, ssl
    Action      string    `json:"action"`
    Payload     string    `json:"payload"`
    RequestedBy string    `json:"requested_by"`
    Notes       string    `json:"notes"`
    Status      string    `json:"status"` // pending|approved|in-progress|done|denied
    CreatedAt   time.Time `json:"created_at"`
    UpdatedAt   time.Time `json:"updated_at"`
}

type store struct {
    mu     sync.Mutex
    path   string
    nextID int
    items  []infraRequest
}

func newStore(path string, logger *log.Logger) *store {
    _ = os.MkdirAll(filepath.Dir(path), 0o755)
    s := &store{path: path}
    s.load(logger)
    return s
}

func (s *store) load(logger *log.Logger) {
    b, err := os.ReadFile(s.path)
    if err != nil {
        if os.IsNotExist(err) {
            return
        }
        logger.Printf("load error: %v", err)
        return
    }
    var payload struct {
        Items  []infraRequest `json:"items"`
        NextID int            `json:"next_id"`
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
        Items  []infraRequest `json:"items"`
        NextID int            `json:"next_id"`
    }{Items: s.items, NextID: s.nextID}
    b, err := json.MarshalIndent(payload, "", "  ")
    if err != nil {
        return
    }
    tmp := s.path + ".tmp"
    if err := os.WriteFile(tmp, b, 0o644); err == nil {
        _ = os.Rename(tmp, s.path)
    }
}

func (s *store) add(req infraRequest) infraRequest {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.nextID++
    req.ID = s.nextID
    req.Status = "pending"
    now := time.Now().UTC()
    req.CreatedAt = now
    req.UpdatedAt = now
    s.items = append(s.items, req)
    s.persist()
    return req
}

func (s *store) list() []infraRequest {
    s.mu.Lock()
    defer s.mu.Unlock()
    out := make([]infraRequest, len(s.items))
    copy(out, s.items)
    return out
}

func (s *store) updateStatus(id int, status, notes string) bool {
    s.mu.Lock()
    defer s.mu.Unlock()
    for i := range s.items {
        if s.items[i].ID == id {
            s.items[i].Status = status
            s.items[i].Notes = notes
            s.items[i].UpdatedAt = time.Now().UTC()
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
    logger := log.New(os.Stdout, "infra-broker ", log.LstdFlags|log.LUTC)
    dataDir := env("DATA_DIR", "/data")
    chat := readChatID()
    notif := &notifier{url: os.Getenv("TELEGRAM_NOTIFY_URL"), chatID: chat, logger: logger}
    st := newStore(filepath.Join(dataDir, "infra_requests.json"), logger)

    mux := http.NewServeMux()
    mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK); w.Write([]byte("ok")) })
    mux.HandleFunc("/infra", func(w http.ResponseWriter, r *http.Request) {
        switch r.Method {
        case http.MethodGet:
            w.Header().Set("Content-Type", "application/json")
            json.NewEncoder(w).Encode(st.list())
        case http.MethodPost:
            var req infraRequest
            if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Category == "" || req.Action == "" {
                http.Error(w, "invalid payload", http.StatusBadRequest)
                return
            }
            created := st.add(req)
            notif.send("🏗️ Infra request " + strconv.Itoa(created.ID) + " (" + created.Category + ":" + created.Action + ")")
            w.Header().Set("Content-Type", "application/json")
            json.NewEncoder(w).Encode(created)
        default:
            w.WriteHeader(http.StatusMethodNotAllowed)
        }
    })
    mux.HandleFunc("/infra/resolve", func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
            w.WriteHeader(http.StatusMethodNotAllowed)
            return
        }
        idStr := r.URL.Query().Get("id")
        status := r.URL.Query().Get("status")
        notes := r.URL.Query().Get("notes")
        id, _ := strconv.Atoi(idStr)
        if id <= 0 || status == "" {
            http.Error(w, "id and status required", http.StatusBadRequest)
            return
        }
        if st.updateStatus(id, status, notes) {
            notif.send("✅ Infra request " + strconv.Itoa(id) + " -> " + status)
            w.WriteHeader(http.StatusNoContent)
        } else {
            http.Error(w, "not found", http.StatusNotFound)
        }
    })

    addr := ":9092"
    logger.Printf("infra-broker listening on %s", addr)
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
