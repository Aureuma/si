package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
)

var versionPattern = regexp.MustCompile(`^const siVersion = "(.*)"$`)
var tagPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$`)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return nil
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]

	switch sub {
	case "build-cli-release-asset":
		return cmdBuildCLIReleaseAsset(rest)
	case "build-cli-release-assets":
		return cmdBuildCLIReleaseAssets(rest)
	case "validate-release-version":
		return cmdValidateReleaseVersion(rest)
	case "npm-build-package":
		return cmdNPMBuildPackage(rest)
	case "npm-publish-package":
		return cmdNPMPublishPackage(rest)
	case "npm-publish-from-vault":
		return cmdNPMPublishFromVault(rest)
	case "homebrew-render-core-formula":
		return cmdHomebrewRenderCoreFormula(rest)
	case "homebrew-render-tap-formula":
		return cmdHomebrewRenderTapFormula(rest)
	case "homebrew-update-tap-repo":
		return cmdHomebrewUpdateTapRepo(rest)
	case "-h", "--help", "help":
		printUsage()
		return nil
	default:
		return fmt.Errorf("unknown subcommand: %s", sub)
	}
}

func printUsage() {
	fmt.Println(`Usage: go run ./tools/si/cmd/releasectl <subcommand> [flags]

Subcommands:
  build-cli-release-asset
  build-cli-release-assets
  validate-release-version
  npm-build-package
  npm-publish-package
  npm-publish-from-vault
  homebrew-render-core-formula
  homebrew-render-tap-formula
  homebrew-update-tap-repo`)
}

type releaseAssetOptions struct {
	Version  string
	GOOS     string
	GOARCH   string
	GOARM    string
	OutDir   string
	RepoRoot string
}

func cmdBuildCLIReleaseAsset(args []string) error {
	fs := flag.NewFlagSet("build-cli-release-asset", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var opts releaseAssetOptions
	fs.StringVar(&opts.Version, "version", "", "version like vX.Y.Z")
	fs.StringVar(&opts.GOOS, "goos", "", "target os")
	fs.StringVar(&opts.GOARCH, "goarch", "", "target arch")
	fs.StringVar(&opts.GOARM, "goarm", "", "target arm version")
	fs.StringVar(&opts.OutDir, "out-dir", "", "output directory")
	fs.StringVar(&opts.RepoRoot, "repo-root", "", "repository root")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unknown argument: %s", fs.Arg(0))
	}
	if strings.TrimSpace(opts.RepoRoot) == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		opts.RepoRoot = cwd
	}
	if strings.TrimSpace(opts.OutDir) == "" {
		opts.OutDir = filepath.Join(opts.RepoRoot, "dist")
	}
	archivePath, err := buildReleaseAsset(opts)
	if err != nil {
		return err
	}
	fmt.Printf("created %s\n", archivePath)
	return nil
}

func cmdBuildCLIReleaseAssets(args []string) error {
	fs := flag.NewFlagSet("build-cli-release-assets", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	version := fs.String("version", "", "version like vX.Y.Z")
	repoRoot := fs.String("repo-root", "", "repository root")
	outDir := fs.String("out-dir", "", "output directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unknown argument: %s", fs.Arg(0))
	}

	resolvedRepo := strings.TrimSpace(*repoRoot)
	if resolvedRepo == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		resolvedRepo = cwd
	}

	resolvedVersion := strings.TrimSpace(*version)
	if resolvedVersion == "" {
		v, err := parseSIVersion(filepath.Join(resolvedRepo, "tools", "si", "version.go"))
		if err != nil {
			return err
		}
		resolvedVersion = v
	}
	if err := validateVersionWithPrefix(resolvedVersion); err != nil {
		return err
	}

	resolvedOut := strings.TrimSpace(*outDir)
	if resolvedOut == "" {
		resolvedOut = filepath.Join(resolvedRepo, "dist")
	}
	if err := os.MkdirAll(resolvedOut, 0o755); err != nil {
		return err
	}

	targets := []releaseAssetOptions{
		{Version: resolvedVersion, GOOS: "linux", GOARCH: "amd64", OutDir: resolvedOut, RepoRoot: resolvedRepo},
		{Version: resolvedVersion, GOOS: "linux", GOARCH: "arm64", OutDir: resolvedOut, RepoRoot: resolvedRepo},
		{Version: resolvedVersion, GOOS: "linux", GOARCH: "arm", GOARM: "7", OutDir: resolvedOut, RepoRoot: resolvedRepo},
		{Version: resolvedVersion, GOOS: "darwin", GOARCH: "amd64", OutDir: resolvedOut, RepoRoot: resolvedRepo},
		{Version: resolvedVersion, GOOS: "darwin", GOARCH: "arm64", OutDir: resolvedOut, RepoRoot: resolvedRepo},
	}

	for _, t := range targets {
		if _, err := buildReleaseAsset(t); err != nil {
			return err
		}
	}

	versionNoV := strings.TrimPrefix(resolvedVersion, "v")
	expected := []string{
		fmt.Sprintf("si_%s_linux_amd64.tar.gz", versionNoV),
		fmt.Sprintf("si_%s_linux_arm64.tar.gz", versionNoV),
		fmt.Sprintf("si_%s_linux_armv7.tar.gz", versionNoV),
		fmt.Sprintf("si_%s_darwin_amd64.tar.gz", versionNoV),
		fmt.Sprintf("si_%s_darwin_arm64.tar.gz", versionNoV),
	}
	for _, name := range expected {
		if _, err := os.Stat(filepath.Join(resolvedOut, name)); err != nil {
			return fmt.Errorf("missing expected release archive: %s", filepath.Join(resolvedOut, name))
		}
	}

	checksumsPath := filepath.Join(resolvedOut, "checksums.txt")
	f, err := os.Create(checksumsPath)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, name := range expected {
		h, err := sha256File(filepath.Join(resolvedOut, name))
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(f, "%s  %s\n", h, name); err != nil {
			return err
		}
	}

	fmt.Println("created release archives:")
	for _, name := range expected {
		fmt.Printf("  - %s\n", filepath.Join(resolvedOut, name))
	}
	fmt.Println("created checksums:")
	fmt.Printf("  - %s\n", checksumsPath)
	return nil
}

func cmdValidateReleaseVersion(args []string) error {
	fs := flag.NewFlagSet("validate-release-version", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	tag := fs.String("tag", "", "release tag")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unknown argument: %s", fs.Arg(0))
	}
	if strings.TrimSpace(*tag) == "" {
		return errors.New("--tag is required")
	}
	if !tagPattern.MatchString(strings.TrimSpace(*tag)) {
		return fmt.Errorf("tag must match vX.Y.Z (optionally with a prerelease/build suffix), got: %s", *tag)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	actual, err := parseSIVersion(filepath.Join(cwd, "tools", "si", "version.go"))
	if err != nil {
		return err
	}
	if actual != strings.TrimSpace(*tag) {
		return fmt.Errorf("tools/si/version.go has %s, but release tag is %s", actual, strings.TrimSpace(*tag))
	}
	fmt.Printf("release tag and tools/si/version.go are aligned (%s)\n", strings.TrimSpace(*tag))
	return nil
}

type npmBuildOptions struct {
	Version  string
	RepoRoot string
	OutDir   string
}

func cmdNPMBuildPackage(args []string) error {
	fs := flag.NewFlagSet("npm-build-package", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var opts npmBuildOptions
	fs.StringVar(&opts.Version, "version", "", "version like vX.Y.Z")
	fs.StringVar(&opts.RepoRoot, "repo-root", "", "repository root")
	fs.StringVar(&opts.OutDir, "out-dir", "", "output directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unknown argument: %s", fs.Arg(0))
	}
	if strings.TrimSpace(opts.RepoRoot) == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		opts.RepoRoot = cwd
	}
	if strings.TrimSpace(opts.OutDir) == "" {
		opts.OutDir = filepath.Join(opts.RepoRoot, "dist", "npm")
	}
	packFile, err := buildNPMPackage(opts)
	if err != nil {
		return err
	}
	fmt.Printf("created npm package: %s\n", packFile)
	return nil
}

type npmPublishOptions struct {
	Version  string
	RepoRoot string
	OutDir   string
	TokenEnv string
	DryRun   bool
}

func cmdNPMPublishPackage(args []string) error {
	fs := flag.NewFlagSet("npm-publish-package", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var opts npmPublishOptions
	fs.StringVar(&opts.Version, "version", "", "version like vX.Y.Z")
	fs.StringVar(&opts.RepoRoot, "repo-root", "", "repository root")
	fs.StringVar(&opts.OutDir, "out-dir", "", "output directory")
	fs.StringVar(&opts.TokenEnv, "token-env", "NPM_TOKEN", "token env var")
	fs.BoolVar(&opts.DryRun, "dry-run", false, "dry-run publish")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unknown argument: %s", fs.Arg(0))
	}
	if strings.TrimSpace(opts.RepoRoot) == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		opts.RepoRoot = cwd
	}
	if strings.TrimSpace(opts.OutDir) == "" {
		opts.OutDir = filepath.Join(opts.RepoRoot, "dist", "npm")
	}

	if opts.TokenEnv == "NPM_TOKEN" && strings.TrimSpace(os.Getenv("NPM_TOKEN")) == "" && strings.TrimSpace(os.Getenv("NPM_GAT_AUREUMA_VANGUARDA")) != "" {
		opts.TokenEnv = "NPM_GAT_AUREUMA_VANGUARDA"
	}

	resolvedVersion := strings.TrimSpace(opts.Version)
	if resolvedVersion == "" {
		v, err := parseSIVersion(filepath.Join(opts.RepoRoot, "tools", "si", "version.go"))
		if err != nil {
			return err
		}
		resolvedVersion = v
	}
	if err := validateVersionWithPrefix(resolvedVersion); err != nil {
		return err
	}
	npmVersion := strings.TrimPrefix(resolvedVersion, "v")

	exists, err := npmPackageExists(fmt.Sprintf("@aureuma/si@%s", npmVersion))
	if err != nil {
		return err
	}
	if exists {
		fmt.Printf("@aureuma/si@%s already published; skipping\n", npmVersion)
		return nil
	}

	packFile, err := buildNPMPackage(npmBuildOptions{Version: resolvedVersion, RepoRoot: opts.RepoRoot, OutDir: opts.OutDir})
	if err != nil {
		return err
	}

	token := strings.TrimSpace(os.Getenv(opts.TokenEnv))
	if token == "" {
		return fmt.Errorf("token environment variable %s is required", opts.TokenEnv)
	}
	npmrc, err := os.CreateTemp("", "si-npmrc-*")
	if err != nil {
		return err
	}
	npmrcPath := npmrc.Name()
	defer os.Remove(npmrcPath)
	if _, err := fmt.Fprintf(npmrc, "//registry.npmjs.org/:_authToken=%s\nalways-auth=true\n", token); err != nil {
		npmrc.Close()
		return err
	}
	if err := npmrc.Close(); err != nil {
		return err
	}

	env := append([]string{}, os.Environ()...)
	env = append(env, "NPM_CONFIG_USERCONFIG="+npmrcPath)
	publishArgs := []string{"publish", packFile, "--access", "public"}
	if opts.DryRun {
		publishArgs = append(publishArgs, "--dry-run")
	}
	if err := runCmd(opts.RepoRoot, env, "npm", publishArgs...); err != nil {
		return err
	}
	if opts.DryRun {
		fmt.Printf("dry-run complete: %s\n", packFile)
		return nil
	}

	if !waitForNPMPackage(fmt.Sprintf("@aureuma/si@%s", npmVersion), 18, 10*time.Second) {
		return errors.New("package publish appears to have failed verification")
	}
	fmt.Printf("published @aureuma/si@%s\n", npmVersion)
	return nil
}

func cmdNPMPublishFromVault(args []string) error {
	vaultFile := ""
	tokenEnv := "NPM_GAT_AUREUMA_VANGUARDA"
	passArgs := []string{}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--file":
			if i+1 >= len(args) {
				return errors.New("--file requires a value")
			}
			vaultFile = strings.TrimSpace(args[i+1])
			i++
		case "--token-env":
			if i+1 >= len(args) {
				return errors.New("--token-env requires a value")
			}
			tokenEnv = strings.TrimSpace(args[i+1])
			i++
		case "--":
			passArgs = append(passArgs, args[i+1:]...)
			i = len(args)
		default:
			passArgs = append(passArgs, arg)
		}
	}
	if tokenEnv == "" {
		return errors.New("--token-env must not be empty")
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	siCmd := filepath.Join(cwd, "si")
	if info, err := os.Stat(siCmd); err != nil || info.Mode()&0o111 == 0 {
		siCmd, err = exec.LookPath("si")
		if err != nil {
			return errors.New("si CLI not found (expected <repo>/si or si in PATH)")
		}
	}

	vaultArgs := []string{}
	if vaultFile != "" {
		vaultArgs = append(vaultArgs, "--file", vaultFile)
	}
	if err := runCmd(cwd, nil, siCmd, append([]string{"vault", "check"}, vaultArgs...)...); err != nil {
		return err
	}
	listOut, err := runCmdOutput(cwd, nil, siCmd, append([]string{"vault", "list"}, vaultArgs...)...)
	if err != nil {
		return err
	}
	if !vaultKeyExists(string(listOut), tokenEnv) {
		return fmt.Errorf("vault key %s not found", tokenEnv)
	}

	publishScript := filepath.Join(cwd, "tools", "release", "npm", "publish-npm-package.sh")
	cmdArgs := append([]string{"vault", "run"}, vaultArgs...)
	cmdArgs = append(cmdArgs, "--", publishScript, "--repo-root", cwd, "--token-env", tokenEnv)
	cmdArgs = append(cmdArgs, passArgs...)
	return runCmd(cwd, nil, siCmd, cmdArgs...)
}

func cmdHomebrewRenderCoreFormula(args []string) error {
	fs := flag.NewFlagSet("homebrew-render-core-formula", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	version := fs.String("version", "", "version like vX.Y.Z")
	output := fs.String("output", "", "output path")
	repo := fs.String("repo", "Aureuma/si", "repo owner/name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unknown argument: %s", fs.Arg(0))
	}
	if strings.TrimSpace(*version) == "" {
		return errors.New("--version is required")
	}
	if err := validateVersionWithPrefix(strings.TrimSpace(*version)); err != nil {
		return err
	}
	if strings.TrimSpace(*output) == "" {
		return errors.New("--output is required")
	}

	sourceURL := fmt.Sprintf("https://github.com/%s/archive/refs/tags/%s.tar.gz", strings.TrimSpace(*repo), strings.TrimSpace(*version))
	tmpFile, err := os.CreateTemp("", "si-homebrew-core-*.tar.gz")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	if err := downloadFile(sourceURL, tmpPath); err != nil {
		return err
	}
	h, err := sha256File(tmpPath)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(strings.TrimSpace(*output)), 0o755); err != nil {
		return err
	}
	content := fmt.Sprintf(`class Si < Formula
  desc "AI-first CLI for orchestrating coding agents and provider operations"
  homepage "https://github.com/%s"
  url "%s"
  sha256 "%s"
  license "AGPL-3.0-only"
  head "https://github.com/%s.git", branch: "main"

  depends_on "go" => :build

  def install
    system "go", "build", *std_go_args(ldflags: "-s -w"), "./tools/si"
  end

  test do
    output = shell_output("#{bin}/si version")
    assert_match "si version", output
  end
