package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"math"
	"net/http"
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

const codexAccountResetKind = "beam.codex_account_reset"

type accountsFile struct {
	Accounts               []accountConfig `json:"accounts"`
	CooldownThresholdPct   float64         `json:"cooldown_threshold_pct"`
	TotalLimitMinutes      int             `json:"total_limit_minutes"`
	PollInterval           string          `json:"poll_interval"`
}

type monitor struct {
	logger            *log.Logger
	managerURL        string
	dockerClient      *dockerClient
	spawnEnabled      bool
	requireRegistered bool
	resetOnCooldown   bool
	thresholdPct      float64
	totalLimitMin     int
	lastCooldown      map[string]bool
	statusMu          sync.Mutex
	statusCache       map[string]statusEntry
}

type usageSnapshot struct {
	RemainingPct           float64
	RemainingMinutes       int
	UsedPct                float64
	WeeklyRemainingPct     float64
	WeeklyRemainingMinutes int
	WeeklyUsedPct          float64
	Email                  string
	Model                  string
	ReasoningEffort        string
	Session                string
}

type appServerRequest struct {
	JSONRPC string      `json:"jsonrpc,omitempty"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
}

type appServerEnvelope struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *appServerError `json:"error"`
}

type appServerError struct {
	Message string `json:"message"`
}

type appRateLimitsResponse struct {
	RateLimits appRateLimitSnapshot `json:"rateLimits"`
}

type appRateLimitSnapshot struct {
	Primary   *appRateLimitWindow `json:"primary"`
	Secondary *appRateLimitWindow `json:"secondary"`
	Credits   *appCreditsSnapshot `json:"credits"`
	PlanType  string             `json:"planType"`
}

type appRateLimitWindow struct {
	UsedPercent        int    `json:"usedPercent"`
	WindowDurationMins *int64 `json:"windowDurationMins"`
	ResetsAt           *int64 `json:"resetsAt"`
}

type appCreditsSnapshot struct {
	HasCredits bool   `json:"hasCredits"`
	Unlimited  bool   `json:"unlimited"`
	Balance    string `json:"balance"`
}

type appAccountResponse struct {
	Account            *appAccount `json:"account"`
	RequiresOpenaiAuth bool        `json:"requiresOpenaiAuth"`
}

type appAccount struct {
	Type     string `json:"type"`
	Email    string `json:"email"`
	PlanType string `json:"planType"`
}

type appConfigResponse struct {
	Config appConfig `json:"config"`
}

type appConfig struct {
	Model                *string `json:"model"`
	ModelReasoningEffort *string `json:"model_reasoning_effort"`
}

const (
	appServerInitID       = 1
	appServerRateLimitsID = 2
	appServerAccountID    = 3
	appServerConfigID     = 4
)

type statusEntry struct {
	Name                   string    `json:"name"`
	Dyad                   string    `json:"dyad"`
	Department             string    `json:"department"`
	Member                 string    `json:"member,omitempty"`
	RemainingPct           float64   `json:"remaining_pct"`
	RemainingMinutes       int       `json:"remaining_minutes"`
	WeeklyRemainingPct     float64   `json:"weekly_remaining_pct"`
	WeeklyRemainingMinutes int       `json:"weekly_remaining_minutes"`
	Cooldown               bool      `json:"cooldown"`
	Email                  string    `json:"email,omitempty"`
	Model                  string    `json:"model,omitempty"`
	ReasoningEffort        string    `json:"reasoning_effort,omitempty"`
	Session                string    `json:"session,omitempty"`
	Note                   string    `json:"note,omitempty"`
	UpdatedAt              time.Time `json:"updated_at"`
}

type dyadSnapshot struct {
	Dyad string `json:"dyad"`
}

type dyadTaskSnapshot struct {
	ID     int    `json:"id"`
	Dyad   string `json:"dyad"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
}

type codexStatusInfo struct {
	Model           string
	ReasoningEffort string
	Session         string
	Email           string
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
	requireRegistered := boolEnv("DYAD_REQUIRE_REGISTERED", true)
	resetOnCooldown := boolEnv("CODEX_RESET_ON_COOLDOWN", true)
	addr := envOr("CODEX_MONITOR_ADDR", ":8086")

	dockerClient, err := newDockerClient()
	if err != nil {
		logger.Fatalf("docker client init: %v", err)
	}
	m := &monitor{
		logger:            logger,
		managerURL:        managerURL,
		dockerClient:      dockerClient,
		spawnEnabled:      spawnEnabled,
		requireRegistered: requireRegistered,
		resetOnCooldown:   resetOnCooldown,
		thresholdPct:      thresholdPct,
		totalLimitMin:     totalLimit,
		lastCooldown:      map[string]bool{},
		statusCache:       map[string]statusEntry{},
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
		a := strings.ToLower(strings.TrimSpace(entries[i].Dyad))
		b := strings.ToLower(strings.TrimSpace(entries[j].Dyad))
		if a != b {
			return a < b
		}
		return memberRank(entries[i].Member) < memberRank(entries[j].Member)
	})
	return entries
}

