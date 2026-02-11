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
	noUpgrade := fs.Bool("no-upgrade", false, "build local binary artifact instead of upgrading installed si")
	output := fs.String("output", "", "output binary path for local builds (default: <repo>/si)")
	installPath := fs.String("install-path", "", "install target path when upgrading (default: host si path from PATH)")
	goBin := fs.String("go-bin", "go", "go executable path")
	quiet := fs.Bool("quiet", false, "suppress go build command echo")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si build self [--repo <path>] [--install-path <path>] [--no-upgrade] [--output <path>] [--go-bin <path>] [--quiet]")
		return
	}
	root, err := resolveSelfRepoRoot(*repo)
	if err != nil {
		fatal(err)
	}
	plan, err := resolveSelfBuildTarget(root, strings.TrimSpace(*installPath), strings.TrimSpace(*output), *noUpgrade)
	if err != nil {
		fatal(err)
	}
	if err := selfBuildBinary(root, plan.Target, strings.TrimSpace(*goBin), *quiet); err != nil {
		fatal(err)
	}
	if plan.Upgrade {
		successf("upgraded si binary: %s", plan.Target)
		return
	}
	successf("built si binary: %s", plan.Target)
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
	target, err := resolveSelfInstallPath(*installPath)
	if err != nil {
		fatal(err)
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
	// #nosec G204 -- goPath is resolved via exec.LookPath and args are constructed locally.
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
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	tmp := filepath.Join(dir, fmt.Sprintf(".si-build-%d-%d", os.Getpid(), time.Now().UnixNano()))
	defer os.Remove(tmp)

	buildArgs := []string{"build", "-buildvcs=false", "-o", tmp, "./tools/si"}
	if !quiet {
		infof("running: %s %s", goPath, strings.Join(buildArgs, " "))
	}
	// #nosec G204 -- goPath is resolved via exec.LookPath and args are constructed locally.
	cmd := exec.Command(goPath, buildArgs...)
	cmd.Dir = root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return err
	}
	// #nosec G302 -- built binary must be executable for subsequent rename/use.
	if err := os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmp, output); err != nil {
		return err
	}
	return nil
}

type selfBuildTarget struct {
	Target  string
	Upgrade bool
}

func resolveSelfBuildTarget(root string, installPath string, output string, noUpgrade bool) (selfBuildTarget, error) {
	if strings.TrimSpace(root) == "" {
		return selfBuildTarget{}, fmt.Errorf("repo root is required")
	}
	if strings.TrimSpace(installPath) != "" && (noUpgrade || strings.TrimSpace(output) != "") {
		return selfBuildTarget{}, fmt.Errorf("--install-path cannot be used with --no-upgrade or --output")
	}
	if noUpgrade || strings.TrimSpace(output) != "" {
		target := strings.TrimSpace(output)
		if target == "" {
			target = filepath.Join(root, "si")
		}
		return selfBuildTarget{Target: makeAbsPath(target), Upgrade: false}, nil
	}
	target, err := resolveSelfInstallPath(installPath)
	if err != nil {
		return selfBuildTarget{}, err
	}
	return selfBuildTarget{Target: target, Upgrade: true}, nil
}

func resolveSelfInstallPath(raw string) (string, error) {
	if strings.TrimSpace(raw) != "" {
		return makeAbsPath(raw), nil
	}
	if found, err := exec.LookPath("si"); err == nil && strings.TrimSpace(found) != "" {
		return makeAbsPath(found), nil
	}
	if exe, err := os.Executable(); err == nil && strings.TrimSpace(exe) != "" {
		return makeAbsPath(exe), nil
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return "", fmt.Errorf("cannot determine install path; set --install-path")
	}
	return filepath.Join(home, ".local", "bin", "si"), nil
}

func makeAbsPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		return path
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}
