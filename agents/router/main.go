package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type dyadTask struct {
	ID          int       `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Kind        string    `json:"kind"`
	Status      string    `json:"status"`
	Priority    string    `json:"priority"`
	Dyad        string    `json:"dyad"`
	Actor       string    `json:"actor"`
	Critic      string    `json:"critic"`
	RequestedBy string    `json:"requested_by"`
	Notes       string    `json:"notes"`
	Link        string    `json:"link"`
	ClaimedBy   string    `json:"claimed_by"`
	ClaimedAt   time.Time `json:"claimed_at"`
	HeartbeatAt time.Time `json:"heartbeat_at"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type rule struct {
	Match   string `json:"match"`   // substring match against title+description
	RouteTo string `json:"routeTo"` // dyad name
}

type rulesFile struct {
	DefaultDyad string `json:"defaultDyad"`
	Rules       []rule `json:"rules"`
}

func main() {
	logger := log.New(os.Stdout, "router ", log.LstdFlags|log.LUTC)
	managerURL := envOr("MANAGER_URL", "http://manager:9090")
	pollEvery := durationEnv("ROUTER_POLL_INTERVAL", 10*time.Second)
	rulesPath := envOr("ROUTER_RULES_FILE", "/configs/router_rules.json")
	rules := loadRules(logger, rulesPath)

	logger.Printf("routing via %s (poll=%s rules=%s)", managerURL, pollEvery, rulesPath)
	tick := time.NewTicker(pollEvery)
	defer tick.Stop()

	for range tick.C {
		tasks, err := listTasks(managerURL)
		if err != nil {
			logger.Printf("list tasks error: %v", err)
			continue
		}
		for _, t := range tasks {
			if t.Status != "todo" {
				continue
			}
			if strings.TrimSpace(t.Dyad) != "" {
				continue
			}
			target := routeTask(rules, t)
			if target == "" {
				continue
			}
			update := map[string]interface{}{
				"id":     t.ID,
				"dyad":   target,
				"actor":  "silexa-actor-" + target,
				"critic": "silexa-critic-" + target,
				"notes":  strings.TrimSpace(t.Notes + "\n[routed] dyad=" + target),
			}
			if err := updateTask(managerURL, update); err != nil {
				logger.Printf("route task %d -> %s error: %v", t.ID, target, err)
				continue
			}
			logger.Printf("routed task %d -> %s", t.ID, target)
		}
	}
}

func routeTask(rules rulesFile, t dyadTask) string {
	hay := strings.ToLower(t.Title + "\n" + t.Description + "\n" + t.Kind)
	for _, r := range rules.Rules {
		if r.Match == "" || r.RouteTo == "" {
			continue
		}
		if strings.Contains(hay, strings.ToLower(r.Match)) {
			return r.RouteTo
		}
	}
	if rules.DefaultDyad != "" {
		return rules.DefaultDyad
	}
	return ""
}

func listTasks(managerURL string) ([]dyadTask, error) {
	resp, err := http.Get(managerURL + "/dyad-tasks")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var tasks []dyadTask
	if err := json.NewDecoder(resp.Body).Decode(&tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

func updateTask(managerURL string, payload map[string]interface{}) error {
	b, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, managerURL+"/dyad-tasks/update", bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return &httpStatusError{Status: resp.Status}
	}
	return nil
}

type httpStatusError struct{ Status string }

func (e *httpStatusError) Error() string { return e.Status }

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func durationEnv(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return def
}

func loadRules(logger *log.Logger, path string) rulesFile {
	b, err := os.ReadFile(path)
	if err != nil {
		logger.Printf("rules file missing (%s): %v; using defaults", path, err)
		return defaultRules()
	}
	var rf rulesFile
	if err := json.Unmarshal(b, &rf); err != nil {
		logger.Printf("rules file invalid (%s): %v; using defaults", path, err)
		return defaultRules()
	}
	if rf.DefaultDyad == "" {
		rf.DefaultDyad = "infra"
	}
	if len(rf.Rules) == 0 {
		rf.Rules = defaultRules().Rules
	}
	return rf
}

func defaultRules() rulesFile {
	return rulesFile{
		DefaultDyad: "infra",
		Rules: []rule{
			{Match: "beam", RouteTo: "infra"},
			{Match: "codex", RouteTo: "infra"},
			{Match: "mcp", RouteTo: "infra"},
			{Match: "stripe", RouteTo: "infra"},
			{Match: "github", RouteTo: "infra"},
			{Match: "ui", RouteTo: "web"},
			{Match: "frontend", RouteTo: "web"},
			{Match: "landing", RouteTo: "marketing"},
			{Match: "blog", RouteTo: "marketing"},
		},
	}
}
