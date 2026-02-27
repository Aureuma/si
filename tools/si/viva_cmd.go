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

const vivaUsageText = "usage: si viva [--repo <path>] [--bin <path>] [--build] [--json] -- <viva-args...>\n       si viva <viva-args...>"
const vivaConfigUsageText = "usage: si viva config <show|set> [args]"

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
