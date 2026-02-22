package main

import (
	"flag"
	"fmt"
	"path/filepath"
	"strings"

	"si/tools/si/internal/vault"
)

func cmdVaultUnset(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("vault unset", flag.ExitOnError)
	fileFlag := fs.String("file", "", "explicit env file path (defaults to the configured vault.file)")
	section := fs.String("section", "", "section name (accepted but unset removes all occurrences)")
	format := fs.Bool("format", false, "run `si vault fmt` after unsetting")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}

	rest := fs.Args()
	if len(rest) != 1 {
		printUsage("usage: si vault unset <KEY> [--file <path>] [--format]")
		return
	}
	_ = section
	key := strings.TrimSpace(rest[0])
	if err := vault.ValidateKeyName(key); err != nil {
		fatal(err)
	}

	target, err := vaultResolveTarget(settings, strings.TrimSpace(*fileFlag), false)
	if err != nil {
		fatal(err)
	}
	doc, err := vault.ReadDotenvFile(target.File)
	if err != nil {
		fatal(err)
	}
	if _, err := vaultRequireTrusted(settings, target, doc); err != nil {
		fatal(err)
	}
	changed, err := doc.Unset(key)
	if err != nil {
		fatal(err)
	}
	if changed {
		if err := vaultWriteDotenvFileAtomic(target.File, doc.Bytes()); err != nil {
			fatal(err)
		}
	}
	if *format {
		formatted, fmtChanged, err := vault.FormatVaultDotenv(doc)
		if err != nil {
			fatal(err)
		}
		if fmtChanged {
			if err := vaultWriteDotenvFileAtomic(target.File, formatted.Bytes()); err != nil {
				fatal(err)
			}
		}
	}

	vaultAuditEvent(settings, target, "unset", map[string]any{
		"envFile": filepath.Clean(target.File),
		"key":     key,
		"changed": changed,
	})

	fmt.Printf("file:  %s\n", filepath.Clean(target.File))
	if changed {
		fmt.Printf("unset: %s\n", key)
	} else {
		fmt.Printf("unset: %s (no-op)\n", key)
	}
}
