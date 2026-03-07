package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"si/tools/si/cmd/agentruntime"
)

type githubEvent struct {
	PullRequest struct {
		Number int `json:"number"`
		Base   struct {
			Ref string `json:"ref"`
		} `json:"base"`
		Head struct {
			Ref  string `json:"ref"`
			Repo struct {
				FullName string `json:"full_name"`
			} `json:"repo"`
		} `json:"head"`
	} `json:"pull_request"`
}

func main() {
	root, err := repoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.Chdir(root); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	rt, err := agentruntime.New("pr-guardian")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer rt.ReleaseLock()

	if !rt.RequireCmd("python3", "git") {
		rt.AppendSummary("## Preconditions", "- FAIL: missing required runtime commands")
		rt.Finalize("failed")
		os.Exit(1)
	}

	prNumber := ""
	baseRef := "main"
	headRef := envOr("GITHUB_REF_NAME", gitCurrentBranch())
	headRepo := envOr("GITHUB_REPOSITORY", "")
	repo := envOr("GITHUB_REPOSITORY", "")
	eventName := envOr("GITHUB_EVENT_NAME", "manual")
	allowPush := envOr("PR_GUARDIAN_ALLOW_PUSH", "false")

	if eventPath := strings.TrimSpace(os.Getenv("GITHUB_EVENT_PATH")); eventPath != "" {
		if ev, err := loadEvent(eventPath); err == nil {
			if ev.PullRequest.Number > 0 {
				prNumber = fmt.Sprintf("%d", ev.PullRequest.Number)
			}
			if strings.TrimSpace(ev.PullRequest.Base.Ref) != "" {
				baseRef = ev.PullRequest.Base.Ref
			}
			if strings.TrimSpace(ev.PullRequest.Head.Ref) != "" {
				headRef = ev.PullRequest.Head.Ref
			}
			if strings.TrimSpace(ev.PullRequest.Head.Repo.FullName) != "" {
				headRepo = ev.PullRequest.Head.Repo.FullName
			}
		}
	}

	if allowPush != "true" && eventName == "pull_request" && prNumber != "" {
		allowPush = "true"
	}

	lockKey := "pr-guardian-" + prNumber
	if prNumber == "" {
		lockKey = "pr-guardian-manual"
	}
	if !rt.AcquireLock(lockKey) {
		rt.AppendSummary("## Lock", "- SKIP: another guardian run is active")
		rt.Finalize("skipped_locked")
		rt.WriteGitHubOutput(map[string]string{
			"risk":            "high",
			"changed_count":   "0",
			"autofix_applied": "false",
			"summary_file":    rt.SummaryFile,
			"run_dir":         rt.RunDir,
		})
		return
	}

	rt.AppendSummary(
		"## Preconditions",
		"- PASS: git/python3 available",
		fmt.Sprintf("- base_ref: `%s`", baseRef),
		fmt.Sprintf("- head_ref: `%s`", defaultIfEmpty(headRef, "unknown")),
		fmt.Sprintf("- event: `%s`", eventName),
		fmt.Sprintf("- push_enabled: `%s`", allowPush),
		"",
	)

	_ = rt.RunLogged("fetch base branch", "git", "fetch", "--no-tags", "--depth=1", "origin", baseRef)

	changedFiles := diffChangedFiles(baseRef)
	changedCount := len(changedFiles)

	mediumCount := intEnv("PR_GUARDIAN_RISK_MEDIUM_FILE_COUNT", 10)
	highCount := intEnv("PR_GUARDIAN_RISK_HIGH_FILE_COUNT", 25)
	maxAutofix := intEnv("PR_GUARDIAN_MAX_AUTOFIX_FILES", 120)

	risk := "low"
	if changedCount > highCount {
		risk = "high"
	} else if changedCount > mediumCount {
		risk = "medium"
	}
	for _, file := range changedFiles {
		switch {
		case strings.HasPrefix(file, ".github/workflows/"), strings.HasPrefix(file, "tools/install-si.sh"), strings.Contains(file, "paas"), strings.HasPrefix(file, "agents/shared/docker/"):
			risk = "high"
		case strings.HasPrefix(file, "tools/"), strings.HasPrefix(file, "docs/"), file == "README.md":
			if risk == "low" {
				risk = "medium"
			}
		}
	}

	rt.AppendSummary(
		"## Triage",
		fmt.Sprintf("- pr: #%s", defaultIfEmpty(prNumber, "manual")),
		fmt.Sprintf("- changed_files: %d", changedCount),
		fmt.Sprintf("- risk: `%s`", risk),
		"",
		"## Safe Auto-Fix Actions",
	)

	statusBefore := gitStatusPorcelain()
	shFiles := []string{}
	goFiles := []string{}
	for _, file := range changedFiles {
		if _, err := os.Stat(file); err != nil {
			continue
		}
		if strings.HasSuffix(file, ".sh") {
			shFiles = append(shFiles, file)
		}
		if strings.HasSuffix(file, ".go") {
			goFiles = append(goFiles, file)
		}
	}
	sort.Strings(shFiles)
	sort.Strings(goFiles)

	if changedCount <= maxAutofix {
		if len(shFiles) > 0 {
			if rt.HaveCmd("shfmt") {
				_ = rt.RunLogged("shfmt changed shell files", "shfmt", append([]string{"-w"}, shFiles...)...)
			} else {
				rt.AppendSummary("- WARN: shfmt unavailable, skipped shell auto-fix")
				rt.Warn("shfmt unavailable; skipping")
			}
		}
		if len(goFiles) > 0 {
			if rt.HaveCmd("gofmt") {
				_ = rt.RunLogged("gofmt changed go files", "gofmt", append([]string{"-w"}, goFiles...)...)
			} else {
				rt.AppendSummary("- WARN: gofmt unavailable, skipped Go auto-fix")
				rt.Warn("gofmt unavailable; skipping")
			}
		}
	} else {
		rt.AppendSummary(fmt.Sprintf("- SKIP: too many files for safe auto-fix (%d)", changedCount))
	}

	autofixApplied := "false"
	if statusBefore != gitStatusPorcelain() {
		autofixApplied = "true"
		if allowPush == "true" && headRepo == repo && strings.TrimSpace(headRef) != "" {
			_ = runCmd("git", "config", "user.name", "si-agent[bot]")
			_ = runCmd("git", "config", "user.email", "si-agent@users.noreply.github.com")
			_ = runCmd("git", "add", "-A")
			_ = rt.RunLogged("commit autofix changes", "git", "commit", "-m", fmt.Sprintf("chore(agent): safe autofix for PR #%s", defaultIfEmpty(prNumber, "manual")))
			_ = rt.RunLogged("push autofix branch updates", "git", "push", "origin", "HEAD:"+headRef)
			rt.AppendSummary(fmt.Sprintf("- PASS: autofix applied and push attempted to `%s`", headRef))
		} else {
			rt.AppendSummary("- SKIP: autofix detected but push disabled (manual/fork/non-PR context)")
		}
	} else {
		rt.AppendSummary("- PASS: no autofix changes required")
	}

	rt.AppendSummary("", "## Changed Files")
	if changedCount == 0 {
		rt.AppendSummary("- none")
	} else {
		for _, file := range changedFiles {
			rt.AppendSummary(fmt.Sprintf("- `%s`", file))
		}
	}

	rt.WriteGitHubOutput(map[string]string{
		"risk":            risk,
		"changed_count":   fmt.Sprintf("%d", changedCount),
		"autofix_applied": autofixApplied,
		"summary_file":    rt.SummaryFile,
		"run_dir":         rt.RunDir,
	})

	rt.Finalize("completed")
	rt.Info("pr-guardian completed")
}

