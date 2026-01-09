package beam

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.temporal.io/sdk/client"

	"silexa/agents/manager/internal/state"
	shared "silexa/agents/shared/docker"
)

type Activities struct {
	docker        *dockerClient
	temporal      client.Client
	telegramURL   string
	telegramChat  string
}

type ActivityConfig struct {
	Temporal        client.Client
	TelegramURL     string
	TelegramChatID  string
}

func NewActivities(cfg ActivityConfig) (*Activities, error) {
	docker, err := newDockerClient()
	if err != nil {
		return nil, err
	}
	telegramURL := strings.TrimSpace(cfg.TelegramURL)
	if telegramURL == "" {
		telegramURL = strings.TrimSpace(os.Getenv("TELEGRAM_NOTIFY_URL"))
	}
	telegramChat := strings.TrimSpace(cfg.TelegramChatID)
	if telegramChat == "" {
		telegramChat = strings.TrimSpace(os.Getenv("TELEGRAM_CHAT_ID"))
	}
	return &Activities{
		docker:        docker,
		temporal:      cfg.Temporal,
		telegramURL:   telegramURL,
		telegramChat:  telegramChat,
	}, nil
}

func (a *Activities) FetchDyadTask(ctx context.Context, id int) (state.DyadTask, error) {
	if a.temporal == nil {
		return state.DyadTask{}, errors.New("temporal client unavailable")
	}
	if id <= 0 {
		return state.DyadTask{}, errors.New("task id required")
	}
	resp, err := a.temporal.QueryWorkflow(ctx, state.WorkflowID, "", "dyad-tasks")
	if err != nil {
		return state.DyadTask{}, err
	}
	var tasks []state.DyadTask
	if err := resp.Get(&tasks); err != nil {
		return state.DyadTask{}, err
	}
	for _, task := range tasks {
		if task.ID == id {
			return task, nil
		}
	}
	return state.DyadTask{}, fmt.Errorf("dyad task %d not found", id)
}

func (a *Activities) CheckCodexLogin(ctx context.Context, req CodexLoginCheck) (CodexLoginStatus, error) {
	if a.docker == nil {
		return CodexLoginStatus{}, errors.New("docker client unavailable")
	}
	dyad := strings.TrimSpace(req.Dyad)
	if dyad == "" {
		return CodexLoginStatus{}, errors.New("dyad required")
	}
	actor := normalizeContainerName(req.Actor)
	if actor == "" {
		actor = "actor"
	}
	containerID, err := a.docker.resolveDyadContainer(ctx, dyad, actor)
	if err != nil {
		return CodexLoginStatus{}, err
	}
	out, err := a.docker.execCapture(ctx, containerID, []string{"codex", "login", "status"})
	if err != nil {
		return CodexLoginStatus{}, err
	}
	return CodexLoginStatus{
		LoggedIn: strings.Contains(out, "Logged in"),
		Raw:      strings.TrimSpace(out),
	}, nil
}