func formatStatusSummary(entries []statusEntry) string {
	if len(entries) == 0 {
		return "🤖 Codex usage: no data"
	}
	var b strings.Builder
	b.WriteString("🤖 Codex usage:\n")
	seen := map[string]bool{}
	order := make([]string, 0, len(entries))
	grouped := map[string][]statusEntry{}
	for _, entry := range entries {
		dyad := strings.TrimSpace(entry.Dyad)
		if dyad == "" {
			dyad = "unknown"
		}
		if !seen[dyad] {
			seen[dyad] = true
			order = append(order, dyad)
		}
		grouped[dyad] = append(grouped[dyad], entry)
	}

	for i, dyad := range order {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("🔹 " + dyad + "\n")
		members := grouped[dyad]
		sort.SliceStable(members, func(i, j int) bool {
			return memberRank(members[i].Member) < memberRank(members[j].Member)
		})
		for _, entry := range members {
			label := memberLabel(entry.Member)
			line := fmt.Sprintf("  • %s %s %s", memberEmoji(entry.Member), label, usageStatusEmoji(entry))
			if entry.Name != "" && entry.Name != entry.Dyad {
				line += " (" + entry.Name + ")"
			}
			b.WriteString(line + "\n")
			if entry.Email != "" {
				b.WriteString("    - 📧 account: " + entry.Email + "\n")
			}
			b.WriteString("    - 🤖 model: " + valueOrNA(entry.Model) + "\n")
			b.WriteString("    - 🧠 reasoning: " + valueOrNA(entry.ReasoningEffort) + "\n")
			b.WriteString("    - 🆔 session: " + valueOrNA(entry.Session) + "\n")
			if entry.RemainingPct >= 0 {
				line := fmt.Sprintf("    - ⏱ 5h remaining: %.1f%%", entry.RemainingPct)
				if bar := progressBar(entry.RemainingPct, 10); bar != "" {
					line += " " + bar
				}
				if entry.RemainingMinutes > 0 {
					line += fmt.Sprintf(" (%dm)", entry.RemainingMinutes)
				}
				if entry.Cooldown {
					line += " ⚠️ cooldown"
				}
				b.WriteString(line + "\n")
			} else {
				b.WriteString("    - ⏱ 5h remaining: n/a\n")
			}
			if entry.WeeklyRemainingPct >= 0 {
				line := fmt.Sprintf("    - 🗓 weekly remaining: %.1f%%", entry.WeeklyRemainingPct)
				if bar := progressBar(entry.WeeklyRemainingPct, 10); bar != "" {
					line += " " + bar
				}
				if entry.WeeklyRemainingMinutes > 0 {
					line += fmt.Sprintf(" (%dm)", entry.WeeklyRemainingMinutes)
				}
				b.WriteString(line + "\n")
			} else {
				b.WriteString("    - 🗓 weekly remaining: n/a\n")
			}
			if entry.Note != "" {
				b.WriteString("    - ℹ️ note: " + entry.Note + "\n")
			}
		}
	}
	return strings.TrimSpace(b.String())
}

func progressBar(pct float64, width int) string {
	if width <= 0 {
		return ""
	}
	if pct < 0 {
		return ""
	}
	clamped := pct
	if clamped < 0 {
		clamped = 0
	}
	if clamped > 100 {
		clamped = 100
	}
	filled := int(math.Round(clamped / 100 * float64(width)))
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	empty := width - filled
	if empty < 0 {
		empty = 0
	}
	if filled == 0 && empty == 0 {
		return ""
	}
	var b strings.Builder
	for i := 0; i < filled; i++ {
		b.WriteString("🟩")
	}
	for i := 0; i < empty; i++ {
		b.WriteString("⬜")
	}
	return b.String()
}

