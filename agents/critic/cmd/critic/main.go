package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func main() {
	logger := log.New(os.Stdout, "critic ", log.LstdFlags|log.LUTC)
	ensureCodexBaseConfig(logger)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runCriticLoop(ctx, logger); err != nil {
		logger.Printf("critic loop error: %v", err)
	}
	logger.Printf("critic idle")
	for ctx.Err() == nil {
		time.Sleep(10 * time.Second)
	}
}

func ensureCodexBaseConfig(logger *log.Logger) {
	home := envOr("HOME", "/root")
	codexHome := envOr("CODEX_HOME", filepath.Join(home, ".codex"))
	codexConfigDir := envOr("CODEX_CONFIG_DIR", codexHome)
	cfg := filepath.Join(codexConfigDir, "config.toml")
	templatePath := envOr("CODEX_CONFIG_TEMPLATE", "/workspace/si/configs/codex-config.template.toml")
	force := envOr("CODEX_INIT_FORCE", "0")

	_ = os.MkdirAll(codexConfigDir, 0o700)

	dyad := envOr("DYAD_NAME", "unknown")
	member := envOr("DYAD_MEMBER", "critic")
	role := envOr("ROLE", "critic")
	model := envOr("CODEX_MODEL", "gpt-5.2-codex")
	effort := envOr("CODEX_REASONING_EFFORT", "medium")

	managed := false
	if existing, err := os.ReadFile(cfg); err == nil {
		managed = strings.Contains(string(existing), "managed by ") &&
			strings.Contains(string(existing), "codex-init")
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
	ensureBrowserMCPConfig(codexConfigDir, logger)
	logger.Printf("codex base config ensured at %s", cfg)
}

const (
	defaultBrowserContainerName = "si-playwright-mcp-headed"
	defaultBrowserMCPPort       = 8931
	defaultBrowserMCPName       = "si_browser"
)

func ensureBrowserMCPConfig(codexHome string, logger *log.Logger) {
	codexHome = strings.TrimSpace(codexHome)
	url := strings.TrimSpace(browserMCPURLFromEnv())
	if codexHome == "" || url == "" {
		return
	}
	_ = os.MkdirAll(codexHome, 0o700)
	cmd := exec.Command("codex", "mcp", "add", defaultBrowserMCPName, "--url", url)
	cmd.Env = append(os.Environ(), "CODEX_HOME="+codexHome)
	if out, err := cmd.CombinedOutput(); err != nil && logger != nil {
		logger.Printf("codex browser MCP setup skipped: %v (%s)", err, strings.TrimSpace(string(out)))
	}
}

func browserMCPURLFromEnv() string {
	if envIsTrue("SI_BROWSER_MCP_DISABLED") {
		return ""
	}
	if explicit := strings.TrimSpace(os.Getenv("SI_BROWSER_MCP_URL_INTERNAL")); explicit != "" {
		return explicit
	}
	if explicit := strings.TrimSpace(os.Getenv("SI_BROWSER_MCP_URL")); explicit != "" {
		return explicit
	}
	containerName := strings.TrimSpace(envOr("SI_BROWSER_CONTAINER", defaultBrowserContainerName))
	port := envOrInt("SI_BROWSER_MCP_PORT", defaultBrowserMCPPort)
	if containerName == "" || port <= 0 {
		return ""
	}
	return fmt.Sprintf("http://%s:%d/mcp", containerName, port)
}

func envOrInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return parsed
}

func envIsTrue(key string) bool {
	val := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch val {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

const defaultCodexTemplate = `# managed by si-codex-init
#
# Shared Codex defaults for si dyads.

model = "__CODEX_MODEL__"
model_reasoning_effort = "__CODEX_REASONING_EFFORT__"

# Codex deprecated [features].web_search_request; configure web search at the top level.
# Values: "live" | "cached" | "disabled"
web_search = "live"

[sandbox_workspace_write]
network_access = true

[si]
dyad = "__DYAD_NAME__"
member = "__DYAD_MEMBER__"
role = "__ROLE__"
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

func envOr(key, def string) string {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return def
	}
	return val
}
