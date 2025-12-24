package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"silexa/agents/critic/internal"
)

func main() {
	actor := envOr("ACTOR_CONTAINER", "silexa-actor-web")
	manager := envOr("MANAGER_URL", "http://manager:9090")
	dyad := envOr("DYAD_NAME", "")
	role := envOr("ROLE", "critic")
	dept := envOr("DEPARTMENT", "unknown")
	taskID := envOr("DYAD_TASK_ID", "")
	telegramURL := envOr("TELEGRAM_NOTIFY_URL", "")
	telegramChatID := envOr("TELEGRAM_CHAT_ID", "")
	sshTarget := envOr("SSH_TARGET", "")
	if strings.TrimSpace(sshTarget) == "" {
		sshTarget = strings.TrimSpace(readSSHConfigTarget())
	}
	nudgeCmd := strings.Fields(envOr("DYAD_NUDGE_CMD", "echo critic-nudge"))
	logInterval := durationEnv("CRITIC_LOG_INTERVAL", 5*time.Second)
	beatInterval := durationEnv("CRITIC_BEAT_INTERVAL", 30*time.Second)
	logger := log.New(os.Stdout, "critic ", log.LstdFlags|log.LUTC)

	ensureCodexBaseConfig(logger)
	ensureDyadRegistered(manager, dyad, logger)

	mon, err := internal.NewMonitor(actor, manager, dyad, role, dept, logger)
	if err != nil {
		logger.Fatalf("init monitor: %v", err)
	}
	mon.TelegramURL = telegramURL
	mon.TelegramChatID = telegramChatID
	mon.SSHTarget = sshTarget

	ctx := context.Background()
	tickLogs := time.NewTicker(logInterval)
	tickBeat := time.NewTicker(beatInterval)
	defer tickLogs.Stop()
	defer tickBeat.Stop()

	logger.Printf("monitoring actor %s", actor)
	for {
		select {
		case <-tickLogs.C:
			if err := mon.StreamOnce(ctx); err != nil {
				logger.Printf("stream error: %v", err)
			}
		case <-tickBeat.C:
			if err := mon.Heartbeat(ctx); err != nil {
				logger.Printf("heartbeat error: %v", err)
			}
			if dyad != "" {
				if err := mon.ReportDyad(ctx, dyad, taskID); err != nil {
					logger.Printf("dyad report error: %v", err)
				}
				// Program manager mode: reconciles global program task sets.
				mon.ReconcileAllPrograms(ctx)
				mon.TickDyadWork(ctx, dyad)
				if len(nudgeCmd) > 0 {
					if err := mon.NudgeActor(ctx, nudgeCmd); err != nil {
						logger.Printf("nudge error: %v", err)
					}
				}
			}
		}
	}
}

func ensureDyadRegistered(managerURL, dyad string, logger *log.Logger) {
	dyad = strings.TrimSpace(dyad)
	if dyad == "" {
		return
	}
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(managerURL, "/")+"/dyads", nil)
	if err != nil {
		logger.Fatalf("dyad registry check error: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		logger.Fatalf("dyad registry check error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		logger.Fatalf("dyad registry check failed: %s", resp.Status)
	}
	var list []struct {
		Dyad string `json:"dyad"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		logger.Fatalf("dyad registry decode error: %v", err)
	}
	for _, entry := range list {
		if strings.TrimSpace(entry.Dyad) == dyad {
			return
		}
	}
	logger.Fatalf("dyad %q not registered; run bin/register-dyad.sh %s", dyad, dyad)
}

func ensureCodexBaseConfig(logger *log.Logger) {
	home := envOr("HOME", "/root")
	codexConfigDir := envOr("CODEX_CONFIG_DIR", filepath.Join(home, ".config", "codex"))
	cfg := filepath.Join(codexConfigDir, "config.toml")

	_ = os.MkdirAll(codexConfigDir, 0o700)

	dyad := envOr("DYAD_NAME", "unknown")
	member := envOr("DYAD_MEMBER", "critic")
	role := envOr("ROLE", "critic")
	dept := envOr("DEPARTMENT", "unknown")
	model := envOr("CODEX_MODEL", "gpt-5.1-codex-max")
	effort := envOr("CODEX_REASONING_EFFORT", "high")

	if _, err := os.Stat(cfg); err == nil {
		return
	}

	content := fmt.Sprintf(`# managed by silexa-codex-init
#
# This file intentionally only stores Silexa metadata; it should not override
# Codex runtime behavior unless explicitly added later.

model = %q
model_reasoning_effort = %q

[silexa]
dyad = %q
member = %q
role = %q
department = %q
`, model, effort, dyad, member, role, dept)

	if err := os.WriteFile(cfg, []byte(content), 0o600); err != nil {
		logger.Printf("codex base config write error: %v", err)
		return
	}
	_ = os.Chmod(cfg, 0o600)
	logger.Printf("codex base config ensured at %s", cfg)
}

func readSSHConfigTarget() string {
	path := strings.TrimSpace(os.Getenv("SSH_TARGET_FILE"))
	if path == "" {
		path = "/configs/ssh_target"
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(b), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Support either:
		// - SSH_TARGET=user@host
		// - export SSH_TARGET=user@host
		line = strings.TrimPrefix(line, "export ")
		if strings.HasPrefix(line, "SSH_TARGET=") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "SSH_TARGET="))
			val = strings.Trim(val, "\"")
			val = strings.Trim(val, "'")
			return val
		}
	}
	return ""
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func durationEnv(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return def
}
