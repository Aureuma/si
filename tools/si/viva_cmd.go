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

	"github.com/pelletier/go-toml/v2"
)

const vivaUsageText = "usage: si viva [--repo <path>] [--bin <path>] [--build] [--json] -- <viva-args...>\n       si viva <viva-args...>"
const vivaConfigUsageText = "usage: si viva config <show|set|tunnel> [args]"
const vivaConfigTunnelUsageText = "usage: si viva config tunnel <show|set-default|import|delete> [args]"

var runVivaExternal = func(bin string, args []string) error {
	cmd := exec.Command(bin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func cmdViva(args []string) {
	if len(args) == 0 {
		printUsage(vivaUsageText)
		return
	}
	if strings.EqualFold(strings.TrimSpace(args[0]), "config") {
		cmdVivaConfig(args[1:])
		return
	}
	if len(args) == 1 {
		head := strings.TrimSpace(strings.ToLower(args[0]))
		if head == "help" || head == "-h" || head == "--help" {
			printUsage(vivaUsageText)
			return
		}
	}
	fs := flag.NewFlagSet("viva", flag.ContinueOnError)
	fs.SetOutput(ioDiscardWriter{})
	repo := fs.String("repo", "", "viva repository path")
	bin := fs.String("bin", "", "viva binary path")
	build := fs.Bool("build", false, "build viva from repo before running")
	jsonOut := fs.Bool("json", false, "print wrapper metadata as json on failure")
	if err := fs.Parse(args); err != nil {
		printUsage(vivaUsageText)
		fatal(err)
	}
	buildFlagSet := vivaFlagProvided(fs, "build")
	rest := fs.Args()
	if len(rest) == 0 {
		printUsage(vivaUsageText)
		return
	}
	settings := loadSettingsOrDefault()
	resolvedRepo := strings.TrimSpace(*repo)
	if resolvedRepo == "" {
		resolvedRepo = strings.TrimSpace(settings.Viva.Repo)
	}
	if resolvedRepo == "" {
		resolvedRepo = defaultVivaRepoPath()
	}
	resolvedBin := strings.TrimSpace(*bin)
	if resolvedBin == "" {
		resolvedBin = strings.TrimSpace(settings.Viva.Bin)
	}
	if resolvedBin == "" {
		resolvedBin = detectVivaBinary(resolvedRepo)
	}
	buildRequested := *build
	if !buildFlagSet && settings.Viva.Build != nil {
		buildRequested = *settings.Viva.Build
	}
	if buildRequested {
		if err := buildVivaBinary(resolvedRepo, resolvedBin); err != nil {
			if *jsonOut {
				printJSONMap(map[string]any{"ok": false, "error": err.Error(), "repo": resolvedRepo, "bin": resolvedBin})
			}
			fatal(err)
		}
	}
	if _, err := os.Stat(resolvedBin); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fatal(fmt.Errorf("viva binary not found at %s (use --build or --bin)", resolvedBin))
		}
		fatal(err)
	}
	if err := runVivaExternal(resolvedBin, rest); err != nil {
		if *jsonOut {
			printJSONMap(map[string]any{"ok": false, "error": err.Error(), "repo": resolvedRepo, "bin": resolvedBin, "args": rest})
		}
		fatal(err)
	}
}

func cmdVivaConfig(args []string) {
	if len(args) == 0 {
		printUsage(vivaConfigUsageText)
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "show":
		cmdVivaConfigShow(rest)
	case "set":
		cmdVivaConfigSet(rest)
	case "tunnel":
		cmdVivaConfigTunnel(rest)
	default:
		fatal(fmt.Errorf("unknown viva config command: %s", sub))
	}
}

func cmdVivaConfigShow(args []string) {
	fs := flag.NewFlagSet("viva config show", flag.ContinueOnError)
	fs.SetOutput(ioDiscardWriter{})
	jsonOut := fs.Bool("json", false, "output json")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		fatal(errors.New("usage: si viva config show [--json]"))
	}
	settings := loadSettingsOrDefault()
	if *jsonOut {
		printJSONMap(map[string]any{"ok": true, "viva": settings.Viva})
		return
	}
	fmt.Printf("si viva config\n")
	fmt.Printf("  repo=%s\n", strings.TrimSpace(settings.Viva.Repo))
	fmt.Printf("  bin=%s\n", strings.TrimSpace(settings.Viva.Bin))
	if settings.Viva.Build != nil {
		fmt.Printf("  build=%t\n", *settings.Viva.Build)
	} else {
		fmt.Printf("  build=\n")
	}
	fmt.Printf("  tunnel.default_profile=%s\n", strings.TrimSpace(settings.Viva.Tunnel.DefaultProfile))
	fmt.Printf("  tunnel.profiles=%d\n", len(settings.Viva.Tunnel.Profiles))
}

