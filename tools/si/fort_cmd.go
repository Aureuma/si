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

const fortUsageText = "usage: si fort [--repo <path>] [--bin <path>] [--build] [--json] -- <fort-args...>\n       si fort <fort-args...>"
const fortConfigUsageText = "usage: si fort config <show|set> [args]"

var runFortExternal = func(bin string, args []string) error {
	cmd := exec.Command(bin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func cmdFort(args []string) {
	if len(args) == 0 {
		printUsage(fortUsageText)
		return
	}
	if strings.EqualFold(strings.TrimSpace(args[0]), "config") {
		cmdFortConfig(args[1:])
		return
	}
	if len(args) == 1 {
		head := strings.TrimSpace(strings.ToLower(args[0]))
		if head == "help" || head == "-h" || head == "--help" {
			printUsage(fortUsageText)
			return
		}
	}
	fs := flag.NewFlagSet("fort", flag.ContinueOnError)
	fs.SetOutput(ioDiscardWriter{})
	repo := fs.String("repo", "", "fort repository path")
	bin := fs.String("bin", "", "fort binary path")
	build := fs.Bool("build", false, "build fort from repo before running")
	jsonOut := fs.Bool("json", false, "print wrapper metadata as json on failure")
	if err := fs.Parse(args); err != nil {
		printUsage(fortUsageText)
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
	if err := prepareFortRuntimeAuth(rest); err != nil {
		warnf("fort auth auto-refresh skipped: %v", err)
	}

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
	build := fs.String("build", "", "default build behavior (true|false)")
	jsonOut := fs.Bool("json", false, "output json")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		fatal(errors.New("usage: si fort config set [--repo <path>] [--bin <path>] [--build true|false] [--json]"))
	}
	settings := loadSettingsOrDefault()
	updated, err := applyFortConfigSet(&settings, fortConfigSetInput{
		RepoProvided:  fortFlagProvided(fs, "repo"),
		Repo:          strings.TrimSpace(*repo),
		BinProvided:   fortFlagProvided(fs, "bin"),
		Bin:           strings.TrimSpace(*bin),
		BuildProvided: fortFlagProvided(fs, "build"),
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
