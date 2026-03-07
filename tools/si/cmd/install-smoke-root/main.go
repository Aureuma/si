package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	sourceDir := os.Getenv("SI_INSTALL_SOURCE_DIR")
	if sourceDir == "" {
		sourceDir = "/workspace/si"
	}
	installer := filepath.Join(sourceDir, "tools", "install-si.sh")
	if _, err := os.Stat(installer); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: installer not found at %s\n", installer)
		os.Exit(1)
	}

	work, err := os.MkdirTemp("", "si-install-root-smoke-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.RemoveAll(work)

	installDir := filepath.Join(work, "bin")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Println("==> root smoke: install from source checkout")
	if err := run(installer, "--source-dir", sourceDir, "--install-dir", installDir, "--force", "--no-buildx", "--quiet"); err != nil {
		os.Exit(1)
	}

	target := filepath.Join(installDir, "si")
	if !isExecutable(target) {
		fmt.Fprintf(os.Stderr, "ERROR: expected binary at %s\n", target)
		os.Exit(1)
	}
	if err := run(target, "version"); err != nil {
		os.Exit(1)
	}
	if err := run(target, "--help"); err != nil {
		os.Exit(1)
	}

	fmt.Println("==> root smoke: uninstall")
	if err := run(installer, "--install-dir", installDir, "--uninstall", "--quiet"); err != nil {
		os.Exit(1)
	}
	if _, err := os.Stat(target); err == nil {
		fmt.Fprintf(os.Stderr, "ERROR: expected uninstall to remove %s\n", target)
		os.Exit(1)
	}

	fmt.Println("OK")
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().Perm()&0o111 != 0
}
