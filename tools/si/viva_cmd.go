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

const vivaUsageText = "usage: si viva [--repo <path>] [--bin <path>] [--build] [--json] -- <viva-args...>\n       si viva <viva-args...>"

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
	rest := fs.Args()
	if len(rest) == 0 {
		printUsage(vivaUsageText)
		return
	}
	resolvedRepo := strings.TrimSpace(*repo)
	if resolvedRepo == "" {
		resolvedRepo = defaultVivaRepoPath()
	}
	resolvedBin := strings.TrimSpace(*bin)
	if resolvedBin == "" {
		resolvedBin = detectVivaBinary(resolvedRepo)
	}
	if *build {
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
