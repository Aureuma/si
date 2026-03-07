package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type drill struct {
	name    string
	pattern string
}

func main() {
	root, err := repoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	drills := []drill{
		{"DRILL-01 canary fanout failure gating", "TestApplyPaasReleaseToTargetsCanaryStopsAfterCanaryFailure"},
		{"DRILL-02 deploy health rollback regression path", "TestPaasRegressionUpgradeDeployRollbackPath"},
		{"DRILL-03 bluegreen post-cutover rollback", "TestRunPaasBlueGreenDeployOnTargetRollsBackOnPostCutoverHealthFailure"},
	}

	for _, d := range drills {
		fmt.Printf("[drill] %s\n", d.name)
		cmd := exec.Command("docker", "run", "--rm", "-v", fmt.Sprintf("%s:/work", root), "-w", "/work", "golang:1.25", "go", "test", "./tools/si", "-run", d.pattern, "-count=1")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		if err := cmd.Run(); err != nil {
			os.Exit(1)
		}
	}

	fmt.Println("[drill] all failure-injection rollback drills passed")
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
