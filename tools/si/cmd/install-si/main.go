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

const usageText = `Install the si CLI.

Usage:
  tools/install-si.sh
  tools/install-si.sh [flags]

Flags:
  --backend <local|sun>
  --sun-url <url>
  --sun-token <token>
  --sun-account <slug>
  --sun-auto-sync
  --no-sun-auto-sync
  --skip-sun-auth
  --source-dir <path>
  --repo <owner/repo>
  --repo-url <url>
  --ref <ref>
  --version <tag|latest>
  --install-dir <dir>
  --install-path <path>
  --force
  --uninstall
  --go-mode <auto|system>
  --go-version <ver>
  --build-tags <tags>
  --build-ldflags <flags>
  --link-go
  --no-link-go
  --with-buildx
  --no-buildx
  --os <linux|darwin>
  --arch <amd64|arm64>
  --tmp-dir <dir>
  -y, --yes
  --dry-run
  --quiet
  --no-path-hint
  -h, --help`

type config struct {
	backend      string
	sourceDir    string
	repo         string
	repoURL      string
	ref          string
	version      string
	installDir   string
	installPath  string
	force        bool
	uninstall    bool
	goMode       string
	goVersion    string
	buildTags    string
	buildLDFlags string
	osOverride   string
	archOverride string
	tmpDir       string
	dryRun       bool
	quiet        bool
	noPathHint   bool
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	cfg, showHelp, err := parseArgs(args)
	if showHelp {
		_, _ = fmt.Fprintln(stdout, usageText)
		return 0
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}

	installPath, err := resolveInstallPath(cfg)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}

	if cfg.uninstall {
		if cfg.dryRun {
			if !cfg.quiet {
				_, _ = fmt.Fprintf(stdout, "dry-run: uninstall %s\n", installPath)
			}
			return 0
		}
		if err := os.Remove(installPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			_, _ = fmt.Fprintf(stderr, "uninstall failed: %v\n", err)
			return 1
		}
		return 0
	}

	if cfg.backend != "local" && cfg.backend != "sun" {
		_, _ = fmt.Fprintf(stderr, "invalid --backend %s (expected local or sun)\n", cfg.backend)
		return 1
	}

	sourceDir, cleanup, err := resolveSourceDir(cfg)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}

	goBin, err := ensureGoToolchain(cfg)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	if cfg.dryRun {
		if !cfg.quiet {
			_, _ = fmt.Fprintf(stdout, "go: using system go (%s)\n", goBin)
		}
		return 0
	}

	if err := ensureInstallWritable(installPath, cfg.force); err != nil {
		_, _ = fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}

	tmpOut, err := os.CreateTemp(filepath.Dir(installPath), "si-build-*")
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "create temp output: %v\n", err)
		return 1
	}
	tmpOutPath := tmpOut.Name()
	_ = tmpOut.Close()
	defer os.Remove(tmpOutPath)

	buildArgs := []string{"build", "-trimpath", "-buildvcs=false", "-o", tmpOutPath}
	if strings.TrimSpace(cfg.buildTags) != "" {
		buildArgs = append(buildArgs, "-tags", strings.TrimSpace(cfg.buildTags))
	}
	if strings.TrimSpace(cfg.buildLDFlags) != "" {
		buildArgs = append(buildArgs, "-ldflags", strings.TrimSpace(cfg.buildLDFlags))
	}
	buildArgs = append(buildArgs, "./tools/si")
	cmd := exec.Command(goBin, buildArgs...)
	cmd.Dir = sourceDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		_, _ = fmt.Fprintf(stderr, "build failed: %v\n", err)
		return 1
	}

	if err := os.MkdirAll(filepath.Dir(installPath), 0o755); err != nil {
		_, _ = fmt.Fprintf(stderr, "create install dir: %v\n", err)
		return 1
	}
	if err := os.Chmod(tmpOutPath, 0o755); err != nil {
		_, _ = fmt.Fprintf(stderr, "chmod built binary: %v\n", err)
		return 1
	}
	if err := os.Rename(tmpOutPath, installPath); err != nil {
		_, _ = fmt.Fprintf(stderr, "install failed: %v\n", err)
		return 1
	}

	if !cfg.noPathHint {
		warnIfPathMissing(filepath.Dir(installPath), stderr)
	}
	return 0
}

