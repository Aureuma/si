package internal

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type Monitor struct {
	ActorContainer string
	ManagerURL     string
	TelegramURL    string
	TelegramChatID string
	SSHTarget      string
	DyadName       string
	Role           string
	Department     string
	Logger         *log.Logger
	lastTimestamp  time.Time
	lastActorLogAt time.Time
	httpClient     *http.Client
	dockerClient   *http.Client
}

func NewMonitor(actor, manager, dyad, role, department string, logger *log.Logger) (*Monitor, error) {
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", "/var/run/docker.sock")
	}
	transport := &http.Transport{
		DialContext: dial,
	}
	return &Monitor{
		ActorContainer: actor,
		ManagerURL:     manager,
		DyadName:       dyad,
		Role:           role,
		Department:     department,
		Logger:         logger,
		lastTimestamp:  time.Now().Add(-30 * time.Second),
		lastActorLogAt: time.Now().Add(-30 * time.Second),
		httpClient:     &http.Client{Transport: transport, Timeout: 10 * time.Second},
		dockerClient:   &http.Client{Transport: transport, Timeout: 10 * time.Second},
	}, nil
}

// Poll actor logs and mirror them to stdout for visibility and potential future parsing.
func (m *Monitor) StreamOnce(ctx context.Context) error {
	text, last, err := m.fetchActorLogs(ctx, m.lastTimestamp, 200, true)
	if err != nil {
		return err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	m.Logger.Printf("[%s logs]\n%s", m.ActorContainer, text)
	if !last.IsZero() {
		// Docker's `since` filter is second-resolution; advance by >=1s to avoid re-reading.
		m.lastTimestamp = last.Truncate(time.Second).Add(1 * time.Second)
	} else {
		m.lastTimestamp = time.Now().UTC()
	}
	m.lastActorLogAt = time.Now().UTC()
	return nil
}

func (m *Monitor) Heartbeat(ctx context.Context) error {
	body, _ := json.Marshal(map[string]string{
		"dyad":       m.DyadName,
		"role":       m.Role,
		"department": m.Department,
		"actor":      m.ActorContainer,
		"critic":     criticID(),
		"status":     "ok",
		"message":    "heartbeat",
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.ManagerURL+"/heartbeat", bytes.NewReader(body))
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
	return nil
}

// ReportDyad posts a status summary to Manager feedback and bumps the dyad task status to in_progress if provided.
func (m *Monitor) ReportDyad(ctx context.Context, dyad, taskID string) error {
	// Fetch beats to capture last timestamps.
	resp, err := http.Get(m.ManagerURL + "/beats")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var hb []map[string]interface{}
	_ = json.Unmarshal(body, &hb)
	var actorBeat, criticID string
	for _, b := range hb {
		if b["actor"] == m.ActorContainer {
			if s, ok := b["when"].(string); ok {
				actorBeat = s
			}
			if c, ok := b["critic"].(string); ok {
				criticID = c
			}
		}
	}

	taskNum := 0
	if taskID != "" {
		if v, err := strconv.Atoi(taskID); err == nil {
			taskNum = v
		}
	}
	if taskNum == 0 {
		taskNum = m.pickDyadTaskID(ctx, dyad)
	}
	if taskNum > 0 {
		_ = m.claimDyadTask(ctx, taskNum, dyad)
	}
	codexStatus, _ := m.ExecInActorCapture(ctx, []string{"codex", "login", "status"})
	codexStatus = strings.TrimSpace(codexStatus)
	if codexStatus == "" {
		codexStatus = "unknown"
	}
	localCodex := strings.TrimSpace(m.LocalCodexStatus(ctx))
	if localCodex == "" {
		localCodex = "unknown"
	}
	taskStr := ""
	if taskNum > 0 {
		taskStr = strconv.Itoa(taskNum)
	}
	actorTail, _, _ := m.fetchActorLogs(ctx, time.Unix(0, 0), 16, false)
	actorTail = truncateLines(strings.TrimSpace(actorTail), 16, 1600)
	actorLogAt := ""
	if !m.lastActorLogAt.IsZero() {
		actorLogAt = m.lastActorLogAt.UTC().Format(time.RFC3339)
	}

	msg := fmt.Sprintf(
		"dyad=%s actor=%s critic=%s task=%s actorBeat=%s actorLogAt=%s codexActor=%s codexCritic=%s",
		dyad, m.ActorContainer, criticID, taskStr, actorBeat, actorLogAt, codexStatus, localCodex,
	)
	feedback := map[string]interface{}{
		"source":   "critic",
		"severity": "info",
		"message":  msg,
		"context":  "dyad-status\n" + actorTail,
	}
	buf, _ := json.Marshal(feedback)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.ManagerURL+"/feedback", bytes.NewReader(buf))
	if err == nil {
		req.Header.Set("Content-Type", "application/json")
		if resp2, err2 := http.DefaultClient.Do(req); err2 == nil {
			resp2.Body.Close()
		} else {
			m.Logger.Printf("feedback send error: %v", err2)
		}
	}

	if taskNum > 0 {
		merged := m.mergeDyadTaskNotes(ctx, taskNum, msg)
		update := map[string]interface{}{"id": taskNum, "notes": merged}
		buf2, _ := json.Marshal(update)
		req2, err := http.NewRequestWithContext(ctx, http.MethodPost, m.ManagerURL+"/dyad-tasks/update", bytes.NewReader(buf2))
		if err == nil {
			req2.Header.Set("Content-Type", "application/json")
			if resp3, err3 := http.DefaultClient.Do(req2); err3 == nil {
				resp3.Body.Close()
			} else {
				m.Logger.Printf("dyad-task update error: %v", err3)
			}
		}
	}

	if !m.lastActorLogAt.IsZero() && time.Since(m.lastActorLogAt) > 5*time.Minute {
		// Actors can be legitimately quiet (e.g., waiting for the next Codex turn). Avoid
		// auto-blocking tasks; just emit a warning in the feedback stream.
		warn := map[string]interface{}{
			"source":   "critic",
			"severity": "warn",
			"message":  fmt.Sprintf("dyad=%s actor=%s appears idle (no logs >5m)", dyad, m.ActorContainer),
			"context":  "stall-detect",
		}
		bufW, _ := json.Marshal(warn)
		reqW, _ := http.NewRequestWithContext(ctx, http.MethodPost, m.ManagerURL+"/feedback", bytes.NewReader(bufW))
		reqW.Header.Set("Content-Type", "application/json")
		_, _ = http.DefaultClient.Do(reqW)
	}
	return nil
}

func (m *Monitor) mergeDyadTaskNotes(ctx context.Context, id int, statusLine string) string {
	existing, ok := m.fetchDyadTaskNotes(ctx, id)
	if !ok {
		return statusLine
	}
	lines := []string{}
	// Always place latest status first.
	lines = append(lines, statusLine)
	for _, ln := range strings.Split(existing, "\n") {
		trim := strings.TrimSpace(ln)
		if trim == "" {
			continue
		}
		// Drop any older status line(s).
		if strings.HasPrefix(trim, "dyad=") {
			continue
		}
		// Keep all other notes, including state lines like "[key]=value".
		lines = append(lines, ln)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func (m *Monitor) fetchDyadTaskNotes(ctx context.Context, id int) (string, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.ManagerURL+"/dyad-tasks", nil)
	if err != nil {
		return "", false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", false
	}
	var tasks []map[string]interface{}
	if err := json.Unmarshal(body, &tasks); err != nil {
		return "", false
	}
	for _, t := range tasks {
		idf, ok := t["id"].(float64)
		if !ok || int(idf) != id {
			continue
		}
		if notes, ok := t["notes"].(string); ok {
			return notes, true
		}
		return "", true
	}
	return "", false
}

func (m *Monitor) LocalCodexStatus(ctx context.Context) string {
	out, err := exec.CommandContext(ctx, "codex", "login", "status").CombinedOutput()
	if err != nil {
		if len(out) > 0 {
			return string(out)
		}
		return err.Error()
	}
	return string(out)
}

func (m *Monitor) pickDyadTaskID(ctx context.Context, dyad string) int {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.ManagerURL+"/dyad-tasks", nil)
	if err != nil {
		return 0
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0
	}
	var tasks []map[string]interface{}
	if err := json.Unmarshal(body, &tasks); err != nil {
		return 0
	}
	self := criticID()
	bestID := 0
	bestScore := -999999
	for _, t := range tasks {
		if t["dyad"] != dyad {
			continue
		}
		status, _ := t["status"].(string)
		if status == "done" {
			continue
		}
		claimedBy, _ := t["claimed_by"].(string)
		if claimedBy != "" && claimedBy != self {
			// Another critic is actively working it.
			continue
		}
		idf, ok := t["id"].(float64)
		if !ok {
			continue
		}
		id := int(idf)
		score := 0
		switch status {
		case "in_progress":
			score = 400
		case "todo":
			score = 300
		case "blocked":
			score = 200
		case "review":
			score = 100
		default:
			score = 50
		}
		if claimedBy == self {
			score += 1000
		}
		if score > bestScore || (score == bestScore && (bestID == 0 || id < bestID)) {
			bestScore = score
			bestID = id
		}
	}
	return bestID
}

func (m *Monitor) claimDyadTask(ctx context.Context, taskID int, dyad string) error {
	payload := map[string]interface{}{"id": taskID, "dyad": dyad, "critic": criticID()}
	b, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.ManagerURL+"/dyad-tasks/claim", bytes.NewReader(b))
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
		return fmt.Errorf("claim task: %s", resp.Status)
	}
	return nil
}