func (a *Activities) StartCodexLogin(ctx context.Context, req CodexLoginRequest) (CodexLoginStart, error) {
	if a.docker == nil {
		return CodexLoginStart{}, errors.New("docker client unavailable")
	}
	dyad := strings.TrimSpace(req.Dyad)
	if dyad == "" {
		return CodexLoginStart{}, errors.New("dyad required")
	}
	actor := normalizeContainerName(req.Actor)
	if actor == "" {
		actor = "actor"
	}
	containerID, err := a.docker.resolveDyadContainer(ctx, dyad, actor)
	if err != nil {
		return CodexLoginStart{}, err
	}

	port := req.Port
	if port <= 0 {
		port = envInt("CODEX_LOGIN_PORT", 1455)
	}
	forwardPort := req.ForwardPort
	explicitForward := strings.TrimSpace(os.Getenv("CODEX_LOGIN_FORWARD_PORT")) != ""
	if forwardPort <= 0 {
		forwardPort = envIntAllowZero("CODEX_LOGIN_FORWARD_PORT", port+1)
	}

	outFile := fmt.Sprintf("/tmp/codex_login_%d.log", port)
	startCmd := fmt.Sprintf(
		"rm -f %q && nohup bash -lc 'codex login --port %d >%q 2>&1' >/dev/null 2>&1 & disown || true",
		outFile,
		port,
		outFile,
	)
	_, _ = a.docker.execCapture(ctx, containerID, []string{"bash", "-lc", startCmd})

	authURL := ""
	detectedPort := 0
	for i := 0; i < 60; i++ {
		raw, _ := a.docker.execCapture(ctx, containerID, []string{"bash", "-lc", "cat " + shellQuote(outFile) + " 2>/dev/null || true"})
		if strings.Contains(raw, "unexpected argument '--port'") {
			outFile = "/tmp/codex_login.log"
			startCmd = fmt.Sprintf(
				"rm -f %q && nohup bash -lc 'codex login >%q 2>&1' >/dev/null 2>&1 & disown || true",
				outFile,
				outFile,
			)
			_, _ = a.docker.execCapture(ctx, containerID, []string{"bash", "-lc", startCmd})
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
		return CodexLoginStart{}, fmt.Errorf("failed to capture auth URL for dyad %s", dyad)
	}
	if detectedPort != 0 && detectedPort != port {
		port = detectedPort
		if !explicitForward {
			forwardPort = port + 1
		}
	}
	hostPort, err := a.docker.hostPortFor(ctx, containerID, forwardPort)
	if err != nil {
		return CodexLoginStart{}, err
	}
	return CodexLoginStart{
		AuthURL:       strings.TrimSpace(authURL),
		Port:          port,
		ForwardPort:   forwardPort,
		HostPort:      hostPort,
		Container:     shared.DyadContainerName(dyad, actor),
	}, nil
}

func (a *Activities) StartSocatForwarder(ctx context.Context, req SocatForwarderRequest) error {
	if a.docker == nil {
		return errors.New("docker client unavailable")
	}
	dyad := strings.TrimSpace(req.Dyad)
	if dyad == "" {
		return errors.New("dyad required")
	}
	if req.ListenPort <= 0 || req.TargetPort <= 0 {
		return fmt.Errorf("invalid forward ports: listen=%d target=%d", req.ListenPort, req.TargetPort)
	}
	actor := normalizeContainerName(req.Actor)
	if actor == "" {
		actor = "actor"
	}
	containerID, err := a.docker.resolveDyadContainer(ctx, dyad, actor)
	if err != nil {
		return err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = fmt.Sprintf("codex-forward-%d", req.ListenPort)
	}
	logFile := fmt.Sprintf("/tmp/%s.log", name)
	pidFile := fmt.Sprintf("/tmp/%s.pid", name)
	cmd := fmt.Sprintf(
		"rm -f %q %q && nohup socat tcp-listen:%d,reuseaddr,fork tcp:127.0.0.1:%d >%q 2>&1 & echo $! >%q",
		logFile,
		pidFile,
		req.ListenPort,
		req.TargetPort,
		logFile,
		pidFile,
	)
	_, err = a.docker.execCapture(ctx, containerID, []string{"bash", "-lc", cmd})
	return err
}

func (a *Activities) StopSocatForwarder(ctx context.Context, req SocatForwarderStop) error {
	if a.docker == nil {
		return errors.New("docker client unavailable")
	}
	dyad := strings.TrimSpace(req.Dyad)
	if dyad == "" {
		return errors.New("dyad required")
	}
	actor := normalizeContainerName(req.Actor)
	if actor == "" {
		actor = "actor"
	}
	containerID, err := a.docker.resolveDyadContainer(ctx, dyad, actor)
	if err != nil {
		return err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil
	}
	logFile := fmt.Sprintf("/tmp/%s.log", name)
	pidFile := fmt.Sprintf("/tmp/%s.pid", name)
	cmd := fmt.Sprintf(
		"if [ -f %q ]; then kill $(cat %q) >/dev/null 2>&1 || true; rm -f %q; fi; rm -f %q",
		pidFile,
		pidFile,
		pidFile,
		logFile,
	)
	_, err = a.docker.execCapture(ctx, containerID, []string{"bash", "-lc", cmd})
	return err
}

func (a *Activities) SendTelegram(ctx context.Context, msg TelegramMessage) error {
	if a.telegramURL == "" {
		return errors.New("missing telegram url")
	}
	chatID, err := strconv.ParseInt(strings.TrimSpace(a.telegramChat), 10, 64)
	if err != nil {
		return fmt.Errorf("invalid telegram chat id: %w", err)
	}
	payload := map[string]interface{}{
		"chat_id":                  chatID,
		"message":                  msg.Message,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	}
	b, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.telegramURL, bytes.NewReader(b))
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

func (a *Activities) ResetCodexState(ctx context.Context, req CodexResetRequest) (CodexResetResult, error) {
	if a.docker == nil {
		return CodexResetResult{}, errors.New("docker client unavailable")
	}
	dyad := strings.TrimSpace(req.Dyad)
	if dyad == "" {
		return CodexResetResult{}, errors.New("dyad required")
	}
	targets := normalizeResetTargets(req.Targets)
	paths := sanitizeResetPaths(req.Paths)
	if len(paths) == 0 {
		paths = defaultCodexResetPaths()
	}
	if len(targets) == 0 {
		return CodexResetResult{}, errors.New("no valid reset targets")
	}

	script := buildCodexResetScript(paths)
	for _, target := range targets {
		containerID, err := a.docker.resolveDyadContainer(ctx, dyad, target)
		if err != nil {
			return CodexResetResult{}, err
		}
		if _, err := a.docker.execCapture(ctx, containerID, []string{"bash", "-lc", script}); err != nil {
			return CodexResetResult{}, fmt.Errorf("reset %s: %w", target, err)
		}
	}

	return CodexResetResult{Targets: targets, Paths: paths}, nil
}

func (a *Activities) HasOpenDyadTask(ctx context.Context, check DyadTaskCheck) (bool, error) {
	if a.temporal == nil {
		return false, errors.New("temporal client unavailable")
	}
	dyad := strings.TrimSpace(check.Dyad)
	kind := strings.ToLower(strings.TrimSpace(check.Kind))
	if dyad == "" || kind == "" {
		return false, errors.New("dyad and kind required")
	}
	resp, err := a.temporal.QueryWorkflow(ctx, state.WorkflowID, "", "dyad-tasks")
	if err != nil {
		return false, err
	}
	var tasks []state.DyadTask
	if err := resp.Get(&tasks); err != nil {
		return false, err
	}
	for _, task := range tasks {
		if strings.TrimSpace(task.Dyad) != dyad {
			continue
		}
		if strings.ToLower(strings.TrimSpace(task.Kind)) != kind {
			continue
		}
		if strings.ToLower(strings.TrimSpace(task.Status)) != "done" {
			return true, nil
		}
	}
	return false, nil
}

func (a *Activities) CreateDyadTask(ctx context.Context, task state.DyadTask) (state.DyadTask, error) {
	if a.temporal == nil {
		return state.DyadTask{}, errors.New("temporal client unavailable")
	}
	task.Dyad = strings.TrimSpace(task.Dyad)
	if task.Dyad == "" {
		return state.DyadTask{}, errors.New("dyad required")
	}
	options := client.UpdateWorkflowOptions{
		WorkflowID:   state.WorkflowID,
		UpdateName:   "add_dyad_task",
		Args:         []interface{}{task},
		WaitForStage: client.WorkflowUpdateStageCompleted,
	}
	handle, err := a.temporal.UpdateWorkflow(ctx, options)
	if err == nil {
		var out state.DyadTask
		if err := handle.Get(ctx, &out); err == nil {
			return out, nil
		} else if !isUnknownUpdate(err) {
			return state.DyadTask{}, err
		}
	} else if !isUnknownUpdate(err) {
		return state.DyadTask{}, err
	}
	if err := a.temporal.SignalWorkflow(ctx, state.WorkflowID, "", "add_dyad_task", task); err != nil {
		return state.DyadTask{}, err
	}
	return task, nil
}

func buildTelegramMessage(hostPort string, url string) string {
	hostPort = strings.TrimSpace(hostPort)
	lines := []string{"🔐 <b>Codex login</b>"}
	if hostPort != "" {
		lines = append(lines, "", "<b>🔌 Host port:</b> <code>"+html.EscapeString(hostPort)+"</code>")
		if target := sshTarget(); target != "" {
			cmd := fmt.Sprintf("ssh -N -L 127.0.0.1:%s:127.0.0.1:%s %s", hostPort, hostPort, target)
			lines = append(lines, "", "<b>🛠 SSH tunnel:</b>\n<pre><code>"+html.EscapeString(cmd)+"</code></pre>")
		}
	}
	lines = append(lines, "", "<b>🌐 URL:</b>\n<pre><code>"+html.EscapeString(url)+"</code></pre>")
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

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

func envIntAllowZero(key string, def int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	if n < 0 {
		return 0
	}
	return n
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

func sshTarget() string {
	if v := strings.TrimSpace(os.Getenv("SSH_TARGET")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("SSH_TARGET_FILE")); v != "" {
		if target := readSSHTargetFile(v); target != "" {
			return target
		}
	}
	if target := readSSHTargetFile("/configs/ssh_target"); target != "" {
		return target
	}
	return readSSHTargetFile("./configs/ssh_target")
}

func readSSHTargetFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "SSH_TARGET=") {
			return strings.TrimSpace(strings.TrimPrefix(line, "SSH_TARGET="))
		}
		return line
	}
	return ""
}

