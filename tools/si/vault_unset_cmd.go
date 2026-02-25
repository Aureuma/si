package main

import (
	"flag"
	"fmt"
	"strings"

	"si/tools/si/internal/vault"
)

func cmdVaultUnset(args []string) {
	settings := loadSettingsOrDefault()
	args = stripeFlagsFirst(args, map[string]bool{"format": true})
	fs := flag.NewFlagSet("vault unset", flag.ExitOnError)
	fileFlag := fs.String("file", "", "vault scope (preferred: --scope)")
	scopeFlag := fs.String("scope", "", "vault scope")
	section := fs.String("section", "", "section name (accepted but unset removes all occurrences)")
	format := fs.Bool("format", false, "run `si vault fmt` after unsetting")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}

	rest := fs.Args()
	if len(rest) != 1 {
		printUsage("usage: si vault unset <KEY> [--scope <name>]")
		return
	}
	_ = section
	key := strings.TrimSpace(rest[0])
	if err := vault.ValidateKeyName(key); err != nil {
		fatal(err)
	}

	scope := strings.TrimSpace(*scopeFlag)
	if scope == "" {
		scope = strings.TrimSpace(*fileFlag)
	}
	target, err := vaultResolveTarget(settings, scope, false)
	if err != nil {
		fatal(err)
	}
	_, changed, _, err := vaultSunKVGetRawValue(settings, target, key)
	if err != nil {
		fatal(err)
	}
	if err := vaultSunKVPutRawValue(settings, target, key, "", "vault_unset", true); err != nil {
		fatal(err)
	}
	if *format {
		warnf("--format is ignored in Sun remote vault mode")
	}

	vaultAuditEvent(settings, target, "unset", map[string]any{
		"scope":   strings.TrimSpace(target.File),
		"key":     key,
		"changed": changed,
	})

	fmt.Printf("scope: %s\n", strings.TrimSpace(target.File))
	if changed {
		fmt.Printf("unset: %s\n", key)
	} else {
		fmt.Printf("unset: %s (no-op)\n", key)
	}
}
