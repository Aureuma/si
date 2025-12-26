package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var startTime = time.Now().UTC()

type heartbeat struct {
	Dyad    string    `json:"dyad"`
	Role    string    `json:"role"`
	Department string `json:"department"`
	Actor   string    `json:"actor"`
	Critic  string    `json:"critic"`
	Status  string    `json:"status"`
	Message string    `json:"message"`
	When    time.Time `json:"when"`
}

type humanTask struct {
	ID          int        `json:"id"`
	Title       string     `json:"title"`
	Commands    string     `json:"commands"`
	URL         string     `json:"url"`
	Timeout     string     `json:"timeout"`
	RequestedBy string     `json:"requested_by"`
	Notes       string     `json:"notes"`
	ChatID      *int64     `json:"chat_id,omitempty"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
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
	ID         int        `json:"id"`
	Requester  string     `json:"requester"` // dyad or actor/critic
	Department string     `json:"department"`
	Resource   string     `json:"resource"`
	Action     string     `json:"action"`
	Reason     string     `json:"reason"`
	Status     string     `json:"status"` // pending|approved|denied
	CreatedAt  time.Time  `json:"created_at"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	ResolvedBy string     `json:"resolved_by,omitempty"`
	Notes      string     `json:"notes,omitempty"`
}

type metric struct {
	ID         int       `json:"id"`
	Dyad       string    `json:"dyad"`
	Department string    `json:"department"`
	Name       string    `json:"name"`
	Value      float64   `json:"value"`
	Unit       string    `json:"unit"`
	RecordedBy string    `json:"recorded_by"`
	CreatedAt  time.Time `json:"created_at"`
}

