package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Store struct {
	mu   sync.RWMutex
	st   State
	path string
}

func NewStore(path string) (*Store, error) {
	store := &Store{path: strings.TrimSpace(path)}
	if store.path != "" {
		if err := store.load(); err != nil {
			return nil, err
		}
	}
	store.mu.Lock()
	store.ensureInitialized(time.Now().UTC())
	store.mu.Unlock()
	return store, nil
}

func (s *Store) Query(name string, out any) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now().UTC()
	switch name {
	case "dyads":
		return assign(out, buildDyadSnapshots(s.st.Dyads, now))
	case "beats":
		return assign(out, append([]Heartbeat(nil), s.st.Beats...))
	case "human-tasks":
		return assign(out, append([]HumanTask(nil), s.st.Tasks...))
	case "dyad-tasks":
		return assign(out, append([]DyadTask(nil), s.st.DyadTasks...))
	case "feedback":
		return assign(out, append([]Feedback(nil), s.st.Feedbacks...))
	case "access-requests":
		return assign(out, append([]AccessRequest(nil), s.st.Access...))
	case "metrics":
		return assign(out, append([]Metric(nil), s.st.Metrics...))
	case "dyad-digest-message-id":
		return assign(out, s.st.DyadDigestMessageID)
	case "healthz":
		return assign(out, buildHealth(&s.st, now))
	default:
		return fmt.Errorf("unknown query: %s", name)
	}
}

func (s *Store) Update(name string, out any, args ...any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	s.ensureInitialized(now)

	switch name {
	case "upsert_dyad":
		if len(args) != 1 {
			return fmt.Errorf("upsert_dyad expects 1 argument")
		}
		update, ok := args[0].(DyadUpdate)
		if !ok {
			return fmt.Errorf("upsert_dyad: expected DyadUpdate")
		}
		result := upsertDyad(&s.st, update, now)
		if err := assign(out, result); err != nil {
			return err
		}
		return s.persistLocked()
	case "heartbeat":
		if len(args) != 1 {
			return fmt.Errorf("heartbeat expects 1 argument")
		}
		hb, ok := args[0].(Heartbeat)
		if !ok {
			return fmt.Errorf("heartbeat: expected Heartbeat")
		}
		hb.When = now
		addHeartbeat(&s.st, hb, now)
		if err := assign(out, hb); err != nil {
			return err
		}
		return s.persistLocked()
	case "add_human_task":
		if len(args) != 1 {
			return fmt.Errorf("add_human_task expects 1 argument")
		}
		task, ok := args[0].(HumanTask)
		if !ok {
			return fmt.Errorf("add_human_task: expected HumanTask")
		}
		result := addHumanTask(&s.st, task, now)
		if err := assign(out, result); err != nil {
			return err
		}
		return s.persistLocked()
	case "complete_human_task":
		if len(args) != 1 {
			return fmt.Errorf("complete_human_task expects 1 argument")
		}
		id, ok := args[0].(int)
		if !ok {
			return fmt.Errorf("complete_human_task: expected int id")
		}
		result := completeHumanTask(&s.st, id, now)
		if err := assign(out, result); err != nil {
			return err
		}
		return s.persistLocked()
	case "add_dyad_task":
		if len(args) != 1 {
			return fmt.Errorf("add_dyad_task expects 1 argument")
		}
		task, ok := args[0].(DyadTask)
		if !ok {
			return fmt.Errorf("add_dyad_task: expected DyadTask")
		}
		result := addDyadTask(&s.st, task, now)
		if err := assign(out, result); err != nil {
			return err
		}
		return s.persistLocked()
	case "update_dyad_task":
		if len(args) != 1 {
			return fmt.Errorf("update_dyad_task expects 1 argument")
		}
		task, ok := args[0].(DyadTask)
		if !ok {
			return fmt.Errorf("update_dyad_task: expected DyadTask")
		}
		updated, found := updateDyadTask(&s.st, task, now)
		result := UpdateResult{Task: updated, Found: found}
		if err := assign(out, result); err != nil {
			return err
		}
		return s.persistLocked()
	case "claim_dyad_task":
		if len(args) != 3 {
			return fmt.Errorf("claim_dyad_task expects 3 arguments")
		}
		id, ok := args[0].(int)
		if !ok {
			return fmt.Errorf("claim_dyad_task: expected int id")
		}
		dyad, ok := args[1].(string)
		if !ok {
			return fmt.Errorf("claim_dyad_task: expected dyad string")
		}
		critic, ok := args[2].(string)
		if !ok {
			return fmt.Errorf("claim_dyad_task: expected critic string")
		}
		task, code, found := claimDyadTask(&s.st, id, dyad, critic, now)
		result := ClaimResult{Task: task, Code: code, Found: found}
		if err := assign(out, result); err != nil {
			return err
		}
		return s.persistLocked()
	case "add_feedback":
		if len(args) != 1 {
			return fmt.Errorf("add_feedback expects 1 argument")
		}
		fb, ok := args[0].(Feedback)
		if !ok {
			return fmt.Errorf("add_feedback: expected Feedback")
		}
		result := addFeedback(&s.st, fb, now)
		if err := assign(out, result); err != nil {
			return err
		}
		return s.persistLocked()
	case "add_access_request":
		if len(args) != 1 {
			return fmt.Errorf("add_access_request expects 1 argument")
		}
		ar, ok := args[0].(AccessRequest)
		if !ok {
			return fmt.Errorf("add_access_request: expected AccessRequest")
		}
		result := addAccessRequest(&s.st, ar, now)
		if err := assign(out, result); err != nil {
			return err
		}
		return s.persistLocked()
	case "resolve_access_request":
		if len(args) != 4 {
			return fmt.Errorf("resolve_access_request expects 4 arguments")
		}
		id, ok := args[0].(int)
		if !ok {
			return fmt.Errorf("resolve_access_request: expected int id")
		}
		status, ok := args[1].(string)
		if !ok {
			return fmt.Errorf("resolve_access_request: expected status string")
		}
		by, ok := args[2].(string)
		if !ok {
			return fmt.Errorf("resolve_access_request: expected by string")
		}
		notes, ok := args[3].(string)
		if !ok {
			return fmt.Errorf("resolve_access_request: expected notes string")
		}
		req, found := resolveAccessRequest(&s.st, id, status, by, notes, now)
		result := ResolveResult{Request: req, Found: found}
		if err := assign(out, result); err != nil {
			return err
		}
		return s.persistLocked()
	case "add_metric":
		if len(args) != 1 {
			return fmt.Errorf("add_metric expects 1 argument")
		}
		metric, ok := args[0].(Metric)
		if !ok {
			return fmt.Errorf("add_metric: expected Metric")
		}
		result := addMetric(&s.st, metric, now)
		if err := assign(out, result); err != nil {
			return err
		}
		return s.persistLocked()
	case "set_dyad_digest_message_id":
		if len(args) != 1 {
			return fmt.Errorf("set_dyad_digest_message_id expects 1 argument")
		}
		id, ok := args[0].(int)
		if !ok {
			return fmt.Errorf("set_dyad_digest_message_id: expected int id")
		}
		s.st.DyadDigestMessageID = id
		if err := assign(out, s.st.DyadDigestMessageID); err != nil {
			return err
		}
		return s.persistLocked()
	default:
		return fmt.Errorf("unknown update: %s", name)
	}
}

