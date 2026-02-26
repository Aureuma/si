package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const surfUsageText = "usage: si surf [--repo <path>] [--bin <path>] [--build] [--json] -- <surf-args...>\n       si surf <surf-args...>"

var runSurfExternal = func(bin string, args []string, env []string) error {
	cmd := exec.Command(bin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = env
	return cmd.Run()
}

func cmdSurf(args []string) {
	if len(args) == 0 {
		printUsage(surfUsageText)
		return
	}
	if len(args) == 1 {
		head := strings.TrimSpace(strings.ToLower(args[0]))
		if head == "help" || head == "-h" || head == "--help" {
			printUsage(surfUsageText)
			return
		}
	}
	fs := flag.NewFlagSet("surf", flag.ContinueOnError)
	fs.SetOutput(ioDiscardWriter{})
	repo := fs.String("repo", "", "surf repository path")
	bin := fs.String("bin", "", "surf binary path")
	build := fs.Bool("build", false, "build surf from repo before running")
	jsonOut := fs.Bool("json", false, "print wrapper metadata as json on failure")
	if err := fs.Parse(args); err != nil {
		printUsage(surfUsageText)
		fatal(err)
	}
	rest := fs.Args()
	if len(rest) == 0 {
		printUsage(surfUsageText)
		return
	}

	resolvedRepo := strings.TrimSpace(*repo)
	if resolvedRepo == "" {
		resolvedRepo = defaultSurfRepoPath()
	}
	resolvedBin := strings.TrimSpace(*bin)
	if resolvedBin == "" {
		resolvedBin = detectSurfBinary(resolvedRepo)
	}
	if *build {
		if err := buildSurfBinary(resolvedRepo, resolvedBin); err != nil {
			if *jsonOut {
				printJSONMap(map[string]any{"ok": false, "error": err.Error(), "repo": resolvedRepo, "bin": resolvedBin})
			}
			fatal(err)
		}
	}
	if _, err := os.Stat(resolvedBin); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fatal(fmt.Errorf("surf binary not found at %s (use --build or --bin)", resolvedBin))
		}
		fatal(err)
	}

	env := append([]string{}, os.Environ()...)
	env = hydrateSurfEnvFromVault(env)
	if err := runSurfExternal(resolvedBin, rest, env); err != nil {
		if *jsonOut {
			printJSONMap(map[string]any{"ok": false, "error": err.Error(), "repo": resolvedRepo, "bin": resolvedBin, "args": rest})
		}
		fatal(err)
	}
}

func defaultSurfRepoPath() string {
	if wd, err := os.Getwd(); err == nil {
		cand := filepath.Join(wd, "surf")
		if fi, err := os.Stat(cand); err == nil && fi.IsDir() {
			return cand
		}
	}
	return filepath.Join("/home", "shawn", "Development", "surf")
}

func detectSurfBinary(repo string) string {
	if p, err := exec.LookPath("surf"); err == nil {
		return p
	}
	if strings.TrimSpace(repo) == "" {
		return "surf"
	}
	return filepath.Join(repo, "bin", "surf")
}

func buildSurfBinary(repo, out string) error {
	if strings.TrimSpace(repo) == "" {
		return fmt.Errorf("--repo is required for --build")
	}
	if out == "surf" {
		out = filepath.Join(repo, "bin", "surf")
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	cmd := exec.Command("go", "build", "-o", out, "./cmd/surf")
	cmd.Dir = repo
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func hydrateSurfEnvFromVault(env []string) []string {
	settings := loadSettingsOrDefault()
	env = ensureEnvFromVault(settings, env, "SURF_CLOUDFLARE_TUNNEL_TOKEN", []string{"SURF_CLOUDFLARE_TUNNEL_TOKEN", "CLOUDFLARE_TUNNEL_TOKEN"})
	env = ensureEnvFromVault(settings, env, "SURF_CLOUDFLARE_API_TOKEN", []string{"SURF_CLOUDFLARE_API_TOKEN", "CLOUDFLARE_API_TOKEN"})
	env = ensureEnvFromVault(settings, env, "SURF_VNC_PASSWORD", []string{"SURF_VNC_PASSWORD", "SI_BROWSER_VNC_PASSWORD"})
	return env
}

func ensureEnvFromVault(settings Settings, env []string, envName string, vaultKeys []string) []string {
	if envHasValue(env, envName) {
		return env
	}
	for _, key := range vaultKeys {
		if strings.TrimSpace(key) == "" {
			continue
		}
		value, ok := resolveVaultKeyValue(settings, key)
		if !ok || strings.TrimSpace(value) == "" {
			continue
		}
		return append(env, envName+"="+value)
	}
	return env
}

func envHasValue(env []string, key string) bool {
	prefix := key + "="
	for _, item := range env {
		if strings.HasPrefix(item, prefix) && strings.TrimSpace(strings.TrimPrefix(item, prefix)) != "" {
			return true
		}
	}
	return false
}
