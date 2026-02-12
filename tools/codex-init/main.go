package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	if err := syncCodexSkills(home, codexHome); err != nil {
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

func syncCodexSkills(home, codexHome string) error {
	home = strings.TrimSpace(home)
	codexHome = strings.TrimSpace(codexHome)
	if home == "" || codexHome == "" {
		return nil
	}
	bundleDir := strings.TrimSpace(envOr("SI_CODEX_SKILLS_BUNDLE_DIR", "/opt/si/codex-skills"))
	if bundleDir == "" {
		return nil
	}
	targetDir := filepath.Join(codexHome, "skills")
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		return err
	}
	if err := copyTree(bundleDir, targetDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	sharedDir := filepath.Join(home, ".si", "codex", "skills")
	if isDir(filepath.Join(home, ".si")) && !isMountPoint(filepath.Join(home, ".si")) {
		if err := os.MkdirAll(sharedDir, 0o700); err == nil {
			_ = copyTree(targetDir, sharedDir)
		}
	}
	return nil
}

func isMountPoint(path string) bool {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" {
		return false
	}
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return false
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		left := line
		if idx := strings.Index(line, " - "); idx >= 0 {
			left = line[:idx]
		}
		fields := strings.Fields(left)
		if len(fields) < 5 {
			continue
		}
		mountPoint := filepath.Clean(decodeMountInfoPath(fields[4]))
		if mountPoint == path {
			return true
		}
	}
	return false
}

func decodeMountInfoPath(raw string) string {
	if raw == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(raw))
	for i := 0; i < len(raw); i++ {
		if raw[i] == '\\' && i+3 < len(raw) {
			if v, err := strconv.ParseUint(raw[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(raw[i])
	}
	return b.String()
}

func copyTree(src, dst string) error {
	src = filepath.Clean(strings.TrimSpace(src))
	dst = filepath.Clean(strings.TrimSpace(dst))
	if src == "" || dst == "" {
		return nil
	}
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}
	mode := fs.FileMode(0o600)
	if info.Mode()&0o111 != 0 {
		mode = 0o755
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), "skill-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dst)
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

func isDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

func fatal(err error, quiet bool) {
	if quiet {
		_ = os.Stdout.Sync()
	}
	_, _ = fmt.Fprintln(os.Stderr, err.Error())
	os.Exit(1)
}