func (m *Monitor) ExecInActorCapture(ctx context.Context, cmd []string) (string, error) {
	return m.ExecInContainerCapture(ctx, m.ActorContainer, cmd)
}

func (m *Monitor) ExecInContainerCapture(ctx context.Context, container string, cmd []string) (string, error) {
	createPayload := map[string]interface{}{
		"AttachStdout": true,
		"AttachStderr": true,
		"Cmd":          cmd,
		"Tty":          false,
	}
	buf, _ := json.Marshal(createPayload)
	createURL := fmt.Sprintf("http://unix/containers/%s/exec", url.PathEscape(container))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, createURL, bytes.NewReader(buf))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.dockerClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var out map[string]interface{}
	_ = json.Unmarshal(body, &out)
	id, _ := out["Id"].(string)
	if id == "" {
		return "", fmt.Errorf("no exec id from docker")
	}

	startPayload := map[string]interface{}{
		"Detach": false,
		"Tty":    false,
	}
	buf2, _ := json.Marshal(startPayload)
	startURL := fmt.Sprintf("http://unix/exec/%s/start", url.PathEscape(id))
	req2, err := http.NewRequestWithContext(ctx, http.MethodPost, startURL, bytes.NewReader(buf2))
	if err != nil {
		return "", err
	}
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := m.dockerClient.Do(req2)
	if err != nil {
		return "", err
	}
	defer resp2.Body.Close()
	outBytes, _ := io.ReadAll(io.LimitReader(resp2.Body, 4096))
	return string(dockerDemux(outBytes)), nil
}

