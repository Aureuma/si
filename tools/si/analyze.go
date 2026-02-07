package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type analyzeModule struct {
	Rel string
	Dir string
}

type goWorkEditJSON struct {
	Use []struct {
		DiskPath string `json:"DiskPath"`
	} `json:"Use"`
}

func cmdAnalyze(args []string) {
	fs := flag.NewFlagSet("analyze", flag.ExitOnError)
	modules := multiFlag{}
	fs.Var(&modules, "module", "module path to analyze (repeatable; default: all go.work modules)")
	skipVet := fs.Bool("skip-vet", false, "skip go vet")
	skipLint := fs.Bool("skip-lint", false, "skip golangci-lint")
	fix := fs.Bool("fix", false, "pass --fix to golangci-lint")
	noFail := fs.Bool("no-fail", false, "always exit zero (still prints failures)")
	fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si analyze [--module <path>] [--skip-vet] [--skip-lint] [--fix] [--no-fail]")
		return
	}

	if *skipVet && *skipLint {
		fatal(errors.New("both analyzers are disabled; remove --skip-vet or --skip-lint"))
	}

	root := mustRepoRoot()
	allModules, err := discoverAnalyzeModules(root)
	if err != nil {
		fatal(err)
	}
	selectedModules, err := resolveAnalyzeModules(allModules, modules)
	if err != nil {
		fatal(err)
	}
	if len(selectedModules) == 0 {
		fatal(errors.New("no modules selected for analysis"))
	}

	if !*skipVet {
		if _, err := exec.LookPath("go"); err != nil {
			fatal(fmt.Errorf("go toolchain not found in PATH"))
		}
	}
	if !*skipLint {
		if _, err := exec.LookPath("golangci-lint"); err != nil {
			fatal(fmt.Errorf("golangci-lint not found in PATH (install: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)"))
		}
	}

	infof("static analysis: %d module(s)", len(selectedModules))
	failed := false
	for _, mod := range selectedModules {
		fmt.Printf("\n%s %s\n", styleSection("module:"), styleCmd(mod.Rel))
		if !*skipVet {
			if err := runAnalyzeCommand(mod.Dir, "go", []string{"vet", "./..."}); err != nil {
				warnf("%s go vet failed: %v", mod.Rel, err)
				failed = true
			}
		}
		if !*skipLint {
			lintArgs := []string{"run", "./..."}
			if *fix {
				lintArgs = append(lintArgs, "--fix")
			}
			if err := runAnalyzeCommand(mod.Dir, "golangci-lint", lintArgs); err != nil {
				warnf("%s golangci-lint failed: %v", mod.Rel, err)
				failed = true
			}
		}
	}

	if failed {
		warnf("static analysis completed with failures")
		if *noFail {
			infof("--no-fail enabled; exiting with status 0")
			return
		}
		os.Exit(1)
	}
	successf("static analysis completed successfully")
}

func runAnalyzeCommand(dir, name string, args []string) error {
	fmt.Printf("%s %s\n", styleDim("$"), styleCmd(name+" "+strings.Join(args, " ")))
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func discoverAnalyzeModules(root string) ([]analyzeModule, error) {
	cmd := exec.Command("go", "work", "edit", "-json")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("discover modules from go.work: %w", err)
	}
	var payload goWorkEditJSON
	if err := json.Unmarshal(out, &payload); err != nil {
		return nil, fmt.Errorf("parse go.work json: %w", err)
	}
	if len(payload.Use) == 0 {
		return nil, errors.New("go.work has no use entries")
	}
	mods := make([]analyzeModule, 0, len(payload.Use))
	seen := map[string]struct{}{}
	for _, use := range payload.Use {
		rel := strings.TrimSpace(use.DiskPath)
		if rel == "" {
			continue
		}
		rel = filepath.Clean(rel)
		if rel == "." {
			rel = "."
		} else {
			rel = strings.TrimPrefix(rel, "./")
		}
		dir := filepath.Join(root, rel)
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}
		mods = append(mods, analyzeModule{Rel: filepath.ToSlash(rel), Dir: dir})
	}
	sort.Slice(mods, func(i, j int) bool { return mods[i].Rel < mods[j].Rel })
	return mods, nil
}

func resolveAnalyzeModules(all []analyzeModule, filters []string) ([]analyzeModule, error) {
	if len(filters) == 0 {
		return all, nil
	}

	resolved := make([]analyzeModule, 0, len(filters))
	used := map[string]struct{}{}
	for _, raw := range filters {
		filter := normalizeAnalyzeModuleFilter(raw)
		if filter == "" {
			continue
		}
		matches := make([]analyzeModule, 0, 2)
		for _, mod := range all {
			if filter == mod.Rel || filter == filepath.Base(mod.Rel) || filter == mod.Dir {
				matches = append(matches, mod)
			}
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("module %q not found in go.work", raw)
		}
		if len(matches) > 1 {
			names := make([]string, 0, len(matches))
			for _, match := range matches {
				names = append(names, match.Rel)
			}
			sort.Strings(names)
			return nil, fmt.Errorf("module %q is ambiguous; use one of: %s", raw, strings.Join(names, ", "))
		}
		match := matches[0]
		if _, ok := used[match.Rel]; ok {
			continue
		}
		used[match.Rel] = struct{}{}
		resolved = append(resolved, match)
	}
	sort.Slice(resolved, func(i, j int) bool { return resolved[i].Rel < resolved[j].Rel })
	return resolved, nil
}

func normalizeAnalyzeModuleFilter(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "./")
	value = filepath.Clean(value)
	if value == "." {
		return ""
	}
	return filepath.ToSlash(value)
}
