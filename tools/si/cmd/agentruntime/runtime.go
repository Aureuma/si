package agentruntime

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Runtime struct {
	AgentName        string
	LogRoot          string
	LockRoot         string
	RetentionDays    int
	RunID            string
	RunDir           string
	LogFile          string
	LogJSONFile      string
	SummaryFile      string
	MetaFile         string
	LockDir          string
	GithubOutputPath string
}

func New(agentName string) (*Runtime, error) {
	if strings.TrimSpace(agentName) == "" {
		return nil, fmt.Errorf("agent name is required")
	}
	logRoot := envOr("AGENT_LOG_ROOT", ".artifacts/agent-logs")
	lockRoot := envOr("AGENT_LOCK_ROOT", ".tmp/agent-locks")
	retention := parseIntEnv("AGENT_LOG_RETENTION_DAYS", 14)
	runID := time.Now().UTC().Format("20060102T150405Z") + fmt.Sprintf("-%d", os.Getpid())
	runDir := filepath.Join(logRoot, agentName, runID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return nil, err
	}
	r := &Runtime{
		AgentName:        agentName,
		LogRoot:          logRoot,
		LockRoot:         lockRoot,
		RetentionDays:    retention,
		RunID:            runID,
		RunDir:           runDir,
		LogFile:          filepath.Join(runDir, "run.log"),
		LogJSONFile:      filepath.Join(runDir, "run.jsonl"),
		SummaryFile:      filepath.Join(runDir, "summary.md"),
		MetaFile:         filepath.Join(runDir, "meta.json"),
		GithubOutputPath: strings.TrimSpace(os.Getenv("GITHUB_OUTPUT")),
	}
	if err := os.WriteFile(r.LogFile, nil, 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(r.LogJSONFile, nil, 0o644); err != nil {
		return nil, err
	}
	summary := fmt.Sprintf("# %s run %s\n\n- started: %s\n- run_dir: `%s`\n\n", r.AgentName, r.RunID, timestampUTC(), r.RunDir)
	if err := os.WriteFile(r.SummaryFile, []byte(summary), 0o644); err != nil {
		return nil, err
	}
	meta := fmt.Sprintf("{\n  \"agent\": \"%s\",\n  \"run_id\": \"%s\",\n  \"started_at\": \"%s\",\n  \"pid\": %d\n}\n", r.AgentName, r.RunID, timestampUTC(), os.Getpid())
	if err := os.WriteFile(r.MetaFile, []byte(meta), 0o644); err != nil {
		return nil, err
	}
	r.Info("initialized %s (run=%s)", r.AgentName, r.RunID)
	r.cleanupOldRuns()
	return r, nil
}

func (r *Runtime) cleanupOldRuns() {
	if r.RetentionDays <= 0 {
		return
	}
	root := r.LogRoot
	if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
		return
	}
	cutoff := time.Now().Add(-time.Duration(r.RetentionDays) * 24 * time.Hour)
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		if strings.Count(filepath.ToSlash(rel), "/") != 2 {
			return nil
		}
		info, statErr := d.Info()
		if statErr != nil {
			return nil
		}
		if info.ModTime().Before(cutoff) {
			_ = os.RemoveAll(path)
		}
		return nil
	})
}

func (r *Runtime) AcquireLock(key string) bool {
	if strings.TrimSpace(key) == "" {
		return false
	}
	if err := os.MkdirAll(r.LockRoot, 0o755); err != nil {
		r.Warn("failed creating lock root: %v", err)
		return false
	}
	r.LockDir = filepath.Join(r.LockRoot, key+".lock")
	if err := os.Mkdir(r.LockDir, 0o755); err != nil {
		r.Warn("lock busy %s; another run is in progress", r.LockDir)
		return false
	}
	_ = os.WriteFile(filepath.Join(r.LockDir, "pid"), []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o644)
	r.Info("acquired lock %s", r.LockDir)
	return true
}

func (r *Runtime) ReleaseLock() {
	if strings.TrimSpace(r.LockDir) == "" {
		return
	}
	if fi, err := os.Stat(r.LockDir); err == nil && fi.IsDir() {
		_ = os.RemoveAll(r.LockDir)
		r.Info("released lock %s", r.LockDir)
	}
}