func cmdVivaConfigSet(args []string) {
	fs := flag.NewFlagSet("viva config set", flag.ContinueOnError)
	fs.SetOutput(ioDiscardWriter{})
	repo := fs.String("repo", "", "default viva repo path")
	bin := fs.String("bin", "", "default viva binary path")
	build := fs.String("build", "", "default build behavior (true|false)")
	jsonOut := fs.Bool("json", false, "output json")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		fatal(errors.New("usage: si viva config set [--repo <path>] [--bin <path>] [--build true|false] [--json]"))
	}
	settings := loadSettingsOrDefault()
	updated, err := applyVivaConfigSet(&settings, vivaConfigSetInput{
		RepoProvided:  vivaFlagProvided(fs, "repo"),
		Repo:          strings.TrimSpace(*repo),
		BinProvided:   vivaFlagProvided(fs, "bin"),
		Bin:           strings.TrimSpace(*bin),
		BuildProvided: vivaFlagProvided(fs, "build"),
		BuildRaw:      strings.TrimSpace(*build),
	})
	if err != nil {
		fatal(err)
	}
	if !updated {
		fatal(errors.New("no settings provided; use one or more --repo/--bin/--build flags"))
	}
	if err := saveSettings(settings); err != nil {
		fatal(err)
	}
	if *jsonOut {
		printJSONMap(map[string]any{"ok": true, "viva": settings.Viva})
		return
	}
	fmt.Println("si viva config set: updated")
}

type vivaConfigSetInput struct {
	RepoProvided bool
	Repo         string

	BinProvided bool
	Bin         string

	BuildProvided bool
	BuildRaw      string
}

func applyVivaConfigSet(settings *Settings, in vivaConfigSetInput) (bool, error) {
	if settings == nil {
		return false, errors.New("settings is nil")
	}
	changed := false
	if in.RepoProvided {
		settings.Viva.Repo = strings.TrimSpace(in.Repo)
		changed = true
	}
	if in.BinProvided {
		settings.Viva.Bin = strings.TrimSpace(in.Bin)
		changed = true
	}
	if in.BuildProvided {
		if strings.TrimSpace(in.BuildRaw) == "" {
			settings.Viva.Build = nil
			changed = true
		} else {
			parsed, err := strconv.ParseBool(strings.TrimSpace(in.BuildRaw))
			if err != nil {
				return false, fmt.Errorf("invalid --build value %q (expected true|false)", in.BuildRaw)
			}
			settings.Viva.Build = boolPtr(parsed)
			changed = true
		}
	}
	return changed, nil
}

