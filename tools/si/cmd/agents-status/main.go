package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

	logRoot := strings.TrimSpace(os.Getenv("AGENT_LOG_ROOT"))
	if logRoot == "" {
		logRoot = ".artifacts/agent-logs"
	}

	printLatest(logRoot, "pr-guardian")
	printLatest(logRoot, "website-sentry")
}

func printLatest(logRoot string, agent string) {
	dir := filepath.Join(logRoot, agent)
	entries, err := os.ReadDir(dir)
	if err != nil {
		fmt.Printf("%s: no runs\n", agent)
		return
	}
	runDirs := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			runDirs = append(runDirs, filepath.Join(dir, e.Name()))
		}
	}
	if len(runDirs) == 0 {
		fmt.Printf("%s: no runs\n", agent)
		return
	}
	sort.Strings(runDirs)
	latest := runDirs[len(runDirs)-1]
	fmt.Printf("%s: %s\n", agent, latest)

	summary := filepath.Join(latest, "summary.md")
	raw, err := os.ReadFile(summary)
	if err == nil {
		lines := strings.Split(string(raw), "\n")
		for i, line := range lines {
			if i >= 20 {
				break
			}
			fmt.Println(line)
		}
	}
	fmt.Println()
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