func parseArgs(args []string) (config, bool, error) {
	cfg := config{
		backend:      "local",
		repo:         "Aureuma/si",
		ref:          "main",
		goMode:       "auto",
		buildLDFlags: "-s -w",
	}
	fs := flag.NewFlagSet("install-si", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	fs.StringVar(&cfg.backend, "backend", cfg.backend, "backend")
	_ = fs.String("sun-url", "", "sun url")
	_ = fs.String("sun-token", "", "sun token")
	_ = fs.String("sun-account", "", "sun account")
	sunAutoSync := fs.Bool("sun-auto-sync", true, "sun auto sync")
	noSunAutoSync := fs.Bool("no-sun-auto-sync", false, "disable sun auto sync")
	_ = fs.Bool("skip-sun-auth", false, "skip sun auth")
	fs.StringVar(&cfg.sourceDir, "source-dir", "", "source dir")
	fs.StringVar(&cfg.repo, "repo", cfg.repo, "repo")
	fs.StringVar(&cfg.repoURL, "repo-url", "", "repo url")
	fs.StringVar(&cfg.ref, "ref", cfg.ref, "ref")
	fs.StringVar(&cfg.version, "version", "", "version")
	fs.StringVar(&cfg.installDir, "install-dir", "", "install dir")
	fs.StringVar(&cfg.installPath, "install-path", "", "install path")
	fs.BoolVar(&cfg.force, "force", false, "force")
	fs.BoolVar(&cfg.uninstall, "uninstall", false, "uninstall")
	fs.StringVar(&cfg.goMode, "go-mode", cfg.goMode, "go mode")
	fs.StringVar(&cfg.goVersion, "go-version", "", "go version")
	fs.StringVar(&cfg.buildTags, "build-tags", "", "build tags")
	fs.StringVar(&cfg.buildLDFlags, "build-ldflags", cfg.buildLDFlags, "build ldflags")
	_ = fs.Bool("link-go", false, "link go")
	_ = fs.Bool("no-link-go", false, "disable link go")
	_ = fs.Bool("with-buildx", false, "with buildx")
	_ = fs.Bool("no-buildx", false, "no buildx")
	fs.StringVar(&cfg.osOverride, "os", "", "os override")
	fs.StringVar(&cfg.archOverride, "arch", "", "arch override")
	fs.StringVar(&cfg.tmpDir, "tmp-dir", "", "tmp dir")
	_ = fs.Bool("yes", false, "assume yes")
	_ = fs.Bool("y", false, "assume yes")
	fs.BoolVar(&cfg.dryRun, "dry-run", false, "dry run")
	fs.BoolVar(&cfg.quiet, "quiet", false, "quiet")
	fs.BoolVar(&cfg.noPathHint, "no-path-hint", false, "no path hint")
	help := fs.Bool("help", false, "help")
	fs.BoolVar(help, "h", false, "help")

	if err := fs.Parse(args); err != nil {
		return config{}, false, err
	}
	if *help {
		return config{}, true, nil
	}
	if fs.NArg() > 0 {
		return config{}, false, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if *noSunAutoSync {
		*sunAutoSync = false
	}
	_ = sunAutoSync

	cfg.backend = strings.ToLower(strings.TrimSpace(cfg.backend))
	cfg.sourceDir = strings.TrimSpace(cfg.sourceDir)
	cfg.repoURL = strings.TrimSpace(cfg.repoURL)
	cfg.ref = strings.TrimSpace(cfg.ref)
	cfg.goMode = strings.ToLower(strings.TrimSpace(cfg.goMode))
	cfg.osOverride = strings.ToLower(strings.TrimSpace(cfg.osOverride))
	cfg.archOverride = strings.ToLower(strings.TrimSpace(cfg.archOverride))
	if cfg.installDir != "" && cfg.installPath != "" {
		return config{}, false, errors.New("--install-dir and --install-path are mutually exclusive")
	}
	if cfg.goMode != "auto" && cfg.goMode != "system" {
		return config{}, false, fmt.Errorf("invalid --go-mode %s (expected auto or system)", cfg.goMode)
	}
	if (cfg.osOverride != "" || cfg.archOverride != "") && !cfg.dryRun {
		return config{}, false, errors.New("--os/--arch overrides require --dry-run")
	}
	if cfg.osOverride != "" && cfg.osOverride != "linux" && cfg.osOverride != "darwin" {
		return config{}, false, fmt.Errorf("invalid --os %s (expected linux or darwin)", cfg.osOverride)
	}
	if cfg.archOverride != "" {
		switch cfg.archOverride {
		case "amd64", "x86_64":
			cfg.archOverride = "amd64"
		case "arm64", "aarch64":
			cfg.archOverride = "arm64"
		default:
			return config{}, false, fmt.Errorf("invalid --arch %s (expected amd64 or arm64)", cfg.archOverride)
		}
	}
	return cfg, false, nil
}

func resolveInstallPath(cfg config) (string, error) {
	if strings.TrimSpace(cfg.installPath) != "" {
		return filepath.Clean(strings.TrimSpace(cfg.installPath)), nil
	}
	installDir := strings.TrimSpace(cfg.installDir)
	if installDir == "" {
		if os.Geteuid() == 0 {
			installDir = "/usr/local/bin"
		} else {
			home, err := os.UserHomeDir()
			if err != nil || strings.TrimSpace(home) == "" {
				return "", errors.New("unable to resolve home directory for default install path")
			}
			installDir = filepath.Join(home, ".local", "bin")
		}
	}
	return filepath.Join(installDir, "si"), nil
}

func resolveSourceDir(cfg config) (string, func(), error) {
	if strings.TrimSpace(cfg.sourceDir) != "" {
		path := filepath.Clean(strings.TrimSpace(cfg.sourceDir))
		if err := validateSourceDir(path); err != nil {
			return "", nil, err
		}
		return path, nil, nil
	}
	if strings.TrimSpace(cfg.repoURL) != "" {
		path := filepath.Clean(strings.TrimSpace(cfg.repoURL))
		if fi, err := os.Stat(path); err == nil && fi.IsDir() {
			if err := validateSourceDir(path); err != nil {
				return "", nil, err
			}
			return path, nil, nil
		}
		tmp, err := os.MkdirTemp("", "si-install-clone-*")
		if err != nil {
			return "", nil, err
		}
		cleanup := func() { _ = os.RemoveAll(tmp) }
		if err := runGitClone(path, cfg.ref, tmp); err != nil {
			cleanup()
			return "", nil, err
		}
		if err := validateSourceDir(tmp); err != nil {
			cleanup()
			return "", nil, err
		}
		return tmp, cleanup, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", nil, err
	}
	if err := validateSourceDir(cwd); err == nil {
		return cwd, nil, nil
	}
	return "", nil, errors.New("source directory required; pass --source-dir")
}

func runGitClone(repoURL, ref, dst string) error {
	if _, err := exec.LookPath("git"); err != nil {
		return errors.New("git is required to clone repository")
	}
	clone := exec.Command("git", "clone", repoURL, dst)
	clone.Stdout = os.Stdout
	clone.Stderr = os.Stderr
	if err := clone.Run(); err != nil {
		return err
	}
	if strings.TrimSpace(ref) != "" {
		checkout := exec.Command("git", "-C", dst, "checkout", ref)
		checkout.Stdout = os.Stdout
		checkout.Stderr = os.Stderr
		if err := checkout.Run(); err != nil {
			return err
		}
	}
	return nil
}

func validateSourceDir(path string) error {
	fi, err := os.Stat(path)
	if err != nil || !fi.IsDir() {
		return fmt.Errorf("source directory not found: %s", path)
	}
	if _, err := os.Stat(filepath.Join(path, "tools", "si", "go.mod")); err != nil {
		return fmt.Errorf("source directory is not an si checkout: %s", path)
	}
	return nil
}

func ensureGoToolchain(cfg config) (string, error) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		if cfg.goMode == "system" {
			return "", errors.New("go is required for --go-mode system")
		}
		return "", errors.New("go toolchain not found on PATH")
	}
	probe := exec.Command(goBin, "version")
	probe.Stdout = io.Discard
	probe.Stderr = io.Discard
	if err := probe.Run(); err != nil {
		if cfg.goMode == "system" {
			return "", errors.New("go is required for --go-mode system")
		}
		return "", errors.New("go toolchain probe failed")
	}
	return goBin, nil
}

func ensureInstallWritable(installPath string, force bool) error {
	if fi, err := os.Stat(installPath); err == nil {
		if fi.IsDir() {
			return fmt.Errorf("install path is a directory: %s", installPath)
		}
		if !force {
			return fmt.Errorf("install target already exists: %s (use --force)", installPath)
		}
	}
	if err := os.MkdirAll(filepath.Dir(installPath), 0o755); err != nil {
		return err
	}
	probePath := filepath.Join(filepath.Dir(installPath), ".si-write-test")
	if err := os.WriteFile(probePath, []byte("ok"), 0o600); err != nil {
		return err
	}
	_ = os.Remove(probePath)
	return nil
}

func warnIfPathMissing(dir string, stderr io.Writer) {
	pathEntries := strings.Split(os.Getenv("PATH"), string(os.PathListSeparator))
	dir = filepath.Clean(strings.TrimSpace(dir))
	for _, entry := range pathEntries {
		if filepath.Clean(strings.TrimSpace(entry)) == dir {
			return
		}
	}
	_, _ = fmt.Fprintf(stderr, "WARNING: install dir is not on PATH for this shell: %s\n", dir)
}