end
`, strings.TrimSpace(*repo), sourceURL, h, strings.TrimSpace(*repo))
	if err := os.WriteFile(strings.TrimSpace(*output), []byte(content), 0o644); err != nil {
		return err
	}
	fmt.Printf("rendered %s\n", strings.TrimSpace(*output))
	return nil
}

func cmdHomebrewRenderTapFormula(args []string) error {
	fs := flag.NewFlagSet("homebrew-render-tap-formula", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	version := fs.String("version", "", "version like vX.Y.Z")
	checksums := fs.String("checksums", "", "checksums file")
	output := fs.String("output", "", "output path")
	repo := fs.String("repo", "Aureuma/si", "repo owner/name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unknown argument: %s", fs.Arg(0))
	}
	return renderTapFormula(strings.TrimSpace(*version), strings.TrimSpace(*checksums), strings.TrimSpace(*output), strings.TrimSpace(*repo))
}

func cmdHomebrewUpdateTapRepo(args []string) error {
	fs := flag.NewFlagSet("homebrew-update-tap-repo", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	version := fs.String("version", "", "version like vX.Y.Z")
	checksums := fs.String("checksums", "", "checksums file")
	tapDir := fs.String("tap-dir", "", "tap checkout path")
	repo := fs.String("repo", "Aureuma/si", "repo owner/name")
	doCommit := fs.Bool("commit", false, "commit formula update")
	doPush := fs.Bool("push", false, "push formula update")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unknown argument: %s", fs.Arg(0))
	}

	versionVal := strings.TrimSpace(*version)
	checksumsVal := strings.TrimSpace(*checksums)
	tapDirVal := strings.TrimSpace(*tapDir)
	if versionVal == "" {
		return errors.New("--version is required")
	}
	if checksumsVal == "" {
		return errors.New("--checksums is required")
	}
	if tapDirVal == "" {
		return errors.New("--tap-dir is required")
	}
	if fi, err := os.Stat(tapDirVal); err != nil || !fi.IsDir() {
		return fmt.Errorf("tap dir does not exist: %s", tapDirVal)
	}

	formulaDir := filepath.Join(tapDirVal, "Formula")
	if err := os.MkdirAll(formulaDir, 0o755); err != nil {
		return err
	}
	formulaPath := filepath.Join(formulaDir, "si.rb")
	if err := renderTapFormula(versionVal, checksumsVal, formulaPath, strings.TrimSpace(*repo)); err != nil {
		return err
	}

	if !*doCommit {
		return nil
	}
	if err := runCmd(tapDirVal, nil, "git", "add", "Formula/si.rb"); err != nil {
		return err
	}
	diffCmd := exec.Command("git", "diff", "--cached", "--quiet")
	diffCmd.Dir = tapDirVal
	if err := diffCmd.Run(); err == nil {
		fmt.Println("no formula changes to commit")
		return nil
	} else {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			return err
		}
	}
	if err := runCmd(tapDirVal, nil, "git", "commit", "-m", fmt.Sprintf("chore: update si formula to %s", versionVal)); err != nil {
		return err
	}
	if *doPush {
		if err := runCmd(tapDirVal, nil, "git", "push"); err != nil {
			return err
		}
	}
	return nil
}

func renderTapFormula(version string, checksumsPath string, outputPath string, repo string) error {
	if version == "" {
		return errors.New("--version is required")
	}
	if err := validateVersionWithPrefix(version); err != nil {
		return err
	}
	if checksumsPath == "" {
		return errors.New("--checksums is required")
	}
	if _, err := os.Stat(checksumsPath); err != nil {
		return fmt.Errorf("checksums file not found: %s", checksumsPath)
	}
	if outputPath == "" {
		return errors.New("--output is required")
	}
	versionNoV := strings.TrimPrefix(version, "v")

	assetDarwinARM64 := fmt.Sprintf("si_%s_darwin_arm64.tar.gz", versionNoV)
	assetDarwinAMD64 := fmt.Sprintf("si_%s_darwin_amd64.tar.gz", versionNoV)
	assetLinuxARM64 := fmt.Sprintf("si_%s_linux_arm64.tar.gz", versionNoV)
	assetLinuxAMD64 := fmt.Sprintf("si_%s_linux_amd64.tar.gz", versionNoV)

	shaByAsset, err := parseChecksumsFile(checksumsPath)
	if err != nil {
		return err
	}
	lookup := func(name string) (string, error) {
		sha, ok := shaByAsset[name]
		if !ok || sha == "" {
			return "", fmt.Errorf("checksum not found for %s", name)
		}
		return sha, nil
	}

	shaDarwinARM64, err := lookup(assetDarwinARM64)
	if err != nil {
		return err
	}
	shaDarwinAMD64, err := lookup(assetDarwinAMD64)
	if err != nil {
		return err
	}
	shaLinuxARM64, err := lookup(assetLinuxARM64)
	if err != nil {
		return err
	}
	shaLinuxAMD64, err := lookup(assetLinuxAMD64)
	if err != nil {
		return err
	}

	baseURL := fmt.Sprintf("https://github.com/%s/releases/download/%s", repo, version)
	content := fmt.Sprintf(`class Si < Formula
  desc "AI-first CLI for orchestrating coding agents and provider operations"
  homepage "https://github.com/%s"
  version "%s"
  license "AGPL-3.0-only"

  on_macos do
    if Hardware::CPU.arm?
      url "%s/%s"
      sha256 "%s"
    else
      url "%s/%s"
      sha256 "%s"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "%s/%s"
      sha256 "%s"
    elsif Hardware::CPU.intel?
      url "%s/%s"
      sha256 "%s"
    end
  end

  def install
    stage = buildpath/"si-stage"
    stage.mkpath
    system "tar", "-xzf", cached_download, "-C", stage

    binary = Dir["#{stage}/si_*/si"].first
    binary = (stage/"si").to_s if binary.nil? && (stage/"si").exist?
    raise "si binary not found in release archive" if binary.nil? || binary.empty?

    bin.install binary => "si"
    chmod 0o755, bin/"si"
  end

  test do
    output = shell_output("#{bin}/si version")
    assert_match "si version", output
  end
