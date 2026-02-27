package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const surfUsageText = "usage: si surf [--repo <path>] [--bin <path>] [--build] [--json] -- <surf-args...>\n       si surf <surf-args...>"
const surfConfigUsageText = "usage: si surf config <show|set> [args]"

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
	if strings.EqualFold(strings.TrimSpace(args[0]), "config") {
		cmdSurfConfig(args[1:])
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
	buildFlagSet := surfFlagProvided(fs, "build")
	rest := fs.Args()
	if len(rest) == 0 {
		printUsage(surfUsageText)
		return
	}
	settings := loadSettingsOrDefault()

	resolvedRepo := strings.TrimSpace(*repo)
	if resolvedRepo == "" {
		resolvedRepo = strings.TrimSpace(settings.Surf.Repo)
	}
	if resolvedRepo == "" {
		resolvedRepo = defaultSurfRepoPath()
	}
	resolvedBin := strings.TrimSpace(*bin)
	if resolvedBin == "" {
		resolvedBin = strings.TrimSpace(settings.Surf.Bin)
	}
	if resolvedBin == "" {
		resolvedBin = detectSurfBinary(resolvedRepo)
	}
	buildRequested := *build
	if !buildFlagSet && settings.Surf.Build != nil {
		buildRequested = *settings.Surf.Build
	}
	if buildRequested {
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
	env = applySurfSettingsEnv(settings, env)
	env = hydrateSurfEnvFromVault(settings, env)
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
	return ""
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

func hydrateSurfEnvFromVault(settings Settings, env []string) []string {
	env = ensureEnvFromVault(settings, env, "SURF_CLOUDFLARE_TUNNEL_TOKEN", []string{"SURF_CLOUDFLARE_TUNNEL_TOKEN", "CLOUDFLARE_TUNNEL_TOKEN"})
	env = ensureEnvFromVault(settings, env, "SURF_CLOUDFLARE_API_TOKEN", []string{"SURF_CLOUDFLARE_API_TOKEN", "CLOUDFLARE_API_TOKEN"})
	env = ensureEnvFromVault(settings, env, "SURF_VNC_PASSWORD", []string{"SURF_VNC_PASSWORD", "SI_BROWSER_VNC_PASSWORD"})
	return env
}

func applySurfSettingsEnv(settings Settings, env []string) []string {
	env = ensureEnvValue(env, "SURF_SETTINGS_FILE", settings.Surf.SettingsFile)
	env = ensureEnvValue(env, "SURF_STATE_DIR", settings.Surf.StateDir)
	env = ensureEnvValue(env, "SURF_TUNNEL_NAME", settings.Surf.Tunnel.Name)
	env = ensureEnvValue(env, "SURF_TUNNEL_MODE", settings.Surf.Tunnel.Mode)
	env = ensureEnvValue(env, "SURF_TUNNEL_VAULT_KEY", settings.Surf.Tunnel.VaultKey)
	return env
}

func ensureEnvValue(env []string, key, value string) []string {
	if strings.TrimSpace(value) == "" {
		return env
	}
	if envHasValue(env, key) {
		return env
	}
	return append(env, key+"="+strings.TrimSpace(value))
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

func surfFlagProvided(fs *flag.FlagSet, name string) bool {
	found := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

func cmdSurfConfig(args []string) {
	if len(args) == 0 {
		printUsage(surfConfigUsageText)
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "show", "get":
		cmdSurfConfigShow(rest)
	case "set":
		cmdSurfConfigSet(rest)
	default:
		fatal(fmt.Errorf("unknown surf config command: %s", sub))
	}
}

func cmdSurfConfigShow(args []string) {
	fs := flag.NewFlagSet("surf config show", flag.ContinueOnError)
	fs.SetOutput(ioDiscardWriter{})
	jsonOut := fs.Bool("json", false, "output json")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		fatal(errors.New("usage: si surf config show [--json]"))
	}
	settings := loadSettingsOrDefault()
	if *jsonOut {
		printJSONMap(map[string]any{"ok": true, "surf": settings.Surf})
		return
	}
	fmt.Printf("si surf config\n")
	fmt.Printf("  repo=%s\n", strings.TrimSpace(settings.Surf.Repo))
	fmt.Printf("  bin=%s\n", strings.TrimSpace(settings.Surf.Bin))
	if settings.Surf.Build != nil {
		fmt.Printf("  build=%t\n", *settings.Surf.Build)
	} else {
		fmt.Printf("  build=\n")
	}
	fmt.Printf("  settings_file=%s\n", strings.TrimSpace(settings.Surf.SettingsFile))
	fmt.Printf("  state_dir=%s\n", strings.TrimSpace(settings.Surf.StateDir))
	fmt.Printf("  tunnel_name=%s\n", strings.TrimSpace(settings.Surf.Tunnel.Name))
	fmt.Printf("  tunnel_mode=%s\n", strings.TrimSpace(settings.Surf.Tunnel.Mode))
	fmt.Printf("  tunnel_vault_key=%s\n", strings.TrimSpace(settings.Surf.Tunnel.VaultKey))
}

func cmdSurfConfigSet(args []string) {
	fs := flag.NewFlagSet("surf config set", flag.ContinueOnError)
	fs.SetOutput(ioDiscardWriter{})
	repo := fs.String("repo", "", "default surf repo path")
	bin := fs.String("bin", "", "default surf binary path")
	build := fs.String("build", "", "default build behavior (true|false)")
	settingsFile := fs.String("settings-file", "", "SURF_SETTINGS_FILE to pass into surf")
	stateDir := fs.String("state-dir", "", "SURF_STATE_DIR to pass into surf")
	tunnelName := fs.String("tunnel-name", "", "SURF_TUNNEL_NAME default")
	tunnelMode := fs.String("tunnel-mode", "", "SURF_TUNNEL_MODE default (quick|token)")
	tunnelVaultKey := fs.String("tunnel-vault-key", "", "SURF_TUNNEL_VAULT_KEY default")
	jsonOut := fs.Bool("json", false, "output json")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		fatal(errors.New("usage: si surf config set [--repo <path>] [--bin <path>] [--build true|false] [--settings-file <path>] [--state-dir <path>] [--tunnel-name <name>] [--tunnel-mode quick|token] [--tunnel-vault-key <key>] [--json]"))
	}
	settings := loadSettingsOrDefault()
	updated, err := applySurfConfigSet(&settings, surfConfigSetInput{
		RepoProvided:           surfFlagProvided(fs, "repo"),
		Repo:                   strings.TrimSpace(*repo),
		BinProvided:            surfFlagProvided(fs, "bin"),
		Bin:                    strings.TrimSpace(*bin),
		BuildProvided:          surfFlagProvided(fs, "build"),
		BuildRaw:               strings.TrimSpace(*build),
		SettingsFileProvided:   surfFlagProvided(fs, "settings-file"),
		SettingsFile:           strings.TrimSpace(*settingsFile),
		StateDirProvided:       surfFlagProvided(fs, "state-dir"),
		StateDir:               strings.TrimSpace(*stateDir),
		TunnelNameProvided:     surfFlagProvided(fs, "tunnel-name"),
		TunnelName:             strings.TrimSpace(*tunnelName),
		TunnelModeProvided:     surfFlagProvided(fs, "tunnel-mode"),
		TunnelMode:             strings.TrimSpace(*tunnelMode),
		TunnelVaultKeyProvided: surfFlagProvided(fs, "tunnel-vault-key"),
		TunnelVaultKey:         strings.TrimSpace(*tunnelVaultKey),
	})
	if err != nil {
		fatal(err)
	}
	if !updated {
		fatal(errors.New("no settings provided; use one or more --repo/--bin/--build/... flags"))
	}
	if err := saveSettings(settings); err != nil {
		fatal(err)
	}
	if *jsonOut {
		printJSONMap(map[string]any{"ok": true, "surf": settings.Surf})
		return
	}
	fmt.Println("si surf config set: updated")
}

type surfConfigSetInput struct {
	RepoProvided bool
	Repo         string

	BinProvided bool
	Bin         string

	BuildProvided bool
	BuildRaw      string

	SettingsFileProvided bool
	SettingsFile         string

	StateDirProvided bool
	StateDir         string

	TunnelNameProvided bool
	TunnelName         string

	TunnelModeProvided bool
	TunnelMode         string

	TunnelVaultKeyProvided bool
	TunnelVaultKey         string
}

func applySurfConfigSet(settings *Settings, in surfConfigSetInput) (bool, error) {
	if settings == nil {
		return false, errors.New("settings is nil")
	}
	changed := false
	if in.RepoProvided {
		settings.Surf.Repo = strings.TrimSpace(in.Repo)
		changed = true
	}
	if in.BinProvided {
		settings.Surf.Bin = strings.TrimSpace(in.Bin)
		changed = true
	}
	if in.BuildProvided {
		if strings.TrimSpace(in.BuildRaw) == "" {
			settings.Surf.Build = nil
			changed = true
		} else {
			parsed, err := strconv.ParseBool(strings.TrimSpace(in.BuildRaw))
			if err != nil {
				return false, fmt.Errorf("invalid --build value %q (expected true|false)", in.BuildRaw)
			}
			settings.Surf.Build = boolPtr(parsed)
			changed = true
		}
	}
	if in.SettingsFileProvided {
		settings.Surf.SettingsFile = strings.TrimSpace(in.SettingsFile)
		changed = true
	}
	if in.StateDirProvided {
		settings.Surf.StateDir = strings.TrimSpace(in.StateDir)
		changed = true
	}
	if in.TunnelNameProvided {
		settings.Surf.Tunnel.Name = strings.TrimSpace(in.TunnelName)
		changed = true
	}
	if in.TunnelModeProvided {
		mode := strings.ToLower(strings.TrimSpace(in.TunnelMode))
		if mode != "" && mode != "quick" && mode != "token" {
			return false, fmt.Errorf("invalid --tunnel-mode value %q (expected quick|token)", in.TunnelMode)
		}
		settings.Surf.Tunnel.Mode = mode
		changed = true
	}
	if in.TunnelVaultKeyProvided {
		settings.Surf.Tunnel.VaultKey = strings.TrimSpace(in.TunnelVaultKey)
		changed = true
	}
	return changed, nil
}
