package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"si/tools/si/internal/vault"
)

func cmdVaultCheck(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("vault check", flag.ExitOnError)
	fileFlag := fs.String("file", "", "explicit env file path to check (overrides --vault-dir)")
	vaultDir := fs.String("vault-dir", settings.Vault.Dir, "vault directory (relative to git root; use '.' when running inside the vault repo)")
	staged := fs.Bool("staged", false, "check staged (git index) contents instead of working tree")
	all := fs.Bool("all", false, "check all dotenv files under the vault dir (or all staged dotenv files)")
	includeExamples := fs.Bool("include-examples", false, "include example/template dotenv files (e.g. .env.example)")
	_ = fs.Parse(args)
	if len(fs.Args()) != 0 {
		printUsage("usage: si vault check [--file <path>] [--vault-dir <path>] [--staged] [--all] [--include-examples]")
		return
	}

	target, err := vaultResolveTarget(settings, *fileFlag, *vaultDir, true, true)
	if err != nil {
		fatal(err)
	}
	if *staged && strings.TrimSpace(target.RepoRoot) == "" {
		fatal(fmt.Errorf("git repo root not found (required with --staged)"))
	}

	// Resolve vault dir when running inside the vault repo itself (common for hooks).
	vaultAbs := strings.TrimSpace(target.VaultDir)
	if strings.TrimSpace(*fileFlag) == "" && strings.TrimSpace(vaultAbs) == "" {
		vaultAbs = target.RepoRoot
	}

	files := []string{}
	if *staged {
		paths, err := vault.GitStagedFiles(target.RepoRoot)
		if err != nil {
			fatal(err)
		}
		for _, p := range paths {
			if !isDotenvPath(p, *includeExamples) {
				continue
			}
			// In staged mode, restrict to vault dir unless --file explicitly points elsewhere.
			if strings.TrimSpace(*fileFlag) == "" {
				abs := filepath.Clean(filepath.Join(target.RepoRoot, p))
				if !isSubpath(abs, vaultAbs) {
					continue
				}
			}
			files = append(files, p)
		}
		if !*all {
			// Default to checking only the resolved target file when not using --all.
			rel, _ := filepath.Rel(target.RepoRoot, target.File)
			files = []string{filepath.ToSlash(rel)}
		}
	} else {
		if *all {
			found, err := listVaultDotenvFiles(vaultAbs, *includeExamples)
			if err != nil {
				fatal(err)
			}
			files = append(files, found...)
		} else {
			files = []string{target.File}
		}
	}

	if len(files) == 0 {
		return
	}
	sort.Strings(files)

	type finding struct {
		File string
		Abs  string
		Keys []string
	}
	findings := []finding{}

	for _, p := range files {
		var doc vault.DotenvFile
		var err error
		display := p
		abs := p
		if *staged {
			data, derr := vault.GitShowIndexFile(target.RepoRoot, p)
			err = derr
			if err == nil {
				doc = vault.ParseDotenv(data)
			}
			abs = filepath.Clean(filepath.Join(target.RepoRoot, filepath.FromSlash(p)))
		} else {
			doc, err = vault.ReadDotenvFile(p)
			display = filepath.Clean(p)
			abs = display
		}
		if err != nil {
			// Ignore deleted/absent files in staged mode.
			if *staged {
				continue
			}
			fatal(err)
		}
		scan, err := vault.ScanDotenvEncryption(doc)
		if err != nil {
			fatal(fmt.Errorf("%s: invalid vault dotenv (%w)", display, err))
		}
		if len(scan.PlaintextKeys) > 0 {
			keys := append([]string(nil), scan.PlaintextKeys...)
			sort.Strings(keys)
			findings = append(findings, finding{File: display, Abs: abs, Keys: keys})
		}
	}

	if len(findings) == 0 {
		return
	}

	var b strings.Builder
	b.WriteString("[si vault] plaintext values detected; encrypt before committing.\n")
	for _, f := range findings {
		b.WriteString("  - ")
		b.WriteString(f.File)
		b.WriteString(": ")
		b.WriteString(strings.Join(f.Keys, ", "))
		b.WriteString("\n")
	}
	b.WriteString("\nFix:\n")
	for _, f := range findings {
		b.WriteString("  si vault encrypt --file ")
		b.WriteString(shellSingleQuote(filepath.Clean(f.Abs)))
		b.WriteString(" --format\n")
	}
	b.WriteString("\nBypass (not recommended): git commit --no-verify\n")
	fmt.Fprint(os.Stderr, b.String())
	os.Exit(2)
}

func isDotenvPath(path string, includeExamples bool) bool {
	p := filepath.ToSlash(strings.TrimSpace(path))
	if p == "" {
		return false
	}
	base := strings.ToLower(filepath.Base(p))
	if base == ".env" || strings.HasPrefix(base, ".env.") {
		if includeExamples {
			return true
		}
		switch base {
		case ".env.example", ".env.sample", ".env.template", ".env.dist":
			return false
		}
		return true
	}
	return false
}

func isSubpath(child, parent string) bool {
	child = filepath.Clean(strings.TrimSpace(child))
	parent = filepath.Clean(strings.TrimSpace(parent))
	if child == "" || parent == "" {
		return false
	}
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..")
}

func listVaultDotenvFiles(vaultDir string, includeExamples bool) ([]string, error) {
	vaultDir = filepath.Clean(strings.TrimSpace(vaultDir))
	if vaultDir == "" {
		return nil, fmt.Errorf("vault dir required")
	}
	entries, err := os.ReadDir(vaultDir)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !isDotenvPath(name, includeExamples) {
			continue
		}
		out = append(out, filepath.Join(vaultDir, name))
	}
	sort.Strings(out)
	return out, nil
}
