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
	fileFlag := fs.String("file", "", "explicit env file path to check (defaults to the configured vault.file)")
	scopeFlag := fs.String("scope", "", "vault scope")
	staged := fs.Bool("staged", false, "check staged (git index) contents instead of working tree")
	all := fs.Bool("all", false, "check all .env* files (staged mode: all staged dotenv files; working tree mode: all .env* in the same directory as the target file)")
	includeExamples := fs.Bool("include-examples", false, "include example/template dotenv files (e.g. .env.example)")
	_ = fs.Parse(args)
	if len(fs.Args()) != 0 {
		printUsage("usage: si vault check [--scope <name>] [--file <name>] [--staged] [--all] [--include-examples]")
		return
	}
	scope := strings.TrimSpace(*scopeFlag)
	if scope == "" {
		scope = strings.TrimSpace(*fileFlag)
	}

	if backend, err := resolveVaultSyncBackend(settings); err == nil && backend.Mode == vaultSyncBackendSun {
		if *staged || *all || *includeExamples {
			warnf("vault check in Sun remote mode ignores --staged/--all/--include-examples")
		}
		target, targetErr := vaultResolveTarget(settings, scope, false)
		if targetErr != nil {
			fatal(targetErr)
		}
		values, used, sunErr := vaultSunKVLoadRawValues(settings, target)
		if sunErr != nil {
			fatal(sunErr)
		}
		if !used {
			fatal(fmt.Errorf("sun vault unavailable: run `si sun auth login --url <url> --token <token> --account <slug>`"))
		}
		keys := make([]string, 0, len(values))
		for key := range values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		plaintext := make([]string, 0)
		for _, key := range keys {
			norm, normErr := vault.NormalizeDotenvValue(values[key])
			if normErr != nil {
				fatal(fmt.Errorf("invalid value for key %s: %w", key, normErr))
			}
			if !vault.IsEncryptedValueV1(norm) {
				plaintext = append(plaintext, key)
			}
		}
		if len(plaintext) == 0 {
			return
		}
		var b strings.Builder
		b.WriteString("[si vault] plaintext values detected in Sun scope; encrypt before running with this scope.\n")
		b.WriteString("  - scope: ")
		b.WriteString(strings.TrimSpace(target.File))
		b.WriteString("\n  - keys: ")
		b.WriteString(strings.Join(plaintext, ", "))
		b.WriteString("\n\nFix:\n")
		b.WriteString("  si vault encrypt --scope ")
		b.WriteString(shellSingleQuote(strings.TrimSpace(target.File)))
		b.WriteString("\n")
		fmt.Fprint(os.Stderr, b.String())
		os.Exit(2)
	}

	target, err := vaultResolveTarget(settings, scope, *all)
	if err != nil {
		fatal(err)
	}
	if *staged && strings.TrimSpace(target.RepoRoot) == "" {
		fatal(fmt.Errorf("git repo root not found (required with --staged)"))
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
			files = append(files, p)
		}
		if !*all {
			// Default to checking only the resolved target file when not using --all.
			rel, err := filepath.Rel(target.RepoRoot, target.File)
			if err != nil {
				fatal(err)
			}
			rel = filepath.ToSlash(filepath.Clean(rel))
			if strings.HasPrefix(rel, "../") || rel == ".." {
				fatal(fmt.Errorf("--staged requires --file to point inside the git repo (%s)", filepath.Clean(target.RepoRoot)))
			}
			files = []string{rel}
		}
	} else {
		if *all {
			found, err := listDotenvFilesInDir(filepath.Dir(target.File), *includeExamples)
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
		var abs string
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

func listDotenvFilesInDir(dir string, includeExamples bool) ([]string, error) {
	dir = filepath.Clean(strings.TrimSpace(dir))
	if dir == "" {
		return nil, fmt.Errorf("dir required")
	}
	entries, err := os.ReadDir(dir)
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
		out = append(out, filepath.Join(dir, name))
	}
	sort.Strings(out)
	return out, nil
}
