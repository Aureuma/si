package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"si/tools/si/internal/vault"
)

func cmdVaultCheck(args []string) {
	fs := flag.NewFlagSet("vault check", flag.ExitOnError)
	var envFiles multiFlag
	fs.Var(&envFiles, "env-file", "dotenv file path (repeatable)")
	fs.Var(&envFiles, "f", "alias for --env-file")
	fileAlias := fs.String("file", "", "alias for --env-file")
	staged := fs.Bool("staged", false, "check staged .env files")
	all := fs.Bool("all", false, "check all .env* files under current repo")
	includeExamples := fs.Bool("include-examples", false, "include .env.example files when --all/--staged is used")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si vault check [--env-file <path>]... [--staged] [--all] [--include-examples]")
		return
	}

	paths := []string{}
	var err error
	switch {
	case *staged:
		paths, err = stagedDotenvFiles(*includeExamples)
	case *all:
		paths, err = discoverDotenvFiles(*includeExamples)
	default:
		paths = collectVaultEnvFiles(envFiles, strings.TrimSpace(*fileAlias))
		if len(paths) == 0 {
			paths = []string{defaultSIVaultDotenvFile}
		}
	}
	if err != nil {
		fatal(err)
	}
	if len(paths) == 0 {
		return
	}

	type finding struct {
		File string
		Keys []string
	}
	findings := []finding{}
	for _, candidate := range paths {
		pathValue := candidate
		if !filepath.IsAbs(pathValue) {
			cwd, cwdErr := os.Getwd()
			if cwdErr == nil {
				pathValue = filepath.Join(cwd, pathValue)
			}
		}
		pathValue = filepath.Clean(pathValue)
		doc, readErr := vault.ReadDotenvFile(pathValue)
		if readErr != nil {
			if os.IsNotExist(readErr) {
				continue
			}
			fatal(readErr)
		}
		entries, entriesErr := vault.Entries(doc)
		if entriesErr != nil {
			fatal(entriesErr)
		}
		plaintext := []string{}
		for _, entry := range entries {
			if strings.TrimSpace(entry.Key) == vault.SIVaultPublicKeyName {
				continue
			}
			if !vault.IsSIVaultEncryptedValue(entry.ValueRaw) {
				plaintext = append(plaintext, entry.Key)
			}
		}
		if len(plaintext) == 0 {
			continue
		}
		sort.Strings(plaintext)
		findings = append(findings, finding{File: pathValue, Keys: plaintext})
	}
	if len(findings) == 0 {
		return
	}
	var b strings.Builder
	b.WriteString("[si vault] plaintext values detected:\n")
	for _, item := range findings {
		b.WriteString("  - ")
		b.WriteString(filepath.Clean(item.File))
		b.WriteString(": ")
		b.WriteString(strings.Join(item.Keys, ", "))
		b.WriteString("\n")
	}
	b.WriteString("\nFix:\n  si vault encrypt --env-file <file>\n")
	fmt.Fprint(os.Stderr, b.String())
	os.Exit(2)
}

func stagedDotenvFiles(includeExamples bool) ([]string, error) {
	// #nosec G204 -- fixed git command and arguments.
	cmd := exec.Command("git", "diff", "--cached", "--name-only", "--diff-filter=ACM")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(out), "\n")
	return filterDotenvCandidates(lines, includeExamples), nil
}

func discoverDotenvFiles(includeExamples bool) ([]string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	root := cwd
	if gitRoot, gitErr := vault.GitRoot(cwd); gitErr == nil && strings.TrimSpace(gitRoot) != "" {
		root = gitRoot
	}
	out := []string{}
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			relative = path
		}
		if !isDotenvCandidate(relative, includeExamples) {
			return nil
		}
		out = append(out, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

func filterDotenvCandidates(raw []string, includeExamples bool) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, line := range raw {
		candidate := strings.TrimSpace(line)
		if candidate == "" {
			continue
		}
		if !isDotenvCandidate(candidate, includeExamples) {
			continue
		}
		candidate = filepath.Clean(candidate)
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		out = append(out, candidate)
	}
	sort.Strings(out)
	return out
}

func isDotenvCandidate(pathValue string, includeExamples bool) bool {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(pathValue)))
	if base == "" {
		return false
	}
	if !strings.HasPrefix(base, ".env") {
		return false
	}
	if !includeExamples && strings.Contains(base, "example") {
		return false
	}
	return true
}
