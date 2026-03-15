package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	siExperimentalRustCLIEnv = "SI_EXPERIMENTAL_RUST_CLI"
	siRustCLIBinEnv          = "SI_RUST_CLI_BIN"
)

var (
	rustCLIExecCommand = exec.Command
	rustCLILookPath    = exec.LookPath
	rustCLIRepoRoot    = repoRoot
)

func runVersionCommand() error {
	delegated, err := maybeDispatchRustCLIReadOnly("version")
	if err != nil {
		return err
	}
	if delegated {
		return nil
	}
	printVersion()
	return nil
}

func runHelpCommand(args []string) error {
	if len(args) <= 1 {
		delegated, err := maybeDispatchRustCLIReadOnly("help", args...)
		if err != nil {
			return err
		}
		if delegated {
			return nil
		}
	}
	usage()
	return nil
}

func maybeDispatchRustCLIReadOnly(command string, args ...string) (bool, error) {
	if !shouldUseExperimentalRustCLI() {
		return false, nil
	}
	bin, err := resolveRustCLIBinary()
	if err != nil {
		return false, err
	}
	cmd := rustCLIExecCommand(bin, append([]string{command}, args...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("run rust si cli %q: %w", command, err)
	}
	return true, nil
}

func shouldUseExperimentalRustCLI() bool {
	if strings.TrimSpace(os.Getenv(siRustCLIBinEnv)) != "" {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv(siExperimentalRustCLIEnv))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func resolveRustCLIBinary() (string, error) {
	if explicit := strings.TrimSpace(os.Getenv(siRustCLIBinEnv)); explicit != "" {
		path, err := resolveExecutablePath(explicit)
		if err != nil {
			return "", fmt.Errorf("resolve %s: %w", siRustCLIBinEnv, err)
		}
		return path, nil
	}

	if root, err := rustCLIRepoRoot(); err == nil {
		candidate := filepath.Join(root, ".artifacts", "cargo-target", "debug", "si-rs")
		if path, err := resolveExecutablePath(candidate); err == nil {
			return path, nil
		}
	}

	path, err := rustCLILookPath("si-rs")
	if err == nil {
		return path, nil
	}
	return "", fmt.Errorf(
		"experimental Rust CLI enabled but no si-rs binary found; set %s or build rust/crates/si-cli",
		siRustCLIBinEnv,
	)
}

func resolveExecutablePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("path is empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory", abs)
	}
	if info.Mode()&0o111 == 0 {
		return "", fmt.Errorf("%s is not executable", abs)
	}
	return abs, nil
}
