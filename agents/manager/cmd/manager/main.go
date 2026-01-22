package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"silexa/agents/manager/internal/state"
)

type server struct {
	logger   *log.Logger
	store    *state.Store
	policy   dyadPolicy
	notifier *notifier
}

func main() {
	logger := log.New(os.Stdout, "manager ", log.LstdFlags|log.LUTC)
	addr := env("ADDR", ":9090")
	policy := loadDyadPolicy()
	notif := buildNotifier(logger)

	statePath := resolveStatePath()
	store, err := state.NewStore(statePath)
	if err != nil {
		logger.Fatalf("state store: %v", err)
	}

	srv := &server{
		logger:   logger,
		store:    store,
		policy:   policy,
		notifier: notif,
	}

	http.HandleFunc("/heartbeat", srv.handleHeartbeat)
	http.HandleFunc("/beats", srv.handleBeats)
	http.HandleFunc("/dyads", srv.handleDyads)
	http.HandleFunc("/human-tasks", srv.handleHumanTasks)
	http.HandleFunc("/human-tasks/complete", srv.handleHumanTasksComplete)
	http.HandleFunc("/feedback", srv.handleFeedback)
	http.HandleFunc("/access-requests", srv.handleAccessRequests)
	http.HandleFunc("/access-requests/resolve", srv.handleAccessResolve)
	http.HandleFunc("/metrics", srv.handleMetrics)
	http.HandleFunc("/healthz", srv.handleHealth)
	http.HandleFunc("/dyad-tasks", srv.handleDyadTasks)
	http.HandleFunc("/dyad-tasks/update", srv.handleDyadTaskUpdate)
	http.HandleFunc("/dyad-tasks/claim", srv.handleDyadTaskClaim)

	srv.startDyadDigest()

	logger.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		logger.Fatalf("server error: %v", err)
	}
}

func (s *server) query(ctx context.Context, name string, out any) error {
	_ = ctx
	return s.store.Query(name, out)
}

func (s *server) update(ctx context.Context, name string, out any, args ...any) error {
	_ = ctx
	return s.store.Update(name, out, args...)
}

func (s *server) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var hb state.Heartbeat
	if err := json.NewDecoder(r.Body).Decode(&hb); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	var out state.Heartbeat
	if err := s.update(ctx, "heartbeat", &out, hb); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleBeats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	var beats []state.Heartbeat
	if err := s.query(ctx, "beats", &beats); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, beats)
}

