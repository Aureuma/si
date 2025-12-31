package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"silexa/agents/critic/internal"
)

func main() {
	actor := envOr("ACTOR_CONTAINER", "actor")
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
	codexHome := envOr("CODEX_HOME", filepath.Join(home, ".codex"))
	codexConfigDir := envOr("CODEX_CONFIG_DIR", codexHome)
	cfg := filepath.Join(codexConfigDir, "config.toml")
	templatePath := envOr("CODEX_CONFIG_TEMPLATE", "/workspace/silexa/configs/codex-config.template.toml")
	force := envOr("CODEX_INIT_FORCE", "0")

	_ = os.MkdirAll(codexConfigDir, 0o700)

	dyad := envOr("DYAD_NAME", "unknown")
	member := envOr("DYAD_MEMBER", "critic")
	role := envOr("ROLE", "critic")
	dept := envOr("DEPARTMENT", "unknown")
	model := envOr("CODEX_MODEL", "gpt-5.2-codex")
	effort := envOr("CODEX_REASONING_EFFORT", "medium")

	managed := false
	if existing, err := os.ReadFile(cfg); err == nil {
		managed = strings.Contains(string(existing), "managed by silexa-codex-init")
		if force != "1" && !managed {
			return
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	values := map[string]string{
		"__CODEX_MODEL__":            escapeTemplateValue(model),
		"__CODEX_REASONING_EFFORT__": escapeTemplateValue(effort),
		"__DYAD_NAME__":              escapeTemplateValue(dyad),
		"__DYAD_MEMBER__":            escapeTemplateValue(member),
		"__ROLE__":                   escapeTemplateValue(role),
		"__DEPARTMENT__":             escapeTemplateValue(dept),
		"__INITIALIZED_UTC__":        escapeTemplateValue(now),
	}

	template := defaultCodexTemplate
	if b, err := os.ReadFile(templatePath); err == nil {
		template = string(b)
	}
	content := renderCodexTemplate(template, values)

	if err := os.WriteFile(cfg, []byte(content), 0o600); err != nil {
		logger.Printf("codex base config write error: %v", err)
		return
	}
	_ = os.Chmod(cfg, 0o600)
	logger.Printf("codex base config ensured at %s", cfg)
}

const defaultCodexTemplate = `# managed by silexa-codex-init
#
# Shared Codex defaults for Silexa dyads.

model = "__CODEX_MODEL__"
model_reasoning_effort = "__CODEX_REASONING_EFFORT__"

[features]
web_search_request = true

[sandbox_workspace_write]
network_access = true

[silexa]
dyad = "__DYAD_NAME__"
member = "__DYAD_MEMBER__"
role = "__ROLE__"
department = "__DEPARTMENT__"
initialized_utc = "__INITIALIZED_UTC__"
`

func renderCodexTemplate(template string, values map[string]string) string {
	out := template
	for key, value := range values {
		out = strings.ReplaceAll(out, key, value)
	}
	return out
}

func escapeTemplateValue(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return value
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