end
`, repo, versionNoV,
		baseURL, assetDarwinARM64, shaDarwinARM64,
		baseURL, assetDarwinAMD64, shaDarwinAMD64,
		baseURL, assetLinuxARM64, shaLinuxARM64,
		baseURL, assetLinuxAMD64, shaLinuxAMD64,
	)

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(outputPath, []byte(content), 0o644); err != nil {
		return err
	}
	fmt.Printf("rendered %s\n", outputPath)
	return nil
}

func buildReleaseAsset(opts releaseAssetOptions) (string, error) {
	opts.Version = strings.TrimSpace(opts.Version)
	opts.GOOS = strings.TrimSpace(opts.GOOS)
	opts.GOARCH = strings.TrimSpace(opts.GOARCH)
	opts.GOARM = strings.TrimSpace(opts.GOARM)
	opts.OutDir = strings.TrimSpace(opts.OutDir)
	opts.RepoRoot = strings.TrimSpace(opts.RepoRoot)

	if opts.Version == "" {
		return "", errors.New("--version is required")
	}
	if opts.GOOS == "" {
		return "", errors.New("--goos is required")
	}
	if opts.GOARCH == "" {
		return "", errors.New("--goarch is required")
	}
	if err := validateVersionWithPrefix(opts.Version); err != nil {
		return "", err
	}
	if opts.RepoRoot == "" {
		return "", errors.New("--repo-root is required")
	}
	if opts.OutDir == "" {
		return "", errors.New("--out-dir is required")
	}

	switch opts.GOOS {
	case "linux", "darwin":
	default:
		return "", fmt.Errorf("unsupported --goos value: %s (expected linux or darwin)", opts.GOOS)
	}
	switch opts.GOARCH {
	case "amd64", "arm64":
	case "arm":
		if opts.GOARM == "" {
			return "", errors.New("--goarm is required when --goarch=arm")
		}
	default:
		return "", fmt.Errorf("unsupported --goarch value: %s (expected amd64, arm64, or arm)", opts.GOARCH)
	}
	if opts.GOARM != "" && opts.GOARCH != "arm" {
		return "", errors.New("--goarm can only be used with --goarch=arm")
	}

	if _, err := exec.LookPath("go"); err != nil {
		return "", errors.New("missing required command: go")
	}

	if _, err := os.Stat(filepath.Join(opts.RepoRoot, "tools", "si", "go.mod")); err != nil {
		return "", errors.New("tools/si/go.mod not found (bad --repo-root?)")
	}
	if _, err := os.Stat(filepath.Join(opts.RepoRoot, "README.md")); err != nil {
		return "", errors.New("README.md not found in repo root")
	}
	if _, err := os.Stat(filepath.Join(opts.RepoRoot, "LICENSE")); err != nil {
		return "", errors.New("LICENSE not found in repo root")
	}

	if err := os.MkdirAll(opts.OutDir, 0o755); err != nil {
		return "", err
	}
	tmpDir, err := os.MkdirTemp("", "si-release-asset-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)

	versionNoV := strings.TrimPrefix(opts.Version, "v")
	archLabel := opts.GOARCH
	if opts.GOARCH == "arm" {
		archLabel = "armv" + opts.GOARM
	}
	artifactStem := fmt.Sprintf("si_%s_%s_%s", versionNoV, opts.GOOS, archLabel)
	stagingDir := filepath.Join(tmpDir, artifactStem)
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return "", err
	}

	buildEnv := append([]string{}, os.Environ()...)
	buildEnv = append(buildEnv, "CGO_ENABLED=0", "GOOS="+opts.GOOS, "GOARCH="+opts.GOARCH)
	if opts.GOARCH == "arm" {
		buildEnv = append(buildEnv, "GOARM="+opts.GOARM)
	}
	if err := runCmd(opts.RepoRoot, buildEnv, "go", "build", "-trimpath", "-buildvcs=false", "-ldflags", "-s -w", "-o", filepath.Join(stagingDir, "si"), "./tools/si"); err != nil {
		return "", err
	}
	if err := os.Chmod(filepath.Join(stagingDir, "si"), 0o755); err != nil {
		return "", err
	}
	if err := copyFile(filepath.Join(opts.RepoRoot, "README.md"), filepath.Join(stagingDir, "README.md"), 0o644); err != nil {
		return "", err
	}
	if err := copyFile(filepath.Join(opts.RepoRoot, "LICENSE"), filepath.Join(stagingDir, "LICENSE"), 0o644); err != nil {
		return "", err
	}

	archivePath := filepath.Join(opts.OutDir, artifactStem+".tar.gz")
	if err := writeTarGz(tmpDir, artifactStem, archivePath); err != nil {
		return "", err
	}
	return archivePath, nil
}

func buildNPMPackage(opts npmBuildOptions) (string, error) {
	opts.Version = strings.TrimSpace(opts.Version)
	opts.RepoRoot = strings.TrimSpace(opts.RepoRoot)
	opts.OutDir = strings.TrimSpace(opts.OutDir)

	if opts.RepoRoot == "" {
		return "", errors.New("repo root is required")
	}
	if opts.OutDir == "" {
		return "", errors.New("out dir is required")
	}
	if _, err := os.Stat(filepath.Join(opts.RepoRoot, "tools", "si", "version.go")); err != nil {
		return "", errors.New("tools/si/version.go not found")
	}
	if _, err := os.Stat(filepath.Join(opts.RepoRoot, "npm", "si")); err != nil {
		return "", errors.New("npm/si not found")
	}
	if _, err := exec.LookPath("node"); err != nil {
		return "", errors.New("missing required command: node")
	}
	if _, err := exec.LookPath("npm"); err != nil {
		return "", errors.New("missing required command: npm")
	}

	version := opts.Version
	if version == "" {
		v, err := parseSIVersion(filepath.Join(opts.RepoRoot, "tools", "si", "version.go"))
		if err != nil {
			return "", err
		}
		version = v
	}
	if err := validateVersionWithPrefix(version); err != nil {
		return "", err
	}
	npmVersion := strings.TrimPrefix(version, "v")

	if err := os.MkdirAll(opts.OutDir, 0o755); err != nil {
		return "", err
	}
	stageDir, err := os.MkdirTemp("", "si-npm-package-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stageDir)

	if err := copyDir(filepath.Join(opts.RepoRoot, "npm", "si"), stageDir); err != nil {
		return "", err
	}
	if err := copyFile(filepath.Join(opts.RepoRoot, "LICENSE"), filepath.Join(stageDir, "LICENSE"), 0o644); err != nil {
		return "", err
	}
	if err := rewritePackageVersion(filepath.Join(stageDir, "package.json"), npmVersion); err != nil {
		return "", err
	}

	if err := runCmd(stageDir, nil, "npm", "pack", "--silent"); err != nil {
		return "", err
	}
	matches, err := filepath.Glob(filepath.Join(stageDir, "*.tgz"))
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", errors.New("npm pack did not produce a tarball")
	}
	sort.Strings(matches)
	src := matches[len(matches)-1]
	dst := filepath.Join(opts.OutDir, filepath.Base(src))
	if err := moveFile(src, dst); err != nil {
		return "", err
	}
	return dst, nil
}

func moveFile(src string, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	} else if !errors.Is(err, syscall.EXDEV) {
		return err
	}

	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := copyFile(src, dst, info.Mode()); err != nil {
		return err
	}
	if err := os.Remove(src); err != nil {
		return err
	}
	return nil
}

func rewritePackageVersion(path string, version string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return err
	}
	payload["version"] = version
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	return os.WriteFile(path, encoded, 0o644)
}

func writeTarGz(parentDir string, topDir string, outPath string) error {
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	defer gz.Close()

	tw := tar.NewWriter(gz)
	defer tw.Close()

	sourceRoot := filepath.Join(parentDir, topDir)
	entries := []string{}
	if err := filepath.WalkDir(sourceRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		entries = append(entries, path)
		return nil
	}); err != nil {
		return err
	}
	sort.Strings(entries)
	epoch := time.Unix(0, 0).UTC()
	for _, path := range entries {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(parentDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		hdr.Name = rel
		hdr.ModTime = epoch
		hdr.AccessTime = epoch
		hdr.ChangeTime = epoch
		hdr.Uid = 0
		hdr.Gid = 0
		hdr.Uname = ""
		hdr.Gname = ""
		if info.IsDir() {
			hdr.Mode = 0o755
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			if _, err := io.Copy(tw, file); err != nil {
				file.Close()
				return err
			}
			if err := file.Close(); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyDir(src string, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			if rel == "." {
				return os.MkdirAll(dst, 0o755)
			}
			return os.MkdirAll(target, 0o755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		mode := info.Mode()
		if mode&0o111 != 0 {
			return copyFile(path, target, 0o755)
		}
		return copyFile(path, target, 0o644)
	})
}

func copyFile(src string, dst string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return nil
}

func parseSIVersion(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("could not read %s: %w", path, err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if m := versionPattern.FindStringSubmatch(strings.TrimSpace(line)); len(m) == 2 {
			return m[1], nil
		}
	}
	return "", fmt.Errorf("could not parse %s", path)
}

func validateVersionWithPrefix(version string) error {
	if !strings.HasPrefix(version, "v") {
		return fmt.Errorf("version must include v prefix, got: %s", version)
	}
	if strings.TrimSpace(version) == "v" {
		return errors.New("invalid version")
	}
	return nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func parseChecksumsFile(path string) (map[string]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	result := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		result[fields[1]] = fields[0]
	}
	return result, nil
}

func downloadFile(url string, path string) error {
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("download failed: %s", resp.Status)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

func npmPackageExists(packageVersion string) (bool, error) {
	cmd := exec.Command("npm", "view", packageVersion, "version")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return false, nil
	}
	return false, err
}

func waitForNPMPackage(packageVersion string, attempts int, delay time.Duration) bool {
	for i := 1; i <= attempts; i++ {
		exists, err := npmPackageExists(packageVersion)
		if err == nil && exists {
			return true
		}
		if i < attempts {
			time.Sleep(delay)
		}
	}
	return false
}

func vaultKeyExists(output string, key string) bool {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) > 0 && fields[0] == key {
			return true
		}
	}
	return false
}

func runCmd(dir string, env []string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = env
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runCmdOutput(dir string, env []string, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = env
	}
	cmd.Stderr = os.Stderr
	return cmd.Output()
}