func vivaFlagProvided(fs *flag.FlagSet, name string) bool {
	found := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

func defaultVivaRepoPath() string {
	if wd, err := os.Getwd(); err == nil {
		cand := filepath.Join(wd, "viva")
		if fi, err := os.Stat(cand); err == nil && fi.IsDir() {
			return cand
		}
	}
	return filepath.Join("/home", "shawn", "Development", "viva")
}

func detectVivaBinary(repo string) string {
	if p, err := exec.LookPath("viva"); err == nil {
		return p
	}
	if strings.TrimSpace(repo) == "" {
		return "viva"
	}
	return filepath.Join(repo, "bin", "viva")
}

func buildVivaBinary(repo, out string) error {
	if strings.TrimSpace(repo) == "" {
		return fmt.Errorf("--repo is required for --build")
	}
	if out == "viva" {
		out = filepath.Join(repo, "bin", "viva")
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	cmd := exec.Command("go", "build", "-o", out, "./cmd/viva")
	cmd.Dir = repo
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

type ioDiscardWriter struct{}

func (ioDiscardWriter) Write(p []byte) (int, error) { return len(p), nil }

func printJSONMap(v map[string]any) {
	printJSON(v)
}

func cmdVivaConfigTunnel(args []string) {
	if len(args) == 0 {
		printUsage(vivaConfigTunnelUsageText)
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "show":
		cmdVivaConfigTunnelShow(rest)
	case "set-default":
		cmdVivaConfigTunnelSetDefault(rest)
	case "import":
		cmdVivaConfigTunnelImport(rest)
	case "delete":
		cmdVivaConfigTunnelDelete(rest)
	default:
		fatal(fmt.Errorf("unknown viva config tunnel command: %s", sub))
	}
}

func cmdVivaConfigTunnelShow(args []string) {
	fs := flag.NewFlagSet("viva config tunnel show", flag.ContinueOnError)
	fs.SetOutput(ioDiscardWriter{})
	profile := fs.String("profile", "", "profile name")
	jsonOut := fs.Bool("json", false, "output json")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		fatal(errors.New("usage: si viva config tunnel show [--profile <name>] [--json]"))
	}
	settings := loadSettingsOrDefault()
	target := strings.ToLower(strings.TrimSpace(*profile))
	if *jsonOut {
		if target == "" {
			printJSONMap(map[string]any{
				"ok":       true,
				"default":  settings.Viva.Tunnel.DefaultProfile,
				"profiles": settings.Viva.Tunnel.Profiles,
			})
			return
		}
		p, ok := settings.Viva.Tunnel.Profiles[target]
		if !ok {
			printJSONMap(map[string]any{"ok": false, "error": fmt.Sprintf("profile %q not found", target)})
			return
		}
		printJSONMap(map[string]any{"ok": true, "profile": target, "value": p})
		return
	}
	fmt.Printf("si viva tunnel config\n")
	fmt.Printf("  default_profile=%s\n", strings.TrimSpace(settings.Viva.Tunnel.DefaultProfile))
	if target != "" {
		p, ok := settings.Viva.Tunnel.Profiles[target]
		if !ok {
			fatal(fmt.Errorf("profile %q not found", target))
		}
		fmt.Printf("  profile=%s container=%s routes=%d vault_env_file=%s\n", target, p.ContainerName, len(p.Routes), p.VaultEnvFile)
		return
	}
	for key, p := range settings.Viva.Tunnel.Profiles {
		fmt.Printf("  profile=%s container=%s routes=%d vault_env_file=%s\n", key, p.ContainerName, len(p.Routes), p.VaultEnvFile)
	}
}

func cmdVivaConfigTunnelSetDefault(args []string) {
	fs := flag.NewFlagSet("viva config tunnel set-default", flag.ContinueOnError)
	fs.SetOutput(ioDiscardWriter{})
	profile := fs.String("profile", "", "profile name")
	jsonOut := fs.Bool("json", false, "output json")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		fatal(errors.New("usage: si viva config tunnel set-default --profile <name> [--json]"))
	}
	key := strings.ToLower(strings.TrimSpace(*profile))
	if key == "" {
		fatal(errors.New("--profile is required"))
	}
	settings := loadSettingsOrDefault()
	if _, ok := settings.Viva.Tunnel.Profiles[key]; !ok {
		fatal(fmt.Errorf("profile %q not found", key))
	}
	settings.Viva.Tunnel.DefaultProfile = key
	if err := saveSettings(settings); err != nil {
		fatal(err)
	}
	if *jsonOut {
		printJSONMap(map[string]any{"ok": true, "default": settings.Viva.Tunnel.DefaultProfile})
		return
	}
	fmt.Printf("si viva config tunnel set-default: %s\n", key)
}

func cmdVivaConfigTunnelDelete(args []string) {
	fs := flag.NewFlagSet("viva config tunnel delete", flag.ContinueOnError)
	fs.SetOutput(ioDiscardWriter{})
	profile := fs.String("profile", "", "profile name")
	jsonOut := fs.Bool("json", false, "output json")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		fatal(errors.New("usage: si viva config tunnel delete --profile <name> [--json]"))
	}
	key := strings.ToLower(strings.TrimSpace(*profile))
	if key == "" {
		fatal(errors.New("--profile is required"))
	}
	settings := loadSettingsOrDefault()
	if _, ok := settings.Viva.Tunnel.Profiles[key]; !ok {
		fatal(fmt.Errorf("profile %q not found", key))
	}
	delete(settings.Viva.Tunnel.Profiles, key)
	if settings.Viva.Tunnel.DefaultProfile == key {
		settings.Viva.Tunnel.DefaultProfile = ""
	}
	if err := saveSettings(settings); err != nil {
		fatal(err)
	}
	if *jsonOut {
		printJSONMap(map[string]any{"ok": true, "deleted": key})
		return
	}
	fmt.Printf("si viva config tunnel delete: %s\n", key)
}