func (m *monitor) pollOnce(cfg accountsFile) {
	var registered map[string]dyadSnapshot
	if m.requireRegistered {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		list, err := m.listDyads(ctx)
		cancel()
		if err != nil {
			m.logger.Printf("dyad registry load error: %v", err)
		} else {
			registered = list
		}
	}
	for _, acct := range cfg.Accounts {
		if !accountEnabled(acct) {
			continue
		}
		if strings.TrimSpace(acct.Dyad) == "" {
			m.logger.Printf("skip account %q: missing dyad", acct.Name)
			continue
		}
		members := membersForAccount(acct)
		if m.requireRegistered {
			if registered == nil {
				m.logger.Printf("skip account %q: dyad registry unavailable", acct.Name)
				m.updateStatusForMembers(acct, members, usageSnapshot{RemainingPct: -1, WeeklyRemainingPct: -1}, false, "dyad registry unavailable")
				continue
			}
			if _, ok := registered[strings.TrimSpace(acct.Dyad)]; !ok {
				m.logger.Printf("skip account %q: dyad not registered", acct.Name)
				m.updateStatusForMembers(acct, members, usageSnapshot{RemainingPct: -1, WeeklyRemainingPct: -1}, false, "dyad not registered")
				continue
			}
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
	if strings.TrimSpace(acct.Dyad) == "" {
		return
	}

	exists, ready, err := m.inspectDyad(ctx, acct.Dyad)
	if err != nil {
		m.logger.Printf("inspect dyad %s error: %v", acct.Dyad, err)
		return
	}

	if !exists {
		if err := m.spawnDyad(ctx, acct); err != nil {
			m.logger.Printf("spawn dyad %s error: %v", acct.Dyad, err)
			return
		}
		return
	}
	if !ready {
		if err := m.restartDyad(ctx, acct.Dyad); err != nil {
			m.logger.Printf("restart dyad %s error: %v", acct.Dyad, err)
		}
	}
}

func (m *monitor) inspectDyad(ctx context.Context, dyad string) (bool, bool, error) {
	return m.dockerClient.dyadReady(ctx, dyad)
}

func (m *monitor) restartDyad(ctx context.Context, dyad string) error {
	return m.dockerClient.restartDyad(ctx, dyad)
}

func (m *monitor) spawnDyad(ctx context.Context, acct accountConfig) error {
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
	opts, err := m.buildDyadOptions(acct.Dyad, role, dept)
	if err != nil {
		return err
	}
	if err := m.dockerClient.ensureDyad(ctx, opts); err != nil {
		return err
	}
	m.logger.Printf("spawned dyad %s via docker", acct.Dyad)
	return nil
}

func (m *monitor) pollAccount(ctx context.Context, acct accountConfig) {
	members := membersForAccount(acct)
	if len(members) == 0 {
		m.updateStatusCache(acct, "", usageSnapshot{RemainingPct: -1, WeeklyRemainingPct: -1}, false, "no members")
		return
	}
	primaryMember := monitorMember(acct)
	if !memberInList(primaryMember, members) {
		primaryMember = members[0]
	}

	var primaryUsage usageSnapshot
	var primaryErr error
	var primaryRaw string
	var primaryCooldown bool

	for _, member := range members {
		usage, raw, err := m.fetchCodexUsageForMember(ctx, acct, member)
		note := ""
		if err != nil {
			m.logger.Printf("codex usage %s/%s error: %v", acct.Dyad, member, err)
			note = shortStatusNote(err)
		} else if usage.RemainingPct < 0 {
			note = "usage missing"
			m.logger.Printf("codex usage missing for %s/%s: %s", acct.Dyad, member, truncateLines(raw, 6, 800))
		}
		cooldown := usage.RemainingPct >= 0 && usage.RemainingPct <= m.thresholdPct
		m.updateStatusCache(acct, member, usage, cooldown, note)
		if member == primaryMember {
			primaryUsage = usage
			primaryErr = err
			primaryRaw = raw
			primaryCooldown = cooldown
		}
	}

	if primaryErr != nil {
		return
	}
	if primaryUsage.RemainingPct < 0 {
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
		Value:      primaryUsage.RemainingPct,
		Unit:       "percent",
		RecordedBy: "codex-monitor",
	})
	if primaryUsage.RemainingMinutes > 0 {
		m.postMetric(ctx, metricPayload{
			Dyad:       acct.Dyad,
			Department: dpt,
			Name:       "codex.remaining_minutes",
			Value:      float64(primaryUsage.RemainingMinutes),
			Unit:       "minutes",
			RecordedBy: "codex-monitor",
		})
	}
	if primaryUsage.WeeklyRemainingPct >= 0 {
		m.postMetric(ctx, metricPayload{
			Dyad:       acct.Dyad,
			Department: dpt,
			Name:       "codex.weekly_remaining_pct",
			Value:      primaryUsage.WeeklyRemainingPct,
			Unit:       "percent",
			RecordedBy: "codex-monitor",
		})
	}
	if primaryUsage.WeeklyRemainingMinutes > 0 {
		m.postMetric(ctx, metricPayload{
			Dyad:       acct.Dyad,
			Department: dpt,
			Name:       "codex.weekly_remaining_minutes",
			Value:      float64(primaryUsage.WeeklyRemainingMinutes),
			Unit:       "minutes",
			RecordedBy: "codex-monitor",
		})
	}
	m.postMetric(ctx, metricPayload{
		Dyad:       acct.Dyad,
		Department: dpt,
		Name:       "codex.cooldown",
		Value:      boolToFloat(primaryCooldown),
		Unit:       "bool",
		RecordedBy: "codex-monitor",
	})

	key := acct.Dyad
	prev, ok := m.lastCooldown[key]
	if !ok || prev != primaryCooldown {
		m.lastCooldown[key] = primaryCooldown
		m.postCooldownFeedback(ctx, acct, primaryUsage, primaryCooldown, primaryRaw)
		if primaryCooldown {
			m.maybeTriggerAccountReset(ctx, acct, primaryUsage, primaryRaw)
		}
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

func (m *monitor) maybeTriggerAccountReset(ctx context.Context, acct accountConfig, usage usageSnapshot, raw string) {
	if !m.resetOnCooldown {
		return
	}
	dyad := strings.TrimSpace(acct.Dyad)
	if dyad == "" {
		return
	}
	open, err := m.hasOpenResetTask(ctx, dyad)
	if err != nil {
		m.logger.Printf("reset task check error for %s: %v", dyad, err)
	}
	if open {
		return
	}
	targets := "actor,critic"
	paths := strings.Join(defaultCodexResetPaths(), ",")
	notes := []string{
		"[beam.codex_account_reset.targets]=" + targets,
		"[beam.codex_account_reset.paths]=" + paths,
		fmt.Sprintf("[beam.codex_account_reset.reason]=cooldown (remaining %.1f%%)", usage.RemainingPct),
	}
	title := fmt.Sprintf("Reset Codex account state for %s", dyad)
	desc := "Clear Codex CLI state so the dyad can switch to a different account."
	task := map[string]interface{}{
		"title":        title,
		"description":  desc,
		"kind":         codexAccountResetKind,
		"priority":     "high",
		"dyad":         dyad,
		"actor":        "actor",
		"critic":       "critic",
		"requested_by": "codex-monitor",
		"notes":        strings.Join(notes, "\n"),
	}
	b, _ := json.Marshal(task)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.managerURL+"/dyad-tasks", bytes.NewReader(b))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		m.logger.Printf("reset task create error for %s: %v", dyad, err)
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 300 {
		m.logger.Printf("reset task create error for %s: %s", dyad, resp.Status)
	}
}

func (m *monitor) hasOpenResetTask(ctx context.Context, dyad string) (bool, error) {
	if strings.TrimSpace(m.managerURL) == "" {
		return false, errors.New("manager url not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.managerURL+"/dyad-tasks", nil)
	if err != nil {
		return false, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return false, fmt.Errorf("manager returned %s", resp.Status)
	}
	var tasks []dyadTaskSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&tasks); err != nil {
		return false, err
	}
	for _, task := range tasks {
		if strings.TrimSpace(task.Dyad) != dyad {
			continue
		}
		if strings.ToLower(strings.TrimSpace(task.Kind)) != codexAccountResetKind {
			continue
		}
		status := strings.ToLower(strings.TrimSpace(task.Status))
		if status != "done" {
			return true, nil
		}
	}
	return false, nil
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

func (m *monitor) listDyads(ctx context.Context) (map[string]dyadSnapshot, error) {
	if strings.TrimSpace(m.managerURL) == "" {
		return nil, errors.New("manager url not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.managerURL+"/dyads", nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("manager returned %s", resp.Status)
	}
	var list []dyadSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, err
	}
	out := map[string]dyadSnapshot{}
	for _, rec := range list {
		if strings.TrimSpace(rec.Dyad) == "" {
			continue
		}
		out[rec.Dyad] = rec
	}
	return out, nil
}

func (m *monitor) fetchCodexUsage(ctx context.Context, acct accountConfig) (usageSnapshot, string, error) {
	member := monitorMember(acct)
	return m.fetchCodexUsageForMember(ctx, acct, member)
}

func (m *monitor) fetchCodexUsageForMember(ctx context.Context, acct accountConfig, member string) (usageSnapshot, string, error) {
	if home, ok := m.resolveCodexHomeForMember(acct, member); ok {
		usage, raw, err := m.execCodexRateLimitsLocal(ctx, home)
		if err == nil {
			usage = m.enrichUsageWithStatus(ctx, acct, member, usage)
			return usage, raw, nil
		}
		m.logger.Printf("codex app-server local error for %s/%s: %v", acct.Dyad, member, err)
	}
	container := memberContainer(acct, member)
	if container == "" {
		return usageSnapshot{RemainingPct: -1, WeeklyRemainingPct: -1}, "", fmt.Errorf("missing %s container", member)
	}
	usage, raw, err := m.execCodexRateLimitsRemote(ctx, acct.Dyad, container)
	if err != nil {
		return usageSnapshot{RemainingPct: -1, WeeklyRemainingPct: -1}, raw, err
	}
	usage = m.enrichUsageWithStatus(ctx, acct, member, usage)
	return usage, raw, nil
}

func (m *monitor) enrichUsageWithStatus(ctx context.Context, acct accountConfig, member string, usage usageSnapshot) usageSnapshot {
	if !needsCodexStatusInfo(usage) {
		return usage
	}
	raw, err := m.fetchCodexStatusForMember(ctx, acct, member)
	if err != nil {
		m.logger.Printf("codex status %s/%s error: %v", acct.Dyad, member, err)
		return usage
	}
	info := parseCodexStatusInfo(raw)
	if strings.TrimSpace(usage.Model) == "" && info.Model != "" {
		usage.Model = info.Model
	}
	if strings.TrimSpace(usage.ReasoningEffort) == "" && info.ReasoningEffort != "" {
		usage.ReasoningEffort = info.ReasoningEffort
	}
	if strings.TrimSpace(usage.Session) == "" && info.Session != "" {
		usage.Session = info.Session
	}
	if strings.TrimSpace(usage.Email) == "" && info.Email != "" {
		usage.Email = info.Email
	}
	return usage
}

func needsCodexStatusInfo(usage usageSnapshot) bool {
	return strings.TrimSpace(usage.Model) == "" ||
		strings.TrimSpace(usage.ReasoningEffort) == "" ||
		strings.TrimSpace(usage.Session) == ""
}

func (m *monitor) fetchCodexStatusForMember(ctx context.Context, acct accountConfig, member string) (string, error) {
	if home, ok := m.resolveCodexHomeForMember(acct, member); ok {
		raw, err := m.execCodexStatusLocal(ctx, home)
		if err == nil {
			return raw, nil
		}
		m.logger.Printf("codex status local error for %s/%s: %v", acct.Dyad, member, err)
	}
	container := memberContainer(acct, member)
	if container == "" {
		return "", fmt.Errorf("missing %s container", member)
	}
	return m.execCodexStatus(ctx, acct.Dyad, container)
}

func (m *monitor) fetchCodexStatus(ctx context.Context, acct accountConfig) (string, error) {
	if home, ok := m.resolveCodexHome(acct); ok {
		return m.execCodexStatusLocal(ctx, home)
	}
	container := monitorContainer(acct)
	if container == "" {
		return "", errors.New("missing monitor container")
	}
	return m.execCodexStatus(ctx, acct.Dyad, container)
}

func (m *monitor) execCodexRateLimitsLocal(ctx context.Context, home string) (usageSnapshot, string, error) {
	if _, err := exec.LookPath("codex"); err != nil {
		return usageSnapshot{RemainingPct: -1, WeeklyRemainingPct: -1}, "", err
	}
	statusCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	input := buildAppServerInput()
	cmd := exec.CommandContext(statusCtx, "codex", "app-server")
	cmd.Env = append(os.Environ(), "HOME="+home, "CODEX_HOME="+filepath.Join(home, ".codex"))
	cmd.Dir = home
	cmd.Stdin = bytes.NewReader(input)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := strings.TrimSpace(stdout.String())
	raw := out
	if stderr.Len() > 0 {
		raw = strings.TrimSpace(raw + "\n" + strings.TrimSpace(stderr.String()))
	}
	if err != nil {
		if raw != "" {
			return usageSnapshot{RemainingPct: -1, WeeklyRemainingPct: -1}, raw, fmt.Errorf("%w: %s", err, raw)
		}
		return usageSnapshot{RemainingPct: -1, WeeklyRemainingPct: -1}, raw, err
	}
	usage, parseErr := parseAppServerUsageOutput(out, m.totalLimitMin)
	if parseErr != nil {
		return usage, raw, parseErr
	}
	return usage, raw, nil
}

func (m *monitor) execCodexRateLimitsRemote(ctx context.Context, dyad, container string) (usageSnapshot, string, error) {
	if strings.TrimSpace(dyad) == "" {
		return usageSnapshot{RemainingPct: -1, WeeklyRemainingPct: -1}, "", fmt.Errorf("dyad required for exec")
	}
	if m.dockerClient == nil {
		return usageSnapshot{RemainingPct: -1, WeeklyRemainingPct: -1}, "", fmt.Errorf("docker client not initialized")
	}
	containerName := normalizeContainerName(container)
	if containerName == "" {
		return usageSnapshot{RemainingPct: -1, WeeklyRemainingPct: -1}, "", fmt.Errorf("container name required")
	}
	containerID, err := m.dockerClient.resolveDyadContainer(ctx, dyad, containerName)
	if err != nil {
		return usageSnapshot{RemainingPct: -1, WeeklyRemainingPct: -1}, "", err
	}

	input := buildAppServerInput()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err = m.dockerClient.exec(ctx, containerID, []string{"codex", "app-server"}, bytes.NewReader(input), &stdout, &stderr, false)
	out := strings.TrimSpace(stdout.String())
	raw := out
	if stderr.Len() > 0 {
		raw = strings.TrimSpace(raw + "\n" + strings.TrimSpace(stderr.String()))
	}
	if err != nil {
		if raw != "" {
			return usageSnapshot{RemainingPct: -1, WeeklyRemainingPct: -1}, raw, fmt.Errorf("%w: %s", err, raw)
		}
		return usageSnapshot{RemainingPct: -1, WeeklyRemainingPct: -1}, raw, err
	}
	usage, parseErr := parseAppServerUsageOutput(out, m.totalLimitMin)
	if parseErr != nil {
		return usage, raw, parseErr
	}
	return usage, raw, nil
}

func buildAppServerInput() []byte {
	reqs := []appServerRequest{
		{
			JSONRPC: "2.0",
			ID:      appServerInitID,
			Method:  "initialize",
			Params: map[string]interface{}{
				"clientInfo": map[string]string{
					"name":    "codex-monitor",
					"version": "0.0.0",
				},
			},
		},
		{
			JSONRPC: "2.0",
			ID:      appServerRateLimitsID,
			Method:  "account/rateLimits/read",
			Params:  nil,
		},
		{
			JSONRPC: "2.0",
			ID:      appServerAccountID,
			Method:  "account/read",
			Params:  map[string]interface{}{},
		},
		{
			JSONRPC: "2.0",
			ID:      appServerConfigID,
			Method:  "config/read",
			Params:  map[string]interface{}{},
		},
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, req := range reqs {
		_ = enc.Encode(req)
	}
	return buf.Bytes()
}

func parseAppServerUsageOutput(raw string, totalLimitMin int) (usageSnapshot, error) {
	usage := usageSnapshot{RemainingPct: -1, WeeklyRemainingPct: -1}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return usage, errors.New("empty app-server output")
	}
	var rateResp appRateLimitsResponse
	var accountResp appAccountResponse
	var configResp appConfigResponse
	var rateSeen bool
	var rateErr error

	scanner := bufio.NewScanner(strings.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 4096), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var envelope appServerEnvelope
		if err := json.Unmarshal([]byte(line), &envelope); err != nil {
			continue
		}
		id, ok := parseAppServerID(envelope.ID)
		if !ok {
			continue
		}
		if envelope.Error != nil {
			if id == appServerRateLimitsID {
				msg := strings.TrimSpace(envelope.Error.Message)
				if msg == "" {
					msg = "rate limits request failed"
				}
				rateErr = errors.New(msg)
			}
			continue
		}
		switch id {
		case appServerRateLimitsID:
			if err := json.Unmarshal(envelope.Result, &rateResp); err == nil {
				rateSeen = true
			}
		case appServerAccountID:
			_ = json.Unmarshal(envelope.Result, &accountResp)
		case appServerConfigID:
			_ = json.Unmarshal(envelope.Result, &configResp)
		}
	}
	if err := scanner.Err(); err != nil {
		return usage, err
	}
	if rateErr != nil {
		return usage, rateErr
	}
	if !rateSeen {
		return usage, errors.New("rate limits missing")
	}

	if rateResp.RateLimits.Primary != nil {
		remainingPct, remainingMinutes, usedPct := windowUsage(rateResp.RateLimits.Primary, totalLimitMin)
		usage.RemainingPct = remainingPct
		usage.RemainingMinutes = remainingMinutes
		usage.UsedPct = usedPct
	}
	if rateResp.RateLimits.Secondary != nil {
		remainingPct, remainingMinutes, usedPct := windowUsage(rateResp.RateLimits.Secondary, 0)
		usage.WeeklyRemainingPct = remainingPct
		usage.WeeklyRemainingMinutes = remainingMinutes
		usage.WeeklyUsedPct = usedPct
	}
	if accountResp.Account != nil && strings.EqualFold(strings.TrimSpace(accountResp.Account.Type), "chatgpt") {
		usage.Email = strings.TrimSpace(accountResp.Account.Email)
	}
	if configResp.Config.Model != nil {
		usage.Model = strings.TrimSpace(*configResp.Config.Model)
	}
	if configResp.Config.ModelReasoningEffort != nil {
		usage.ReasoningEffort = strings.TrimSpace(*configResp.Config.ModelReasoningEffort)
	}
	return usage, nil
}

func parseAppServerID(raw json.RawMessage) (int, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	var id int
	if err := json.Unmarshal(raw, &id); err == nil {
		return id, true
	}
	var idStr string
	if err := json.Unmarshal(raw, &idStr); err == nil {
		parsed, parseErr := strconv.Atoi(idStr)
		if parseErr == nil {
			return parsed, true
		}
	}
	return 0, false
}

func windowUsage(window *appRateLimitWindow, fallbackMinutes int) (float64, int, float64) {
	if window == nil {
		return -1, 0, -1
	}
	used := float64(window.UsedPercent)
	if used < 0 || used > 100 {
		return -1, 0, used
	}
	remaining := 100 - used
	windowMinutes := 0
	if window.WindowDurationMins != nil {
		windowMinutes = int(*window.WindowDurationMins)
	} else if fallbackMinutes > 0 {
		windowMinutes = fallbackMinutes
	}
	remainingMinutes := 0
	if windowMinutes > 0 {
		remainingMinutes = int(math.Round(float64(windowMinutes) * remaining / 100.0))
	}
	return remaining, remainingMinutes, used
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

func (m *monitor) resolveCodexHomeForMember(acct accountConfig, member string) (string, bool) {
	member = strings.ToLower(strings.TrimSpace(member))
	if member == "" {
		return m.resolveCodexHome(acct)
	}
	path := strings.TrimSpace(acct.CodexHome)
	if path != "" {
		if strings.ToLower(strings.TrimSpace(filepath.Base(path))) == member {
			if home, ok := resolveHomeFromPath(path, acct.Dyad); ok {
				return home, true
			}
		}
		if home, ok := resolveHomeFromPath(filepath.Join(path, member), acct.Dyad); ok {
			return home, true
		}
	}
	if strings.TrimSpace(acct.Dyad) == "" {
		return "", false
	}
	base := filepath.Join("/data/codex", strings.TrimSpace(acct.Dyad), member)
	if home, ok := resolveHomeFromPath(base, acct.Dyad); ok {
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

func (m *monitor) execCodexStatusRemote(ctx context.Context, dyad, container string) (string, error) {
	if strings.TrimSpace(dyad) == "" {
		return "", fmt.Errorf("dyad required for exec")
	}
	if m.dockerClient == nil {
		return "", fmt.Errorf("docker client not initialized")
	}
	containerName := normalizeContainerName(container)
	if containerName == "" {
		return "", fmt.Errorf("container name required")
	}
	containerID, err := m.dockerClient.resolveDyadContainer(ctx, dyad, containerName)
	if err != nil {
		return "", err
	}

	statusCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	defer stdinW.Close()
	defer stdoutR.Close()

	var writeMu sync.Mutex
	write := func(payload []byte) {
		if len(payload) == 0 {
			return
		}
		writeMu.Lock()
		_, _ = stdinW.Write(payload)
		writeMu.Unlock()
	}

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
				write([]byte("\x1b[1;1R"))
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
			write([]byte("/status\r"))
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
		maxSeq := 0
		for _, seq := range cursorRequests {
			if len(seq) > maxSeq {
				maxSeq = len(seq)
			}
		}
		for {
			n, readErr := stdoutR.Read(tmp)
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
						write([]byte("\x1b[1;1R"))
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
						write([]byte(promptHandlers[i].reply))
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
				write([]byte("/exit\r"))
				return
			case <-hardTimer.C:
				write([]byte("/exit\r"))
				return
			case <-statusCtx.Done():
				return
			}
		}
	}()

	execErrCh := make(chan error, 1)
	go func() {
		cmd := []string{"codex"}
		err := m.dockerClient.execWithSize(statusCtx, containerID, cmd, stdinR, stdoutW, 40, 120)
		_ = stdoutW.Close()
		execErrCh <- err
	}()

	var out []byte
	var execErr error
	for out == nil || execErrCh != nil {
		select {
		case <-statusCtx.Done():
			bufMu.Lock()
			snapshot := strings.TrimSpace(buf.String())
			bufMu.Unlock()
			if snapshot != "" {
				return snapshot, nil
			}
			return "", statusCtx.Err()
		case readErr := <-errCh:
			return "", readErr
		case out = <-outCh:
			if execErrCh == nil {
				break
			}
			execErr = <-execErrCh
			execErrCh = nil
		case execErr = <-execErrCh:
			execErrCh = nil
			if out == nil {
				select {
				case out = <-outCh:
				case <-time.After(500 * time.Millisecond):
				}
			}
		}
		if execErrCh == nil && out != nil {
			break
		}
	}
	if execErr != nil {
		trimmed := strings.TrimSpace(string(out))
		if trimmed != "" {
			return "", fmt.Errorf("%w: %s", execErr, trimmed)
		}
		return "", execErr
	}
	return string(out), nil
}

func (m *monitor) updateStatusCache(acct accountConfig, member string, usage usageSnapshot, cooldown bool, note string) {
	entry := statusEntry{
		Name:                   strings.TrimSpace(acct.Name),
		Dyad:                   strings.TrimSpace(acct.Dyad),
		Department:             strings.TrimSpace(acct.Department),
		Member:                 strings.ToLower(strings.TrimSpace(member)),
		RemainingPct:           usage.RemainingPct,
		RemainingMinutes:       usage.RemainingMinutes,
		WeeklyRemainingPct:     usage.WeeklyRemainingPct,
		WeeklyRemainingMinutes: usage.WeeklyRemainingMinutes,
		Cooldown:               cooldown,
		Email:                  strings.TrimSpace(usage.Email),
		Model:                  strings.TrimSpace(usage.Model),
		ReasoningEffort:        strings.TrimSpace(usage.ReasoningEffort),
		Session:                strings.TrimSpace(usage.Session),
		Note:                   cleanStatusNote(note),
		UpdatedAt:              time.Now().UTC(),
	}
	if entry.Name == "" {
		entry.Name = entry.Dyad
	}
	m.statusMu.Lock()
	key := entry.Dyad
	if entry.Member != "" {
		key = entry.Dyad + ":" + entry.Member
	}
	m.statusCache[key] = entry
	m.statusMu.Unlock()
}

func (m *monitor) updateStatusForMembers(acct accountConfig, members []string, usage usageSnapshot, cooldown bool, note string) {
	if len(members) == 0 {
		m.updateStatusCache(acct, "", usage, cooldown, note)
		return
	}
	for _, member := range members {
		m.updateStatusCache(acct, member, usage, cooldown, note)
	}
}

func (m *monitor) execCodexStatus(ctx context.Context, dyad, container string) (string, error) {
	return m.execCodexStatusRemote(ctx, dyad, container)
}

func (m *monitor) execInDyad(ctx context.Context, dyad, container string, cmd []string) (string, error) {
	if strings.TrimSpace(dyad) == "" {
		return "", fmt.Errorf("dyad required for exec")
	}
	containerName := normalizeContainerName(container)
	if containerName == "" {
		return "", fmt.Errorf("container name required")
	}
	containerID, err := m.dockerClient.resolveDyadContainer(ctx, dyad, containerName)
	if err != nil {
		return "", err
	}
	return execOutput(ctx, m.dockerClient, containerID, cmd)
}

func parseUsage(raw string, totalLimitMinutes int) usageSnapshot {
	raw = stripANSI(raw)
	raw = stripInvisibles(raw)
	remainingPct := -1.0
	usedPct := -1.0
	remainingMinutes := 0
	weeklyRemainingPct := -1.0
	weeklyUsedPct := -1.0
	weeklyRemainingMinutes := 0
	email := parseEmail(raw)
	model, reasoning := parseModelInfo(raw)
	session := parseSessionID(raw)
	percentRe := regexp.MustCompile(`(\d+(?:\.\d+)?)\s*%`)
	wordsRe := regexp.MustCompile(`(?i)(remaining|left|available)`)         // remaining context
	usedRe := regexp.MustCompile(`(?i)(used|consumed|spent|utilized)`)       // used context

	lines := strings.Split(raw, "\n")
	fallbackPercents := []float64{}
	weeklyFallbackPercents := []float64{}
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		isWeekly := isWeeklyLine(trim)
		percentMatches := percentRe.FindAllStringSubmatch(trim, -1)
		for _, match := range percentMatches {
			val, _ := strconv.ParseFloat(match[1], 64)
			if wordsRe.MatchString(trim) {
				if isWeekly {
					weeklyRemainingPct = val
				} else {
					remainingPct = val
				}
			} else if usedRe.MatchString(trim) {
				if isWeekly {
					weeklyUsedPct = val
				} else {
					usedPct = val
				}
			} else {
				if isWeekly {
					weeklyFallbackPercents = append(weeklyFallbackPercents, val)
				} else {
					fallbackPercents = append(fallbackPercents, val)
				}
			}
		}
		if mins := parseMinutes(trim); mins > 0 {
			if isWeekly {
				if weeklyRemainingMinutes == 0 && (wordsRe.MatchString(trim) || len(percentMatches) > 0) {
					weeklyRemainingMinutes = mins
				}
			} else if remainingMinutes == 0 && (wordsRe.MatchString(trim) || len(percentMatches) > 0) {
				remainingMinutes = mins
			}
		}
	}
	if remainingPct < 0 && usedPct >= 0 {
		remainingPct = 100 - usedPct
	}
	if weeklyRemainingPct < 0 && weeklyUsedPct >= 0 {
		weeklyRemainingPct = 100 - weeklyUsedPct
	}
	if remainingPct < 0 && len(fallbackPercents) == 1 {
		remainingPct = fallbackPercents[0]
	}
	if weeklyRemainingPct < 0 && len(weeklyFallbackPercents) == 1 {
		weeklyRemainingPct = weeklyFallbackPercents[0]
	}
	if remainingMinutes == 0 {
		for _, line := range lines {
			if isWeeklyLine(line) {
				continue
			}
			if mins := parseMinutes(line); mins > 0 {
				remainingMinutes = mins
				break
			}
		}
	}
	if weeklyRemainingMinutes == 0 {
		for _, line := range lines {
			if !isWeeklyLine(line) {
				continue
			}
			if mins := parseMinutes(line); mins > 0 {
				weeklyRemainingMinutes = mins
				break
			}
		}
	}
	if remainingPct < 0 && remainingMinutes > 0 && totalLimitMinutes > 0 {
		remainingPct = float64(remainingMinutes) / float64(totalLimitMinutes) * 100
	}
	return usageSnapshot{
		RemainingPct:           remainingPct,
		RemainingMinutes:       remainingMinutes,
		UsedPct:                usedPct,
		WeeklyRemainingPct:     weeklyRemainingPct,
		WeeklyRemainingMinutes: weeklyRemainingMinutes,
		WeeklyUsedPct:          weeklyUsedPct,
		Email:                  email,
		Model:                  model,
		ReasoningEffort:        reasoning,
		Session:                session,
	}
}

func stripANSI(s string) string {
	if s == "" {
		return s
	}
	// Strip common ANSI escape sequences.
	reCSI := regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	reOSC := regexp.MustCompile(`\x1b\][^\x07]*(\x07|\x1b\\)`)
	out := reCSI.ReplaceAllString(s, "")
	out = reOSC.ReplaceAllString(out, "")
	return out
}

func stripInvisibles(s string) string {
	if s == "" {
		return s
	}
	return strings.Map(func(r rune) rune {
		switch r {
		case 0, '\u200b', '\u200c', '\u200d', '\ufeff':
			return -1
		default:
			return r
		}
	}, s)
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

func isWeeklyLine(line string) bool {
	lower := strings.ToLower(line)
	if strings.Contains(lower, "weekly") || strings.Contains(lower, "week") {
		return true
	}
	if strings.Contains(lower, "7-day") || strings.Contains(lower, "7 day") || strings.Contains(lower, "7day") {
		return true
	}
	return false
}

func parseEmail(raw string) string {
	if raw == "" {
		return ""
	}
	emailRe := regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	if match := emailRe.FindString(raw); match != "" {
		return match
	}
	spacedRe := regexp.MustCompile(`([A-Za-z0-9._%+\-]+)\s*@\s*([A-Za-z0-9.\-]+\s*(?:\.\s*[A-Za-z0-9.\-]+)+)`)
	if match := spacedRe.FindString(raw); match != "" {
		compact := strings.Join(strings.Fields(match), "")
		if found := emailRe.FindString(compact); found != "" {
			return found
		}
	}
	return ""
}

func parseModelInfo(raw string) (string, string) {
	if raw == "" {
		return "", ""
	}
	model := ""
	reasoning := ""
	lines := strings.Split(raw, "\n")
	modelRe := regexp.MustCompile(`(?i)\bmodel\b\s*[:=]\s*([A-Za-z0-9._:-]+)`)
	modelAltRe := regexp.MustCompile(`(?i)using\s+model\s+([A-Za-z0-9._:-]+)`)
	reasoningRe := regexp.MustCompile(`(?i)\breasoning(?:\s*(?:effort|level|mode))?\b\s*[:=]\s*([A-Za-z0-9._-]+)`)
	reasoningInlineRe := regexp.MustCompile(`(?i)\breasoning\s+([A-Za-z0-9._-]+)`)
	effortRe := regexp.MustCompile(`(?i)\beffort\b\s*[:=]\s*([A-Za-z0-9._-]+)`)
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		if model == "" {
			if match := modelRe.FindStringSubmatch(trim); len(match) == 2 {
				model = match[1]
			} else if match := modelAltRe.FindStringSubmatch(trim); len(match) == 2 {
				model = match[1]
			}
		}
		if reasoning == "" {
			if match := reasoningRe.FindStringSubmatch(trim); len(match) == 2 {
				reasoning = match[1]
			} else if match := reasoningInlineRe.FindStringSubmatch(trim); len(match) == 2 {
				reasoning = match[1]
			} else if match := effortRe.FindStringSubmatch(trim); len(match) == 2 && strings.Contains(strings.ToLower(trim), "reason") {
				reasoning = match[1]
			}
		}
		if model != "" && reasoning != "" {
			break
		}
	}
	return model, reasoning
}

func parseCodexStatusInfo(raw string) codexStatusInfo {
	clean := stripANSI(raw)
	clean = stripInvisibles(clean)
	model, reasoning := parseModelInfo(clean)
	return codexStatusInfo{
		Model:           model,
		ReasoningEffort: reasoning,
		Session:         parseSessionID(clean),
		Email:           parseEmail(clean),
	}
}

func parseSessionID(raw string) string {
	if raw == "" {
		return ""
	}
	sessionRe := regexp.MustCompile(`(?i)\bsession\b\s*[:=]\s*([A-Za-z0-9._-]+)`)
	if match := sessionRe.FindStringSubmatch(raw); len(match) == 2 {
		return strings.TrimSpace(match[1])
	}
	return ""
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

func memberRank(member string) int {
	switch strings.ToLower(strings.TrimSpace(member)) {
	case "actor":
		return 0
	case "critic":
		return 1
	default:
		return 2
	}
}

func memberLabel(member string) string {
	member = strings.ToLower(strings.TrimSpace(member))
	if member == "" {
		return "member"
	}
	return member
}

func memberEmoji(member string) string {
	switch strings.ToLower(strings.TrimSpace(member)) {
	case "actor":
		return "🎭"
	case "critic":
		return "🛡"
	default:
		return "👤"
	}
}

func usageStatusEmoji(entry statusEntry) string {
	note := strings.ToLower(strings.TrimSpace(entry.Note))
	if entry.RemainingPct < 0 {
		switch {
		case strings.Contains(note, "auth required"):
			return "🔒"
		case strings.Contains(note, "dyad not registered"):
			return "🚫"
		case strings.Contains(note, "registry unavailable"):
			return "⚠️"
		case strings.Contains(note, "missing monitor container"), strings.Contains(note, "missing actor container"), strings.Contains(note, "missing critic container"):
			return "📦"
		case strings.Contains(note, "timeout"):
			return "⏱️"
		case strings.Contains(note, "connection refused"), strings.Contains(note, "no such host"):
			return "📡"
		default:
			return "❔"
		}
	}
	if entry.Cooldown {
		return "⚠️"
	}
	return "✅"
}

func valueOrNA(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "n/a"
	}
	return value
}

func shortStatusNote(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.TrimSpace(err.Error())
	if msg == "" {
		return ""
	}
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "authentication required"):
		return "auth required"
	case strings.Contains(lower, "dyad not registered"):
		return "dyad not registered"
	case strings.Contains(lower, "dyad registry unavailable"):
		return "dyad registry unavailable"
	case strings.Contains(lower, "missing monitor container"):
		return "missing monitor container"
	case strings.Contains(lower, "missing actor container"):
		return "missing actor container"
	case strings.Contains(lower, "missing critic container"):
		return "missing critic container"
	case strings.Contains(lower, "rate limits missing"):
		return "rate limits missing"
	case strings.Contains(lower, "empty app-server output"):
		return "empty app-server output"
	case strings.Contains(lower, "context deadline exceeded"):
		return "timeout"
	case strings.Contains(lower, "connection refused"):
		return "connection refused"
	case strings.Contains(lower, "no such host"):
		return "no such host"
	default:
		return cleanStatusNote(msg)
	}
}

func cleanStatusNote(note string) string {
	note = strings.TrimSpace(note)
	if note == "" {
		return ""
	}
	note = strings.ReplaceAll(note, "\n", " ")
	note = strings.Join(strings.Fields(note), " ")
	const maxLen = 80
	if len(note) > maxLen {
		note = note[:maxLen-3] + "..."
	}
	return note
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

func membersForAccount(acct accountConfig) []string {
	members := make([]string, 0, 2)
	if actorContainer(acct) != "" {
		members = append(members, "actor")
	}
	if criticContainer(acct) != "" {
		members = append(members, "critic")
	}
	return members
}

func memberInList(member string, list []string) bool {
	member = strings.ToLower(strings.TrimSpace(member))
	for _, item := range list {
		if strings.ToLower(strings.TrimSpace(item)) == member {
			return true
		}
	}
	return false
}

func monitorMember(acct accountConfig) string {
	role := normalizeContainerName(strings.TrimSpace(acct.MonitorRole))
	if role == "" {
		return "critic"
	}
	if role == "actor" || role == "critic" {
		return role
	}
	return "critic"
}

func memberContainer(acct accountConfig, member string) string {
	switch strings.ToLower(strings.TrimSpace(member)) {
	case "actor":
		return actorContainer(acct)
	case "critic":
		return criticContainer(acct)
	default:
		return normalizeContainerName(member)
	}
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

func alternateContainer(acct accountConfig, current string) string {
	current = normalizeContainerName(strings.TrimSpace(current))
	if current == "" {
		return actorContainer(acct)
	}
	actor := actorContainer(acct)
	critic := criticContainer(acct)
	if current == actor {
		return critic
	}
	if current == critic {
		return actor
	}
	if actor != "" {
		return actor
	}
	return critic
}

func actorContainer(acct accountConfig) string {
	if strings.TrimSpace(acct.Actor) != "" {
		return normalizeContainerName(strings.TrimSpace(acct.Actor))
	}
	if strings.TrimSpace(acct.Dyad) == "" {
		return ""
	}
	return "actor"
}

func criticContainer(acct accountConfig) string {
	if strings.TrimSpace(acct.Critic) != "" {
		return normalizeContainerName(strings.TrimSpace(acct.Critic))
	}
	if strings.TrimSpace(acct.Dyad) == "" {
		return ""
	}
	return "critic"
}

func normalizeContainerName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if name == "actor" || name == "critic" {
		return name
	}
	if strings.HasPrefix(name, "actor-") || strings.HasPrefix(name, "silexa-actor-") {
		return "actor"
	}
	if strings.HasPrefix(name, "critic-") || strings.HasPrefix(name, "silexa-critic-") {
		return "critic"
	}
	return name
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

func defaultCodexResetPaths() []string {
	return []string{
		"/root/.codex",
		"/root/.config/openai-codex",
		"/root/.config/codex",
		"/root/.cache/openai-codex",
		"/root/.cache/codex",
	}
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
