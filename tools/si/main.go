package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func main() {
	if tryExecRustPrimary() {
		return
	}
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	if !dispatchRootCommand(cmd, args) {
		printUnknown("", cmd)
		usage()
		os.Exit(1)
	}
}

func tryExecRustPrimary() bool {
	candidates := []string{}
	if explicit := strings.TrimSpace(os.Getenv("SI_RUST_BIN")); explicit != "" {
		candidates = append(candidates, explicit)
	}
	candidates = append(candidates, "si-rs")

	self, _ := os.Executable()
	selfEval, _ := filepath.EvalSymlinks(self)
	for _, candidate := range candidates {
		path, ok := resolveRustPrimaryPath(candidate)
		if !ok {
			continue
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil && selfEval != "" && resolved == selfEval {
			continue
		}
		if err := syscall.Exec(path, append([]string{path}, os.Args[1:]...), os.Environ()); err == nil {
			return true
		}
	}
	return false
}

func resolveRustPrimaryPath(candidate string) (string, bool) {
	if strings.Contains(candidate, "/") {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, true
		}
		return "", false
	}
	path, err := exec.LookPath(candidate)
	if err != nil {
		return "", false
	}
	return path, true
}
