package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const usageText = `Usage: ./tools/test-install-si-docker.sh

Builds and runs installer smoke tests in Docker:
- root install/uninstall
- non-root install/uninstall

Environment overrides:
  SI_INSTALL_SMOKE_IMAGE
  SI_INSTALL_NONROOT_IMAGE
  SI_INSTALL_SOURCE_DIR
  SI_INSTALL_SMOKE_SKIP_NONROOT=1`

type config struct {
	RootDir      string
	SmokeImage   string
	NonrootImage string
	SkipNonroot  bool
	SourceDir    string
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	help, err := parseCLI(args)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%v\n", err)
		_, _ = fmt.Fprintln(stderr, "Run ./tools/test-install-si-docker.sh --help for usage.")
		return 1
	}
	if help {
		_, _ = fmt.Fprintln(stdout, usageText)
		return 0
	}

	rootDir, err := os.Getwd()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "resolve root dir: %v\n", err)
		return 1
	}
	cfg := loadConfig(rootDir, os.Getenv)

	if _, err := exec.LookPath("docker"); err != nil {
		_, _ = fmt.Fprintln(stderr, "SKIP: docker is not available; skipping Docker installer smoke tests")
		return 0
	}

	if _, err := os.Stat(filepath.Join(cfg.SourceDir, "tools", "install-si.sh")); err != nil {
		_, _ = fmt.Fprintf(stderr, "FAIL: installer not found under source dir: %s\n", cfg.SourceDir)
		return 1
	}

	fmt.Fprintf(stdout, "==> Build root smoke image: %s\n", cfg.SmokeImage)
	if err := dockerBuildImage(cfg.SmokeImage, filepath.Join(cfg.RootDir, "tools", "docker", "install-sh-smoke", "Dockerfile"), filepath.Join(cfg.RootDir, "tools", "docker", "install-sh-smoke"), stderr); err != nil {
		_, _ = fmt.Fprintf(stderr, "FAIL: build root smoke image: %v\n", err)
		return 1
	}

	fmt.Fprintln(stdout, "==> Run root installer smoke")
	if err := runCmd("docker", "run", "--rm", "-t", "-v", cfg.SourceDir+":/workspace/si:ro", "-e", "SI_INSTALL_SOURCE_DIR=/workspace/si", cfg.SmokeImage); err != nil {
		_, _ = fmt.Fprintf(stderr, "FAIL: run root installer smoke: %v\n", err)
		return 1
	}

	if cfg.SkipNonroot {
		fmt.Fprintln(stdout, "==> Skip non-root smoke (SI_INSTALL_SMOKE_SKIP_NONROOT=1)")
		return 0
	}

	fmt.Fprintf(stdout, "==> Build non-root smoke image: %s\n", cfg.NonrootImage)
	if err := dockerBuildImage(cfg.NonrootImage, filepath.Join(cfg.RootDir, "tools", "docker", "install-sh-nonroot", "Dockerfile"), filepath.Join(cfg.RootDir, "tools", "docker", "install-sh-nonroot"), stderr); err != nil {
		_, _ = fmt.Fprintf(stderr, "FAIL: build non-root smoke image: %v\n", err)
		return 1
	}

	fmt.Fprintln(stdout, "==> Run non-root installer smoke")
	if err := runCmd("docker", "run", "--rm", "-t", "-v", cfg.SourceDir+":/workspace/si:ro", "-e", "SI_INSTALL_SOURCE_DIR=/workspace/si", cfg.NonrootImage); err != nil {
		_, _ = fmt.Fprintf(stderr, "FAIL: run non-root installer smoke: %v\n", err)
		return 1
	}
	return 0
}

func parseCLI(args []string) (bool, error) {
	fs := flag.NewFlagSet("test-install-si-docker", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	help := fs.Bool("help", false, "show help")
	fs.BoolVar(help, "h", false, "show help")
	if err := fs.Parse(args); err != nil {
		return false, err
	}
	if *help {
		return true, nil
	}
	if fs.NArg() > 0 {
		return false, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	return false, nil
}

func loadConfig(rootDir string, getenv func(string) string) config {
	smokeImage := strings.TrimSpace(getenv("SI_INSTALL_SMOKE_IMAGE"))
	if smokeImage == "" {
		smokeImage = "si-install-smoke:local"
	}
	nonrootImage := strings.TrimSpace(getenv("SI_INSTALL_NONROOT_IMAGE"))
	if nonrootImage == "" {
		nonrootImage = "si-install-nonroot:local"
	}
	sourceDir := strings.TrimSpace(getenv("SI_INSTALL_SOURCE_DIR"))
	if sourceDir == "" {
		sourceDir = rootDir
	}
	skipNonroot := strings.TrimSpace(getenv("SI_INSTALL_SMOKE_SKIP_NONROOT")) == "1"
	return config{
		RootDir:      rootDir,
		SmokeImage:   smokeImage,
		NonrootImage: nonrootImage,
		SkipNonroot:  skipNonroot,
		SourceDir:    sourceDir,
	}
}

func dockerBuildImage(image, dockerfile, context string, stderr io.Writer) error {
	if hasDockerBuildx() {
		return runCmd("docker", "buildx", "build", "--load", "-t", image, "-f", dockerfile, context)
	}
	_, _ = fmt.Fprintln(stderr, "WARNING: docker buildx is not available; falling back to docker build")
	return runCmd("docker", "build", "-t", image, "-f", dockerfile, context)
}

func hasDockerBuildx() bool {
	cmd := exec.Command("docker", "buildx", "version")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run() == nil
}

func runCmd(name string, args ...string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("command required")
	}
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}
