package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

	fmt.Println("== Agent doctor ==")

	required := []string{"bash", "git", "python3", "go"}
	optional := []string{"shfmt", "gofmt"}

	for _, cmd := range required {
		if hasCmd(cmd) {
			fmt.Printf("PASS required command: %s\n", cmd)
		} else {
			fmt.Printf("FAIL required command missing: %s\n", cmd)
			os.Exit(1)
		}
	}
	for _, cmd := range optional {
		if hasCmd(cmd) {
			fmt.Printf("PASS optional command: %s\n", cmd)
		} else {
			fmt.Printf("WARN optional command missing: %s\n", cmd)
		}
	}

	fmt.Println()
	fmt.Println("== Syntax checks ==")
	files := []string{
		"tools/agents/config.sh",
		"tools/agents/lib.sh",
		"tools/agents/pr-guardian.sh",
		"tools/agents/website-sentry.sh",
		"tools/agents/status.sh",
	}
	for _, file := range files {
		if err := runCmd("bash", "-n", file); err != nil {
			os.Exit(1)
		}
	}
	fmt.Println("PASS shell syntax")
}

func hasCmd(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
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