func normalizeResetTargets(targets []string) []string {
	seen := map[string]bool{}
	for _, target := range targets {
		name := normalizeContainerName(target)
		if name == "actor" || name == "critic" {
			seen[name] = true
		}
	}
	if len(seen) == 0 {
		seen["actor"] = true
		seen["critic"] = true
	}
	out := make([]string, 0, len(seen))
	if seen["actor"] {
		out = append(out, "actor")
	}
	if seen["critic"] {
		out = append(out, "critic")
	}
	return out
}

func sanitizeResetPaths(paths []string) []string {
	seen := map[string]bool{}
	for _, raw := range paths {
		p := strings.TrimSpace(raw)
		if p == "" {
			continue
		}
		if !strings.HasPrefix(p, "/root/") {
			continue
		}
		if strings.Contains(p, "..") {
			continue
		}
		seen[p] = true
	}
	out := make([]string, 0, len(seen))
	for _, p := range defaultCodexResetPaths() {
		if seen[p] {
			out = append(out, p)
			delete(seen, p)
		}
	}
	for p := range seen {
		out = append(out, p)
	}
	return out
}

func buildCodexResetScript(paths []string) string {
	quoted := make([]string, 0, len(paths))
	for _, path := range paths {
		quoted = append(quoted, shellQuote(path))
	}
	list := strings.Join(quoted, " ")
	return fmt.Sprintf(`set -euo pipefail
for p in %s; do
  if [ "$p" = "/root/.codex" ] && [ -d "$p" ]; then
    find "$p" -mindepth 1 -maxdepth 1 -exec rm -rf {} + >/dev/null 2>&1 || true
    continue
  fi
  if [ -e "$p" ]; then
    rm -rf "$p" >/dev/null 2>&1 || true
  fi
done
if [ -x /usr/local/bin/silexa-codex-init ]; then
  /usr/local/bin/silexa-codex-init --quiet >/proc/1/fd/1 2>/proc/1/fd/2 || true
fi
`, list)
}

var urlRe = regexp.MustCompile(`https://[^\s]+`)
var codexPortRe = regexp.MustCompile(`(?:localhost|127\.0\.0\.1):([0-9]+)`)

func isUnknownUpdate(err error) bool {
	return err != nil && strings.Contains(err.Error(), "unknown update")
}
