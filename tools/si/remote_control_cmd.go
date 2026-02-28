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

const remoteControlUsageText = "usage: si remote-control [--repo <path>] [--bin <path>] [--build] [--json] -- <remote-control-args...>\n       si remote-control <remote-control-args...>\n       si rc <remote-control-args...>"
const remoteControlConfigUsageText = "usage: si remote-control config <show|set> [args]"

var runRemoteControlExternal = func(bin string, args []string) error {
	cmd := exec.Command(bin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func cmdRemoteControl(args []string) {
	if len(args) == 0 {
		printUsage(remoteControlUsageText)
		return
	}
	if strings.EqualFold(strings.TrimSpace(args[0]), "config") {
		cmdRemoteControlConfig(args[1:])
		return
	}
	if len(args) == 1 {
		head := strings.TrimSpace(strings.ToLower(args[0]))
		if head == "help" || head == "-h" || head == "--help" {
			printUsage(remoteControlUsageText)
			return
		}
	}

	fs := flag.NewFlagSet("remote-control", flag.ContinueOnError)
	fs.SetOutput(ioDiscardWriter{})
	repo := fs.String("repo", "", "remote-control repository path")
	bin := fs.String("bin", "", "remote-control binary path")
	build := fs.Bool("build", false, "build remote-control from repo before running")
	jsonOut := fs.Bool("json", false, "print wrapper metadata as json on failure")
	if err := fs.Parse(args); err != nil {
		printUsage(remoteControlUsageText)
		fatal(err)
	}
	buildFlagSet := remoteControlFlagProvided(fs, "build")
	rest := fs.Args()
	if len(rest) == 0 {
		printUsage(remoteControlUsageText)
		return
	}

	settings := loadSettingsOrDefault()
	resolvedRepo := strings.TrimSpace(*repo)
	if resolvedRepo == "" {
		resolvedRepo = strings.TrimSpace(settings.RemoteControl.Repo)
	}
	if resolvedRepo == "" {
		resolvedRepo = defaultRemoteControlRepoPath()
	}

	resolvedBin := strings.TrimSpace(*bin)
	if resolvedBin == "" {
		resolvedBin = strings.TrimSpace(settings.RemoteControl.Bin)
	}
	if resolvedBin == "" {
		resolvedBin = detectRemoteControlBinary(resolvedRepo)
	}

	buildRequested := *build
	if !buildFlagSet && settings.RemoteControl.Build != nil {
		buildRequested = *settings.RemoteControl.Build
	}
	if buildRequested {
		if err := buildRemoteControlBinary(resolvedRepo, resolvedBin); err != nil {
			if *jsonOut {
				printJSONMap(map[string]any{"ok": false, "error": err.Error(), "repo": resolvedRepo, "bin": resolvedBin})
			}
			fatal(err)
		}
	}

	if _, err := os.Stat(resolvedBin); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fatal(fmt.Errorf("remote-control binary not found at %s (use --build or --bin)", resolvedBin))
		}
		fatal(err)
	}
	if err := runRemoteControlExternal(resolvedBin, rest); err != nil {
		if *jsonOut {
			printJSONMap(map[string]any{"ok": false, "error": err.Error(), "repo": resolvedRepo, "bin": resolvedBin, "args": rest})
		}
		fatal(err)
	}
}

func cmdRemoteControlConfig(args []string) {
	if len(args) == 0 {
		printUsage(remoteControlConfigUsageText)
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "show", "get":
		cmdRemoteControlConfigShow(rest)
	case "set":
		cmdRemoteControlConfigSet(rest)
	default:
		fatal(fmt.Errorf("unknown remote-control config command: %s", sub))
	}
}

