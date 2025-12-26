package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
)

type accountConfig struct {
	Name         string `json:"name"`
	Dyad         string `json:"dyad"`
	Role         string `json:"role"`
	Department   string `json:"department"`
	Actor        string `json:"actor"`
	Critic       string `json:"critic"`
	MonitorRole  string `json:"monitor_role"` // actor|critic (default critic)
	CodexHome    string `json:"codex_home"`   // optional HOME dir for codex status
	Enabled      *bool  `json:"enabled"`
	Spawn        *bool  `json:"spawn"`
}

type accountsFile struct {
	Accounts               []accountConfig `json:"accounts"`
	CooldownThresholdPct   float64         `json:"cooldown_threshold_pct"`
	TotalLimitMinutes      int             `json:"total_limit_minutes"`
	PollInterval           string          `json:"poll_interval"`
}

type monitor struct {
	logger           *log.Logger
	managerURL       string
	dockerClient     *http.Client
	spawnScript      string
	spawnEnabled     bool
	thresholdPct     float64
	totalLimitMin    int
	lastCooldown     map[string]bool
	statusMu         sync.Mutex
	statusCache      map[string]statusEntry
}

type usageSnapshot struct {
	RemainingPct     float64
	RemainingMinutes int
	UsedPct          float64
	Email            string
}

