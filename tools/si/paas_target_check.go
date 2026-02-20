package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

type paasTargetCheckResult struct {
	Target         string `json:"target"`
	User           string `json:"user"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	CPUArch        string `json:"cpu_arch,omitempty"`
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

func runPaasTargetCheck(target paasTarget, timeout time.Duration, imagePlatformArch string) paasTargetCheckResult {
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

	if isPaasLocalTarget(target) {
		result.Reachable = true
		result.SSHOK = true
	} else {
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
	}

	if out, err := runPaasSSHCommand(ctx, target, "uname -m"); err != nil {
		result.Error = "architecture check failed: " + err.Error()
		result.DurationMs = time.Since(started).Milliseconds()
		return result
	} else {
		result.CPUArch = normalizeCPUArch(out)
	}
	if imagePlatformArch != "" && result.CPUArch != "" && result.CPUArch != imagePlatformArch {
		result.Error = fmt.Sprintf("architecture mismatch: target %s is not compatible with requested image platform arch %s", result.CPUArch, imagePlatformArch)
		result.DurationMs = time.Since(started).Milliseconds()
		return result
	}

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

func printPaasTargetCheckResults(jsonOut bool, timeout time.Duration, imagePlatformArch string, results []paasTargetCheckResult) {
	hasFailure := false
	for _, row := range results {
		if row.Status != "ok" {
			hasFailure = true
			break
		}
	}
	if jsonOut {
		payload := map[string]any{
			"ok":                  !hasFailure,
			"command":             "target check",
			"context":             currentPaasContext(),
			"mode":                "live",
			"timeout":             timeout.String(),
			"image_platform_arch": imagePlatformArch,
			"count":               len(results),
			"data":                results,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		status := "succeeded"
		if hasFailure {
			status = "failed"
		}
		_ = recordPaasAuditEvent("target check", status, "live", map[string]string{
			"count":               intString(len(results)),
			"timeout":             timeout.String(),
			"image_platform_arch": imagePlatformArch,
		}, nil)
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
	status := "succeeded"
	if hasFailure {
		status = "failed"
	}
	_ = recordPaasAuditEvent("target check", status, "live", map[string]string{
		"count":               intString(len(results)),
		"timeout":             timeout.String(),
		"image_platform_arch": imagePlatformArch,
	}, nil)
	if hasFailure {
		os.Exit(1)
	}
}

func normalizeCPUArch(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "x86_64", "amd64":
		return "amd64"
	case "aarch64", "arm64":
		return "arm64"
	default:
		return value
	}
}

func normalizeImagePlatformArch(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return ""
	}
	parts := strings.Split(value, "/")
	if len(parts) > 0 {
		value = strings.TrimSpace(parts[len(parts)-1])
	}
	return normalizeCPUArch(value)
}
