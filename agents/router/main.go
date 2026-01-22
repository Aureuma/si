package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
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

type metric struct {
	ID        int       `json:"id"`
	Dyad      string    `json:"dyad"`
	Name      string    `json:"name"`
	Value     float64   `json:"value"`
	CreatedAt time.Time `json:"created_at"`
}

type rule struct {
	Match   string `json:"match"`   // substring match against title+description
	RouteTo string `json:"routeTo"` // dyad name
}

type rulesFile struct {
	DefaultDyad string `json:"defaultDyad"`
	Rules       []rule `json:"rules"`
}

type codexAccount struct {
	Name       string `json:"name"`
	Dyad       string `json:"dyad"`
	Department string `json:"department"`
	Enabled    *bool  `json:"enabled"`
}

type accountsFile struct {
	Accounts             []codexAccount `json:"accounts"`
	CooldownThresholdPct float64        `json:"cooldown_threshold_pct"`
}

type dyadSnapshot struct {
	Dyad       string `json:"dyad"`
	Department string `json:"department,omitempty"`
	Available  bool   `json:"available"`
	State      string `json:"state"`
}

type dyadPolicy struct {
	RequireRegistered bool
	RequireAvailable  bool
	RequireOnline     bool
	MaxOpenPerDyad    int
}

type usageStatus struct {
	RemainingPct float64
	SeenAt       time.Time
}