type statusEntry struct {
	Name             string    `json:"name"`
	Dyad             string    `json:"dyad"`
	Department       string    `json:"department"`
	RemainingPct     float64   `json:"remaining_pct"`
	RemainingMinutes int       `json:"remaining_minutes"`
	Cooldown         bool      `json:"cooldown"`
	Email            string    `json:"email,omitempty"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type metricPayload struct {
	Dyad       string  `json:"dyad"`
	Department string  `json:"department"`
	Name       string  `json:"name"`
	Value      float64 `json:"value"`
	Unit       string  `json:"unit"`
	RecordedBy string  `json:"recorded_by"`
}

func main() {
	logger := log.New(os.Stdout, "codex-monitor ", log.LstdFlags|log.LUTC)

	managerURL := strings.TrimRight(envOr("MANAGER_URL", "http://manager:9090"), "/")
	cfgPath := envOr("CODEX_ACCOUNTS_FILE", "/configs/codex_accounts.json")
	pollInterval := durationEnv("CODEX_STATUS_POLL_INTERVAL", 2*time.Minute)
	thresholdPct, thresholdSet := floatEnv("CODEX_COOLDOWN_THRESHOLD_PCT")
	if !thresholdSet {
		thresholdPct = 10
	}
	totalLimit, totalSet := intEnv("CODEX_PLAN_LIMIT_MINUTES")
	if !totalSet {
		totalLimit = 300
	}
	spawnEnabled := boolEnv("CODEX_SPAWN_DYADS", true)
	spawnScript := envOr("CODEX_SPAWN_SCRIPT", "/workspace/silexa/bin/spawn-dyad.sh")
	addr := envOr("CODEX_MONITOR_ADDR", ":8086")

	dockerClient := newDockerClient()
	m := &monitor{
		logger:        logger,
		managerURL:    managerURL,
		dockerClient:  dockerClient,
		spawnScript:   spawnScript,
		spawnEnabled:  spawnEnabled,
		thresholdPct:  thresholdPct,
		totalLimitMin: totalLimit,
		lastCooldown:  map[string]bool{},
		statusCache:   map[string]statusEntry{},
	}

	go m.serveStatus(addr)

	logger.Printf("starting (manager=%s accounts=%s interval=%s)", managerURL, cfgPath, pollInterval)
	for {
		cfg := loadAccounts(cfgPath, logger)
		m.applyConfigOverrides(cfg, &pollInterval)
		if len(cfg.Accounts) == 0 {
			logger.Printf("no codex accounts configured")
		}
		m.pollOnce(cfg)
		time.Sleep(pollInterval)
	}
}

func (m *monitor) applyConfigOverrides(cfg accountsFile, pollInterval *time.Duration) {
	if cfg.CooldownThresholdPct > 0 {
		m.thresholdPct = cfg.CooldownThresholdPct
	}
	if cfg.TotalLimitMinutes > 0 {
		m.totalLimitMin = cfg.TotalLimitMinutes
	}
	if cfg.PollInterval != "" {
		if d, err := time.ParseDuration(cfg.PollInterval); err == nil && d > 0 {
			*pollInterval = d
		}
	}
}

func (m *monitor) serveStatus(addr string) {
	if strings.TrimSpace(addr) == "" {
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		m.handleStatus(w, r, false)
	})
	mux.HandleFunc("/status.json", func(w http.ResponseWriter, r *http.Request) {
		m.handleStatus(w, r, true)
	})
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	m.logger.Printf("status server listening on %s", addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		m.logger.Printf("status server error: %v", err)
	}
}

func (m *monitor) handleStatus(w http.ResponseWriter, r *http.Request, forceJSON bool) {
	entries := m.statusEntries()
	if forceJSON || strings.EqualFold(r.URL.Query().Get("format"), "json") {
		payload := map[string]interface{}{
			"updated_at": time.Now().UTC(),
			"accounts":   entries,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	_, _ = io.WriteString(w, formatStatusSummary(entries))
}

func (m *monitor) statusEntries() []statusEntry {
	m.statusMu.Lock()
	defer m.statusMu.Unlock()
	entries := make([]statusEntry, 0, len(m.statusCache))
	for _, entry := range m.statusCache {
		entries = append(entries, entry)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].RemainingPct != entries[j].RemainingPct {
			return entries[i].RemainingPct < entries[j].RemainingPct
		}
		return entries[i].Dyad < entries[j].Dyad
	})
	return entries
}

func formatStatusSummary(entries []statusEntry) string {
	if len(entries) == 0 {
		return "Codex usage: no data"
	}
	var b strings.Builder
	b.WriteString("Codex usage:\n")
	for _, entry := range entries {
		line := "- " + entry.Dyad
		if entry.Name != "" && entry.Name != entry.Dyad {
			line += " (" + entry.Name + ")"
		}
		if entry.Email != "" {
			line += " <" + entry.Email + ">"
		}
		if entry.RemainingPct >= 0 {
			line += fmt.Sprintf(": %.1f%% remaining", entry.RemainingPct)
			if entry.RemainingMinutes > 0 {
				line += fmt.Sprintf(" (%dm)", entry.RemainingMinutes)
			}
			if entry.Cooldown {
				line += " [cooldown]"
			}
		} else {
			line += ": n/a"
		}
		b.WriteString(line + "\n")
	}
	return strings.TrimSpace(b.String())
}

func (m *monitor) pollOnce(cfg accountsFile) {
	for _, acct := range cfg.Accounts {
		if !accountEnabled(acct) {
			continue
		}
		if strings.TrimSpace(acct.Dyad) == "" {
			m.logger.Printf("skip account %q: missing dyad", acct.Name)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		m.ensureDyad(ctx, acct)
		m.pollAccount(ctx, acct)
		cancel()
	}
}

func (m *monitor) ensureDyad(ctx context.Context, acct accountConfig) {
	if !m.spawnEnabled || !accountSpawnEnabled(acct, m.spawnEnabled) {
		return
	}
	actor := actorContainer(acct)
	critic := criticContainer(acct)
	if actor == "" || critic == "" {
		return
	}

	actorExists, actorRunning, err := m.inspectContainer(ctx, actor)
	if err != nil {
		m.logger.Printf("inspect actor %s error: %v", actor, err)
		return
	}
	criticExists, criticRunning, err := m.inspectContainer(ctx, critic)
	if err != nil {
		m.logger.Printf("inspect critic %s error: %v", critic, err)
		return
	}

	if !actorExists || !criticExists {
		if err := m.spawnDyad(acct); err != nil {
			m.logger.Printf("spawn dyad %s error: %v", acct.Dyad, err)
			return
		}
		return
	}
	if !actorRunning {
		if err := m.startContainer(ctx, actor); err != nil {
			m.logger.Printf("start actor %s error: %v", actor, err)
		}
	}
	if !criticRunning {
		if err := m.startContainer(ctx, critic); err != nil {
			m.logger.Printf("start critic %s error: %v", critic, err)
		}
	}
}

func (m *monitor) spawnDyad(acct accountConfig) error {
	if strings.TrimSpace(m.spawnScript) == "" {
		return errors.New("spawn script not configured")
	}
	if _, err := os.Stat(m.spawnScript); err != nil {
		return fmt.Errorf("spawn script missing: %w", err)
	}
	role := strings.TrimSpace(acct.Role)
	if role == "" {
		role = strings.TrimSpace(acct.Department)
	}
	if role == "" {
		role = acct.Dyad
	}
	dept := strings.TrimSpace(acct.Department)
	if dept == "" {
		dept = role
	}
	cmd := exec.Command(m.spawnScript, acct.Dyad, role, dept)
	cmd.Env = append(os.Environ(), "CODEX_PER_DYAD=1")
	cmd.Dir = filepath.Dir(m.spawnScript)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("spawn dyad failed: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	m.logger.Printf("spawned dyad %s: %s", acct.Dyad, strings.TrimSpace(string(out)))
	return nil
}

func (m *monitor) pollAccount(ctx context.Context, acct accountConfig) {
	status, err := m.fetchCodexStatus(ctx, acct)
	if err != nil {
		m.logger.Printf("codex status %s error: %v", acct.Dyad, err)
		m.updateStatusCache(acct, usageSnapshot{RemainingPct: -1}, false)
		return
	}
	usage := parseUsage(status, m.totalLimitMin)
	cooldown := usage.RemainingPct >= 0 && usage.RemainingPct <= m.thresholdPct
	m.updateStatusCache(acct, usage, cooldown)
	if usage.RemainingPct < 0 {
		m.logger.Printf("codex status parse failed for %s: %s", acct.Dyad, truncateLines(status, 6, 800))
		return
	}
	dpt := strings.TrimSpace(acct.Department)
	if dpt == "" {
		dpt = strings.TrimSpace(acct.Role)
	}

	m.postMetric(ctx, metricPayload{
		Dyad:       acct.Dyad,
		Department: dpt,
		Name:       "codex.remaining_pct",
		Value:      usage.RemainingPct,
		Unit:       "percent",
		RecordedBy: "codex-monitor",
	})
	if usage.RemainingMinutes > 0 {
		m.postMetric(ctx, metricPayload{
			Dyad:       acct.Dyad,
			Department: dpt,
			Name:       "codex.remaining_minutes",
			Value:      float64(usage.RemainingMinutes),
			Unit:       "minutes",
			RecordedBy: "codex-monitor",
		})
	}
	m.postMetric(ctx, metricPayload{
		Dyad:       acct.Dyad,
		Department: dpt,
		Name:       "codex.cooldown",
		Value:      boolToFloat(cooldown),
		Unit:       "bool",
		RecordedBy: "codex-monitor",
	})

	key := acct.Dyad
	prev, ok := m.lastCooldown[key]
	if !ok || prev != cooldown {
		m.lastCooldown[key] = cooldown
		m.postCooldownFeedback(ctx, acct, usage, cooldown, status)
	}
}

func (m *monitor) postCooldownFeedback(ctx context.Context, acct accountConfig, usage usageSnapshot, cooldown bool, raw string) {
	msg := fmt.Sprintf("codex usage %s for dyad=%s remaining=%.1f%%", boolWord(!cooldown, "healthy", "low"), acct.Dyad, usage.RemainingPct)
	severity := "info"
	if cooldown {
		severity = "warn"
	}
	payload := map[string]interface{}{
		"source":   "codex-monitor",
		"severity": severity,
		"message":  msg,
		"context":  "codex-status\n" + truncateLines(raw, 12, 1200),
	}
	b, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.managerURL+"/feedback", bytes.NewReader(b))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
}

func (m *monitor) postMetric(ctx context.Context, metric metricPayload) {
	b, _ := json.Marshal(metric)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.managerURL+"/metrics", bytes.NewReader(b))
	if err != nil {
		m.logger.Printf("metric build error: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		m.logger.Printf("metric send error: %v", err)
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
}

func (m *monitor) fetchCodexStatus(ctx context.Context, acct accountConfig) (string, error) {
	if home, ok := m.resolveCodexHome(acct); ok {
		return m.execCodexStatusLocal(ctx, home)
	}
	container := monitorContainer(acct)
	if container == "" {
		return "", errors.New("missing monitor container")
	}
	return m.execCodexStatus(ctx, container)
}

func (m *monitor) resolveCodexHome(acct accountConfig) (string, bool) {
	if home, ok := resolveHomeFromPath(acct.CodexHome, acct.Dyad); ok {
		return home, true
	}
	if acct.Dyad == "" {
		return "", false
	}
	base := "/data/codex/" + strings.TrimSpace(acct.Dyad)
	if home, ok := resolveHomeFromPath(filepath.Join(base, "critic"), acct.Dyad); ok {
		return home, true
	}
	if home, ok := resolveHomeFromPath(filepath.Join(base, "actor"), acct.Dyad); ok {
		return home, true
	}
	return "", false
}

func resolveHomeFromPath(path string, dyad string) (string, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", false
	}
	if stat, err := os.Stat(path); err != nil || !stat.IsDir() {
		return "", false
	}
	if stat, err := os.Stat(filepath.Join(path, ".codex")); err == nil && stat.IsDir() && isCodexDir(filepath.Join(path, ".codex")) {
		if isWritableDir(filepath.Join(path, ".codex")) {
			return path, true
		}
		return tempHomeWithCodex(filepath.Join(path, ".codex"), dyad)
	}
	if isCodexDir(path) {
		if isWritableDir(path) {
			return tempHomeWithSymlink(path, dyad)
		}
		return tempHomeWithCodex(path, dyad)
	}
	return "", false
}

func safeName(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "unknown"
	}
	out := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' {
			return r
		}
		if r >= 'A' && r <= 'Z' {
			return r
		}
		if r >= '0' && r <= '9' {
			return r
		}
		if r == '-' || r == '_' {
			return r
		}
		return '-'
	}, v)
	return out
}

func (m *monitor) execCodexStatusLocal(ctx context.Context, home string) (string, error) {
	if _, err := exec.LookPath("codex"); err != nil {
		return "", err
	}
	statusCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	cmd := exec.CommandContext(statusCtx, "codex")
	cmd.Env = append(os.Environ(), "HOME="+home, "TERM=xterm-256color", "CODEX_HOME="+filepath.Join(home, ".codex"))
	cmd.Dir = home
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return "", err
	}
	defer ptmx.Close()
	_ = pty.Setsize(ptmx, &pty.Winsize{Rows: 40, Cols: 120})
	_, _ = ptmx.Write([]byte("\x1b[1;1R"))
	readyCh := make(chan struct{})
	var readyOnce sync.Once
	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		timeout := time.NewTimer(2 * time.Second)
		defer timeout.Stop()
		for {
			select {
			case <-ticker.C:
				_, _ = ptmx.Write([]byte("\x1b[1;1R"))
			case <-timeout.C:
				return
			case <-statusCtx.Done():
				return
			}
		}
	}()
	statusSent := make(chan struct{})
	var statusOnce sync.Once
	go func() {
		select {
		case <-readyCh:
			_, _ = ptmx.Write([]byte("/status\r"))
			statusOnce.Do(func() { close(statusSent) })
		case <-statusCtx.Done():
		}
	}()

	var bufMu sync.Mutex
	var buf bytes.Buffer

	outCh := make(chan []byte, 1)
	errCh := make(chan error, 1)
	activityCh := make(chan struct{}, 1)
	go func() {
		select {
		case <-activityCh:
		case <-statusCtx.Done():
			return
		}
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()
		select {
		case <-timer.C:
			readyOnce.Do(func() { close(readyCh) })
		case <-statusCtx.Done():
		}
	}()
	go func() {
		var tail []byte
		var promptTail []byte
		var promptTailClean []byte
		var promptTailCleanLower []byte
		tmp := make([]byte, 2048)
		cursorRequests := [][]byte{
			[]byte("\x1b[6n"),
			[]byte("\x1b[?6n"),
		}
		type promptHandler struct {
			needle   []byte
			reply    string
			once     bool
			cooldown time.Duration
			lastSent time.Time
		}
		promptHandlers := []promptHandler{
			{needle: []byte("allow codex to work in this folder"), reply: "2\r", once: true},
			{needle: []byte("allow codex to work in this folder without asking for approval"), reply: "2\r", once: true},
			{needle: []byte("ask me to approve edits and commands"), reply: "2\r", once: true},
			{needle: []byte("require approval of edits and commands"), reply: "2\r", once: true},
			{needle: []byte("press enter to continue"), reply: "\r", cooldown: 500 * time.Millisecond},
			{needle: []byte("press enter to confirm"), reply: "\r", cooldown: 500 * time.Millisecond},
			{needle: []byte("try new model"), reply: "\r", cooldown: 500 * time.Millisecond},
		}
		readyNeedles := [][]byte{
			[]byte("openai codex"),
			[]byte("to get started"),
			[]byte("/status"),
		}
		maxPromptLen := 0
		for _, handler := range promptHandlers {
			if len(handler.needle) > maxPromptLen {
				maxPromptLen = len(handler.needle)
			}
		}
		for {
			n, readErr := ptmx.Read(tmp)
			if n > 0 {
				chunk := tmp[:n]
				cleanChunk := stripANSI(string(chunk))
				cleanChunkLower := strings.ToLower(cleanChunk)
				bufMu.Lock()
				buf.Write(chunk)
				bufMu.Unlock()
				select {
				case activityCh <- struct{}{}:
				default:
				}
				for _, seq := range cursorRequests {
					if containsSequence(tail, chunk, seq) {
						_, _ = ptmx.Write([]byte("\x1b[1;1R"))
					}
				}
				for _, needle := range readyNeedles {
					if containsSequence(promptTailCleanLower, []byte(cleanChunkLower), needle) {
						readyOnce.Do(func() { close(readyCh) })
					}
				}
				for i := range promptHandlers {
					if containsSequence(promptTailCleanLower, []byte(cleanChunkLower), promptHandlers[i].needle) {
						now := time.Now()
						if promptHandlers[i].once && !promptHandlers[i].lastSent.IsZero() {
							continue
						}
						if promptHandlers[i].cooldown > 0 && now.Sub(promptHandlers[i].lastSent) < promptHandlers[i].cooldown {
							continue
						}
						promptHandlers[i].lastSent = now
						_, _ = ptmx.Write([]byte(promptHandlers[i].reply))
					}
				}
				maxSeq := 0
				for _, seq := range cursorRequests {
					if len(seq) > maxSeq {
						maxSeq = len(seq)
					}
				}
				tail = append(tail[:0], tailBytes(tail, chunk, maxSeq)...)
				promptTail = append(promptTail[:0], tailBytes(promptTail, chunk, maxPromptLen)...)
				promptTailClean = append(promptTailClean[:0], tailBytes(promptTailClean, []byte(cleanChunk), maxPromptLen)...)
				promptTailCleanLower = append(promptTailCleanLower[:0], tailBytes(promptTailCleanLower, []byte(cleanChunkLower), maxPromptLen)...)
			}
			if readErr != nil {
				if !errors.Is(readErr, io.EOF) {
					errCh <- readErr
					return
				}
				break
			}
		}
		bufMu.Lock()
		outCh <- append([]byte(nil), buf.Bytes()...)
		bufMu.Unlock()
	}()

	go func() {
		select {
		case <-statusSent:
		case <-statusCtx.Done():
			return
		}
		idleTimer := time.NewTimer(1200 * time.Millisecond)
		hardTimer := time.NewTimer(8 * time.Second)
		defer idleTimer.Stop()
		defer hardTimer.Stop()
		for {
			select {
			case <-activityCh:
				if !idleTimer.Stop() {
					select {
					case <-idleTimer.C:
					default:
					}
				}
				idleTimer.Reset(1200 * time.Millisecond)
			case <-idleTimer.C:
				_, _ = ptmx.Write([]byte("/exit\r"))
				return
			case <-hardTimer.C:
				_, _ = ptmx.Write([]byte("/exit\r"))
				return
			case <-statusCtx.Done():
				return
			}
		}
	}()

	select {
	case <-statusCtx.Done():
		_ = cmd.Process.Kill()
		bufMu.Lock()
		out := strings.TrimSpace(buf.String())
		bufMu.Unlock()
		if out != "" {
			return out, nil
		}
		return "", statusCtx.Err()
	case readErr := <-errCh:
		_ = cmd.Wait()
		return "", readErr
	case out := <-outCh:
		waitErr := cmd.Wait()
		if waitErr != nil && len(out) == 0 {
			return "", waitErr
		}
		return string(out), nil
	}
}

func (m *monitor) updateStatusCache(acct accountConfig, usage usageSnapshot, cooldown bool) {
	entry := statusEntry{
		Name:             strings.TrimSpace(acct.Name),
		Dyad:             strings.TrimSpace(acct.Dyad),
		Department:       strings.TrimSpace(acct.Department),
		RemainingPct:     usage.RemainingPct,
		RemainingMinutes: usage.RemainingMinutes,
		Cooldown:         cooldown,
		Email:            strings.TrimSpace(usage.Email),
		UpdatedAt:        time.Now().UTC(),
	}
	if entry.Name == "" {
		entry.Name = entry.Dyad
	}
	m.statusMu.Lock()
	m.statusCache[entry.Dyad] = entry
	m.statusMu.Unlock()
}

func (m *monitor) execCodexStatus(ctx context.Context, container string) (string, error) {
	commands := [][]string{{"codex", "/status"}, {"codex", "status"}, {"codex", "usage"}}
	var lastErr error
	for _, cmd := range commands {
		out, err := m.execInContainer(ctx, container, cmd)
		if err == nil && strings.TrimSpace(out) != "" {
			return out, nil
		}
		if err != nil {
			lastErr = err
		}
	}
	if lastErr == nil {
		lastErr = errors.New("empty status output")
	}
	return "", lastErr
}

func (m *monitor) execInContainer(ctx context.Context, container string, cmd []string) (string, error) {
	containerID, err := m.resolveContainerID(ctx, container)
	if err != nil {
		return "", err
	}
	createPayload := map[string]interface{}{
		"AttachStdin":  true,
		"AttachStdout": true,
		"AttachStderr": true,
		"Cmd":          cmd,
		"Tty":          true,
	}
	buf, _ := json.Marshal(createPayload)
	createURL := fmt.Sprintf("http://unix/containers/%s/exec", url.PathEscape(containerID))
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
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("docker exec create %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	body, _ := io.ReadAll(resp.Body)
	var out map[string]interface{}
	_ = json.Unmarshal(body, &out)
	id, _ := out["Id"].(string)
	if id == "" {
		return "", errors.New("no exec id from docker")
	}

	startPayload := map[string]interface{}{
		"Detach": false,
		"Tty":    true,
	}
	buf2, _ := json.Marshal(startPayload)
	startURL := fmt.Sprintf("http://unix/exec/%s/start", id)
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
	outBytes, _ := io.ReadAll(io.LimitReader(resp2.Body, 64*1024))
	return string(dockerDemux(outBytes)), nil
}

func (m *monitor) inspectContainer(ctx context.Context, name string) (bool, bool, error) {
	id, running, err := m.inspectContainerByName(ctx, name)
	if err != nil {
		return false, false, err
	}
	if id != "" {
		return true, running, nil
	}
	for _, svc := range serviceCandidates(name) {
		exists, svcRunning, err := m.inspectService(ctx, svc)
		if err != nil {
			return false, false, err
		}
		if exists {
			return true, svcRunning, nil
		}
	}
	return false, false, nil
}

func (m *monitor) startContainer(ctx context.Context, name string) error {
	id, _, err := m.inspectContainerByName(ctx, name)
	if err != nil {
		return err
	}
	if id != "" {
		url := fmt.Sprintf("http://unix/containers/%s/start", url.PathEscape(id))
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
		if err != nil {
			return err
		}
		resp, err := m.dockerClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("start container %s: %s", name, strings.TrimSpace(string(body)))
		}
		return nil
	}
	for _, svc := range serviceCandidates(name) {
		exists, err := m.serviceExists(ctx, svc)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		return m.forceServiceUpdate(ctx, svc)
	}
	return fmt.Errorf("container or service %s not found", name)
}

func (m *monitor) inspectContainerByName(ctx context.Context, name string) (string, bool, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false, nil
	}
	endpoint := fmt.Sprintf("http://unix/containers/%s/json", url.PathEscape(name))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", false, err
	}
	resp, err := m.dockerClient.Do(req)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", false, nil
	}
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return "", false, fmt.Errorf("inspect container %s: %s", name, strings.TrimSpace(string(body)))
	}
	var payload struct {
		ID    string `json:"Id"`
		State struct {
			Running bool `json:"Running"`
		} `json:"State"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", false, err
	}
	return payload.ID, payload.State.Running, nil
}