func (s *Store) ensureInitialized(now time.Time) {
	if s.st.StartedAt.IsZero() {
		s.st.StartedAt = now
	}
	if s.st.NextTaskID == 0 {
		s.st.NextTaskID = 1
	}
	if s.st.NextDyadTaskID == 0 {
		s.st.NextDyadTaskID = 1
	}
	if s.st.NextFeedbackID == 0 {
		s.st.NextFeedbackID = 1
	}
	if s.st.NextAccessID == 0 {
		s.st.NextAccessID = 1
	}
	if s.st.NextMetricID == 0 {
		s.st.NextMetricID = 1
	}
}

func buildHealth(st *State, now time.Time) map[string]interface{} {
	openTasks := 0
	for _, t := range st.Tasks {
		if t.Status != "done" {
			openTasks++
		}
	}
	pendingAccess := 0
	for _, a := range st.Access {
		if a.Status == "pending" {
			pendingAccess++
		}
	}
	beatsCount := len(st.Beats)
	metricsCount := len(st.Metrics)
	uptime := int64(0)
	if !st.StartedAt.IsZero() {
		uptime = int64(now.Sub(st.StartedAt).Seconds())
	}
	resp := map[string]interface{}{
		"status":         "ok",
		"tasks_open":     openTasks,
		"access_pending": pendingAccess,
		"metrics_count":  metricsCount,
		"beats_recent":   beatsCount,
		"uptime_seconds": uptime,
	}
	if beatsCount > 0 {
		last := st.Beats[beatsCount-1].When
		resp["last_beat"] = last.UTC()
	}
	return resp
}

func assign[T any](out any, value T) error {
	if out == nil {
		return nil
	}
	ptr, ok := out.(*T)
	if !ok {
		return fmt.Errorf("unexpected output type %T", out)
	}
	*ptr = value
	return nil
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if strings.TrimSpace(string(data)) == "" {
		return nil
	}
	if err := json.Unmarshal(data, &s.st); err != nil {
		return err
	}
	return nil
}

func (s *Store) persistLocked() error {
	if s.path == "" {
		return nil
	}
	dir := filepath.Dir(s.path)
	if dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(&s.st, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
