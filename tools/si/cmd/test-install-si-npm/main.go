package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var versionPattern = regexp.MustCompile(`(?m)^const siVersion = "([^"]+)"$`)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("test-install-si-npm", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	help := fs.Bool("help", false, "show help")
	fs.BoolVar(help, "h", false, "show help")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *help {
		printUsage()
		return 0
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "unexpected arguments: %s\n", strings.Join(fs.Args(), " "))
		printUsage()
		return 1
	}

	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve cwd: %v\n", err)
		return 1
	}
	version, err := resolveSIVersion(filepath.Join(root, "tools", "si", "version.go"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}

	tmpDir, err := os.MkdirTemp("", "si-install-npm-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create tmp dir: %v\n", err)
		return 1
	}
	defer os.RemoveAll(tmpDir)

	assetsDir := filepath.Join(tmpDir, "assets")
	npmOut := filepath.Join(tmpDir, "npm")
	prefixDir := filepath.Join(tmpDir, "prefix")
	for _, dir := range []string{assetsDir, npmOut, prefixDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "mkdir %s: %v\n", dir, err)
			return 1
		}
	}

	if err := runCmd(root, nil, "tools/release/build-cli-release-assets.sh", "--version", version, "--out-dir", assetsDir); err != nil {
		fmt.Fprintf(os.Stderr, "build release assets: %v\n", err)
		return 1
	}
	if err := runCmd(root, nil, "tools/release/npm/build-npm-package.sh", "--version", version, "--out-dir", npmOut); err != nil {
		fmt.Fprintf(os.Stderr, "build npm package: %v\n", err)
		return 1
	}

	packFile, err := findNpmPackageTarball(npmOut)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	if err := runCmd(root, nil, "npm", "install", "--silent", "--global", "--prefix", prefixDir, packFile); err != nil {
		fmt.Fprintf(os.Stderr, "npm install smoke: %v\n", err)
		return 1
	}

	launcher := filepath.Join(prefixDir, "bin", "si")
	info, err := os.Stat(launcher)
	if err != nil || info.IsDir() || info.Mode().Perm()&0o111 == 0 {
		fmt.Fprintf(os.Stderr, "si launcher not installed at %s\n", launcher)
		return 1
	}
	env := map[string]string{"SI_NPM_LOCAL_ARCHIVE_DIR": assetsDir}
	if err := runCmd(root, env, launcher, "version"); err != nil {
		fmt.Fprintf(os.Stderr, "run installed si launcher: %v\n", err)
		return 1
	}

	fmt.Println("npm install smoke passed")
	return 0
}

func printUsage() {
	fmt.Println("Usage: ./tools/test-install-si-npm.sh")
}

func resolveSIVersion(versionFilePath string) (string, error) {
	raw, err := os.ReadFile(versionFilePath)
	if err != nil {
		return "", fmt.Errorf("read version file: %w", err)
	}
	matches := versionPattern.FindStringSubmatch(string(raw))
	if len(matches) < 2 {
		return "", errors.New("failed to resolve version")
	}
	version := strings.TrimSpace(matches[1])
	if version == "" {
		return "", errors.New("failed to resolve version")
	}
	return version, nil
}

func findNpmPackageTarball(npmOut string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(npmOut, "aureuma-si-*.tgz"))
	if err != nil {
		return "", fmt.Errorf("find npm package tarball: %w", err)
	}
	if len(matches) == 0 {
		return "", errors.New("npm package tarball not found")
	}
	sort.Strings(matches)
	return matches[0], nil
}

func runCmd(dir string, extraEnv map[string]string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if len(extraEnv) > 0 {
		env := append([]string{}, os.Environ()...)
		for k, v := range extraEnv {
			env = append(env, k+"="+v)
		}
		cmd.Env = env
	}
	return cmd.Run()
}
