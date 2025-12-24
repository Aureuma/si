package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
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
		if !actorLoggedIn && kind != "beam.codex_login" {
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
		case "beam.codex_login":
			m.runBeamCodexLogin(ctx, dyad, t)
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
	case "beam.codex_login", "test.codex_loop", "codex.exec":
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
	case "beam.codex_login":
		return 1000
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

func (m *Monitor) runBeamCodexLogin(ctx context.Context, dyad string, task DyadTask) {
	actor := task.Actor
	if actor == "" {
		actor = m.ActorContainer
	}
	if actor == "" {
		m.Logger.Printf("beam.codex_login missing actor")
		return
	}

	status, _ := m.ExecInContainerCapture(ctx, actor, []string{"codex", "login", "status"})
	if strings.Contains(status, "Logged in") {
		_ = m.updateDyadTask(ctx, map[string]interface{}{
			"id":     task.ID,
			"status": "done",
			"notes":  strings.TrimSpace(task.Notes + "\n[beam.codex_login] already logged in"),
		})
		return
	}

	alreadySent := strings.Contains(task.Notes, "[beam.codex_login] sent")
	if alreadySent {
		// Human still needs to complete browser callback.
		return
	}

	if m.TelegramURL == "" || m.TelegramChatID == "" || m.SSHTarget == "" {
		_ = m.updateDyadTask(ctx, map[string]interface{}{
			"id":     task.ID,
			"status": "blocked",
			"notes":  strings.TrimSpace(task.Notes + "\n[beam.codex_login] missing TELEGRAM_NOTIFY_URL / TELEGRAM_CHAT_ID / SSH_TARGET in critic env"),
		})
		return
	}

	port := envInt("CODEX_LOGIN_PORT", 1455)
	forwardPort := envInt("CODEX_LOGIN_FORWARD_PORT", port+1)

	containerIP, err := m.containerIPv4(ctx, actor)
	if err != nil || containerIP == "" {
		m.Logger.Printf("beam.codex_login: inspect actor ip error: %v", err)
		return
	}

	outFile := fmt.Sprintf("/tmp/codex_login_%d.log", port)
	startCmd := fmt.Sprintf(
		"rm -f %q && nohup bash -lc 'codex login --port %d >%q 2>&1' >/dev/null 2>&1 & disown || true",
		outFile,
		port,
		outFile,
	)
	_ = m.NudgeContainer(ctx, actor, []string{"bash", "-lc", startCmd})

	authURL := ""
	detectedPort := 0
	for i := 0; i < 60; i++ {
		raw, _ := m.ExecInContainerCapture(ctx, actor, []string{"bash", "-lc", "cat " + shellQuote(outFile) + " 2>/dev/null || true"})
		if strings.Contains(raw, "unexpected argument '--port'") {
			// Retry without --port (some Codex CLI builds).
			outFile = "/tmp/codex_login.log"
			_ = m.NudgeContainer(ctx, actor, []string{"bash", "-lc", "rm -f " + shellQuote(outFile) + " && nohup bash -lc 'codex login >" + shellQuote(outFile) + " 2>&1' >/dev/null 2>&1 & disown || true"})
			time.Sleep(1 * time.Second)
			continue
		}
		if detectedPort == 0 {
			detectedPort = parseCodexLoginPort(raw)
		}
		authURL = firstAuthURL(raw)
		if authURL != "" && detectedPort != 0 {
			break
		}
		time.Sleep(1 * time.Second)
	}
	if authURL == "" {
		m.Logger.Printf("beam.codex_login: failed to capture auth URL from %s:%s", actor, outFile)
		return
	}
	if detectedPort != 0 && detectedPort != port {
		port = detectedPort
		// recompute forwardPort if not explicitly set
		if strings.TrimSpace(os.Getenv("CODEX_LOGIN_FORWARD_PORT")) == "" {
			forwardPort = port + 1
		}
	}

	forwardName := fmt.Sprintf("%s-codex-forward-%d", actor, port)
	if err := m.RunSocatForwarder(ctx, forwardName, actor, forwardPort, port); err != nil {
		m.Logger.Printf("beam.codex_login: socat forward error: %v", err)
		return
	}

	tunnel := fmt.Sprintf("ssh -N -L 127.0.0.1:%d:%s:%d %s", port, containerIP, forwardPort, m.SSHTarget)
	msg := strings.TrimSpace(fmt.Sprintf(
		"🔐 <b>Codex login</b>\n\n<b>🛠 Tunnel:</b>\n<pre><code>%s</code></pre>\n\n<b>🌐 URL:</b>\n<pre><code>%s</code></pre>",
		html.EscapeString(tunnel),
		html.EscapeString(authURL),
	))

	if err := m.telegramNotify(ctx, msg); err != nil {
		m.Logger.Printf("beam.codex_login: telegram notify error: %v", err)
		return
	}

	_ = m.updateDyadTask(ctx, map[string]interface{}{
		"id":     task.ID,
		"status": "blocked",
		"notes":  strings.TrimSpace(task.Notes + fmt.Sprintf("\n[beam.codex_login] sent tunnel+URL to telegram (dyad=%s); waiting for browser callback", dyad)),
	})
}

func (m *Monitor) telegramNotify(ctx context.Context, message string) error {
	if m.TelegramURL == "" {
		return errors.New("missing telegram url")
	}
	chatID, err := strconv.ParseInt(strings.TrimSpace(m.TelegramChatID), 10, 64)
	if err != nil {
		return fmt.Errorf("invalid telegram chat id: %w", err)
	}
	payload := map[string]interface{}{
		"chat_id": chatID,
		"message": message,
		"parse_mode": "HTML",
		"disable_web_page_preview": true,
	}
	b, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.TelegramURL, bytes.NewReader(b))
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
		return fmt.Errorf("telegram notify non-2xx: %s", resp.Status)
	}
	return nil
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

func (m *Monitor) containerIPv4(ctx context.Context, container string) (string, error) {
	endpoint := fmt.Sprintf("http://unix/containers/%s/json", url.PathEscape(container))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	resp, err := m.dockerClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var obj struct {
		NetworkSettings struct {
			Networks map[string]struct {
				IPAddress string `json:"IPAddress"`
			} `json:"Networks"`
		} `json:"NetworkSettings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&obj); err != nil {
		return "", err
	}
	for _, n := range obj.NetworkSettings.Networks {
		if n.IPAddress != "" {
			return n.IPAddress, nil
		}
	}
	return "", nil
}

var urlRe = regexp.MustCompile(`https://[^\s]+`)
var codexPortRe = regexp.MustCompile(`(?:localhost|127\.0\.0\.1):([0-9]+)`)

func firstAuthURL(raw string) string {
	m := urlRe.FindString(raw)
	return strings.TrimSpace(m)
}

func parseCodexLoginPort(raw string) int {
	m := codexPortRe.FindStringSubmatch(raw)
	if len(m) != 2 {
		return 0
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

func envInt(key string, def int) int {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			return i
		}
	}
	return def
}

func shellQuote(s string) string {
	// minimal quoting for paths we control; avoids importing extra packages
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}
