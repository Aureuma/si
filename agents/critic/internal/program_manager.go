package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type programSpec struct {
	Program     string            `json:"program"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Tasks       []programSpecTask `json:"tasks"`
}

type programSpecTask struct {
	Key         string `json:"key"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Kind        string `json:"kind"`
	Priority    string `json:"priority"`
	RouteHint   string `json:"route_hint"`
}

func (m *Monitor) ProgramManagerEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("PROGRAM_MANAGER")))
	return v == "1" || v == "true" || v == "yes"
}

func (m *Monitor) ProgramManagerTick(ctx context.Context) {
	if !m.ProgramManagerEnabled() {
		return
	}
	cfg := strings.TrimSpace(os.Getenv("PROGRAM_CONFIG_FILE"))
	if cfg == "" {
		cfg = "/configs/programs/web_hosting.json"
	}
	_ = m.ReconcileProgramFile(ctx, cfg)
}

func (m *Monitor) ReconcileProgramFile(ctx context.Context, cfgFile string) error {
	prog, err := readProgramFile(cfgFile)
	if err != nil {
		return err
	}
	if strings.TrimSpace(prog.Program) == "" {
		return fmt.Errorf("program config missing program name: %s", cfgFile)
	}

	tasks, err := m.listDyadTasks(ctx)
	if err != nil {
		return err
	}

	budget := &pmBudget{Remaining: pmMaxNewPerTick()}
	return m.reconcileProgramSpec(ctx, prog, tasks, budget)
}

type pmBudget struct {
	Remaining int
}

type pmCandidate struct {
	Index       int
	Task        programSpecTask
	DyadKey     string
	IsPool      bool
	Need        int
	OpenCount   int
	Priority    int
	ProgramName string
	ProgramTitle string
}

