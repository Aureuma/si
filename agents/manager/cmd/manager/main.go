package main

import (
    "encoding/json"
    "log"
    "net/http"
    "os"
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

type store struct {
    mu    sync.Mutex
    beats []heartbeat
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

func main() {
    logger := log.New(os.Stdout, "manager ", log.LstdFlags|log.LUTC)
    st := &store{}

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

    addr := ":9090"
    logger.Printf("manager listening on %s", addr)
    if err := http.ListenAndServe(addr, nil); err != nil {
        logger.Fatalf("server error: %v", err)
    }
}
