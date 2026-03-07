package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"si/tools/si/cmd/agentruntime"
)

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

	rt, err := agentruntime.New("website-sentry")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer rt.ReleaseLock()

	if !rt.AcquireLock("website-sentry") {
		rt.AppendSummary("## Lock", "- SKIP: another website-sentry run is active")
		rt.Finalize("skipped_locked")
		rt.WriteGitHubOutput(map[string]string{
			"status":       "skipped_locked",
			"summary_file": rt.SummaryFile,
			"run_dir":      rt.RunDir,
		})
		return
	}

	rt.AppendSummary("## Preconditions")
	if !rt.RequireCmd("go", "git", "bash") {
		rt.AppendSummary("- FAIL: required commands missing")
		rt.Finalize("failed")
		rt.WriteGitHubOutput(map[string]string{
			"status":       "failed",
			"summary_file": rt.SummaryFile,
			"run_dir":      rt.RunDir,
		})
		os.Exit(1)
	}
	rt.AppendSummary("- PASS: go/git/bash available")
	rt.AppendSummary("", "## Health Checks")

	retryAttempts := intEnv("WEBSITE_SENTRY_RETRY_ATTEMPTS", 2)
	retryDelay := time.Duration(intEnv("WEBSITE_SENTRY_RETRY_DELAY_SECONDS", 3)) * time.Second
	maxRemediation := intEnv("WEBSITE_SENTRY_MAX_REMEDIATION_ATTEMPTS", 2)

	check := func(label string, cmd []string) bool {
		err := rt.RunWithRetry(retryAttempts, retryDelay, label, func() error {
			return rt.RunLogged(label, cmd[0], cmd[1:]...)
		})
		if err == nil {
			rt.AppendSummary(fmt.Sprintf("- PASS: %s", label))
			return true
		}
		rt.AppendSummary(fmt.Sprintf("- FAIL: %s", label))
		return false
	}

	runHealthSuite := func() bool {
		healthy := true
		if !check("go test tools/si", []string{"go", "test", "./tools/si/..."}) {
			healthy = false
		}
		if !check("workspace tests", []string{"./tools/test.sh"}) {
			healthy = false
		}
		if rt.HaveCmd("docker") {
			if !check("installer docker smoke", []string{"./tools/test-install-si-docker.sh"}) {
				healthy = false
			}
		} else {
			rt.AppendSummary("- WARN: docker not available; skipped installer docker smoke")
		}
		return healthy
	}

	if runHealthSuite() {
		rt.AppendSummary("", "No remediation required.")
		status := "healthy"
		rt.Finalize(status)
		rt.WriteGitHubOutput(map[string]string{
			"status":       status,
			"summary_file": rt.SummaryFile,
			"run_dir":      rt.RunDir,
		})
		rt.Info("website-sentry completed without remediation")
		return
	}

	rt.AppendSummary("", "## Remediation", "- strategy: format/lint repair, then re-run health suite")
	status := "failed"
	for attempt := 1; attempt <= maxRemediation; attempt++ {
		rt.AppendSummary(fmt.Sprintf("- remediation attempt %d/%d", attempt, maxRemediation))
		goFiles := gitTrackedGoFiles()
		if len(goFiles) > 0 {
			args := append([]string{"-w"}, goFiles...)
			_ = rt.RunLogged("gofmt repo go files", "gofmt", args...)
		}
		if rt.HaveCmd("shfmt") {
			_ = rt.RunLogged("shfmt agent scripts", "shfmt", "-w", "tools/agents/*.sh")
		}

		if runHealthSuite() {
			if gitDiffQuiet() {
				status = "recovered_without_changes"
				rt.AppendSummary("- PASS: recovered without source changes")
			} else {
				status = "remediated_with_changes"
				rt.AppendSummary("- PASS: recovered with source changes")
			}
			break
		}
	}

	if status == "failed" {
		rt.AppendSummary("- FAIL: remediation exhausted")
	}

	rt.WriteGitHubOutput(map[string]string{
		"status":       status,
		"summary_file": rt.SummaryFile,
		"run_dir":      rt.RunDir,
	})
	rt.Finalize(status)
	if status == "failed" {
		rt.Error("website-sentry failed after remediation attempts")
		os.Exit(1)
	}
	rt.Info("website-sentry completed (status=%s)", status)
}

func gitTrackedGoFiles() []string {
	out, err := runCmdOutput("git", "ls-files", "*.go")
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

func gitDiffQuiet() bool {
	cmd := exec.Command("git", "diff", "--quiet")
	return cmd.Run() == nil
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
