package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type Program struct {
	Program     string        `json:"program"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	Tasks       []ProgramTask `json:"tasks"`
}

type ProgramTask struct {
	Key        string `json:"key"`
	Title      string `json:"title"`
	Description string `json:"description"`
	Kind       string `json:"kind"`
	Priority   string `json:"priority"`
	RouteHint  string `json:"route_hint"`
}

type DyadTask struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Kind        string `json:"kind"`
	Status      string `json:"status"`
	Priority    string `json:"priority"`
	Dyad        string `json:"dyad"`
	Actor       string `json:"actor"`
	Critic      string `json:"critic"`
	RequestedBy string `json:"requested_by"`
	Notes       string `json:"notes"`
}

type reconciler struct {
	managerURL string
	http       *http.Client
	logger     *log.Logger
}

func main() {
	logger := log.New(os.Stdout, "program-manager ", log.LstdFlags|log.LUTC)
	managerURL := envOr("MANAGER_URL", "http://manager:9090")
	cfgURL := strings.TrimSpace(os.Getenv("PROGRAM_CONFIG_URL"))
	cfgFile := strings.TrimSpace(os.Getenv("PROGRAM_CONFIG_FILE"))
	if cfgURL == "" && cfgFile == "" {
		cfgFile = "/configs/programs/web_hosting.json"
	}

	interval := durationEnv("PROGRAM_RECONCILE_INTERVAL", 30*time.Second)
	r := &reconciler{
		managerURL: strings.TrimRight(managerURL, "/"),
		http:       &http.Client{Timeout: 10 * time.Second},
		logger:     logger,
	}

	ctx := context.Background()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	logger.Printf("starting (manager=%s interval=%s config_file=%s config_url=%s)", r.managerURL, interval, cfgFile, cfgURL)
	for {
		if err := r.reconcileOnce(ctx, cfgFile, cfgURL); err != nil {
			logger.Printf("reconcile error: %v", err)
		}
		<-ticker.C
	}
}

func (r *reconciler) reconcileOnce(ctx context.Context, cfgFile, cfgURL string) error {
	prog, err := loadProgram(ctx, cfgFile, cfgURL)
	if err != nil {
		return err
	}
	if strings.TrimSpace(prog.Program) == "" {
		return errors.New("missing program name")
	}

	existing, err := r.listDyadTasks(ctx)
	if err != nil {
		return err
	}
	existingKeys := map[string]DyadTask{}
	for _, t := range existing {
		if k := stateValue(t.Notes, "pm.key"); k != "" {
			existingKeys[k] = t
		}
	}

	for _, pt := range prog.Tasks {
		key := strings.TrimSpace(pt.Key)
		if key == "" {
			continue
		}
		if t, ok := existingKeys[key]; ok {
			// Keep idempotent: don't overwrite human/agent updates.
			if strings.TrimSpace(t.Kind) == "" && strings.TrimSpace(pt.Kind) != "" {
				_ = r.updateDyadTask(ctx, map[string]interface{}{"id": t.ID, "kind": pt.Kind})
			}
			// If the program-manager created the task, keep it "fully assigned" for dyads.
			if strings.TrimSpace(t.RequestedBy) == "program-manager" && strings.TrimSpace(t.Dyad) != "" && !isPoolTarget(t.Dyad) {
				wantActor := dyadActorName(t.Dyad)
				wantCritic := dyadCriticName(t.Dyad)
				payload := map[string]interface{}{"id": t.ID}
				changed := false
				if strings.TrimSpace(t.Actor) == "" && wantActor != "" {
					payload["actor"] = wantActor
					changed = true
				}
				if strings.TrimSpace(t.Critic) == "" && wantCritic != "" {
					payload["critic"] = wantCritic
					changed = true
				}
				if changed {
					_ = r.updateDyadTask(ctx, payload)
				}
			}
			continue
		}

		notes := strings.TrimSpace(strings.Join([]string{
			fmt.Sprintf("[pm.program]=%s", prog.Program),
			fmt.Sprintf("[pm.key]=%s", key),
			fmt.Sprintf("[pm.title]=%s", prog.Title),
		}, "\n"))

		dyad := strings.TrimSpace(pt.RouteHint)
		isPool := isPoolTarget(dyad)

		actor := ""
		critic := ""
		if dyad != "" && !isPool {
			actor = dyadActorName(dyad)
			critic = dyadCriticName(dyad)
		}
		create := map[string]interface{}{
			"title":        pt.Title,
			"description":  pt.Description,
			"kind":         pt.Kind,
			"priority":     normalizePriority(pt.Priority),
			"dyad":         dyad, // allow router to correct/override if empty
			"actor":        actor,
			"critic":       critic,
			"requested_by": "program-manager",
			"notes":        notes,
		}
		if err := r.createDyadTask(ctx, create); err != nil {
			r.logger.Printf("create task failed key=%s: %v", key, err)
		} else {
			r.logger.Printf("created task key=%s title=%q dyad=%q", key, pt.Title, dyad)
		}
	}

	return nil
}

func (r *reconciler) listDyadTasks(ctx context.Context) ([]DyadTask, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.managerURL+"/dyad-tasks", nil)
	if err != nil {
		return nil, err
	}
	resp, err := r.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list dyad-tasks: %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	var out []DyadTask
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *reconciler) createDyadTask(ctx context.Context, payload map[string]interface{}) error {
	b, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.managerURL+"/dyad-tasks", bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("create dyad-task: %s", resp.Status)
	}
	return nil
}

func (r *reconciler) updateDyadTask(ctx context.Context, payload map[string]interface{}) error {
	b, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.managerURL+"/dyad-tasks/update", bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("update dyad-task: %s", resp.Status)
	}
	return nil
}

func loadProgram(ctx context.Context, filePath, url string) (*Program, error) {
	if strings.TrimSpace(url) != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			b, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("config url %s: %s: %s", url, resp.Status, strings.TrimSpace(string(b)))
		}
		var p Program
		if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
			return nil, err
		}
		return &p, nil
	}

	b, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	var p Program
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func normalizePriority(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "high", "p0", "urgent":
		return "high"
	case "low", "p2":
		return "low"
	default:
		return "normal"
	}
}

func stateValue(notes, key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	for _, line := range strings.Split(notes, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "[") || !strings.Contains(line, "]=") {
			continue
		}
		end := strings.Index(line, "]=")
		if end <= 1 {
			continue
		}
		k := strings.TrimSpace(line[1:end])
		v := strings.TrimSpace(line[end+2:])
		if k == key {
			return v
		}
	}
	return ""
}

func envOr(key, def string) string {
	if v := os.Getenv(key); strings.TrimSpace(v) != "" {
		return v
	}
	return def
}

func durationEnv(key string, def time.Duration) time.Duration {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return def
}

func isPoolTarget(dyad string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(dyad)), "pool:")
}

func dyadActorName(dyad string) string {
	dyad = strings.TrimSpace(dyad)
	if dyad == "" {
		return ""
	}
	return "actor-" + dyad
}

func dyadCriticName(dyad string) string {
	dyad = strings.TrimSpace(dyad)
	if dyad == "" {
		return ""
	}
	return "critic-" + dyad
}
