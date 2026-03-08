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

const fortUsageText = `usage: si fort [--repo <path>] [--bin <path>] [--build] [--json] -- <fort-args...>
       si fort [--repo <path>] [--bin <path>] [--build] [--json] <fort-subcommand> [fort-args...]
       si fort config <show|set> [args]

wrapper flags:
  --repo <path>  Fort repository path (build source)
  --bin <path>   Fort binary path
  --build        Build fort from repo before running
  --json         Print wrapper metadata as JSON on wrapper failure

token/auth model:
  - Host bootstrap admin auth resolves from FORT_TOKEN or FORT_TOKEN_FILE.
  - Runtime container auth uses file paths FORT_TOKEN_PATH and FORT_REFRESH_TOKEN_PATH.
  - This wrapper auto-refreshes runtime sessions when possible, then injects --token to fort.
  - FORT_TOKEN and FORT_REFRESH_TOKEN are sanitized from child process environment.

examples:
  si fort doctor
  si fort get --repo releasemind --env dev --key RM_OPENAI_API_KEY
  si fort -- --host https://fort.aureuma.ai doctor
  si fort config set --host https://fort.aureuma.ai --container-host https://fort.aureuma.ai`
const fortConfigUsageText = `usage: si fort config <show|set> [args]
       si fort config show [--json]
       si fort config set [--repo <path>] [--bin <path>] [--host <url>] [--container-host <url>] [--build true|false] [--json]`

