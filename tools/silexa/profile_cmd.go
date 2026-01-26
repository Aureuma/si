package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func cmdPersona(args []string) {
	fs := flag.NewFlagSet("persona", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() < 1 {
		printUsage("usage: si persona <name>")
		return
	}
	name := strings.TrimSpace(fs.Arg(0))
	if name == "" {
		printUsage("usage: si persona <name>")
		return
	}
	if strings.HasSuffix(name, ".md") {
		name = strings.TrimSuffix(name, ".md")
	}
	if !isValidSlug(name) {
		fatal(fmt.Errorf("invalid profile name %q", name))
	}
	root := mustRepoRoot()
	path := filepath.Join(root, "profiles", name+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			available := listProfiles(root)
			if len(available) > 0 {
				fatal(fmt.Errorf("profile not found: %s (available: %s)", name, strings.Join(available, ", ")))
			}
			fatal(fmt.Errorf("profile not found: %s", name))
		}
		fatal(err)
	}
	_, _ = os.Stdout.Write(data)
	if len(data) > 0 && data[len(data)-1] != '\n' {
		fmt.Println()
	}
}

func cmdSkill(args []string) {
	fs := flag.NewFlagSet("skill", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() < 1 {
		printUsage("usage: si skill <role>")
		return
	}
	role := strings.TrimSpace(fs.Arg(0))
	if role == "" {
		printUsage("usage: si skill <role>")
		return
	}
	text, ok := skillText(role)
	if !ok {
		roles := skillRoles()
		if len(roles) > 0 {
			fatal(fmt.Errorf("unknown role %q (available: %s)", role, strings.Join(roles, ", ")))
		}
		fatal(fmt.Errorf("unknown role %q", role))
	}
	fmt.Println(text)
}

func listProfiles(root string) []string {
	profilesDir := filepath.Join(root, "profiles")
	entries, err := os.ReadDir(profilesDir)
	if err != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		names = append(names, strings.TrimSuffix(name, ".md"))
	}
	sort.Strings(names)
	return names
}
