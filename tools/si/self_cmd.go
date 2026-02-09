package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func cmdSelfBuild(args []string) {
	fs := flag.NewFlagSet("self build", flag.ExitOnError)
	repo := fs.String("repo", "", "path inside si repo checkout")
	output := fs.String("output", "", "output binary path (default: <repo>/si)")
	goBin := fs.String("go-bin", "go", "go executable path")
	quiet := fs.Bool("quiet", false, "suppress go build command echo")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si build self [--repo <path>] [--output <path>] [--go-bin <path>] [--quiet]")
		return
	}
	root, err := resolveSelfRepoRoot(*repo)
	if err != nil {
		fatal(err)
	}
	target := strings.TrimSpace(*output)
	if target == "" {
		target = filepath.Join(root, "si")
	}
	if !filepath.IsAbs(target) {
		abs, absErr := filepath.Abs(target)
		if absErr == nil {
			target = abs
		}
	}
	if err := selfBuildBinary(root, target, strings.TrimSpace(*goBin), *quiet); err != nil {
		fatal(err)
	}
	successf("built si binary: %s", target)
}

func cmdSelfUpgrade(args []string) {
	fs := flag.NewFlagSet("self upgrade", flag.ExitOnError)
	repo := fs.String("repo", "", "path inside si repo checkout")
	installPath := fs.String("install-path", "", "install target path (default: current executable path)")
	goBin := fs.String("go-bin", "go", "go executable path")
	quiet := fs.Bool("quiet", false, "suppress go build command echo")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si build self upgrade [--repo <path>] [--install-path <path>] [--go-bin <path>] [--quiet]")
		return
	}
	root, err := resolveSelfRepoRoot(*repo)
	if err != nil {
		fatal(err)
	}
	target := strings.TrimSpace(*installPath)
	if target == "" {
		exe, exeErr := os.Executable()
		if exeErr == nil && strings.TrimSpace(exe) != "" {
			target = exe
		}
	}
	if target == "" {
		home, homeErr := os.UserHomeDir()
		if homeErr != nil || strings.TrimSpace(home) == "" {
			fatal(fmt.Errorf("cannot determine install path; set --install-path"))
		}
		target = filepath.Join(home, ".local", "bin", "si")
	}
	if !filepath.IsAbs(target) {
		abs, absErr := filepath.Abs(target)
		if absErr == nil {
			target = abs
		}
	}
	if err := selfBuildBinary(root, target, strings.TrimSpace(*goBin), *quiet); err != nil {
		fatal(err)
	}
	successf("upgraded si binary: %s", target)
}

func cmdSelfRun(args []string) {
	fs := flag.NewFlagSet("self run", flag.ExitOnError)
	repo := fs.String("repo", "", "path inside si repo checkout")
	goBin := fs.String("go-bin", "go", "go executable path")
	_ = fs.Parse(args)
	forward := fs.Args()
	if len(forward) > 0 && strings.TrimSpace(forward[0]) == "--" {
		forward = forward[1:]
	}
	root, err := resolveSelfRepoRoot(*repo)
	if err != nil {
		fatal(err)
	}
	goPath, err := exec.LookPath(strings.TrimSpace(*goBin))
	if err != nil {
		fatal(fmt.Errorf("go executable not found: %s", strings.TrimSpace(*goBin)))
	}
	runArgs := []string{"run", "-buildvcs=false", "./tools/si"}
	runArgs = append(runArgs, forward...)
	cmd := exec.Command(goPath, runArgs...)
	cmd.Dir = root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		fatal(err)
	}
}

func resolveSelfRepoRoot(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw != "" {
		if !filepath.IsAbs(raw) {
			abs, err := filepath.Abs(raw)
			if err != nil {
				return "", err
			}
			raw = abs
		}
		return repoRootFrom(raw)
	}
	if root, err := repoRoot(); err == nil {
		return root, nil
	}
	if root, err := repoRootFromExecutable(); err == nil {
		return root, nil
	}
	return "", fmt.Errorf("si repo root not found; run from the repo or pass --repo <path>")
}

func selfBuildBinary(root string, output string, goBin string, quiet bool) error {
	if strings.TrimSpace(root) == "" {
		return fmt.Errorf("repo root is required")
	}
	if strings.TrimSpace(output) == "" {
		return fmt.Errorf("output path is required")
	}
	if strings.TrimSpace(goBin) == "" {
		goBin = "go"
	}
	goPath, err := exec.LookPath(goBin)
	if err != nil {
		return fmt.Errorf("go executable not found: %s", goBin)
	}

	dir := filepath.Dir(output)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(dir, fmt.Sprintf(".si-build-%d-%d", os.Getpid(), time.Now().UnixNano()))
	defer os.Remove(tmp)

	buildArgs := []string{"build", "-buildvcs=false", "-o", tmp, "./tools/si"}
	if !quiet {
		infof("running: %s %s", goPath, strings.Join(buildArgs, " "))
	}
	cmd := exec.Command(goPath, buildArgs...)
	cmd.Dir = root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmp, output); err != nil {
		return err
	}
	return nil
}
