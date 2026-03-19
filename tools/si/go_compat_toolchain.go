package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func resolveGoCompatToolchain(output string, goBin string) (string, error) {
	goBin = strings.TrimSpace(goBin)
	if goBin == "" {
		goBin = "go"
	}

	if strings.Contains(goBin, "/") || strings.Contains(goBin, string(os.PathSeparator)) {
		if info, err := os.Stat(goBin); err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
			return goBin, nil
		}
		return "", fmt.Errorf("go executable not found: %s", goBin)
	}

	if path, err := exec.LookPath(goBin); err == nil && strings.TrimSpace(path) != "" {
		return path, nil
	}
	if goBin != "go" {
		return "", fmt.Errorf("go executable not found: %s", goBin)
	}

	if path, ok := resolveSiblingGo(output); ok {
		return path, nil
	}
	if path, ok := resolveExecutableSiblingGo(); ok {
		return path, nil
	}

	return "", fmt.Errorf(
		"go executable not found: go (install Go 1.25+ or use the Rust-primary CLI paths instead of the remaining Go compatibility surface)",
	)
}

func resolveSiblingGo(output string) (string, bool) {
	dir := filepath.Dir(strings.TrimSpace(output))
	if strings.TrimSpace(dir) == "" {
		return "", false
	}
	path := filepath.Join(dir, "go")
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
		return path, true
	}
	return "", false
}

func resolveExecutableSiblingGo() (string, bool) {
	exe, err := os.Executable()
	if err != nil || strings.TrimSpace(exe) == "" {
		return "", false
	}
	path := filepath.Join(filepath.Dir(exe), "go")
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
		return path, true
	}
	return "", false
}
