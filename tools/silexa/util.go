package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/term"
)

func usage() {
	fmt.Print(colorizeHelp(`si [command] [args]

Holistic CLI for Silexa. This help includes all commands, flags, and core features.

Features:
  - Dyads: spawn paired actor/critic containers, exec into them, manage logs.
  - Codex containers: spawn/list/status/report/login/exec/tail/clone/remove/stop/start.
  - Codex one-off exec: run codex exec in an isolated container (with MCP disabled if desired).
  - Image build helpers for local dev.
  - Docker passthrough for raw docker CLI calls.

Usage:
  si <command> [subcommand] [args...]
  si help | -h | --help

Core:
  si dyad spawn|list|remove|recreate|status|exec|logs|restart|cleanup|copy-login
  si codex spawn|respawn|list|status|report|login|ps|exec|logs|tail|clone|remove|stop|start
  si docker <args...>

Build:
  si images build
  si image build -t <tag> [-f <Dockerfile>] [--build-arg KEY=VALUE] <context>

Profiles:
  si profile <profile-name>
  si capability <role>

Command details
---------------

dyad:
  si dyad spawn <name> [role] [department]
    --role <role>
    --department <dept>
    --actor-image <image>
    --critic-image <image>
    --codex-model <model>
    --codex-effort-actor <effort>
    --codex-effort-critic <effort>
    --codex-model-low <model>
    --codex-model-medium <model>
    --codex-model-high <model>
    --codex-effort-low <effort>
    --codex-effort-medium <effort>
    --codex-effort-high <effort>
    --workspace <host path>
    --configs <host path>
    --forward-ports <range>

  si dyad list                    (no flags)
  si dyad remove <name>           (aliases: teardown, destroy)
  si dyad recreate <name> [role] [department]
  si dyad status <name>
  si dyad exec <dyad> [--member actor|critic] [--tty] -- <cmd...>
    --member <actor|critic>
    --tty
  si dyad logs <dyad> [--member actor|critic] [--tail N]
    --member <actor|critic>
    --tail <lines>
  si dyad restart <name>
  si dyad cleanup
  si dyad copy-login <dyad>
    --source <si-codex container name or suffix>
    --member <actor|critic>
    --source-home <path>
    --target-home <path>

codex:
  si codex spawn <name>
  si codex respawn <name> [--volumes]
    --image <docker image>
    --workspace <host path>
    --network <network>
    --repo <Org/Repo>
    --gh-pat <token>
    --cmd <command>
    --workdir <path>
    --codex-volume <volume>
    --gh-volume <volume>
    --clean-slate / --clean-slate=false
    --detach / --detach=false
    --env KEY=VALUE        (repeatable)
    --port HOST:CONTAINER  (repeatable)

  si codex list [--json]
    --json

  si codex status <name>
    --json
    --raw
    --timeout <duration>
    --tmux-capture <alt|main>
    --tmux-keep
    --status-only
    --debug
    --status-attempts <n>
    --status-window <duration>
    --status-deadline <duration>
    --retry-delay <duration>
    --prompt-lines <n>
    --require-context / --require-context=false
    --allow-mcp-startup
    --lock-timeout <duration>
    --lock-stale <duration>
    --cleanup-stale-sessions / --cleanup-stale-sessions=false

  si codex report <name>
    --json
    --raw
    --ansi
    --turn-timeout <duration>
    --ready-timeout <duration>
    --poll-interval <duration>
    --submit-attempts <n>
    --submit-delay <duration>
    --prompt-lines <n>
    --allow-mcp-startup
    --tmux-capture <alt|main>
    --tmux-keep
    --debug
    --lock-timeout <duration>
    --lock-stale <duration>
    --prompts-file <path>
    --prompt <text>         (repeatable)

  si codex login <name> [--device-auth]
    --device-auth / --device-auth=false

  si codex exec (two modes)
    One-off exec (isolated container):
      si codex exec --prompt "..." [--output-only] [--no-mcp]
      si codex exec "..." [--output-only] [--no-mcp]
      --one-off
      --prompt <text>
      --output-only
      --no-mcp
      --image <docker image>
      --workspace <host path>
      --workdir <path>
      --network <network>
      --codex-volume <volume>
      --gh-volume <volume>
      --model <model>
      --effort <effort>
      --keep
      --env KEY=VALUE        (repeatable)

    Exec into existing container:
      si codex exec <name> [--] <command>

  si codex logs <name> [--tail N]
  si codex tail <name> [--tail N]
  si codex clone <name> <Org/Repo> [--gh-pat TOKEN]
  si codex remove <name> [--volumes]
  si codex stop <name>
  si codex start <name>

images:
  si images build
  si image build -t <tag> [-f <Dockerfile>] [--build-arg KEY=VALUE] <context>
    -t, --tag <tag>
    -f, --file <Dockerfile>
    --build-arg KEY=VALUE   (repeatable)

profile:
  si profile <name>

capability:
  si capability <role>

Environment defaults (selected)
-------------------------------
  ACTOR_IMAGE, CRITIC_IMAGE, SI_CODEX_IMAGE, SI_NETWORK
  CODEX_MODEL, CODEX_REASONING_EFFORT, CODEX_MODEL_LOW|MEDIUM|HIGH
  CODEX_REASONING_EFFORT_LOW|MEDIUM|HIGH
  SILEXA_WORKSPACE_HOST, SILEXA_CONFIGS_HOST, SILEXA_DYAD_FORWARD_PORTS
  SI_CODEX_EXEC_VOLUME, GH_PAT, GH_TOKEN, GITHUB_TOKEN
`))
}