func (m *monitor) serviceExists(ctx context.Context, name string) (bool, error) {
	endpoint := fmt.Sprintf("http://unix/services/%s", url.PathEscape(name))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, err
	}
	resp, err := m.dockerClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return false, fmt.Errorf("inspect service %s: %s", name, strings.TrimSpace(string(body)))
	}
	return true, nil
}

func (m *monitor) inspectService(ctx context.Context, name string) (bool, bool, error) {
	exists, err := m.serviceExists(ctx, name)
	if err != nil || !exists {
		return exists, false, err
	}
	id, err := m.serviceTaskContainerID(ctx, name)
	if err != nil {
		return true, false, err
	}
	return true, id != "", nil
}

func (m *monitor) resolveContainerID(ctx context.Context, name string) (string, error) {
	id, _, err := m.inspectContainerByName(ctx, name)
	if err != nil {
		return "", err
	}
	if id != "" {
		return id, nil
	}
	for _, svc := range serviceCandidates(name) {
		id, err := m.serviceTaskContainerID(ctx, svc)
		if err != nil {
			return "", err
		}
		if id != "" {
			return id, nil
		}
	}
	return "", fmt.Errorf("container or service %s not found", name)
}

func (m *monitor) serviceTaskContainerID(ctx context.Context, service string) (string, error) {
	filters := fmt.Sprintf(`{\"service\":{\"%s\":true},\"desired-state\":{\"running\":true}}`, service)
	endpoint := fmt.Sprintf("http://unix/tasks?filters=%s", url.QueryEscape(filters))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	resp, err := m.dockerClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("list service tasks %s: %s", service, strings.TrimSpace(string(body)))
	}
	var tasks []struct {
		Status struct {
			State           string `json:"State"`
			ContainerStatus struct {
				ContainerID string `json:"ContainerID"`
			} `json:"ContainerStatus"`
		} `json:"Status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tasks); err != nil {
		return "", err
	}
	for _, task := range tasks {
		if strings.TrimSpace(task.Status.State) != "running" {
			continue
		}
		if id := strings.TrimSpace(task.Status.ContainerStatus.ContainerID); id != "" {
			return id, nil
		}
	}
	return "", nil
}

func (m *monitor) forceServiceUpdate(ctx context.Context, service string) error {
	cmd := exec.CommandContext(ctx, "docker", "service", "update", "--force", service)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("service update %s: %w (%s)", service, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func newDockerClient() *http.Client {
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", "/var/run/docker.sock")
	}
	transport := &http.Transport{DialContext: dial}
	return &http.Client{Transport: transport, Timeout: 10 * time.Second}
}

func parseUsage(raw string, totalLimitMinutes int) usageSnapshot {
	raw = stripANSI(raw)
	remainingPct := -1.0
	usedPct := -1.0
	remainingMinutes := 0
	email := parseEmail(raw)
	percentRe := regexp.MustCompile(`(\d+(?:\.\d+)?)\s*%`)
	wordsRe := regexp.MustCompile(`(?i)(remaining|left|available)`) // remaining context
	usedRe := regexp.MustCompile(`(?i)(used|consumed|spent|utilized)`) // used context

	lines := strings.Split(raw, "\n")
	fallbackPercents := []float64{}
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		percentMatches := percentRe.FindAllStringSubmatch(trim, -1)
		for _, match := range percentMatches {
			val, _ := strconv.ParseFloat(match[1], 64)
			if wordsRe.MatchString(trim) {
				remainingPct = val
			} else if usedRe.MatchString(trim) {
				usedPct = val
			} else {
				fallbackPercents = append(fallbackPercents, val)
			}
		}
		if remainingMinutes == 0 && wordsRe.MatchString(trim) {
			if mins := parseMinutes(trim); mins > 0 {
				remainingMinutes = mins
			}
		}
	}
	if remainingPct < 0 && usedPct >= 0 {
		remainingPct = 100 - usedPct
	}
	if remainingPct < 0 && len(fallbackPercents) == 1 {
		remainingPct = fallbackPercents[0]
	}
	if remainingMinutes == 0 {
		for _, line := range lines {
			if mins := parseMinutes(line); mins > 0 {
				remainingMinutes = mins
				break
			}
		}
	}
	if remainingPct < 0 && remainingMinutes > 0 && totalLimitMinutes > 0 {
		remainingPct = float64(remainingMinutes) / float64(totalLimitMinutes) * 100
	}
	return usageSnapshot{
		RemainingPct:     remainingPct,
		RemainingMinutes: remainingMinutes,
		UsedPct:          usedPct,
		Email:            email,
	}
}

func stripANSI(s string) string {
	if s == "" {
		return s
	}
	// Strip common ANSI escape sequences.
	re := regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	return re.ReplaceAllString(s, "")
}

func isCodexDir(path string) bool {
	if stat, err := os.Stat(filepath.Join(path, "auth.json")); err == nil && !stat.IsDir() {
		return true
	}
	if stat, err := os.Stat(filepath.Join(path, "config.toml")); err == nil && !stat.IsDir() {
		return true
	}
	return false
}

func isWritableDir(path string) bool {
	test := filepath.Join(path, ".codex-monitor-write")
	f, err := os.OpenFile(test, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return false
	}
	_ = f.Close()
	_ = os.Remove(test)
	return true
}

func tempHomeWithCodex(src string, dyad string) (string, bool) {
	home := filepath.Join(os.TempDir(), "codex-home-"+safeName(dyad))
	if err := os.MkdirAll(home, 0o700); err != nil {
		return "", false
	}
	dst := filepath.Join(home, ".codex")
	if err := refreshDir(src, dst); err != nil {
		return "", false
	}
	return home, true
}

func tempHomeWithSymlink(src string, dyad string) (string, bool) {
	home := filepath.Join(os.TempDir(), "codex-home-"+safeName(dyad))
	if err := os.MkdirAll(home, 0o700); err != nil {
		return "", false
	}
	target := filepath.Join(home, ".codex")
	if fi, err := os.Lstat(target); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 || fi.IsDir() {
			_ = os.RemoveAll(target)
		}
	}
	if err := os.Symlink(src, target); err != nil && !os.IsExist(err) {
		return "", false
	}
	return home, true
}

func refreshDir(src string, dst string) error {
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return err
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == src {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		destPath := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		mode := info.Mode()
		switch {
		case mode&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(target, destPath)
		case mode.IsDir():
			return os.MkdirAll(destPath, mode.Perm())
		default:
			if err := os.MkdirAll(filepath.Dir(destPath), 0o700); err != nil {
				return err
			}
			return copyFile(path, destPath, mode.Perm())
		}
	})
}

func copyFile(src string, dst string, perm fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	return err
}

func containsSequence(tail []byte, chunk []byte, seq []byte) bool {
	if len(seq) == 0 {
		return false
	}
	if bytes.Contains(chunk, seq) {
		return true
	}
	if len(tail) == 0 {
		return false
	}
	combined := append(tail, chunk...)
	return bytes.Contains(combined, seq)
}

func tailBytes(prev []byte, chunk []byte, size int) []byte {
	combined := append(prev, chunk...)
	if len(combined) <= size {
		return combined
	}
	return combined[len(combined)-size:]
}

func parseMinutes(line string) int {
	lower := strings.ToLower(line)
	hoursRe := regexp.MustCompile(`(\d+)\s*h`)
	minsRe := regexp.MustCompile(`(\d+)\s*m`)
	wordsHours := regexp.MustCompile(`(\d+)\s*hours?`)
	wordsMins := regexp.MustCompile(`(\d+)\s*(mins?|minutes?)`)
	h := 0
	m := 0
	if match := hoursRe.FindStringSubmatch(lower); len(match) == 2 {
		h, _ = strconv.Atoi(match[1])
	}
	if match := minsRe.FindStringSubmatch(lower); len(match) == 2 {
		m, _ = strconv.Atoi(match[1])
	}
	if h == 0 {
		if match := wordsHours.FindStringSubmatch(lower); len(match) == 2 {
			h, _ = strconv.Atoi(match[1])
		}
	}
	if m == 0 {
		if match := wordsMins.FindStringSubmatch(lower); len(match) == 3 {
			m, _ = strconv.Atoi(match[1])
		}
	}
	if h == 0 && m == 0 {
		return 0
	}
	return h*60 + m
}

func parseEmail(raw string) string {
	if raw == "" {
		return ""
	}
	emailRe := regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	return emailRe.FindString(raw)
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

func dockerDemux(in []byte) []byte {
	if len(in) < 8 {
		return in
	}
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

func loadAccounts(path string, logger *log.Logger) accountsFile {
	b, err := os.ReadFile(path)
	if err != nil {
		logger.Printf("accounts file read error (%s): %v", path, err)
		return accountsFile{}
	}
	var cfg accountsFile
	if err := json.Unmarshal(b, &cfg); err != nil {
		logger.Printf("accounts file parse error (%s): %v", path, err)
		return accountsFile{}
	}
	return cfg
}

func accountEnabled(acct accountConfig) bool {
	if acct.Enabled == nil {
		return true
	}
	return *acct.Enabled
}

func accountSpawnEnabled(acct accountConfig, def bool) bool {
	if acct.Spawn == nil {
		return def
	}
	return *acct.Spawn
}

func monitorContainer(acct accountConfig) string {
	role := strings.ToLower(strings.TrimSpace(acct.MonitorRole))
	if role == "" || role == "critic" {
		if c := criticContainer(acct); c != "" {
			return c
		}
		return actorContainer(acct)
	}
	if role == "actor" {
		if a := actorContainer(acct); a != "" {
			return a
		}
		return criticContainer(acct)
	}
	return criticContainer(acct)
}

func actorContainer(acct accountConfig) string {
	if strings.TrimSpace(acct.Actor) != "" {
		return strings.TrimSpace(acct.Actor)
	}
	if strings.TrimSpace(acct.Dyad) == "" {
		return ""
	}
	return "actor-" + strings.TrimSpace(acct.Dyad)
}

func criticContainer(acct accountConfig) string {
	if strings.TrimSpace(acct.Critic) != "" {
		return strings.TrimSpace(acct.Critic)
	}
	if strings.TrimSpace(acct.Dyad) == "" {
		return ""
	}
	return "critic-" + strings.TrimSpace(acct.Dyad)
}

func stackName() string {
	if v := strings.TrimSpace(os.Getenv("SILEXA_STACK")); v != "" {
		return v
	}
	return "silexa"
}

func normalizeTarget(name string) string {
	name = strings.TrimSpace(name)
	if strings.HasPrefix(name, "silexa-actor-") {
		return "actor-" + strings.TrimPrefix(name, "silexa-actor-")
	}
	if strings.HasPrefix(name, "silexa-critic-") {
		return "critic-" + strings.TrimPrefix(name, "silexa-critic-")
	}
	return name
}

func serviceCandidates(name string) []string {
	name = normalizeTarget(name)
	if name == "" {
		return nil
	}
	stack := stackName()
	prefixed := stack + "_" + name
	if strings.HasPrefix(name, stack+"_") {
		return []string{name}
	}
	return []string{name, prefixed}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func boolEnv(key string, def bool) bool {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		switch strings.ToLower(v) {
		case "1", "true", "yes", "y":
			return true
		case "0", "false", "no", "n":
			return false
		}
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

func floatEnv(key string) (float64, bool) {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

func intEnv(key string) (int, bool) {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i, true
		}
	}
	return 0, false
}

func boolToFloat(v bool) float64 {
	if v {
		return 1
	}
	return 0
}

func boolWord(v bool, t string, f string) string {
	if v {
		return t
	}
	return f
}