func (s *server) handleDyads(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	switch r.Method {
	case http.MethodGet:
		var dyads []state.DyadSnapshot
		if err := s.query(ctx, "dyads", &dyads); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusOK, dyads)
	case http.MethodPost:
		var update state.DyadUpdate
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil || strings.TrimSpace(update.Dyad) == "" {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
		var out state.DyadRecord
		if err := s.update(ctx, "upsert_dyad", &out, update); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusOK, out)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *server) handleHumanTasks(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	switch r.Method {
	case http.MethodGet:
		var tasks []state.HumanTask
		if err := s.query(ctx, "human-tasks", &tasks); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusOK, tasks)
	case http.MethodPost:
		var ht state.HumanTask
		if err := json.NewDecoder(r.Body).Decode(&ht); err != nil || strings.TrimSpace(ht.Title) == "" || strings.TrimSpace(ht.Commands) == "" {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
		var out state.HumanTask
		if err := s.update(ctx, "add_human_task", &out, ht); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		if s.notifier != nil {
			go s.notifier.maybeSend(formatTaskMessage(out), out.ChatID)
		}
		writeJSON(w, http.StatusOK, out)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *server) handleHumanTasksComplete(w http.ResponseWriter, r *http.Request) {
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
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	var ok bool
	if err := s.update(ctx, "complete_human_task", &ok, id); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Error(w, "not found", http.StatusNotFound)
}

func (s *server) handleFeedback(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	switch r.Method {
	case http.MethodGet:
		var out []state.Feedback
		if err := s.query(ctx, "feedback", &out); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusOK, out)
	case http.MethodPost:
		var fb state.Feedback
		if err := json.NewDecoder(r.Body).Decode(&fb); err != nil || fb.Message == "" {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
		var out state.Feedback
		if err := s.update(ctx, "add_feedback", &out, fb); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusOK, out)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *server) handleAccessRequests(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	switch r.Method {
	case http.MethodGet:
		var out []state.AccessRequest
		if err := s.query(ctx, "access-requests", &out); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusOK, out)
	case http.MethodPost:
		var ar state.AccessRequest
		if err := json.NewDecoder(r.Body).Decode(&ar); err != nil || ar.Requester == "" || ar.Resource == "" || ar.Action == "" {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
		var out state.AccessRequest
		if err := s.update(ctx, "add_access_request", &out, ar); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusOK, out)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *server) handleAccessResolve(w http.ResponseWriter, r *http.Request) {
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

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	var out state.ResolveResult
	if err := s.update(ctx, "resolve_access_request", &out, id, status, by, notes); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if !out.Found {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, out.Request)
}

func (s *server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	switch r.Method {
	case http.MethodGet:
		var out []state.Metric
		if err := s.query(ctx, "metrics", &out); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusOK, out)
	case http.MethodPost:
		var m state.Metric
		if err := json.NewDecoder(r.Body).Decode(&m); err != nil || m.Name == "" {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
		var out state.Metric
		if err := s.update(ctx, "add_metric", &out, m); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusOK, out)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	var resp map[string]interface{}
	if err := s.query(ctx, "healthz", &resp); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *server) handleDyadTasks(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	switch r.Method {
	case http.MethodGet:
		var out []state.DyadTask
		if err := s.query(ctx, "dyad-tasks", &out); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusOK, out)
	case http.MethodPost:
		var dt state.DyadTask
		if err := json.NewDecoder(r.Body).Decode(&dt); err != nil || strings.TrimSpace(dt.Title) == "" {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
		dt.Dyad = strings.TrimSpace(dt.Dyad)
		dt.Status = normalizeStatus(dt.Status)
		if dt.Status == "" {
			dt.Status = "todo"
		}
		if code, msg, err := s.validateDyadPolicy(ctx, dt.Dyad, dt.Status, "", 0, nil, nil); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		} else if code != 0 {
			http.Error(w, msg, code)
			return
		}
		var out state.DyadTask
		if err := s.update(ctx, "add_dyad_task", &out, dt); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
	if s.notifier != nil && shouldNotifyDyadTask(out) {
		if messageID, _ := s.notifier.upsertDyadTaskMessage(out); messageID > 0 && messageID != out.TelegramMessageID {
			var updated state.UpdateResult
			_ = s.update(ctx, "update_dyad_task", &updated, state.DyadTask{ID: out.ID, TelegramMessageID: messageID})
			out.TelegramMessageID = messageID
		}
	}
	writeJSON(w, http.StatusOK, out)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *server) handleDyadTaskUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var dt state.DyadTask
	if err := json.NewDecoder(r.Body).Decode(&dt); err != nil || dt.ID == 0 {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	var tasks []state.DyadTask
	if err := s.query(ctx, "dyad-tasks", &tasks); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	existing, ok := findDyadTask(tasks, dt.ID)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	newDyad := strings.TrimSpace(existing.Dyad)
	if strings.TrimSpace(dt.Dyad) != "" {
		newDyad = strings.TrimSpace(dt.Dyad)
	}
	newStatus := normalizeStatus(existing.Status)
	if strings.TrimSpace(dt.Status) != "" {
		newStatus = normalizeStatus(dt.Status)
		dt.Status = newStatus
	}
	if code, msg, err := s.validateDyadPolicy(ctx, newDyad, newStatus, existing.Dyad, existing.ID, tasks, nil); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	} else if code != 0 {
		http.Error(w, msg, code)
		return
	}
	var out state.UpdateResult
	if err := s.update(ctx, "update_dyad_task", &out, dt); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if !out.Found {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if s.notifier != nil && shouldNotifyDyadTask(out.Task) {
		if messageID, _ := s.notifier.upsertDyadTaskMessage(out.Task); messageID > 0 && messageID != out.Task.TelegramMessageID {
			var updated state.UpdateResult
			_ = s.update(ctx, "update_dyad_task", &updated, state.DyadTask{ID: out.Task.ID, TelegramMessageID: messageID})
			out.Task.TelegramMessageID = messageID
		}
	}
	writeJSON(w, http.StatusOK, out.Task)
}

func (s *server) handleDyadTaskClaim(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var payload struct {
		ID     int    `json:"id"`
		Dyad   string `json:"dyad"`
		Critic string `json:"critic"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.ID <= 0 || payload.Critic == "" {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	payload.Dyad = strings.TrimSpace(payload.Dyad)
	if payload.Dyad == "" || isPoolDyad(payload.Dyad) {
		http.Error(w, "dyad required", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	var tasks []state.DyadTask
	if err := s.query(ctx, "dyad-tasks", &tasks); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	existing, ok := findDyadTask(tasks, payload.ID)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if code, msg, err := s.validateDyadPolicy(ctx, payload.Dyad, "in_progress", existing.Dyad, existing.ID, tasks, nil); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	} else if code != 0 {
		http.Error(w, msg, code)
		return
	}
	var out state.ClaimResult
	if err := s.update(ctx, "claim_dyad_task", &out, payload.ID, payload.Dyad, payload.Critic); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if !out.Found {
		w.WriteHeader(out.Code)
		return
	}
	if s.notifier != nil && shouldNotifyDyadTask(out.Task) {
		if messageID, _ := s.notifier.upsertDyadTaskMessage(out.Task); messageID > 0 && messageID != out.Task.TelegramMessageID {
			var updated state.UpdateResult
			_ = s.update(ctx, "update_dyad_task", &updated, state.DyadTask{ID: out.Task.ID, TelegramMessageID: messageID})
			out.Task.TelegramMessageID = messageID
		}
	}
	writeJSON(w, http.StatusOK, out.Task)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func resolveStatePath() string {
	statePath := strings.TrimSpace(os.Getenv("STATE_PATH"))
	dataDir := strings.TrimSpace(os.Getenv("DATA_DIR"))
	if statePath == "" && dataDir != "" {
		statePath = filepath.Join(dataDir, "manager_state.json")
	}
	if statePath == "" {
		statePath = "manager_state.json"
	}
	return statePath
}
