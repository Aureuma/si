package main

import (
	"flag"
	"fmt"
	"path/filepath"
	"strings"

	"si/tools/si/internal/vault"
)

func cmdVaultList(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("vault list", flag.ExitOnError)
	fileFlag := fs.String("file", "", "explicit env file path (defaults to the configured vault.file)")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}

	if len(fs.Args()) != 0 {
		printUsage("usage: si vault list [--file <path>]")
		return
	}

	target, err := vaultResolveTarget(settings, strings.TrimSpace(*fileFlag), false)
	if err != nil {
		fatal(err)
	}
	doc, err := vault.ReadDotenvFile(target.File)
	if err != nil {
		fatal(err)
	}
	entries, err := vault.Entries(doc)
	if err != nil {
		fatal(err)
	}

	fmt.Printf("file: %s\n", filepath.Clean(target.File))
	for _, e := range entries {
		if e.Encrypted {
			fmt.Printf("%s\t(encrypted)\n", e.Key)
		} else {
			fmt.Printf("%s\t(plaintext)\n", e.Key)
		}
	}
}