func main() {
	logger := log.New(os.Stdout, "router ", log.LstdFlags|log.LUTC)
	managerURL := envOr("MANAGER_URL", "http://manager:9090")
	pollEvery := durationEnv("ROUTER_POLL_INTERVAL", 10*time.Second)
	rulesPath := envOr("ROUTER_RULES_FILE", "/configs/router_rules.json")
	accountsPath := envOr("CODEX_ACCOUNTS_FILE", "/configs/codex_accounts.json")
	thresholdPct, thresholdSet := floatEnv("CODEX_COOLDOWN_THRESHOLD_PCT")
	if !thresholdSet {
		thresholdPct = 10
	}
	rules := loadRules(logger, rulesPath)
	policy := dyadPolicy{
		RequireRegistered: boolEnv("DYAD_REQUIRE_REGISTERED", true),
		RequireAvailable:  boolEnv("DYAD_ENFORCE_AVAILABLE", true),
		RequireOnline:     boolEnv("DYAD_REQUIRE_ONLINE", true),
		MaxOpenPerDyad:    intEnv("DYAD_MAX_OPEN_PER_DYAD", 10),
	}

	logger.Printf("routing via %s (poll=%s rules=%s)", managerURL, pollEvery, rulesPath)
	tick := time.NewTicker(pollEvery)
	defer tick.Stop()

	for range tick.C {
		accountsCfg := loadAccounts(logger, accountsPath)
		accounts := filterEnabled(accountsCfg.Accounts)
		if accountsCfg.CooldownThresholdPct > 0 {
			thresholdPct = accountsCfg.CooldownThresholdPct
		}
		usage := map[string]usageStatus{}
		if len(accounts) > 0 {
			metrics, err := listMetrics(managerURL)
			if err != nil {
				logger.Printf("list metrics error: %v", err)
			} else {
				usage = indexUsage(metrics)
			}
		}

		dyads, err := listDyads(managerURL)
		if err != nil {
			logger.Printf("list dyads error: %v", err)
		}
		dyadIndex := indexDyads(dyads)

		tasks, err := listTasks(managerURL)
		if err != nil {
			logger.Printf("list tasks error: %v", err)
			continue
		}
		openCounts := countOpenTasks(tasks)
		for _, t := range tasks {
			if t.Status != "todo" {
				continue
			}
			target := strings.TrimSpace(t.Dyad)
			if target != "" && !strings.HasPrefix(strings.ToLower(target), "pool:") {
				continue
			}
			if target == "" {
				target = routeTask(rules, t)
			}
			if target == "" {
				continue
			}
			target = resolveTarget(target, accounts, usage, thresholdPct, dyadIndex, openCounts, policy)
			if target == "" {
				continue
			}
			update := map[string]interface{}{
				"id":     t.ID,
				"dyad":   target,
				"actor":  dyadActorName(target),
				"critic": dyadCriticName(target),
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

func dyadActorName(dyad string) string {
	dyad = strings.TrimSpace(dyad)
	if dyad == "" {
		return ""
	}
	return "actor"
}

func dyadCriticName(dyad string) string {
	dyad = strings.TrimSpace(dyad)
	if dyad == "" {
		return ""
	}
	return "critic"
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

func listDyads(managerURL string) ([]dyadSnapshot, error) {
	resp, err := http.Get(managerURL + "/dyads")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var dyads []dyadSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&dyads); err != nil {
		return nil, err
	}
	return dyads, nil
}

func listMetrics(managerURL string) ([]metric, error) {
	resp, err := http.Get(managerURL + "/metrics")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, &httpStatusError{Status: resp.Status}
	}
	var metrics []metric
	if err := json.NewDecoder(resp.Body).Decode(&metrics); err != nil {
		return nil, err
	}
	return metrics, nil
}

func indexDyads(list []dyadSnapshot) map[string]dyadSnapshot {
	out := map[string]dyadSnapshot{}
	for _, d := range list {
		if strings.TrimSpace(d.Dyad) == "" {
			continue
		}
		out[d.Dyad] = d
	}
	return out
}

func countOpenTasks(tasks []dyadTask) map[string]int {
	out := map[string]int{}
	for _, t := range tasks {
		dyad := strings.TrimSpace(t.Dyad)
		if dyad == "" {
			continue
		}
		if strings.ToLower(strings.TrimSpace(t.Status)) == "done" {
			continue
		}
		out[dyad]++
	}
	return out
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

func boolEnv(key string, def bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	switch strings.ToLower(raw) {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return def
	}
}

func intEnv(key string, def int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	if v, err := strconv.Atoi(raw); err == nil {
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

func floatEnv(key string) (float64, bool) {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f, true
		}
	}
	return 0, false
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
		DefaultDyad: "pool:infra",
		Rules: []rule{
			{Match: "codex", RouteTo: "pool:infra"},
			{Match: "mcp", RouteTo: "pool:infra"},
			{Match: "stripe", RouteTo: "pool:infra"},
			{Match: "github", RouteTo: "pool:infra"},
			{Match: "ui", RouteTo: "pool:web"},
			{Match: "frontend", RouteTo: "pool:web"},
			{Match: "landing", RouteTo: "pool:marketing"},
			{Match: "blog", RouteTo: "pool:marketing"},
		},
	}
}

func loadAccounts(logger *log.Logger, path string) accountsFile {
	b, err := os.ReadFile(path)
	if err != nil {
		logger.Printf("accounts file missing (%s): %v", path, err)
		return accountsFile{}
	}
	var cfg accountsFile
	if err := json.Unmarshal(b, &cfg); err != nil {
		logger.Printf("accounts file invalid (%s): %v", path, err)
		return accountsFile{}
	}
	return cfg
}

func filterEnabled(accounts []codexAccount) []codexAccount {
	out := make([]codexAccount, 0, len(accounts))
	for _, acct := range accounts {
		if acct.Enabled != nil && !*acct.Enabled {
			continue
		}
		if strings.TrimSpace(acct.Dyad) == "" {
			continue
		}
		out = append(out, acct)
	}
	return out
}

func resolveTarget(target string, accounts []codexAccount, usage map[string]usageStatus, thresholdPct float64, dyads map[string]dyadSnapshot, openCounts map[string]int, policy dyadPolicy) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(target), "pool:") {
		dept := strings.TrimSpace(strings.TrimPrefix(target, "pool:"))
		return pickDyadFromPool(dept, accounts, usage, thresholdPct, dyads, openCounts, policy)
	}
	if acct, ok := findAccountByDyad(accounts, target); ok {
		if !isDyadEligible(target, dyads, openCounts, policy) || isCooling(usage[target], thresholdPct) {
			alt := pickDyadFromPool(acct.Department, accounts, usage, thresholdPct, dyads, openCounts, policy)
			if alt != "" && alt != target {
				return alt
			}
			return ""
		}
		return target
	}
	if deptMatch(target, accounts) {
		return pickDyadFromPool(target, accounts, usage, thresholdPct, dyads, openCounts, policy)
	}
	if !isDyadEligible(target, dyads, openCounts, policy) {
		dept := deptForDyad(target, dyads, accounts)
		if dept != "" {
			return pickDyadFromPool(dept, accounts, usage, thresholdPct, dyads, openCounts, policy)
		}
		return ""
	}
	return target
}

func findAccountByDyad(accounts []codexAccount, dyad string) (codexAccount, bool) {
	for _, acct := range accounts {
		if strings.TrimSpace(acct.Dyad) == dyad {
			return acct, true
		}
	}
	return codexAccount{}, false
}

func deptForDyad(dyad string, dyads map[string]dyadSnapshot, accounts []codexAccount) string {
	if rec, ok := dyads[dyad]; ok && strings.TrimSpace(rec.Department) != "" {
		return strings.TrimSpace(rec.Department)
	}
	if acct, ok := findAccountByDyad(accounts, dyad); ok {
		return strings.TrimSpace(acct.Department)
	}
	return ""
}

func deptMatch(dept string, accounts []codexAccount) bool {
	for _, acct := range accounts {
		if strings.TrimSpace(acct.Department) == dept {
			return true
		}
	}
	return false
}

func isDyadEligible(dyad string, dyads map[string]dyadSnapshot, openCounts map[string]int, policy dyadPolicy) bool {
	dyad = strings.TrimSpace(dyad)
	if dyad == "" {
		return false
	}
	rec, ok := dyads[dyad]
	if !ok {
		if policy.RequireRegistered || policy.RequireAvailable || policy.RequireOnline {
			return false
		}
	} else {
		if policy.RequireAvailable && !rec.Available {
			return false
		}
		if policy.RequireOnline && rec.State != "" && rec.State != "online" {
			return false
		}
	}
	if policy.MaxOpenPerDyad > 0 && openCounts[dyad] >= policy.MaxOpenPerDyad {
		return false
	}
	return true
}

func pickDyadFromPool(dept string, accounts []codexAccount, usage map[string]usageStatus, thresholdPct float64, dyads map[string]dyadSnapshot, openCounts map[string]int, policy dyadPolicy) string {
	dept = strings.TrimSpace(dept)
	if dept == "" {
		return ""
	}
	candidates := make([]codexAccount, 0, len(accounts))
	for _, acct := range accounts {
		if strings.TrimSpace(acct.Department) != dept {
			continue
		}
		if !isDyadEligible(acct.Dyad, dyads, openCounts, policy) {
			continue
		}
		candidates = append(candidates, acct)
	}
	if len(candidates) == 0 {
		if isDyadEligible(dept, dyads, openCounts, policy) {
			return dept
		}
		return ""
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		ai := usage[candidates[i].Dyad]
		aj := usage[candidates[j].Dyad]
		if ai.RemainingPct != aj.RemainingPct {
			return ai.RemainingPct > aj.RemainingPct
		}
		return candidates[i].Dyad < candidates[j].Dyad
	})
	for _, acct := range candidates {
		if !isCooling(usage[acct.Dyad], thresholdPct) {
			return acct.Dyad
		}
	}
	return candidates[0].Dyad
}

func isCooling(status usageStatus, thresholdPct float64) bool {
	if status.RemainingPct == 0 && status.SeenAt.IsZero() {
		return false
	}
	if thresholdPct <= 0 {
		return false
	}
	return status.RemainingPct <= thresholdPct
}

func indexUsage(metrics []metric) map[string]usageStatus {
	out := map[string]usageStatus{}
	for _, m := range metrics {
		if m.Name != "codex.remaining_pct" || strings.TrimSpace(m.Dyad) == "" {
			continue
		}
		prev, ok := out[m.Dyad]
		if !ok || m.CreatedAt.After(prev.SeenAt) {
			out[m.Dyad] = usageStatus{RemainingPct: m.Value, SeenAt: m.CreatedAt}
		}
	}
	return out
}
