package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

type DyadTask struct {
	ID          int       `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Kind        string    `json:"kind"`
	Status      string    `json:"status"`
	Priority    string    `json:"priority"`
	Complexity  string    `json:"complexity"`
	Dyad        string    `json:"dyad"`
	Actor       string    `json:"actor"`
	Critic      string    `json:"critic"`
	RequestedBy string    `json:"requested_by"`
	Notes       string    `json:"notes"`
	Link        string    `json:"link"`
	ClaimedBy   string    `json:"claimed_by"`
	HeartbeatAt time.Time `json:"heartbeat_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (m *Monitor) TickDyadWork(ctx context.Context, dyad string) {
	tasks, err := m.listDyadTasks(ctx)
	if err != nil {
		m.Logger.Printf("list dyad tasks error: %v", err)
		return
	}

	// Gate non-login work until the actor is authenticated.
	actorLoggedIn := false
	if m.ActorContainer != "" {
		if out, err := m.ExecInContainerCapture(ctx, m.ActorContainer, []string{"codex", "login", "status"}); err == nil {
			actorLoggedIn = strings.Contains(out, "Logged in")
		}
	}

	candidates := make([]DyadTask, 0, len(tasks))
	for _, t := range tasks {
		if t.Dyad != dyad || t.Status == "done" {
			continue
		}
		kind := strings.ToLower(strings.TrimSpace(t.Kind))
		if strings.HasPrefix(kind, "beam.") {
			continue
		}
		if !actorLoggedIn {
			continue
		}
		if isExecutableKind(kind) {
			candidates = append(candidates, t)
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		ki := kindScore(candidates[i].Kind)
		kj := kindScore(candidates[j].Kind)
		if ki != kj {
			return ki > kj
		}
		pi := priorityScore(candidates[i].Priority)
		pj := priorityScore(candidates[j].Priority)
		if pi != pj {
			return pi > pj
		}
		return candidates[i].ID < candidates[j].ID
	})

	for _, t := range candidates {
		// Only act on a task if we can claim it (avoids getting stuck on stale claimed tasks).
		if err := m.claimDyadTask(ctx, t.ID, dyad); err != nil {
			continue
		}
		m.Logger.Printf("dyad work: claim ok id=%d kind=%s", t.ID, t.Kind)
		kind := strings.ToLower(strings.TrimSpace(t.Kind))
		switch kind {
		case "test.codex_loop":
			m.DriveCodexLoopTest(ctx, dyad, t)
		case "codex.exec":
			m.DriveCodexExecTask(ctx, dyad, t)
		default:
			if strings.HasPrefix(kind, "hardening.") ||
				strings.HasPrefix(kind, "ops.") ||
				strings.HasPrefix(kind, "web.") ||
				strings.HasPrefix(kind, "infra.") {
				m.DriveCodexExecTask(ctx, dyad, t)
			}
		}
		return
	}
}

func isExecutableKind(kind string) bool {
	kind = strings.ToLower(strings.TrimSpace(kind))
	switch kind {
	case "test.codex_loop", "codex.exec":
		return true
	}
	return strings.HasPrefix(kind, "hardening.") ||
		strings.HasPrefix(kind, "ops.") ||
		strings.HasPrefix(kind, "web.") ||
		strings.HasPrefix(kind, "infra.")
}

func priorityScore(p string) int {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "high":
		return 300
	case "normal":
		return 200
	case "low":
		return 100
	default:
		return 150
	}
}

func kindScore(kind string) int {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "test.codex_loop":
		return 500
	case "codex.exec":
		return 100
	default:
		k := strings.ToLower(strings.TrimSpace(kind))
		if strings.HasPrefix(k, "hardening.") {
			return 200
		}
		if strings.HasPrefix(k, "ops.") {
			return 150
		}
		if strings.HasPrefix(k, "infra.") {
			return 120
		}
		if strings.HasPrefix(k, "web.") {
			return 110
		}
		return 0
	}
}

func (m *Monitor) listDyadTasks(ctx context.Context) ([]DyadTask, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.ManagerURL+"/dyad-tasks", nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var tasks []DyadTask
	if err := json.NewDecoder(resp.Body).Decode(&tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

func (m *Monitor) updateDyadTask(ctx context.Context, payload map[string]interface{}) error {
	b, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.ManagerURL+"/dyad-tasks/update", bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("update task non-2xx: %s", resp.Status)
	}
	return nil
}

func shellQuote(s string) string {
	// minimal quoting for paths we control; avoids importing extra packages
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}
