package main

import (
	"flag"
	"fmt"
	"path/filepath"
	"strings"
)

func cmdVaultUse(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("vault use", flag.ExitOnError)
	fileFlag := fs.String("file", "", "vault env file path to set as default")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if len(fs.Args()) != 0 || strings.TrimSpace(*fileFlag) == "" {
		printUsage("usage: si vault use --file <path>")
		return
	}

	target, err := vaultResolveTarget(settings, strings.TrimSpace(*fileFlag), false)
	if err != nil {
		fatal(err)
	}
	settings.Vault.File = target.File
	if err := saveSettings(settings); err != nil {
		fatal(err)
	}

	successf("vault default file set")
	fmt.Printf("  file=%s\n", filepath.Clean(target.File))
	if strings.TrimSpace(target.RepoRoot) != "" {
		fmt.Printf("  repo=%s\n", filepath.Clean(target.RepoRoot))
	}
}
