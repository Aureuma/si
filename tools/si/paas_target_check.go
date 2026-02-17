package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"
)

const paasSSHBinEnvKey = "SI_PAAS_SSH_BIN"

type paasTargetCheckResult struct {
	Target         string `json:"target"`
	User           string `json:"user"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Reachable      bool   `json:"reachable"`
	SSHOK          bool   `json:"ssh_ok"`
	DockerOK       bool   `json:"docker_ok"`
	ComposeOK      bool   `json:"compose_ok"`
	Status         string `json:"status"`
	Error          string `json:"error,omitempty"`
	DurationMs     int64  `json:"duration_ms"`
	DockerVersion  string `json:"docker_version,omitempty"`
	ComposeVersion string `json:"compose_version,omitempty"`
}

func runPaasTargetCheck(target paasTarget, timeout time.Duration) paasTargetCheckResult {
	started := time.Now()
	result := paasTargetCheckResult{
		Target: target.Name,
		User:   target.User,
		Host:   target.Host,
		Port:   target.Port,
		Status: "failed",
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := paasTCPDialCheck(ctx, target.Host, target.Port); err != nil {
		result.Error = "network check failed: " + err.Error()
		result.DurationMs = time.Since(started).Milliseconds()
		return result
	}
	result.Reachable = true

	if _, err := runPaasSSHCommand(ctx, target, "echo si-preflight-ok"); err != nil {
		result.Error = "ssh check failed: " + err.Error()
		result.DurationMs = time.Since(started).Milliseconds()
		return result
	}
	result.SSHOK = true

	if out, err := runPaasSSHCommand(ctx, target, "docker version --format '{{.Server.Version}}'"); err != nil {
		result.Error = "docker check failed: " + err.Error()
		result.DurationMs = time.Since(started).Milliseconds()
		return result
	} else {
		result.DockerOK = true
		result.DockerVersion = strings.TrimSpace(out)
	}

	composeCmd := "docker compose version --short 2>/dev/null || docker-compose version --short 2>/dev/null"
	if out, err := runPaasSSHCommand(ctx, target, composeCmd); err != nil {
		result.Error = "compose check failed: " + err.Error()
		result.DurationMs = time.Since(started).Milliseconds()
		return result
	} else {
		result.ComposeOK = true
		result.ComposeVersion = strings.TrimSpace(out)
	}

	result.Status = "ok"
	result.DurationMs = time.Since(started).Milliseconds()
	return result
}

func paasTCPDialCheck(ctx context.Context, host string, port int) error {
	addr := net.JoinHostPort(strings.TrimSpace(host), fmt.Sprintf("%d", port))
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	_ = conn.Close()
	return nil
}

func runPaasSSHCommand(ctx context.Context, target paasTarget, remoteCmd string) (string, error) {
	bin := strings.TrimSpace(os.Getenv(paasSSHBinEnvKey))
	if bin == "" {
		bin = "ssh"
	}
	args := []string{
		"-p", fmt.Sprintf("%d", target.Port),
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ConnectTimeout=5",
		fmt.Sprintf("%s@%s", target.User, target.Host),
		remoteCmd,
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		return "", fmt.Errorf("%s", errMsg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func printPaasTargetCheckResults(jsonOut bool, timeout time.Duration, results []paasTargetCheckResult) {
	hasFailure := false
	for _, row := range results {
		if row.Status != "ok" {
			hasFailure = true
			break
		}
	}
	if jsonOut {
		payload := map[string]any{
			"ok":      !hasFailure,
			"command": "target check",
			"context": currentPaasContext(),
			"mode":    "live",
			"timeout": timeout.String(),
			"count":   len(results),
			"data":    results,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		if hasFailure {
			os.Exit(1)
		}
		return
	}
	fmt.Printf("%s %d\n", styleHeading("paas target check:"), len(results))
	for _, row := range results {
		prefix := "ok"
		if row.Status != "ok" {
			prefix = "fail"
		}
		fmt.Printf("  [%s] %s (%s@%s:%d) %dms\n", prefix, row.Target, row.User, row.Host, row.Port, row.DurationMs)
		if strings.TrimSpace(row.Error) != "" {
			fmt.Printf("    %s\n", styleDim(row.Error))
		}
	}
	if hasFailure {
		os.Exit(1)
	}
}
