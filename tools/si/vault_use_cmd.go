package main

import (
	"flag"
	"fmt"
	"strings"
)

func cmdVaultUse(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("vault use", flag.ExitOnError)
	fileFlag := fs.String("file", "", "vault scope (preferred: --scope)")
	scopeFlag := fs.String("scope", "", "vault scope")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	scope := strings.TrimSpace(*scopeFlag)
	if scope == "" {
		scope = strings.TrimSpace(*fileFlag)
	}
	if len(fs.Args()) != 0 || strings.TrimSpace(scope) == "" {
		printUsage("usage: si vault use --scope <name>")
		return
	}

	target, err := vaultResolveTarget(settings, scope, false)
	if err != nil {
		fatal(err)
	}
	settings.Vault.File = target.File
	if err := saveSettings(settings); err != nil {
		fatal(err)
	}

	successf("vault default scope set")
	fmt.Printf("  scope=%s\n", strings.TrimSpace(target.File))
}