func envOr(key, def string) string {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return def
	}
	return val
}

func readFileTrim(path string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}
	return strings.TrimSpace(string(data)), true, nil
}

func mustRepoRoot() string {
	root, err := repoRoot()
	if err != nil {
		fatal(err)
	}
	return root
}

func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if exists(filepath.Join(dir, "configs")) && exists(filepath.Join(dir, "agents")) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("repo root not found (expected configs/ and agents/)")
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func fatal(err error) {
	_, _ = fmt.Fprintln(os.Stderr, styleError(err.Error()))
	os.Exit(1)
}

func validateSlug(name string) error {
	if name == "" {
		return errors.New("name required")
	}
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_' {
			continue
		}
		return fmt.Errorf("invalid name %q (allowed: letters, numbers, - and _)", name)
	}
	return nil
}

func isValidSlug(name string) bool {
	if strings.TrimSpace(name) == "" {
		return false
	}
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_' {
			continue
		}
		return false
	}
	return true
}

var ansiEnabled = initAnsiEnabled()

func initAnsiEnabled() bool {
	if strings.TrimSpace(os.Getenv("NO_COLOR")) != "" || strings.TrimSpace(os.Getenv("SI_NO_COLOR")) != "" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("TERM")), "dumb") {
		return false
	}
	if force := strings.TrimSpace(os.Getenv("SI_COLOR")); force != "" {
		return force == "1" || strings.EqualFold(force, "true")
	}
	if force := strings.TrimSpace(os.Getenv("CLICOLOR_FORCE")); force != "" && force != "0" {
		return true
	}
	return term.IsTerminal(int(os.Stdout.Fd()))
}

func ansi(codes ...string) string {
	return "\x1b[" + strings.Join(codes, ";") + "m"
}

func colorize(s string, codes ...string) string {
	if !ansiEnabled || s == "" {
		return s
	}
	return ansi(codes...) + s + ansi("0")
}

func styleHeading(s string) string { return colorize(s, "1", "36") }
func styleSection(s string) string { return colorize(s, "1", "34") }
func styleCmd(s string) string     { return colorize(s, "1", "32") }
func styleFlag(s string) string    { return colorize(s, "33") }
func styleArg(s string) string     { return colorize(s, "35") }
func styleDim(s string) string     { return colorize(s, "90") }
func styleInfo(s string) string    { return colorize(s, "36") }
func styleSuccess(s string) string { return colorize(s, "32") }
func styleWarn(s string) string    { return colorize(s, "33") }
func styleError(s string) string   { return colorize(s, "31") }
func styleUsage(s string) string   { return colorize(s, "1", "33") }