func cmdRemoteControlConfigShow(args []string) {
	fs := flag.NewFlagSet("remote-control config show", flag.ContinueOnError)
	fs.SetOutput(ioDiscardWriter{})
	jsonOut := fs.Bool("json", false, "output json")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		fatal(errors.New("usage: si remote-control config show [--json]"))
	}
	settings := loadSettingsOrDefault()
	if *jsonOut {
		printJSONMap(map[string]any{"ok": true, "remote_control": settings.RemoteControl})
		return
	}
	fmt.Printf("si remote-control config\n")
	fmt.Printf("  repo=%s\n", strings.TrimSpace(settings.RemoteControl.Repo))
	fmt.Printf("  bin=%s\n", strings.TrimSpace(settings.RemoteControl.Bin))
	if settings.RemoteControl.Build != nil {
		fmt.Printf("  build=%t\n", *settings.RemoteControl.Build)
	} else {
		fmt.Printf("  build=\n")
	}
}

func cmdRemoteControlConfigSet(args []string) {
	fs := flag.NewFlagSet("remote-control config set", flag.ContinueOnError)
	fs.SetOutput(ioDiscardWriter{})
	repo := fs.String("repo", "", "default remote-control repo path")
	bin := fs.String("bin", "", "default remote-control binary path")
	build := fs.String("build", "", "default build behavior (true|false)")
	jsonOut := fs.Bool("json", false, "output json")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		fatal(errors.New("usage: si remote-control config set [--repo <path>] [--bin <path>] [--build true|false] [--json]"))
	}
	settings := loadSettingsOrDefault()
	updated, err := applyRemoteControlConfigSet(&settings, remoteControlConfigSetInput{
		RepoProvided:  remoteControlFlagProvided(fs, "repo"),
		Repo:          strings.TrimSpace(*repo),
		BinProvided:   remoteControlFlagProvided(fs, "bin"),
		Bin:           strings.TrimSpace(*bin),
		BuildProvided: remoteControlFlagProvided(fs, "build"),
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
		printJSONMap(map[string]any{"ok": true, "remote_control": settings.RemoteControl})
		return
	}
	fmt.Println("si remote-control config set: updated")
}

type remoteControlConfigSetInput struct {
	RepoProvided bool
	Repo         string

	BinProvided bool
	Bin         string

	BuildProvided bool
	BuildRaw      string
}

func applyRemoteControlConfigSet(settings *Settings, in remoteControlConfigSetInput) (bool, error) {
	if settings == nil {
		return false, errors.New("settings is nil")
	}
	changed := false
	if in.RepoProvided {
		settings.RemoteControl.Repo = strings.TrimSpace(in.Repo)
		changed = true
	}
	if in.BinProvided {
		settings.RemoteControl.Bin = strings.TrimSpace(in.Bin)
		changed = true
	}
	if in.BuildProvided {
		if strings.TrimSpace(in.BuildRaw) == "" {
			settings.RemoteControl.Build = nil
			changed = true
		} else {
			parsed, err := strconv.ParseBool(strings.TrimSpace(in.BuildRaw))
			if err != nil {
				return false, fmt.Errorf("invalid --build value %q (expected true|false)", in.BuildRaw)
			}
			settings.RemoteControl.Build = boolPtr(parsed)
			changed = true
		}
	}
	return changed, nil
}

func remoteControlFlagProvided(fs *flag.FlagSet, name string) bool {
	found := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

func defaultRemoteControlRepoPath() string {
	if wd, err := os.Getwd(); err == nil {
		cand := filepath.Join(wd, "remote-control")
		if fi, err := os.Stat(cand); err == nil && fi.IsDir() {
			return cand
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		cand := filepath.Join(home, "Development", "remote-control")
		if fi, err := os.Stat(cand); err == nil && fi.IsDir() {
			return cand
		}
	}
	return ""
}

func detectRemoteControlBinary(repo string) string {
	if p, err := exec.LookPath("remote-control"); err == nil {
		return p
	}
	if strings.TrimSpace(repo) == "" {
		return "remote-control"
	}
	return filepath.Join(repo, "bin", "remote-control")
}

func buildRemoteControlBinary(repo, out string) error {
	if strings.TrimSpace(repo) == "" {
		return fmt.Errorf("--repo is required for --build")
	}
	if out == "remote-control" {
		out = filepath.Join(repo, "bin", "remote-control")
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	cmd := exec.Command("go", "build", "-buildvcs=false", "-o", out, "./cmd/remote-control")
	cmd.Dir = repo
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
