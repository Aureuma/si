package main

import (
	"flag"
	"fmt"
	"strings"
)

func cmdVaultInit(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("vault init", flag.ExitOnError)
	fileFlag := fs.String("file", "", "vault scope (preferred: --scope)")
	scopeFlag := fs.String("scope", "", "vault scope")
	setDefault := fs.Bool("set-default", false, "set the target scope as vault.file in settings")
	keyBackend := fs.String("key-backend", "", "deprecated in sun mode; ignored")
	keyFile := fs.String("key-file", "", "deprecated in sun mode; ignored")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if len(fs.Args()) != 0 {
		printUsage("usage: si vault init [--scope <name>] [--set-default]")
		return
	}
	scope := strings.TrimSpace(*scopeFlag)
	if scope == "" {
		scope = strings.TrimSpace(*fileFlag)
	}
	target, err := vaultResolveTarget(settings, scope, true)
	if err != nil {
		fatal(err)
	}
	identity, err := vaultEnsureStrictSunIdentity(settings, "vault_init")
	if err != nil {
		fatal(err)
	}
	if identity == nil {
		fatal(fmt.Errorf("sun vault identity unavailable"))
	}
	if strings.TrimSpace(*keyBackend) != "" || strings.TrimSpace(*keyFile) != "" {
		warnf("--key-backend/--key-file are ignored in Sun remote vault mode")
	}
	settingsChanged := false
	if cur := strings.TrimSpace(settings.Vault.File); cur == "" || *setDefault || strings.TrimSpace(scope) != "" {
		settings.Vault.File = target.File
		settingsChanged = true
	}
	if settingsChanged {
		if err := saveSettings(settings); err != nil {
			fatal(err)
		}
	}

	vaultAuditEvent(settings, target, "init", map[string]any{
		"scope":      strings.TrimSpace(target.File),
		"recipient":  strings.TrimSpace(identity.Recipient().String()),
		"setDefault": *setDefault,
	})

	fmt.Printf("scope:     %s\n", strings.TrimSpace(target.File))
	fmt.Printf("recipient: %s\n", strings.TrimSpace(identity.Recipient().String()))
	fmt.Printf("trust:     n/a (sun-managed)\n")
	if settingsChanged && *setDefault {
		fmt.Printf("default:   updated\n")
	}
	fmt.Printf("key:       ok (sun identity)\n")
}