// NudgeActor runs a lightweight command inside the actor to ensure exec path is healthy.
func (m *Monitor) NudgeActor(ctx context.Context, cmd []string) error {
	return m.NudgeContainer(ctx, m.ActorContainer, cmd)
}

func (m *Monitor) NudgeContainer(ctx context.Context, container string, cmd []string) error {
	createPayload := map[string]interface{}{
		"AttachStdout": false,
		"AttachStderr": false,
		"Cmd":          cmd,
		"Tty":          false,
	}
	buf, _ := json.Marshal(createPayload)
	createURL := fmt.Sprintf("http://unix/containers/%s/exec", url.PathEscape(container))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, createURL, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.dockerClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var out map[string]interface{}
	_ = json.Unmarshal(body, &out)
	id, _ := out["Id"].(string)
	if id == "" {
		return fmt.Errorf("no exec id from docker")
	}
	startPayload := map[string]interface{}{
		"Detach": true,
		"Tty":    false,
	}
	buf2, _ := json.Marshal(startPayload)
	startURL := fmt.Sprintf("http://unix/exec/%s/start", url.PathEscape(id))
	req2, err := http.NewRequestWithContext(ctx, http.MethodPost, startURL, bytes.NewReader(buf2))
	if err != nil {
		return err
	}
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := m.dockerClient.Do(req2)
	if err != nil {
		return err
	}
	defer resp2.Body.Close()
	io.Copy(io.Discard, resp2.Body)
	return nil
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return h
}

func criticID() string {
	if v := strings.TrimSpace(os.Getenv("CRITIC_ID")); v != "" {
		return v
	}
	return hostname()
}

func (m *Monitor) EnsureImage(ctx context.Context, image string) error {
	// Pull image (no-op if already present, depending on daemon cache).
	endpoint := fmt.Sprintf("http://unix/images/create?fromImage=%s", url.QueryEscape(image))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := m.dockerClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("pull image %s: %s", image, resp.Status)
	}
	return nil
}