func styleStatus(s string) string {
	val := strings.ToLower(strings.TrimSpace(s))
	switch val {
	case "running", "ok", "ready", "done", "success", "yes", "true", "available", "up":
		return styleSuccess(s)
	case "blocked", "warning", "warn", "pending":
		return styleWarn(s)
	case "failed", "error", "missing", "stopped", "exited", "not found", "no", "false", "down":
		return styleError(s)
	default:
		return styleInfo(s)
	}
}

func printUsage(line string) {
	raw := strings.TrimSpace(line)
	if strings.HasPrefix(raw, "usage:") {
		rest := strings.TrimSpace(strings.TrimPrefix(raw, "usage:"))
		fmt.Printf("%s %s\n", styleUsage("usage:"), rest)
		return
	}
	fmt.Println(styleUsage(raw))
}

func printUnknown(kind, cmd string) {
	kind = strings.TrimSpace(kind)
	if kind != "" {
		kind = kind + " "
	}
	fmt.Fprintf(os.Stderr, "%s %s%s\n", styleError("unknown"), kind+"command:", styleCmd(cmd))
}

func warnf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if containsANSI(msg) {
		fmt.Fprintln(os.Stderr, styleWarn("warning:")+" "+msg)
		return
	}
	fmt.Fprintln(os.Stderr, styleWarn("warning:")+" "+msg)
}

func infof(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if containsANSI(msg) {
		fmt.Println(msg)
		return
	}
	fmt.Println(styleInfo(msg))
}

func successf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if containsANSI(msg) {
		fmt.Println(msg)
		return
	}
	fmt.Println(styleSuccess(msg))
}

func colorizeHelp(text string) string {
	if !ansiEnabled {
		return text
	}
	sectionRe := regexp.MustCompile(`^[A-Za-z][A-Za-z0-9 /-]*:$`)
	cmdRe := regexp.MustCompile(`\\b(si|dyad|codex|docker|images|image|profile|capability)\\b`)
	flagRe := regexp.MustCompile(`--[a-zA-Z0-9-]+`)
	shortFlagRe := regexp.MustCompile(`(^|\\s)(-[a-zA-Z])\\b`)
	argRe := regexp.MustCompile(`<[^>]+>`)
	dividerRe := regexp.MustCompile(`^-{3,}$`)

	lines := strings.Split(text, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if dividerRe.MatchString(trimmed) {
			lines[i] = indentLine(line, styleDim(trimmed))
			continue
		}
		if sectionRe.MatchString(trimmed) {
			lines[i] = indentLine(line, styleHeading(trimmed))
			continue
		}
		if strings.HasPrefix(trimmed, "Usage:") || strings.HasPrefix(trimmed, "Features:") || strings.HasPrefix(trimmed, "Core:") || strings.HasPrefix(trimmed, "Build:") || strings.HasPrefix(trimmed, "Profiles:") || strings.HasPrefix(trimmed, "Command details") || strings.HasPrefix(trimmed, "Environment defaults") {
			lines[i] = indentLine(line, styleHeading(trimmed))
			continue
		}
		if strings.HasPrefix(strings.ToLower(trimmed), "usage:") {
			parts := strings.SplitN(trimmed, ":", 2)
			if len(parts) == 2 {
				lines[i] = indentLine(line, styleUsage(parts[0]+":")+" "+strings.TrimSpace(parts[1]))
				continue
			}
		}
		line = flagRe.ReplaceAllStringFunc(line, styleFlag)
		line = shortFlagRe.ReplaceAllStringFunc(line, func(m string) string {
			trim := strings.TrimSpace(m)
			if trim == "" {
				return m
			}
			return strings.Replace(m, trim, styleFlag(trim), 1)
		})
		line = argRe.ReplaceAllStringFunc(line, styleArg)
		line = cmdRe.ReplaceAllStringFunc(line, styleCmd)
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

func indentLine(line, replacement string) string {
	prefix := line[:len(line)-len(strings.TrimLeft(line, " "))]
	return prefix + replacement
}

var ansiStripRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSIForPad(s string) string {
	return ansiStripRe.ReplaceAllString(s, "")
}

func padRightANSI(s string, width int) string {
	visible := len(stripANSIForPad(s))
	if visible >= width {
		return s
	}
	return s + strings.Repeat(" ", width-visible)
}

func containsANSI(s string) bool {
	return ansiStripRe.MatchString(s)
}
