package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"si/tools/si/internal/vault"
)

func cmdVaultSet(args []string) {
	settings := loadSettingsOrDefault()
	args = stripeFlagsFirst(args, map[string]bool{"stdin": true, "format": true})
	fs := flag.NewFlagSet("vault set", flag.ExitOnError)
	fileFlag := fs.String("file", "", "explicit env file path (defaults to the configured vault.file)")
	section := fs.String("section", "", "section name (e.g. stripe, workos)")
	stdin := fs.Bool("stdin", false, "read value from stdin (avoids shell history)")
	format := fs.Bool("format", false, "run `si vault fmt` after setting")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}

	rest := fs.Args()
	if len(rest) < 1 {
		printUsage("usage: si vault set <KEY> <VALUE> [--file <path>] [--section <name>] [--stdin] [--format]")
		return
	}
	key := strings.TrimSpace(rest[0])
	if err := vault.ValidateKeyName(key); err != nil {
		fatal(err)
	}

	value := ""
	if *stdin {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			fatal(err)
		}
		value = strings.TrimRight(string(data), "\r\n")
	} else {
		if len(rest) < 2 {
			fatal(fmt.Errorf("value required (use --stdin for multiline/safer input)"))
		}
		value = rest[1]
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
	recipients := vault.ParseRecipientsFromDotenv(doc)
	if len(recipients) == 0 {
		fatal(fmt.Errorf("no recipients found (expected %q lines); run `si vault init`", vault.VaultRecipientPrefix))
	}

	cipher, err := vault.EncryptStringV1(value, recipients)
	if err != nil {
		fatal(err)
	}
	changed, err := doc.Set(key, cipher, vault.SetOptions{Section: *section})
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

	vaultAuditEvent(settings, target, "set", map[string]any{
		"envFile":   filepath.Clean(target.File),
		"key":       key,
		"section":   strings.TrimSpace(*section),
		"changed":   changed,
		"fromStdin": *stdin,
	})

	fmt.Printf("file: %s\n", filepath.Clean(target.File))
	fmt.Printf("set:  %s\n", key)
	if err := maybeHeliaAutoBackupVault("vault_set", target.File); err != nil {
		fatal(err)
	}
}