func (m *Monitor) reconcileProgramSpec(ctx context.Context, prog *programSpec, tasks []DyadTask, budget *pmBudget) error {
	if prog == nil {
		return nil
	}
	if budget == nil {
		budget = &pmBudget{Remaining: pmMaxNewPerTick()}
	}

	// key -> task
	existing := map[string]DyadTask{}
	orphaned := make([]DyadTask, 0, 8)
	openCounts := map[string]int{}
	openTotal := 0
	for _, t := range tasks {
		key := parseState(t.Notes)["pm.key"]
		if key != "" {
			existing[key] = t
		} else if strings.TrimSpace(t.RequestedBy) == "program-manager" {
			// If PM metadata lines were overwritten in notes, try to re-associate later.
			orphaned = append(orphaned, t)
		}
		if strings.ToLower(strings.TrimSpace(t.Status)) != "done" {
			openTotal++
			dk := strings.TrimSpace(t.Dyad)
			if dk == "" {
				dk = "unassigned"
			}
			openCounts[dk]++
		}
	}

	maxGlobal := pmMaxOpenGlobal()
	if openTotal >= maxGlobal {
		m.Logger.Printf("program reconcile skip (global open=%d >= max=%d)", openTotal, maxGlobal)
		return nil
	}

	maxPerDyad := pmMaxOpenPerDyad()
	minPerDyad := pmMinOpenPerDyad()

	candidates := make([]pmCandidate, 0, len(prog.Tasks))
	for idx, pt := range prog.Tasks {
		key := strings.TrimSpace(pt.Key)
		if key == "" {
			continue
		}
		// Recover PM metadata if the task exists but lost its [pm.key] state lines.
		if _, ok := existing[key]; !ok && len(orphaned) > 0 {
			matchIdx := -1
			for i := range orphaned {
				ot := orphaned[i]
				if strings.TrimSpace(ot.Kind) != strings.TrimSpace(pt.Kind) {
					continue
				}
				if strings.TrimSpace(ot.Title) != strings.TrimSpace(pt.Title) {
					continue
				}
				if strings.TrimSpace(ot.Dyad) != strings.TrimSpace(pt.RouteHint) {
					continue
				}
				if matchIdx != -1 {
					// Ambiguous: multiple matches; don't guess.
					matchIdx = -2
					break
				}
				matchIdx = i
			}
			if matchIdx >= 0 {
				ot := orphaned[matchIdx]
				notes := strings.TrimSpace(strings.Join([]string{
					fmt.Sprintf("[pm.program]=%s", strings.TrimSpace(prog.Program)),
					fmt.Sprintf("[pm.key]=%s", key),
					fmt.Sprintf("[pm.title]=%s", strings.TrimSpace(prog.Title)),
				}, "\n"))
				if strings.TrimSpace(ot.Notes) != "" {
					notes = notes + "\n" + strings.TrimSpace(ot.Notes)
				}
				_ = m.updateDyadTask(ctx, map[string]interface{}{"id": ot.ID, "notes": notes})
				ot.Notes = notes
				existing[key] = ot
				// Remove from orphan list to reduce further scan cost.
				orphaned = append(orphaned[:matchIdx], orphaned[matchIdx+1:]...)
			}
		}
		if t, ok := existing[key]; ok {
			// Ensure program-managed tasks remain fully assigned (dyad + actor/critic fields).
			if strings.TrimSpace(t.RequestedBy) == "program-manager" && strings.TrimSpace(t.Dyad) != "" && !isPoolTarget(t.Dyad) {
				wantActor := dyadActorName(t.Dyad)
				wantCritic := dyadCriticName(t.Dyad)
				payload := map[string]interface{}{"id": t.ID}
				changed := false
				if strings.TrimSpace(t.Actor) == "" {
					payload["actor"] = wantActor
					changed = true
				}
				if strings.TrimSpace(t.Critic) == "" {
					payload["critic"] = wantCritic
					changed = true
				}
				if changed {
					_ = m.updateDyadTask(ctx, payload)
				}
			}
			continue
		}

		dyad := strings.TrimSpace(pt.RouteHint)
		isPool := isPoolTarget(dyad)
		dyadKey := dyad
		if dyadKey == "" {
			dyadKey = "unassigned"
		}
		openCount := openCounts[dyadKey]
		need := 0
		if dyadKey != "unassigned" && openCount < minPerDyad {
			need = minPerDyad - openCount
		}
		candidates = append(candidates, pmCandidate{
			Index:        idx,
			Task:         pt,
			DyadKey:      dyadKey,
			IsPool:       isPool,
			Need:         need,
			OpenCount:    openCount,
			Priority:     pmPriorityScore(pt.Priority),
			ProgramName:  strings.TrimSpace(prog.Program),
			ProgramTitle: strings.TrimSpace(prog.Title),
		})
	}

	if len(candidates) == 0 {
		// Optionally garbage-collect orphaned program-manager tasks (missing pm.key) that are
		// clearly duplicates of an existing keyed task.
		m.pmGCOrphanDuplicates(ctx, prog, existing, orphaned)
		return nil
	}

	// Prioritize keeping dyads fed (avoid starvation) without choking the board.
	sort.SliceStable(candidates, func(i, j int) bool {
		// 1) Dyads below minimum open tasks first.
		ni := candidates[i].Need
		nj := candidates[j].Need
		if ni != nj {
			return ni > nj
		}
		// 2) Higher priority first.
		pi := candidates[i].Priority
		pj := candidates[j].Priority
		if pi != pj {
			return pi > pj
		}
		// 3) Fewer open tasks first.
		oi := candidates[i].OpenCount
		oj := candidates[j].OpenCount
		if oi != oj {
			return oi < oj
		}
		// 4) Stable file order.
		return candidates[i].Index < candidates[j].Index
	})

	created := 0
	for _, c := range candidates {
		if budget.Remaining <= 0 {
			break
		}
		if openTotal >= maxGlobal {
			break
		}
		// Only gate on per-dyad max if dyad is explicitly known.
		if c.DyadKey != "unassigned" && openCounts[c.DyadKey] >= maxPerDyad {
			continue
		}

		key := strings.TrimSpace(c.Task.Key)
		dyad := strings.TrimSpace(c.Task.RouteHint)
		actor := ""
		critic := ""
		if dyad != "" && !c.IsPool {
			actor = dyadActorName(dyad)
			critic = dyadCriticName(dyad)
		}
		notes := strings.TrimSpace(strings.Join([]string{
			fmt.Sprintf("[pm.program]=%s", c.ProgramName),
			fmt.Sprintf("[pm.key]=%s", key),
			fmt.Sprintf("[pm.title]=%s", c.ProgramTitle),
		}, "\n"))

		payload := map[string]interface{}{
			"title":        c.Task.Title,
			"description":  c.Task.Description,
			"kind":         c.Task.Kind,
			"priority":     normalizePriority(c.Task.Priority),
			"dyad":         dyad,
			"actor":        actor,
			"critic":       critic,
			"requested_by": "program-manager",
			"notes":        notes,
		}
		if err := m.createDyadTask(ctx, payload); err != nil {
			m.Logger.Printf("program reconcile create failed key=%s: %v", key, err)
			continue
		}
		created++
		budget.Remaining--
		openTotal++
		openCounts[c.DyadKey]++
		m.Logger.Printf("program reconcile created key=%s dyad=%s (budget_remaining=%d)", key, dyad, budget.Remaining)
	}

	if created > 0 {
		m.Logger.Printf("program reconcile created=%d open_total=%d", created, openTotal)
	}

	// GC any remaining orphan duplicates after creating.
	m.pmGCOrphanDuplicates(ctx, prog, existing, orphaned)
	return nil
}