func loadEvent(path string) (*githubEvent, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var ev githubEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		return nil, err
	}
	return &ev, nil
}

func diffChangedFiles(baseRef string) []string {
	args := []string{"diff", "--name-only", fmt.Sprintf("origin/%s...HEAD", baseRef)}
	if err := runCmdSilent("git", "show-ref", "--quiet", fmt.Sprintf("refs/remotes/origin/%s", baseRef)); err != nil {
		args = []string{"diff", "--name-only", "HEAD~1...HEAD"}
	}
	out, err := runCmdOutput("git", args...)
	if err != nil {
		return nil
	}
	lines := []string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func gitStatusPorcelain() string {
	out, err := runCmdOutput("git", "status", "--porcelain")
	if err != nil {
		return ""
	}
	return string(out)
}

func gitCurrentBranch() string {
	out, err := runCmdOutput("git", "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func intEnv(name string, def int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def
	}
	v := 0
	for _, ch := range raw {
		if ch < '0' || ch > '9' {
			return def
		}
		v = v*10 + int(ch-'0')
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

func defaultIfEmpty(v string, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func runCmdSilent(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runCmdOutput(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Stderr = os.Stderr
	return cmd.Output()
}

func repoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(cwd, "go.work")); err == nil {
		return cwd, nil
	}
	return "", fmt.Errorf("go.work not found; run from repo root")
}
