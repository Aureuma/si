package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func main() {
	quiet, execArgs := parseArgs(os.Args[1:])

	home := envOr("HOME", "/root")
	codexHome := envOr("CODEX_HOME", filepath.Join(home, ".codex"))
	configDir := envOr("CODEX_CONFIG_DIR", codexHome)
	configPath := filepath.Join(configDir, "config.toml")
	templatePath := envOr("CODEX_CONFIG_TEMPLATE", "")

	if err := os.MkdirAll(configDir, 0o700); err != nil {
		fatal(err, quiet)
	}

	managed := false
	if data, err := os.ReadFile(configPath); err == nil {
		if bytes.Contains(data, []byte("managed by ")) && bytes.Contains(data, []byte("codex-init")) {
			managed = true
		}
	}

	force := strings.TrimSpace(os.Getenv("CODEX_INIT_FORCE")) == "1"
	if _, err := os.Stat(configPath); errors.Is(err, os.ErrNotExist) || force || managed {
		if err := writeConfig(configPath, templatePath); err != nil {
			fatal(err, quiet)
		}
	}

	// Avoid "dubious ownership" errors when running as root in a bind-mounted workspace.
	ensureGitSafeDirectory("/workspace")

	if !quiet {
		fmt.Printf("codex-init ok (dyad=%s member=%s role=%s)\n",
			envOr("DYAD_NAME", "unknown"),
			envOr("DYAD_MEMBER", "unknown"),
			envOr("ROLE", "unknown"),
		)
	}

	if len(execArgs) > 0 {
		if !strings.Contains(execArgs[0], "/") {
			resolved, err := exec.LookPath(execArgs[0])
			if err != nil {
				fatal(err, quiet)
			}
			execArgs[0] = resolved
		}
		if err := syscall.Exec(execArgs[0], execArgs, os.Environ()); err != nil {
			fatal(err, quiet)
		}
	}
}

func ensureGitSafeDirectory(path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	// Best-effort; ignore errors if git isn't available or config can't be written.
	_ = exec.Command("git", "config", "--global", "--add", "safe.directory", path).Run()
}

func parseArgs(args []string) (bool, []string) {
	quiet := false
	execArgs := []string{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--quiet":
			quiet = true
		case "--exec":
			if i+1 < len(args) {
				execArgs = append(execArgs, args[i+1:]...)
			}
			return quiet, execArgs
		}
	}
	return quiet, execArgs
}

func writeConfig(path, templatePath string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	model := envOr("CODEX_MODEL", "gpt-5.2-codex")
	effort := envOr("CODEX_REASONING_EFFORT", "medium")
	dyad := envOr("DYAD_NAME", "unknown")
	member := envOr("DYAD_MEMBER", "unknown")
	role := envOr("ROLE", "unknown")

	var content []byte
	if templatePath != "" {
		if data, err := os.ReadFile(templatePath); err == nil {
			content = applyTemplate(string(data), map[string]string{
				"__CODEX_MODEL__":            model,
				"__CODEX_REASONING_EFFORT__": effort,
				"__DYAD_NAME__":              dyad,
				"__DYAD_MEMBER__":            member,
				"__ROLE__":                   role,
				"__INITIALIZED_UTC__":        now,
			})
		}
	}
	if len(content) == 0 {
		content = []byte(defaultConfig(model, effort, dyad, member, role, now))
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), "codex-config-*.toml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func applyTemplate(input string, values map[string]string) []byte {
	out := input
	for key, val := range values {
		out = strings.ReplaceAll(out, key, escapeValue(val))
	}
	return []byte(out)
}

func escapeValue(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "&", "\\&")
	return value
}

func defaultConfig(model, effort, dyad, member, role, now string) string {
	return fmt.Sprintf(`# managed by si-codex-init
#
# Shared Codex defaults for si dyads.

model = "%s"
model_reasoning_effort = "%s"

# Codex deprecated [features].web_search_request; configure web search at the top level.
# Values: "live" | "cached" | "disabled"
web_search = "live"

[sandbox_workspace_write]
network_access = true

[si]
dyad = "%s"
member = "%s"
role = "%s"
initialized_utc = "%s"
`, escapeValue(model), escapeValue(effort), escapeValue(dyad), escapeValue(member), escapeValue(role), escapeValue(now))
}

func envOr(key, def string) string {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return def
	}
	return val
}

func fatal(err error, quiet bool) {
	if quiet {
		_ = os.Stdout.Sync()
	}
	_, _ = fmt.Fprintln(os.Stderr, err.Error())
	os.Exit(1)
}