func (r *Runtime) AppendSummary(lines ...string) {
	f, err := os.OpenFile(r.SummaryFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	defer f.Close()
	for _, line := range lines {
		_, _ = fmt.Fprintln(f, line)
	}
}

func (r *Runtime) Finalize(status string) {
	r.AppendSummary(
		"",
		"## Run Metadata",
		fmt.Sprintf("- completed: %s", timestampUTC()),
		fmt.Sprintf("- status: `%s`", status),
		fmt.Sprintf("- run log: `%s`", r.LogFile),
		fmt.Sprintf("- json log: `%s`", r.LogJSONFile),
	)
}

func (r *Runtime) WriteGitHubOutput(values map[string]string) {
	if strings.TrimSpace(r.GithubOutputPath) == "" {
		return
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	f, err := os.OpenFile(r.GithubOutputPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		r.Warn("failed writing GITHUB_OUTPUT: %v", err)
		return
	}
	defer f.Close()
	for _, k := range keys {
		_, _ = fmt.Fprintf(f, "%s=%s\n", k, values[k])
	}
}

func (r *Runtime) RequireCmd(cmds ...string) bool {
	missing := []string{}
	for _, cmd := range cmds {
		if _, err := exec.LookPath(cmd); err != nil {
			missing = append(missing, cmd)
		}
	}
	if len(missing) == 0 {
		return true
	}
	r.Error("missing required commands: %s", strings.Join(missing, " "))
	return false
}

func (r *Runtime) HaveCmd(cmd string) bool {
	_, err := exec.LookPath(cmd)
	return err == nil
}

func (r *Runtime) RunLogged(label string, name string, args ...string) error {
	start := time.Now()
	r.Info("BEGIN %s :: %s %s", label, name, strings.Join(args, " "))
	cmd := exec.Command(name, args...)
	cmd.Stdout = mustOpenAppend(r.LogFile)
	cmd.Stderr = mustOpenAppend(r.LogFile)
	cmd.Stdin = os.Stdin
	err := cmd.Run()
	dur := time.Since(start).Round(time.Second)
	if err == nil {
		r.Info("END %s :: rc=0 duration=%s", label, dur)
		return nil
	}
	r.Error("END %s :: rc=1 duration=%s", label, dur)
	return err
}

func (r *Runtime) RunWithRetry(attempts int, delay time.Duration, label string, fn func() error) error {
	if attempts <= 0 {
		attempts = 1
	}
	currDelay := delay
	var lastErr error
	for i := 1; i <= attempts; i++ {
		err := fn()
		if err == nil {
			return nil
		}
		lastErr = err
		if i < attempts {
			r.Warn("%s failed on attempt %d, retrying in %s", label, i, currDelay)
			time.Sleep(currDelay)
			currDelay *= 2
		}
	}
	return lastErr
}

func (r *Runtime) runLogLine(level string, msg string) {
	ts := timestampUTC()
	line := fmt.Sprintf("%s [%s] %s\n", ts, level, msg)
	_, _ = os.Stdout.WriteString(line)
	f := mustOpenAppend(r.LogFile)
	_, _ = f.WriteString(line)
	_ = f.Close()

	jsonLine := fmt.Sprintf(`{"ts":"%s","level":"%s","msg":"%s"}`+"\n", ts, level, jsonEscape(msg))
	jf := mustOpenAppend(r.LogJSONFile)
	_, _ = jf.WriteString(jsonLine)
	_ = jf.Close()
}

func (r *Runtime) Info(format string, args ...any) {
	r.runLogLine("INFO", fmt.Sprintf(format, args...))
}

func (r *Runtime) Warn(format string, args ...any) {
	r.runLogLine("WARN", fmt.Sprintf(format, args...))
}

func (r *Runtime) Error(format string, args ...any) {
	r.runLogLine("ERROR", fmt.Sprintf(format, args...))
}

func mustOpenAppend(path string) *os.File {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return f
}

func timestampUTC() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05Z")
}

func parseIntEnv(name string, def int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def
	}
	v, err := strconvAtoi(raw)
	if err != nil {
		return def
	}
	return v
}

func envOr(name string, def string) string {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return def
	}
	return v
}

func jsonEscape(s string) string {
	s = strings.ReplaceAll(s, `\\`, `\\\\`)
	s = strings.ReplaceAll(s, `"`, `\\"`)
	return s
}

func strconvAtoi(s string) (int, error) {
	n := 0
	sign := 1
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	if s[0] == '-' {
		sign = -1
		s = s[1:]
	}
	if s == "" {
		return 0, fmt.Errorf("invalid")
	}
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("invalid")
		}
		n = n*10 + int(ch-'0')
	}
	return sign * n, nil
}