func (m *Monitor) pmGCOrphanDuplicates(ctx context.Context, prog *programSpec, existing map[string]DyadTask, orphaned []DyadTask) {
	if prog == nil || len(orphaned) == 0 {
		return
	}
	if !pmGCOrphanDupesEnabled() {
		return
	}
	// Map (kind,title,dyad) -> key for program tasks.
	type tuple struct {
		kind string
		title string
		dyad string
	}
	expect := map[tuple]string{}
	for _, pt := range prog.Tasks {
		t := tuple{
			kind:  strings.TrimSpace(pt.Kind),
			title: strings.TrimSpace(pt.Title),
			dyad:  strings.TrimSpace(pt.RouteHint),
		}
		if t.kind == "" || t.title == "" {
			continue
		}
		if k := strings.TrimSpace(pt.Key); k != "" {
			expect[t] = k
		}
	}
	for _, ot := range orphaned {
		t := tuple{
			kind:  strings.TrimSpace(ot.Kind),
			title: strings.TrimSpace(ot.Title),
			dyad:  strings.TrimSpace(ot.Dyad),
		}
		key, ok := expect[t]
		if !ok {
			continue
		}
		if _, ok := existing[key]; !ok {
			continue
		}
		notes := strings.TrimSpace(fmt.Sprintf("[pm.gc] duplicate of key=%s", key))
		if strings.TrimSpace(ot.Notes) != "" {
			notes = notes + "\n" + strings.TrimSpace(ot.Notes)
		}
		_ = m.updateDyadTask(ctx, map[string]interface{}{"id": ot.ID, "status": "done", "notes": notes})
		m.Logger.Printf("program gc: closed orphan duplicate id=%d key=%s", ot.ID, key)
	}
}

func readProgramFile(path string) (*programSpec, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p programSpec
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

func (m *Monitor) createDyadTask(ctx context.Context, payload map[string]interface{}) error {
	b, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.ManagerURL+"/dyad-tasks", bytes.NewReader(b))
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
		return fmt.Errorf("create dyad task non-2xx: %s", resp.Status)
	}
	return nil
}

func ensureConfigsMountWarning(logger interface{ Printf(string, ...any) }) {
	if _, err := os.Stat("/configs"); err != nil {
		logger.Printf("PROGRAM_MANAGER enabled but /configs not mounted: %v", err)
	}
}

func ensureProgramConfigExists(logger interface{ Printf(string, ...any) }, file string) {
	if strings.TrimSpace(file) == "" {
		return
	}
	if _, err := os.Stat(file); err != nil {
		logger.Printf("program config missing: %s (%v)", file, err)
	}
}

func listProgramFiles(dir string) ([]string, error) {
	out := []string{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(strings.ToLower(d.Name()), ".json") {
			out = append(out, path)
		}
		return nil
	})
	return out, err
}

func (m *Monitor) ReconcileAllPrograms(ctx context.Context) {
	if !m.ProgramManagerEnabled() {
		return
	}
	ensureConfigsMountWarning(m.Logger)
	dir := strings.TrimSpace(os.Getenv("PROGRAM_CONFIG_DIR"))
	if dir == "" {
		dir = "/configs/programs"
	}
	if _, err := os.Stat(dir); err != nil {
		// Fall back to single file.
		cfg := strings.TrimSpace(os.Getenv("PROGRAM_CONFIG_FILE"))
		if cfg == "" {
			cfg = "/configs/programs/web_hosting.json"
		}
		ensureProgramConfigExists(m.Logger, cfg)
		tasks, err := m.listDyadTasks(ctx)
		if err != nil {
			return
		}
		_ = m.reconcileProgramSpec(ctx, mustReadProgram(cfg, m.Logger), tasks, &pmBudget{Remaining: pmMaxNewPerTick()})
		return
	}
	files, err := listProgramFiles(dir)
	if err != nil {
		m.Logger.Printf("program list error: %v", err)
		return
	}
	tasks, err := m.listDyadTasks(ctx)
	if err != nil {
		m.Logger.Printf("program list tasks error: %v", err)
		return
	}
	budget := &pmBudget{Remaining: pmMaxNewPerTick()}
	// Avoid tight loops if dir is large.
	start := time.Now()
	for _, f := range files {
		prog, err := readProgramFile(f)
		if err != nil {
			continue
		}
		_ = m.reconcileProgramSpec(ctx, prog, tasks, budget)
		if budget.Remaining <= 0 {
			break
		}
		if time.Since(start) > 5*time.Second {
			break
		}
	}
}

func mustReadProgram(path string, logger interface{ Printf(string, ...any) }) *programSpec {
	p, err := readProgramFile(path)
	if err != nil {
		logger.Printf("program read error: %v", err)
		return &programSpec{Program: strings.TrimSpace(filepath.Base(path))}
	}
	return p
}

func pmMaxNewPerTick() int {
	return envIntAllowZero("PM_MAX_NEW_PER_TICK", 3)
}

func pmMaxOpenPerDyad() int {
	return envIntAllowZero("PM_MAX_OPEN_PER_DYAD", 5)
}

func pmMinOpenPerDyad() int {
	return envIntAllowZero("PM_MIN_OPEN_PER_DYAD", 1)
}

func pmMaxOpenGlobal() int {
	return envIntAllowZero("PM_MAX_OPEN_GLOBAL", 30)
}

func pmPriorityScore(p string) int {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "high", "p0", "urgent":
		return 300
	case "normal", "medium", "p1", "":
		return 200
	case "low", "p2":
		return 100
	default:
		return 150
	}
}

func pmGCOrphanDupesEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("PM_GC_ORPHAN_DUPES")))
	if v == "" {
		return true
	}
	return v == "1" || v == "true" || v == "yes"
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