// Dyad-level work items (task board for actors/critics)
type dyadTask struct {
	ID          int       `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Kind        string    `json:"kind"`   // free-form: e.g. codex_exec|beam.codex_login
	Status      string    `json:"status"` // todo|in_progress|review|blocked|done
	Priority    string    `json:"priority"`
	Dyad        string    `json:"dyad"`
	Actor       string    `json:"actor"`
	Critic      string    `json:"critic"`
	RequestedBy string    `json:"requested_by"`
	Notes       string    `json:"notes"`
	Link        string    `json:"link"`
	TelegramMessageID int `json:"telegram_message_id,omitempty"`
	ClaimedBy   string    `json:"claimed_by"`
	ClaimedAt   time.Time `json:"claimed_at"`
	HeartbeatAt time.Time `json:"heartbeat_at"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type dyadRecord struct {
	Dyad          string    `json:"dyad"`
	Department    string    `json:"department,omitempty"`
	Role          string    `json:"role,omitempty"`
	Team          string    `json:"team,omitempty"`
	Assignment    string    `json:"assignment,omitempty"`
	Tags          []string  `json:"tags,omitempty"`
	Actor         string    `json:"actor,omitempty"`
	Critic        string    `json:"critic,omitempty"`
	Status        string    `json:"status,omitempty"`
	Message       string    `json:"message,omitempty"`
	Available     bool      `json:"available"`
	LastHeartbeat time.Time `json:"last_heartbeat,omitempty"`
	UpdatedAt     time.Time `json:"updated_at,omitempty"`
}

type dyadUpdate struct {
	Dyad       string `json:"dyad"`
	Department string `json:"department,omitempty"`
	Role       string `json:"role,omitempty"`
	Team       string `json:"team,omitempty"`
	Assignment string `json:"assignment,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	Actor      string `json:"actor,omitempty"`
	Critic     string `json:"critic,omitempty"`
	Status     string `json:"status,omitempty"`
	Message    string `json:"message,omitempty"`
	Available  *bool  `json:"available,omitempty"`
}

type dyadSnapshot struct {
	Dyad                  string    `json:"dyad"`
	Department            string    `json:"department,omitempty"`
	Role                  string    `json:"role,omitempty"`
	Team                  string    `json:"team,omitempty"`
	Assignment            string    `json:"assignment,omitempty"`
	Tags                  []string  `json:"tags,omitempty"`
	Actor                 string    `json:"actor,omitempty"`
	Critic                string    `json:"critic,omitempty"`
	Status                string    `json:"status,omitempty"`
	Message               string    `json:"message,omitempty"`
	Available             bool      `json:"available"`
	State                 string    `json:"state"`
	LastHeartbeat         time.Time `json:"last_heartbeat,omitempty"`
	LastHeartbeatAgeSec   int64     `json:"last_heartbeat_age_sec,omitempty"`
	UpdatedAt             time.Time `json:"updated_at,omitempty"`
}

type store struct {
	mu             sync.Mutex
	beats          []heartbeat
	tasks          []humanTask
	nextTaskID     int
	feedbacks      []feedback
	nextFeedbackID int
	access         []accessRequest
	nextAccessID   int
	metrics        []metric
	nextMetricID   int
	dyadTasks      []dyadTask
	nextDyadTaskID int
	dyads          []dyadRecord
	meta           storeMeta
	dataPath       string
}

type storeMeta struct {
	DyadDigestTelegramMessageID int `json:"dyad_digest_telegram_message_id,omitempty"`
}

func (s *store) add(h heartbeat) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.beats = append(s.beats, h)
	if len(s.beats) > 1000 {
		s.beats = s.beats[len(s.beats)-1000:]
	}
	s.updateDyadFromHeartbeatLocked(h)
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

func (s *store) addMetric(m metric) metric {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextMetricID++
	m.ID = s.nextMetricID
	m.CreatedAt = time.Now().UTC()
	s.metrics = append(s.metrics, m)
	s.persistLocked()
	return m
}

func (s *store) addDyadTask(t dyadTask) dyadTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextDyadTaskID++
	t.ID = s.nextDyadTaskID
	if t.Status == "" {
		t.Status = "todo"
	}
	if t.Priority == "" {
		t.Priority = "normal"
	}
	t.CreatedAt = time.Now().UTC()
	t.UpdatedAt = t.CreatedAt
	s.dyadTasks = append(s.dyadTasks, t)
	s.persistLocked()
	return t
}

func (s *store) listDyadTasks() []dyadTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]dyadTask, len(s.dyadTasks))
	copy(out, s.dyadTasks)
	return out
}

func (s *store) listDyads() []dyadRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]dyadRecord, len(s.dyads))
	copy(out, s.dyads)
	return out
}

func (s *store) upsertDyad(update dyadUpdate) dyadRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	updatedAt := time.Now().UTC()
	dyad := strings.TrimSpace(update.Dyad)
	if dyad == "" {
		return dyadRecord{}
	}
	for i := range s.dyads {
		if s.dyads[i].Dyad != dyad {
			continue
		}
		if update.Department != "" {
			s.dyads[i].Department = update.Department
		}
		if update.Role != "" {
			s.dyads[i].Role = update.Role
		}
		if update.Team != "" {
			s.dyads[i].Team = update.Team
		}
		if update.Assignment != "" {
			s.dyads[i].Assignment = update.Assignment
		}
		if update.Tags != nil {
			s.dyads[i].Tags = update.Tags
		}
		if update.Actor != "" {
			s.dyads[i].Actor = update.Actor
		}
		if update.Critic != "" {
			s.dyads[i].Critic = update.Critic
		}
		if update.Status != "" {
			s.dyads[i].Status = update.Status
		}
		if update.Message != "" {
			s.dyads[i].Message = update.Message
		}
		if update.Available != nil {
			s.dyads[i].Available = *update.Available
		}
		s.dyads[i].UpdatedAt = updatedAt
		s.persistLocked()
		return s.dyads[i]
	}
	record := dyadRecord{
		Dyad:       dyad,
		Available:  true,
		UpdatedAt:  updatedAt,
		Department: update.Department,
		Role:       update.Role,
		Team:       update.Team,
		Assignment: update.Assignment,
		Tags:       update.Tags,
		Actor:      update.Actor,
		Critic:     update.Critic,
		Status:     update.Status,
		Message:    update.Message,
	}
	if update.Available != nil {
		record.Available = *update.Available
	}
	s.dyads = append(s.dyads, record)
	s.persistLocked()
	return record
}

func (s *store) updateDyadFromHeartbeatLocked(h heartbeat) {
	dyad := strings.TrimSpace(h.Dyad)
	if dyad == "" {
		dyad = dyadFromContainer(h.Actor)
	}
	if dyad == "" {
		dyad = dyadFromContainer(h.Critic)
	}
	if dyad == "" {
		return
	}
	for i := range s.dyads {
		if s.dyads[i].Dyad != dyad {
			continue
		}
		if h.Department != "" {
			s.dyads[i].Department = h.Department
		}
		if h.Role != "" {
			s.dyads[i].Role = h.Role
		}
		if h.Actor != "" {
			s.dyads[i].Actor = h.Actor
		}
		if h.Critic != "" {
			s.dyads[i].Critic = h.Critic
		}
		if h.Status != "" {
			s.dyads[i].Status = h.Status
		}
		if h.Message != "" {
			s.dyads[i].Message = h.Message
		}
		s.dyads[i].LastHeartbeat = h.When
		s.dyads[i].UpdatedAt = h.When
		return
	}
}

func (s *store) updateDyadTask(updated dyadTask) (dyadTask, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.dyadTasks {
		if s.dyadTasks[i].ID == updated.ID {
			// apply partial updates
			if updated.Title != "" {
				s.dyadTasks[i].Title = updated.Title
			}
			if updated.Description != "" {
				s.dyadTasks[i].Description = updated.Description
			}
			if updated.Kind != "" {
				s.dyadTasks[i].Kind = updated.Kind
			}
			if updated.Status != "" {
				s.dyadTasks[i].Status = updated.Status
			}
			if updated.Priority != "" {
				s.dyadTasks[i].Priority = updated.Priority
			}
			if updated.Dyad != "" {
				s.dyadTasks[i].Dyad = updated.Dyad
			}
			if updated.Actor != "" {
				s.dyadTasks[i].Actor = updated.Actor
			}
			if updated.Critic != "" {
				s.dyadTasks[i].Critic = updated.Critic
			}
			if updated.RequestedBy != "" {
				s.dyadTasks[i].RequestedBy = updated.RequestedBy
			}
			if updated.Notes != "" {
				s.dyadTasks[i].Notes = updated.Notes
			}
			if updated.Link != "" {
				s.dyadTasks[i].Link = updated.Link
			}
			if updated.TelegramMessageID != 0 {
				s.dyadTasks[i].TelegramMessageID = updated.TelegramMessageID
			}
			if updated.ClaimedBy != "" {
				s.dyadTasks[i].ClaimedBy = updated.ClaimedBy
				if !updated.ClaimedAt.IsZero() {
					s.dyadTasks[i].ClaimedAt = updated.ClaimedAt
				} else if s.dyadTasks[i].ClaimedAt.IsZero() {
					s.dyadTasks[i].ClaimedAt = time.Now().UTC()
				}
			}
			if !updated.HeartbeatAt.IsZero() {
				s.dyadTasks[i].HeartbeatAt = updated.HeartbeatAt
			}
			s.dyadTasks[i].UpdatedAt = time.Now().UTC()
			s.persistLocked()
			return s.dyadTasks[i], true
		}
	}
	return dyadTask{}, false
}

func (s *store) setDyadTaskTelegramMessageID(id int, messageID int) (dyadTask, bool) {
	if id <= 0 || messageID <= 0 {
		return dyadTask{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.dyadTasks {
		if s.dyadTasks[i].ID != id {
			continue
		}
		if s.dyadTasks[i].TelegramMessageID == messageID {
			return s.dyadTasks[i], true
		}
		s.dyadTasks[i].TelegramMessageID = messageID
		s.dyadTasks[i].UpdatedAt = time.Now().UTC()
		s.persistLocked()
		return s.dyadTasks[i], true
	}
	return dyadTask{}, false
}

func (s *store) getDyadDigestTelegramMessageID() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.meta.DyadDigestTelegramMessageID
}

func (s *store) setDyadDigestTelegramMessageID(messageID int) {
	if messageID <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.meta.DyadDigestTelegramMessageID == messageID {
		return
	}
	s.meta.DyadDigestTelegramMessageID = messageID
	s.persistLocked()
}

func (s *store) claimDyadTask(id int, dyad, critic string) (dyadTask, int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.dyadTasks {
		if s.dyadTasks[i].ID != id {
			continue
		}
		if s.dyadTasks[i].Status == "done" {
			return dyadTask{}, http.StatusConflict, false
		}
		if dyad != "" && s.dyadTasks[i].Dyad != "" && s.dyadTasks[i].Dyad != dyad {
			return dyadTask{}, http.StatusConflict, false
		}
		now := time.Now().UTC()
		// Allow claim if unclaimed, self-claimed, or stale heartbeat (5m).
		if s.dyadTasks[i].ClaimedBy != "" && s.dyadTasks[i].ClaimedBy != critic {
			if !s.dyadTasks[i].HeartbeatAt.IsZero() && now.Sub(s.dyadTasks[i].HeartbeatAt) < 5*time.Minute {
				return dyadTask{}, http.StatusConflict, false
			}
		}
		if dyad != "" && s.dyadTasks[i].Dyad == "" {
			s.dyadTasks[i].Dyad = dyad
		}
		if critic != "" {
			if s.dyadTasks[i].ClaimedBy == "" || s.dyadTasks[i].ClaimedBy != critic {
				s.dyadTasks[i].ClaimedBy = critic
				s.dyadTasks[i].ClaimedAt = now
			}
			s.dyadTasks[i].HeartbeatAt = now
		}
		if s.dyadTasks[i].Status == "todo" {
			s.dyadTasks[i].Status = "in_progress"
		}
		s.dyadTasks[i].UpdatedAt = now
		s.persistLocked()
		return s.dyadTasks[i], http.StatusOK, true
	}
	return dyadTask{}, http.StatusNotFound, false
}

func (s *store) listMetrics() []metric {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]metric, len(s.metrics))
	copy(out, s.metrics)
	return out
}

func (s *store) summary() (tasksOpen, accessPending, metricsCount, beatsCount int, lastBeat *time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.tasks {
		if t.Status != "done" {
			tasksOpen++
		}
	}
	for _, a := range s.access {
		if a.Status == "pending" {
			accessPending++
		}
	}
	metricsCount = len(s.metrics)
	beatsCount = len(s.beats)
	if beatsCount > 0 {
		last := s.beats[beatsCount-1].When
		lastBeat = &last
	}
	return
}

type notifier struct {
	url    string
	chatID *int64
	logger *log.Logger
}

type notifyResult struct {
	OK        bool `json:"ok"`
	Edited    bool `json:"edited"`
	MessageID int  `json:"message_id"`
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
	payload := map[string]interface{}{
		"message":                 msg,
		"parse_mode":              "HTML",
		"disable_web_page_preview": true,
	}
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

func (n *notifier) upsertDyadTaskMessage(t dyadTask) (int, bool) {
	if n == nil || n.url == "" {
		return 0, false
	}
	if n.chatID == nil {
		n.logger.Printf("skip notify: no chat id provided")
		return 0, false
	}
	payload := map[string]interface{}{
		"chat_id":                  *n.chatID,
		"message":                  formatDyadTaskMessage(t),
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	}
	if t.TelegramMessageID != 0 {
		payload["message_id"] = t.TelegramMessageID
	}
	b, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, n.url, bytes.NewReader(b))
	if err != nil {
		n.logger.Printf("notify build error: %v", err)
		return 0, false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		n.logger.Printf("notify send error: %v", err)
		return 0, false
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 300 {
		n.logger.Printf("notify non-2xx: %s", resp.Status)
		return 0, false
	}
	var out notifyResult
	if err := json.Unmarshal(body, &out); err != nil {
		// Still consider it successful even if we can't parse (back-compat).
		return 0, false
	}
	return out.MessageID, out.Edited
}

func (n *notifier) upsertMessageHTML(message string, messageID int) (int, bool) {
	if n == nil || n.url == "" {
		return 0, false
	}
	if n.chatID == nil {
		n.logger.Printf("skip notify: no chat id provided")
		return 0, false
	}
	payload := map[string]interface{}{
		"chat_id":                  *n.chatID,
		"message":                  message,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	}
	if messageID > 0 {
		payload["message_id"] = messageID
	}
	b, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, n.url, bytes.NewReader(b))
	if err != nil {
		n.logger.Printf("notify build error: %v", err)
		return 0, false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		n.logger.Printf("notify send error: %v", err)
		return 0, false
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 300 {
		n.logger.Printf("notify non-2xx: %s", resp.Status)
		return 0, false
	}
	var out notifyResult
	if err := json.Unmarshal(body, &out); err != nil {
		return 0, false
	}
	return out.MessageID, out.Edited
}

func main() {
	logger := log.New(os.Stdout, "manager ", log.LstdFlags|log.LUTC)
	dataDir := envOr("DATA_DIR", "/data")
	st := newStore(dataDir, logger)
	notif := buildNotifier(logger)
	startDyadDigest(st, notif, logger)

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

	http.HandleFunc("/dyads", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			list := st.listDyads()
			snapshots := buildDyadSnapshots(list)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(snapshots)
		case http.MethodPost:
			var update dyadUpdate
			if err := json.NewDecoder(r.Body).Decode(&update); err != nil || strings.TrimSpace(update.Dyad) == "" {
				http.Error(w, "invalid payload", http.StatusBadRequest)
				return
			}
			updated := st.upsertDyad(update)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(updated)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
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

	http.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			list := st.listMetrics()
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(list)
		case http.MethodPost:
			var m metric
			if err := json.NewDecoder(r.Body).Decode(&m); err != nil || m.Name == "" {
				http.Error(w, "invalid payload", http.StatusBadRequest)
				return
			}
			created := st.addMetric(m)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(created)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	http.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		openTasks, pendingAccess, metricsCount, beatsCount, lastBeat := st.summary()
		resp := map[string]interface{}{
			"status":         "ok",
			"tasks_open":     openTasks,
			"access_pending": pendingAccess,
			"metrics_count":  metricsCount,
			"beats_recent":   beatsCount,
			"uptime_seconds": int64(time.Since(startTime).Seconds()),
		}
		if lastBeat != nil {
			resp["last_beat"] = lastBeat.UTC()
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	http.HandleFunc("/dyad-tasks", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			list := st.listDyadTasks()
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(list)
		case http.MethodPost:
			var dt dyadTask
			if err := json.NewDecoder(r.Body).Decode(&dt); err != nil || dt.Title == "" {
				http.Error(w, "invalid payload", http.StatusBadRequest)
				return
			}
			created := st.addDyadTask(dt)
			if shouldNotifyDyadTask(created) {
				if messageID, _ := notif.upsertDyadTaskMessage(created); messageID > 0 && messageID != created.TelegramMessageID {
					st.setDyadTaskTelegramMessageID(created.ID, messageID)
					created.TelegramMessageID = messageID
				}
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(created)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	http.HandleFunc("/dyad-tasks/update", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var dt dyadTask
		if err := json.NewDecoder(r.Body).Decode(&dt); err != nil || dt.ID == 0 {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
		updated, ok := st.updateDyadTask(dt)
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if shouldNotifyDyadTask(updated) {
			if messageID, _ := notif.upsertDyadTaskMessage(updated); messageID > 0 && messageID != updated.TelegramMessageID {
				st.setDyadTaskTelegramMessageID(updated.ID, messageID)
				updated.TelegramMessageID = messageID
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(updated)
	})

	http.HandleFunc("/dyad-tasks/claim", func(w http.ResponseWriter, r *http.Request) {
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
		updated, code, ok := st.claimDyadTask(payload.ID, payload.Dyad, payload.Critic)
		if !ok {
			w.WriteHeader(code)
			return
		}
		if shouldNotifyDyadTask(updated) {
			if messageID, _ := notif.upsertDyadTaskMessage(updated); messageID > 0 && messageID != updated.TelegramMessageID {
				st.setDyadTaskTelegramMessageID(updated.ID, messageID)
				updated.TelegramMessageID = messageID
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(updated)
	})

	addr := ":9090"
	logger.Printf("manager listening on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		logger.Fatalf("server error: %v", err)
	}
}

func startDyadDigest(st *store, notif *notifier, logger *log.Logger) {
	if notif == nil || notif.url == "" || notif.chatID == nil {
		return
	}
	interval := envOr("DYAD_TASK_DIGEST_INTERVAL", "10m")
	d, err := time.ParseDuration(interval)
	if err != nil || d <= 0 {
		d = 10 * time.Minute
	}
	go func() {
		// Send one initial snapshot after startup.
		time.Sleep(3 * time.Second)
		for {
			sendDyadDigestOnce(st, notif, logger)
			time.Sleep(d)
		}
	}()
}

func sendDyadDigestOnce(st *store, notif *notifier, logger *log.Logger) {
	tasks := st.listDyadTasks()
	open := make([]dyadTask, 0, len(tasks))
	for _, t := range tasks {
		if strings.ToLower(strings.TrimSpace(t.Status)) == "done" {
			continue
		}
		open = append(open, t)
	}
	sort.Slice(open, func(i, j int) bool {
		pi := priorityRank(open[i].Priority)
		pj := priorityRank(open[j].Priority)
		if pi != pj {
			return pi > pj
		}
		si := statusRank(open[i].Status)
		sj := statusRank(open[j].Status)
		if si != sj {
			return si < sj
		}
		if open[i].Dyad != open[j].Dyad {
			return open[i].Dyad < open[j].Dyad
		}
		return open[i].ID < open[j].ID
	})

	var b strings.Builder
	b.WriteString("🧭 <b>Dyad Task Board</b>\n")
	b.WriteString("<b>Open:</b> " + strconv.Itoa(len(open)) + "\n")
	b.WriteString("<b>When (UTC):</b> " + formatUTCWhen(time.Now().UTC()) + "\n")
	if len(open) == 0 {
		b.WriteString("\n✅ <b>All clear</b>")
	} else {
		b.WriteString("\n")
		for i, t := range open {
			if i >= 20 {
				b.WriteString("…\n")
				break
			}
			line := fmt.Sprintf("%s %s <b>#%d</b> %s",
				statusEmoji(t.Status),
				kindEmoji(t.Kind),
				t.ID,
				html.EscapeString(strings.TrimSpace(t.Title)),
			)
			if strings.TrimSpace(t.Dyad) != "" {
				line += " <i>(" + html.EscapeString(strings.TrimSpace(t.Dyad)) + ")</i>"
			}
			if strings.TrimSpace(t.ClaimedBy) != "" {
				line += " — " + html.EscapeString(strings.TrimSpace(t.ClaimedBy))
			}
			b.WriteString(line + "\n")
		}
	}

	prevID := st.getDyadDigestTelegramMessageID()
	newID, _ := notif.upsertMessageHTML(strings.TrimSpace(b.String()), prevID)
	if newID > 0 && newID != prevID {
		st.setDyadDigestTelegramMessageID(newID)
		logger.Printf("dyad digest anchored message_id=%d", newID)
	}
}

func statusRank(status string) int {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "blocked":
		return 0
	case "review":
		return 1
	case "in_progress":
		return 2
	case "todo":
		return 3
	default:
		return 9
	}
}

func priorityRank(priority string) int {
	switch strings.ToLower(strings.TrimSpace(priority)) {
	case "high", "p0", "urgent":
		return 300
	case "normal", "medium", "p1", "":
		return 200
	case "low", "p2":
		return 100
	default:
		return 150
	}
}

func dyadFromContainer(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	prefixes := []string{"actor-", "critic-", "silexa-actor-", "silexa-critic-"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(name, prefix) {
			return strings.TrimPrefix(name, prefix)
		}
	}
	if idx := strings.Index(name, "_"); idx != -1 {
		trimmed := name[idx+1:]
		for _, prefix := range []string{"actor-", "critic-"} {
			if strings.HasPrefix(trimmed, prefix) {
				return strings.TrimPrefix(trimmed, prefix)
			}
		}
	}
	return ""
}

func buildDyadSnapshots(list []dyadRecord) []dyadSnapshot {
	out := make([]dyadSnapshot, 0, len(list))
	cutoff := 5 * time.Minute
	now := time.Now().UTC()
	for _, rec := range list {
		state := "unknown"
		ageSec := int64(0)
		if !rec.LastHeartbeat.IsZero() {
			age := now.Sub(rec.LastHeartbeat)
			ageSec = int64(age.Seconds())
			if age <= cutoff {
				state = "online"
			} else {
				state = "stale"
			}
		}
		available := rec.Available
		if !available && rec.UpdatedAt.IsZero() {
			available = true
		}
		out = append(out, dyadSnapshot{
			Dyad:                rec.Dyad,
			Department:          rec.Department,
			Role:                rec.Role,
			Team:                rec.Team,
			Assignment:          rec.Assignment,
			Tags:                rec.Tags,
			Actor:               rec.Actor,
			Critic:              rec.Critic,
			Status:              rec.Status,
			Message:             rec.Message,
			Available:           available,
			State:               state,
			LastHeartbeat:       rec.LastHeartbeat,
			LastHeartbeatAgeSec: ageSec,
			UpdatedAt:           rec.UpdatedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Department != out[j].Department {
			return out[i].Department < out[j].Department
		}
		return out[i].Dyad < out[j].Dyad
	})
	return out
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
	title := strings.TrimSpace(t.Title)
	if title == "" {
		title = fmt.Sprintf("Task #%d", t.ID)
	}

	var b strings.Builder
	b.WriteString("🧑‍💻 <b>Human Task</b>\n")
	b.WriteString("<b>Title:</b> " + html.EscapeString(title) + "\n")
	if strings.TrimSpace(t.Commands) != "" {
		b.WriteString("<b>Command:</b>\n<pre><code>" + html.EscapeString(strings.TrimSpace(t.Commands)) + "</code></pre>\n")
	}
	if strings.TrimSpace(t.URL) != "" {
		b.WriteString("<b>URL:</b> " + html.EscapeString(strings.TrimSpace(t.URL)) + "\n")
	}
	if strings.TrimSpace(t.Timeout) != "" {
		b.WriteString("<b>Timeout:</b> " + html.EscapeString(strings.TrimSpace(t.Timeout)) + "\n")
	}
	if strings.TrimSpace(t.RequestedBy) != "" {
		b.WriteString("<b>Requested By:</b> " + html.EscapeString(strings.TrimSpace(t.RequestedBy)) + "\n")
	}
	b.WriteString("<b>Status:</b> " + html.EscapeString(strings.TrimSpace(t.Status)) + "\n")
	b.WriteString("<b>Created:</b> " + formatUTCWhen(t.CreatedAt) + "\n")
	if strings.TrimSpace(t.Notes) != "" {
		b.WriteString("<b>Notes:</b>\n<pre><code>" + html.EscapeString(truncate(t.Notes, 900)) + "</code></pre>\n")
	}
	return strings.TrimSpace(b.String())
}

func formatDyadTaskMessage(t dyadTask) string {
	title := strings.TrimSpace(t.Title)
	if title == "" {
		title = fmt.Sprintf("Task #%d", t.ID)
	}
	status := strings.TrimSpace(t.Status)
	priority := strings.TrimSpace(t.Priority)
	kind := strings.TrimSpace(t.Kind)

	var b strings.Builder
	b.WriteString(statusEmoji(status) + " " + kindEmoji(kind) + " <b>Dyad Task</b>\n")
	b.WriteString("<b>Title:</b> " + html.EscapeString(title) + "\n")
	b.WriteString("<b>Status:</b> " + statusEmoji(status) + " " + html.EscapeString(status) + "\n")
	if priority != "" {
		b.WriteString("<b>Priority:</b> " + priorityEmoji(priority) + " " + html.EscapeString(priority) + "\n")
	}
	if kind != "" {
		b.WriteString("<b>Kind:</b> " + kindEmoji(kind) + " " + html.EscapeString(kind) + "\n")
	}
	if strings.TrimSpace(t.Dyad) != "" {
		b.WriteString("<b>Dyad:</b> " + html.EscapeString(strings.TrimSpace(t.Dyad)) + "\n")
	}
	if strings.TrimSpace(t.ClaimedBy) != "" {
		b.WriteString("<b>Owner:</b> " + html.EscapeString(strings.TrimSpace(t.ClaimedBy)) + "\n")
	}
	if strings.TrimSpace(t.Link) != "" {
		b.WriteString("<b>Link:</b> " + html.EscapeString(strings.TrimSpace(t.Link)) + "\n")
	}
	if strings.TrimSpace(t.Description) != "" {
		b.WriteString("<b>Details:</b>\n<pre><code>" + html.EscapeString(truncate(t.Description, 900)) + "</code></pre>\n")
	}
	if strings.TrimSpace(t.Notes) != "" {
		b.WriteString("<b>Notes:</b>\n<pre><code>" + html.EscapeString(truncate(t.Notes, 900)) + "</code></pre>\n")
	}

	when := t.UpdatedAt
	if when.IsZero() {
		when = t.CreatedAt
	}
	if !when.IsZero() {
		b.WriteString("<b>When (UTC):</b> " + formatUTCWhen(when) + "\n")
	}
	return strings.TrimSpace(b.String())
}

func shouldNotifyDyadTask(t dyadTask) bool {
	kind := strings.ToLower(strings.TrimSpace(t.Kind))
	status := strings.ToLower(strings.TrimSpace(t.Status))
	requestedBy := strings.ToLower(strings.TrimSpace(t.RequestedBy))
	priority := strings.ToLower(strings.TrimSpace(t.Priority))

	// Humans must see anything requiring attention or confirmation.
	if strings.HasPrefix(requestedBy, "human") {
		return true
	}

	// Beams are explicitly human-in-the-loop runbooks.
	if strings.HasPrefix(kind, "beam.") {
		return true
	}

	// Only notify on higher-signal task states.
	switch status {
	case "blocked", "review", "done":
		return true
	}

	// High priority tasks are worth surfacing even while in progress.
	switch priority {
	case "high", "p0", "urgent":
		return true
	}

	return false
}

func statusEmoji(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "todo", "open":
		return "📝"
	case "in_progress":
		return "🚧"
	case "review":
		return "🔎"
	case "blocked":
		return "⛔"
	case "done":
		return "✅"
	default:
		return "📌"
	}
}

func priorityEmoji(priority string) string {
	switch strings.ToLower(strings.TrimSpace(priority)) {
	case "high", "p0", "urgent":
		return "🔥"
	case "low", "p2":
		return "🟩"
	case "normal", "medium", "p1", "":
		return "🟦"
	default:
		return "🟪"
	}
}

func kindEmoji(kind string) string {
	k := strings.ToLower(strings.TrimSpace(kind))
	switch {
	case strings.HasPrefix(k, "beam."):
		return "⚡"
	case strings.Contains(k, "stripe") || strings.Contains(k, "billing") || strings.Contains(k, "payments"):
		return "💳"
	case strings.Contains(k, "github"):
		return "🐙"
	case strings.Contains(k, "mcp"):
		return "🔌"
	case strings.Contains(k, "codex"):
		return "🧠"
	case strings.HasPrefix(k, "test."):
		return "🧪"
	case strings.Contains(k, "docs"):
		return "📚"
	case strings.Contains(k, "infra"):
		return "🏗️"
	default:
		return "🧩"
	}
}

func formatUTCWhen(t time.Time) string {
	if t.IsZero() {
		return "n/a"
	}
	return t.UTC().Format("Mon 2006-01-02 15:04 UTC")
}

func truncate(s string, max int) string {
	trimmed := strings.TrimSpace(s)
	if max <= 0 || len(trimmed) <= max {
		return trimmed
	}
	return strings.TrimSpace(trimmed[:max]) + "…"
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
		Tasks        []humanTask     `json:"tasks"`
		Feedbacks    []feedback      `json:"feedbacks"`
		Access       []accessRequest `json:"access_requests"`
		Metrics      []metric        `json:"metrics"`
		DyadTasks    []dyadTask      `json:"dyad_tasks"`
		Dyads        []dyadRecord    `json:"dyads"`
		Meta         storeMeta       `json:"meta"`
		NextTask     int             `json:"next_id"`
		NextFeedback int             `json:"next_feedback_id"`
		NextAccess   int             `json:"next_access_id"`
		NextMetric   int             `json:"next_metric_id"`
		NextDyadTask int             `json:"next_dyad_task_id"`
	}
	if err := json.Unmarshal(b, &payload); err != nil {
		logger.Printf("tasks decode error: %v", err)
		return
	}
	s.tasks = payload.Tasks
	s.feedbacks = payload.Feedbacks
	s.access = payload.Access
	s.metrics = payload.Metrics
	s.dyadTasks = payload.DyadTasks
	s.dyads = payload.Dyads
	s.meta = payload.Meta
	s.nextTaskID = payload.NextTask
	s.nextFeedbackID = payload.NextFeedback
	s.nextAccessID = payload.NextAccess
	s.nextMetricID = payload.NextMetric
	s.nextDyadTaskID = payload.NextDyadTask
}

func (s *store) persistLocked() {
	if s.dataPath == "" {
		return
	}
	payload := struct {
		Tasks        []humanTask     `json:"tasks"`
		Feedbacks    []feedback      `json:"feedbacks"`
		NextTask     int             `json:"next_id"`
		NextFeedback int             `json:"next_feedback_id"`
		Access       []accessRequest `json:"access_requests,omitempty"`
		NextAccess   int             `json:"next_access_id,omitempty"`
		Metrics      []metric        `json:"metrics,omitempty"`
		NextMetric   int             `json:"next_metric_id,omitempty"`
		DyadTasks    []dyadTask      `json:"dyad_tasks,omitempty"`
		NextDyadTask int             `json:"next_dyad_task_id,omitempty"`
		Dyads        []dyadRecord    `json:"dyads,omitempty"`
		Meta         storeMeta       `json:"meta,omitempty"`
	}{Tasks: s.tasks, Feedbacks: s.feedbacks, NextTask: s.nextTaskID, NextFeedback: s.nextFeedbackID, Access: s.access, NextAccess: s.nextAccessID, Metrics: s.metrics, NextMetric: s.nextMetricID, DyadTasks: s.dyadTasks, NextDyadTask: s.nextDyadTaskID, Dyads: s.dyads, Meta: s.meta}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return
	}
	tmp := s.dataPath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err == nil {
		_ = os.Rename(tmp, s.dataPath)
	}
}
