package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	paasAgentExecutionModeDeferred         = "deferred"
	paasAgentExecutionModeOfflineFakeCodex = "offline-fake-codex"

	paasAgentOfflineFakeCodexEnvKey    = "SI_PAAS_AGENT_OFFLINE_FAKE_CODEX"
	paasAgentOfflineFakeCodexCmdEnvKey = "SI_PAAS_AGENT_OFFLINE_FAKE_CODEX_CMD"

	paasAgentExecutionTimeout = 15 * time.Second
)

type paasAgentExecutionResult struct {
	Mode     string
	Executed bool
	Note     string
}

func executePaasAgentAction(plan paasAgentRuntimeAdapterPlan) (paasAgentExecutionResult, error) {
	if !plan.Ready {
		return paasAgentExecutionResult{
			Mode:     paasAgentExecutionModeDeferred,
			Executed: false,
			Note:     "runtime adapter not ready; execution deferred",
		}, nil
	}
	if !isPaasAgentOfflineFakeCodexEnabled() {
		return paasAgentExecutionResult{
			Mode:     paasAgentExecutionModeDeferred,
			Executed: false,
			Note:     "runtime action queued for worker execution",
		}, nil
	}
	cmdLine, err := resolvePaasAgentOfflineFakeCodexCommand()
	if err != nil {
		return paasAgentExecutionResult{}, err
	}
	report, err := runPaasAgentOfflineFakeCodex(cmdLine, plan.Prompt)
	if err != nil {
		return paasAgentExecutionResult{}, err
	}
	return paasAgentExecutionResult{
		Mode:     paasAgentExecutionModeOfflineFakeCodex,
		Executed: true,
		Note:     summarizePaasAgentExecutionReport(report),
	}, nil
}

func isPaasAgentOfflineFakeCodexEnabled() bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(paasAgentOfflineFakeCodexEnvKey)))
	switch value {
	case "1", "true", "yes", "on":
		return true
	}
	startCmd := strings.ToLower(strings.TrimSpace(os.Getenv("DYAD_CODEX_START_CMD")))
	return strings.Contains(startCmd, "fake-codex.sh")
}

func resolvePaasAgentOfflineFakeCodexCommand() (string, error) {
	cmdLine := strings.TrimSpace(os.Getenv(paasAgentOfflineFakeCodexCmdEnvKey))
	if cmdLine != "" {
		return cmdLine, nil
	}
	startCmd := strings.TrimSpace(os.Getenv("DYAD_CODEX_START_CMD"))
	if strings.Contains(strings.ToLower(startCmd), "fake-codex.sh") {
		if strings.Contains(startCmd, "/workspace/tools/dyad/fake-codex.sh") {
			localPath, err := resolvePaasAgentOfflineFakeCodexPath()
			if err != nil {
				return "", err
			}
			return quoteSingle(localPath), nil
		}
		return startCmd, nil
	}
	localPath, err := resolvePaasAgentOfflineFakeCodexPath()
	if err != nil {
		return "", err
	}
	return quoteSingle(localPath), nil
}

func resolvePaasAgentOfflineFakeCodexPath() (string, error) {
	root, err := repoRoot()
	if err != nil {
		return "", fmt.Errorf("resolve offline fake-codex path: %w", err)
	}
	path := filepath.Join(root, "tools", "dyad", "fake-codex.sh")
	info, err := os.Stat(path) // #nosec G304 -- derived from repository root.
	if err != nil {
		return "", fmt.Errorf("offline fake-codex binary unavailable: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("offline fake-codex path is a directory: %s", path)
	}
	return path, nil
}

func runPaasAgentOfflineFakeCodex(cmdLine, prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), paasAgentExecutionTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-lc", strings.TrimSpace(cmdLine))
	cmd.Env = append(os.Environ(),
		"DYAD_MEMBER=actor",
		"FAKE_CODEX_DELAY_SECONDS=0",
		"FAKE_CODEX_LONG_LINES=0",
		"FAKE_CODEX_NO_MARKERS=0",
	)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}
	message := strings.TrimSpace(prompt)
	if message == "" {
		message = "deterministic remediation smoke execution"
	}
	_, _ = stdin.Write([]byte(message + "\n"))
	_, _ = stdin.Write([]byte("/exit\n"))
	_ = stdin.Close()
	waitErr := cmd.Wait()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", fmt.Errorf("offline fake-codex execution timed out after %s", paasAgentExecutionTimeout)
	}
	if waitErr != nil {
		return "", fmt.Errorf("offline fake-codex execution failed: %w (output=%s)", waitErr, compactPaasAgentExecutionOutput(output.String()))
	}
	report := extractPaasAgentWorkReport(output.String())
	if strings.TrimSpace(report) == "" {
		return "", fmt.Errorf("offline fake-codex execution produced no work report")
	}
	return report, nil
}

func extractPaasAgentWorkReport(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	const begin = "<<WORK_REPORT_BEGIN>>"
	const end = "<<WORK_REPORT_END>>"
	start := strings.Index(trimmed, begin)
	if start == -1 {
		return trimmed
	}
	chunk := trimmed[start+len(begin):]
	stop := strings.Index(chunk, end)
	if stop >= 0 {
		chunk = chunk[:stop]
	}
	return strings.TrimSpace(chunk)
}

func summarizePaasAgentExecutionReport(report string) string {
	return compactPaasAgentExecutionOutput(report)
}

func compactPaasAgentExecutionOutput(value string) string {
	compact := strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	const maxLen = 240
	if len(compact) <= maxLen {
		return compact
	}
	return strings.TrimSpace(compact[:maxLen-3]) + "..."
}