func cmdVivaConfigTunnelImport(args []string) {
	fs := flag.NewFlagSet("viva config tunnel import", flag.ContinueOnError)
	fs.SetOutput(ioDiscardWriter{})
	profile := fs.String("profile", "", "profile name")
	file := fs.String("file", "", "legacy tunnel TOML file")
	setDefault := fs.Bool("set-default", false, "set imported profile as default")
	jsonOut := fs.Bool("json", false, "output json")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		fatal(errors.New("usage: si viva config tunnel import --profile <name> --file <path> [--set-default] [--json]"))
	}
	key := strings.ToLower(strings.TrimSpace(*profile))
	if key == "" {
		fatal(errors.New("--profile is required"))
	}
	legacyPath := strings.TrimSpace(*file)
	if legacyPath == "" {
		fatal(errors.New("--file is required"))
	}
	absLegacyPath, err := filepath.Abs(legacyPath)
	if err != nil {
		fatal(err)
	}
	imported, err := importLegacyTunnelProfile(absLegacyPath)
	if err != nil {
		fatal(err)
	}
	settings := loadSettingsOrDefault()
	if settings.Viva.Tunnel.Profiles == nil {
		settings.Viva.Tunnel.Profiles = map[string]VivaTunnelProfile{}
	}
	settings.Viva.Tunnel.Profiles[key] = imported
	if *setDefault || settings.Viva.Tunnel.DefaultProfile == "" {
		settings.Viva.Tunnel.DefaultProfile = key
	}
	if err := saveSettings(settings); err != nil {
		fatal(err)
	}
	if *jsonOut {
		printJSONMap(map[string]any{"ok": true, "profile": key, "default": settings.Viva.Tunnel.DefaultProfile, "value": imported})
		return
	}
	fmt.Printf("si viva config tunnel import: profile=%s file=%s\n", key, absLegacyPath)
}

type legacyTunnelConfig struct {
	Version int `toml:"version"`
	Tunnel  struct {
		Name              string `toml:"name"`
		ContainerName     string `toml:"container_name"`
		TunnelIDEnvKey    string `toml:"tunnel_id_env_key"`
		CredentialsEnvKey string `toml:"credentials_env_key"`
		MetricsAddr       string `toml:"metrics_addr"`
	} `toml:"tunnel"`
	Runtime struct {
		Image        string `toml:"image"`
		NetworkMode  string `toml:"network_mode"`
		NoAutoupdate bool   `toml:"no_autoupdate"`
		PullImage    bool   `toml:"pull_image"`
		Dir          string `toml:"dir"`
	} `toml:"runtime"`
	Vault struct {
		EnvFile string `toml:"env_file"`
		Repo    string `toml:"repo"`
		Env     string `toml:"env"`
	} `toml:"vault"`
	Routes []VivaTunnelRoute `toml:"routes"`
}

func importLegacyTunnelProfile(path string) (VivaTunnelProfile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return VivaTunnelProfile{}, err
	}
	var legacy legacyTunnelConfig
	if err := toml.Unmarshal(data, &legacy); err != nil {
		return VivaTunnelProfile{}, err
	}
	baseDir := filepath.Dir(path)
	p := VivaTunnelProfile{
		Name:              strings.TrimSpace(legacy.Tunnel.Name),
		ContainerName:     strings.TrimSpace(legacy.Tunnel.ContainerName),
		TunnelIDEnvKey:    strings.TrimSpace(legacy.Tunnel.TunnelIDEnvKey),
		CredentialsEnvKey: strings.TrimSpace(legacy.Tunnel.CredentialsEnvKey),
		MetricsAddr:       strings.TrimSpace(legacy.Tunnel.MetricsAddr),
		Image:             strings.TrimSpace(legacy.Runtime.Image),
		NetworkMode:       strings.TrimSpace(legacy.Runtime.NetworkMode),
		NoAutoupdate:      boolPtr(legacy.Runtime.NoAutoupdate),
		PullImage:         boolPtr(legacy.Runtime.PullImage),
		RuntimeDir:        resolveLegacyPath(baseDir, strings.TrimSpace(legacy.Runtime.Dir)),
		VaultEnvFile:      resolveLegacyPath(baseDir, strings.TrimSpace(legacy.Vault.EnvFile)),
		VaultRepo:         strings.TrimSpace(legacy.Vault.Repo),
		VaultEnv:          strings.TrimSpace(legacy.Vault.Env),
		Routes:            legacy.Routes,
	}
	return normalizeVivaTunnelProfile(p), nil
}

func resolveLegacyPath(baseDir string, path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(baseDir, path))
}
