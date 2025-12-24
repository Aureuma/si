package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type accountConfig struct {
	Name         string `json:"name"`
	Dyad         string `json:"dyad"`
	Role         string `json:"role"`
	Department   string `json:"department"`
	Actor        string `json:"actor"`
	Critic       string `json:"critic"`
	MonitorRole  string `json:"monitor_role"` // actor|critic (default critic)
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
}

type usageSnapshot struct {
	RemainingPct     float64
	RemainingMinutes int
	UsedPct          float64
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
	}

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
	container := monitorContainer(acct)
	if container == "" {
		m.logger.Printf("skip account %q: missing monitor container", acct.Name)
		return
	}

	status, err := m.execCodexStatus(ctx, container)
	if err != nil {
		m.logger.Printf("codex status %s error: %v", container, err)
		return
	}
	usage := parseUsage(status, m.totalLimitMin)
	if usage.RemainingPct < 0 {
		m.logger.Printf("codex status parse failed for %s", container)
		return
	}

	cooldown := usage.RemainingPct <= m.thresholdPct
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
	createPayload := map[string]interface{}{
		"AttachStdout": true,
		"AttachStderr": true,
		"Cmd":          cmd,
		"Tty":          false,
	}
	buf, _ := json.Marshal(createPayload)
	createURL := fmt.Sprintf("http://unix/containers/%s/exec", container)
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
		"Tty":    false,
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
	url := fmt.Sprintf("http://unix/containers/%s/json", name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, false, err
	}
	resp, err := m.dockerClient.Do(req)
	if err != nil {
		return false, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return false, false, nil
	}
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return false, false, fmt.Errorf("inspect container %s: %s", name, strings.TrimSpace(string(body)))
	}
	var payload struct {
		State struct {
			Running bool `json:"Running"`
		} `json:"State"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return true, false, err
	}
	return true, payload.State.Running, nil
}

func (m *monitor) startContainer(ctx context.Context, name string) error {
	url := fmt.Sprintf("http://unix/containers/%s/start", name)
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

func newDockerClient() *http.Client {
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", "/var/run/docker.sock")
	}
	transport := &http.Transport{DialContext: dial}
	return &http.Client{Transport: transport, Timeout: 10 * time.Second}
}

func parseUsage(raw string, totalLimitMinutes int) usageSnapshot {
	remainingPct := -1.0
	usedPct := -1.0
	remainingMinutes := 0
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
	}
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
	return "silexa-actor-" + strings.TrimSpace(acct.Dyad)
}

func criticContainer(acct accountConfig) string {
	if strings.TrimSpace(acct.Critic) != "" {
		return strings.TrimSpace(acct.Critic)
	}
	if strings.TrimSpace(acct.Dyad) == "" {
		return ""
	}
	return "silexa-critic-" + strings.TrimSpace(acct.Dyad)
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