func (m *Monitor) RemoveContainerIfExists(ctx context.Context, name string) error {
	endpoint := fmt.Sprintf("http://unix/containers/%s?force=1", url.PathEscape(name))
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := m.dockerClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	// 204/200 ok, 404 ok
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("remove container %s: %s", name, resp.Status)
	}
	return nil
}

func (m *Monitor) RunSocatForwarder(ctx context.Context, name, networkContainer string, listenPort, targetPort int) error {
	if listenPort <= 0 || targetPort <= 0 {
		return fmt.Errorf("invalid forward ports: listen=%d target=%d", listenPort, targetPort)
	}
	image := "alpine/socat"
	if err := m.EnsureImage(ctx, image); err != nil {
		return err
	}
	_ = m.RemoveContainerIfExists(ctx, name)

	createPayload := map[string]interface{}{
		"Image": image,
		"Cmd": []string{
			fmt.Sprintf("tcp-listen:%d,reuseaddr,fork", listenPort),
			fmt.Sprintf("tcp:127.0.0.1:%d", targetPort),
		},
		"HostConfig": map[string]interface{}{
			"NetworkMode": "container:" + networkContainer,
			"RestartPolicy": map[string]interface{}{
				"Name": "unless-stopped",
			},
		},
	}
	buf, _ := json.Marshal(createPayload)
	createURL := fmt.Sprintf("http://unix/containers/create?name=%s", url.QueryEscape(name))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, createURL, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.dockerClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("create forwarder %s: %s: %s", name, resp.Status, string(body))
	}
	var out map[string]interface{}
	_ = json.Unmarshal(body, &out)
	id, _ := out["Id"].(string)
	if id == "" {
		return fmt.Errorf("create forwarder %s: missing id", name)
	}

	startURL := fmt.Sprintf("http://unix/containers/%s/start", url.PathEscape(id))
	req2, err := http.NewRequestWithContext(ctx, http.MethodPost, startURL, nil)
	if err != nil {
		return err
	}
	resp2, err := m.dockerClient.Do(req2)
	if err != nil {
		return err
	}
	defer resp2.Body.Close()
	io.Copy(io.Discard, resp2.Body)
	if resp2.StatusCode >= 300 {
		return fmt.Errorf("start forwarder %s: %s", name, resp2.Status)
	}
	return nil
}

func (m *Monitor) fetchActorLogs(ctx context.Context, since time.Time, tail int, timestamps bool) (string, time.Time, error) {
	endpoint := fmt.Sprintf(
		"http://unix/containers/%s/logs?stdout=1&stderr=1&since=%d&tail=%d&timestamps=%d",
		url.PathEscape(m.ActorContainer),
		since.Unix(),
		tail,
		boolToInt(timestamps),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", time.Time{}, err
	}
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return "", time.Time{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return "", time.Time{}, err
	}
	text := string(dockerDemux(data))
	var last time.Time
	if timestamps {
		last = lastDockerLogTimestamp(text)
	}
	return text, last, nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func lastDockerLogTimestamp(text string) time.Time {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if ts, err := time.Parse(time.RFC3339Nano, fields[0]); err == nil {
			return ts.UTC()
		}
	}
	return time.Time{}
}

// dockerDemux strips the Docker log multiplexing header (when container TTY=false).
// If the byte stream isn't in multiplexed format, it returns the input unchanged.
func dockerDemux(in []byte) []byte {
	if len(in) < 8 {
		return in
	}
	// Heuristic: docker multiplex header has 1-byte stream type then 3x 0x00 bytes.
	if in[1] != 0 || in[2] != 0 || in[3] != 0 {
		return in
	}

	var out bytes.Buffer
	for len(in) >= 8 {
		frameLen := int(binary.BigEndian.Uint32(in[4:8]))
		in = in[8:]
		if frameLen <= 0 || frameLen > len(in) {
			break
		}
		out.Write(in[:frameLen])
		in = in[frameLen:]
	}
	if out.Len() == 0 {
		return in
	}
	return out.Bytes()
}

func truncateLines(text string, maxLines int, maxBytes int) string {
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	out := strings.Join(lines, "\n")
	if len(out) > maxBytes {
		out = out[len(out)-maxBytes:]
	}
	return out
}
