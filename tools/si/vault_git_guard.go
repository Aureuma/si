package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"

	"si/tools/si/internal/vault"
)

type vaultGitIndexFlags struct {
	Tracked         bool
	AssumeUnchanged bool
	SkipWorktree    bool
}

func vaultWriteDotenvFileAtomic(path string, contents []byte) error {
	if shouldEnforceVaultGitIndexGuard() {
		if err := vaultRefuseHiddenGitIndexEdits(path); err != nil {
			return err
		}
	}
	return vault.WriteDotenvFileAtomic(path, contents)
}

func shouldEnforceVaultGitIndexGuard() bool {
	settings := loadVaultSettingsOrFail()
	return shouldEnforceVaultRepoScope(settings)
}

func vaultRefuseHiddenGitIndexEdits(path string) error {
	repoRoot, relPath, flags, err := vaultGitIndexFlagsForPath(path)
	if err != nil {
		return err
	}
	if !flags.Tracked || (!flags.SkipWorktree && !flags.AssumeUnchanged) {
		return nil
	}

	modes := make([]string, 0, 2)
	fixes := make([]string, 0, 2)
	if flags.SkipWorktree {
		modes = append(modes, "skip-worktree")
		fixes = append(fixes, fmt.Sprintf("git -C %s update-index --no-skip-worktree -- %s", shellSingleQuote(repoRoot), shellSingleQuote(relPath)))
	}
	if flags.AssumeUnchanged {
		modes = append(modes, "assume-unchanged")
		fixes = append(fixes, fmt.Sprintf("git -C %s update-index --no-assume-unchanged -- %s", shellSingleQuote(repoRoot), shellSingleQuote(relPath)))
	}
	return fmt.Errorf(
		"refusing to write %s: git index flag(s) hide file updates (%s); clear flag(s) first:\n  %s",
		filepath.Clean(path),
		strings.Join(modes, ", "),
		strings.Join(fixes, "\n  "),
	)
}

func vaultGitIndexFlagsForPath(path string) (string, string, vaultGitIndexFlags, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" {
		return "", "", vaultGitIndexFlags{}, nil
	}
	repoRoot, err := vault.GitRoot(filepath.Dir(path))
	if err != nil {
		// Non-git files are allowed.
		return "", "", vaultGitIndexFlags{}, nil
	}
	absPath := absPathOrSelf(path)
	repoRoot = absPathOrSelf(repoRoot)
	if !isPathWithin(absPath, repoRoot) {
		return "", "", vaultGitIndexFlags{}, nil
	}
	relPath, err := filepath.Rel(repoRoot, absPath)
	if err != nil {
		return "", "", vaultGitIndexFlags{}, nil
	}
	relPath = filepath.ToSlash(filepath.Clean(relPath))
	if relPath == "." || strings.HasPrefix(relPath, "../") {
		return "", "", vaultGitIndexFlags{}, nil
	}

	cmd := exec.Command("git", "ls-files", "-v", "--", relPath)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return "", "", vaultGitIndexFlags{}, err
	}
	lines := strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n")
	for _, line := range lines {
		if flags, ok := parseVaultGitLsFilesLine(line); ok {
			return repoRoot, relPath, flags, nil
		}
	}
	return repoRoot, relPath, vaultGitIndexFlags{}, nil
}

func parseVaultGitLsFilesLine(line string) (vaultGitIndexFlags, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return vaultGitIndexFlags{}, false
	}
	r := rune(line[0])
	flags := vaultGitIndexFlags{Tracked: true}
	switch r {
	case 'S':
		flags.SkipWorktree = true
	case 's':
		flags.SkipWorktree = true
		flags.AssumeUnchanged = true
	default:
		if unicode.IsLower(r) {
			flags.AssumeUnchanged = true
		}
	}
	return flags, true
}