var runFortExternal = func(bin string, args []string) error {
	cmd := exec.Command(bin, args...)
	cmd.Env = fortSanitizedEnv(os.Environ())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func fortSanitizedEnv(env []string) []string {
	if len(env) == 0 {
		return nil
	}
	out := make([]string, 0, len(env))
	for _, entry := range env {
		if strings.HasPrefix(entry, "FORT_TOKEN=") || strings.HasPrefix(entry, "FORT_REFRESH_TOKEN=") {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func fortArgsContainToken(args []string) bool {
	for i := 0; i < len(args); i++ {
		part := strings.TrimSpace(args[i])
		if part == "--token" {
			return true
		}
		if strings.HasPrefix(part, "--token=") {
			return true
		}
	}
	return false
}

func fortArgsWithToken(args []string, token string) []string {
	token = strings.TrimSpace(token)
	if token == "" || fortArgsContainToken(args) {
		return args
	}
	out := make([]string, 0, len(args)+2)
	out = append(out, "--token", token)
	out = append(out, args...)
	return out
}

func cmdFort(args []string) {
	if len(args) == 0 {
		printUsage(fortUsageText)
		return
	}
	head := strings.TrimSpace(strings.ToLower(args[0]))
	if head == "help" || head == "-h" || head == "--help" {
		printUsage(fortUsageText)
		return
	}
	if strings.EqualFold(strings.TrimSpace(args[0]), "config") {
		cmdFortConfig(args[1:])
		return
	}
	fs := flag.NewFlagSet("fort", flag.ContinueOnError)
	fs.SetOutput(ioDiscardWriter{})
	repo := fs.String("repo", "", "fort repository path")
	bin := fs.String("bin", "", "fort binary path")
	build := fs.Bool("build", false, "build fort from repo before running")
	jsonOut := fs.Bool("json", false, "print wrapper metadata as json on failure")
	if err := fs.Parse(args); err != nil {
		printUsage(fortUsageText)
		if strings.Contains(err.Error(), "flag provided but not defined") {
			fatal(fmt.Errorf("%w (if the flag belongs to fort, pass it after --, for example: si fort -- --token <token> auth whoami)", err))
		}
		fatal(err)
	}
	buildFlagSet := fortFlagProvided(fs, "build")
	rest := fs.Args()
	if len(rest) == 0 {
		printUsage(fortUsageText)
		return
	}
	settings := loadSettingsOrDefault()

	resolvedRepo := strings.TrimSpace(*repo)
	if resolvedRepo == "" {
		resolvedRepo = strings.TrimSpace(settings.Fort.Repo)
	}
	if resolvedRepo == "" {
		resolvedRepo = defaultFortRepoPath()
	}
	resolvedBin := strings.TrimSpace(*bin)
	if resolvedBin == "" {
		resolvedBin = strings.TrimSpace(settings.Fort.Bin)
	}
	if resolvedBin == "" {
		resolvedBin = detectFortBinary(resolvedRepo)
	}
	buildRequested := *build
	if !buildFlagSet && settings.Fort.Build != nil {
		buildRequested = *settings.Fort.Build
	}
	if strings.TrimSpace(os.Getenv("FORT_HOST")) == "" && strings.TrimSpace(settings.Fort.Host) != "" {
		_ = os.Setenv("FORT_HOST", strings.TrimSpace(settings.Fort.Host))
	}
	if strings.TrimSpace(os.Getenv("SI_FORT_HOST")) == "" && strings.TrimSpace(settings.Fort.Host) != "" {
		_ = os.Setenv("SI_FORT_HOST", strings.TrimSpace(settings.Fort.Host))
	}
	if strings.TrimSpace(os.Getenv("SI_FORT_CONTAINER_HOST")) == "" && strings.TrimSpace(settings.Fort.ContainerHost) != "" {
		_ = os.Setenv("SI_FORT_CONTAINER_HOST", strings.TrimSpace(settings.Fort.ContainerHost))
	}
	if buildRequested {
		if err := buildFortBinary(resolvedRepo, resolvedBin); err != nil {
			if *jsonOut {
				printJSONMap(map[string]any{"ok": false, "error": err.Error(), "repo": resolvedRepo, "bin": resolvedBin})
			}
			fatal(err)
		}
	}
	if _, err := os.Stat(resolvedBin); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fatal(fmt.Errorf("fort binary not found at %s (use --build or --bin)", resolvedBin))
		}
		fatal(err)
	}
	accessToken, err := prepareFortRuntimeAuth(rest)
	if err != nil {
		warnf("fort auth auto-refresh skipped: %v", err)
	}
	rest = fortArgsWithToken(rest, accessToken)

	if err := runFortExternal(resolvedBin, rest); err != nil {
		if *jsonOut {
			printJSONMap(map[string]any{"ok": false, "error": err.Error(), "repo": resolvedRepo, "bin": resolvedBin, "args": rest})
		}
		fatal(err)
	}
}

func defaultFortRepoPath() string {
	if wd, err := os.Getwd(); err == nil {
		cand := filepath.Join(wd, "fort")
		if fi, err := os.Stat(cand); err == nil && fi.IsDir() {
			return cand
		}
	}
	return ""
}

func detectFortBinary(repo string) string {
	if p, err := exec.LookPath("fort"); err == nil {
		return p
	}
	if strings.TrimSpace(repo) == "" {
		return "fort"
	}
	return filepath.Join(repo, "bin", "fort")
}

func buildFortBinary(repo, out string) error {
	if strings.TrimSpace(repo) == "" {
		return fmt.Errorf("--repo is required for --build")
	}
	if out == "fort" {
		out = filepath.Join(repo, "bin", "fort")
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	cmd := exec.Command("go", "build", "-o", out, "./cmd/fort")
	cmd.Dir = repo
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func fortFlagProvided(fs *flag.FlagSet, name string) bool {
	found := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

func cmdFortConfig(args []string) {
	if len(args) == 0 {
		printUsage(fortConfigUsageText)
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "show":
		cmdFortConfigShow(rest)
	case "set":
		cmdFortConfigSet(rest)
	default:
		fatal(fmt.Errorf("unknown fort config command: %s", sub))
	}
}

func cmdFortConfigShow(args []string) {
	fs := flag.NewFlagSet("fort config show", flag.ContinueOnError)
	fs.SetOutput(ioDiscardWriter{})
	jsonOut := fs.Bool("json", false, "output json")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		fatal(errors.New("usage: si fort config show [--json]"))
	}
	settings := loadSettingsOrDefault()
	if *jsonOut {
		printJSONMap(map[string]any{"ok": true, "fort": settings.Fort})
		return
	}
	fmt.Printf("si fort config\n")
	fmt.Printf("  repo=%s\n", strings.TrimSpace(settings.Fort.Repo))
	fmt.Printf("  bin=%s\n", strings.TrimSpace(settings.Fort.Bin))
	fmt.Printf("  host=%s\n", strings.TrimSpace(settings.Fort.Host))
	fmt.Printf("  container_host=%s\n", strings.TrimSpace(settings.Fort.ContainerHost))
	if settings.Fort.Build != nil {
		fmt.Printf("  build=%t\n", *settings.Fort.Build)
	} else {
		fmt.Printf("  build=\n")
	}
}

func cmdFortConfigSet(args []string) {
	fs := flag.NewFlagSet("fort config set", flag.ContinueOnError)
	fs.SetOutput(ioDiscardWriter{})
	repo := fs.String("repo", "", "default fort repo path")
	bin := fs.String("bin", "", "default fort binary path")
	host := fs.String("host", "", "default fort API host URL")
	containerHost := fs.String("container-host", "", "default fort API host URL used inside containers")
	build := fs.String("build", "", "default build behavior (true|false)")
	jsonOut := fs.Bool("json", false, "output json")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		fatal(errors.New("usage: si fort config set [--repo <path>] [--bin <path>] [--host <url>] [--container-host <url>] [--build true|false] [--json]"))
	}
	settings := loadSettingsOrDefault()
	updated, err := applyFortConfigSet(&settings, fortConfigSetInput{
		RepoProvided:          fortFlagProvided(fs, "repo"),
		Repo:                  strings.TrimSpace(*repo),
		BinProvided:           fortFlagProvided(fs, "bin"),
		Bin:                   strings.TrimSpace(*bin),
		HostProvided:          fortFlagProvided(fs, "host"),
		Host:                  strings.TrimSpace(*host),
		ContainerHostProvided: fortFlagProvided(fs, "container-host"),
		ContainerHost:         strings.TrimSpace(*containerHost),
		BuildProvided:         fortFlagProvided(fs, "build"),
		BuildRaw:              strings.TrimSpace(*build),
	})
	if err != nil {
		fatal(err)
	}
	if !updated {
		fatal(errors.New("no settings provided; use one or more --repo/--bin/--host/--container-host/--build flags"))
	}
	if err := saveSettings(settings); err != nil {
		fatal(err)
	}
	if *jsonOut {
		printJSONMap(map[string]any{"ok": true, "fort": settings.Fort})
		return
	}
	fmt.Println("si fort config set: updated")
}

type fortConfigSetInput struct {
	RepoProvided bool
	Repo         string

	BinProvided bool
	Bin         string

	HostProvided bool
	Host         string

	ContainerHostProvided bool
	ContainerHost         string

	BuildProvided bool
	BuildRaw      string
}

func applyFortConfigSet(settings *Settings, in fortConfigSetInput) (bool, error) {
	if settings == nil {
		return false, errors.New("settings is nil")
	}
	changed := false
	if in.RepoProvided {
		settings.Fort.Repo = strings.TrimSpace(in.Repo)
		changed = true
	}
	if in.BinProvided {
		settings.Fort.Bin = strings.TrimSpace(in.Bin)
		changed = true
	}
	if in.HostProvided {
		settings.Fort.Host = strings.TrimSpace(in.Host)
		changed = true
	}
	if in.ContainerHostProvided {
		settings.Fort.ContainerHost = strings.TrimSpace(in.ContainerHost)
		changed = true
	}
	if in.BuildProvided {
		if strings.TrimSpace(in.BuildRaw) == "" {
			settings.Fort.Build = nil
			changed = true
		} else {
			parsed, err := strconv.ParseBool(strings.TrimSpace(in.BuildRaw))
			if err != nil {
				return false, fmt.Errorf("invalid --build value %q (expected true|false)", in.BuildRaw)
			}
			settings.Fort.Build = boolPtr(parsed)
			changed = true
		}
	}
	return changed, nil
}
