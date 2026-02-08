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
	fs := flag.NewFlagSet("vault set", flag.ExitOnError)
	fileFlag := fs.String("file", "", "explicit env file path (overrides --vault-dir/--env)")
	vaultDir := fs.String("vault-dir", settings.Vault.Dir, "vault directory (relative to host git root)")
	env := fs.String("env", settings.Vault.DefaultEnv, "environment name (maps to .env.<env>)")
	section := fs.String("section", "", "section name (e.g. stripe, workos)")
	stdin := fs.Bool("stdin", false, "read value from stdin (avoids shell history)")
	format := fs.Bool("format", false, "run `si vault fmt` after setting")
	fs.Parse(args)

	rest := fs.Args()
	if len(rest) < 1 {
		printUsage("usage: si vault set <KEY> <VALUE> [--vault-dir <path>] [--env <name>] [--section <name>] [--stdin] [--format]")
		return
	}
	key := strings.TrimSpace(rest[0])
	if key == "" {
		fatal(fmt.Errorf("key required"))
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

	target, err := vaultResolveTarget(settings, *fileFlag, *vaultDir, *env, false, false)
	if err != nil {
		fatal(err)
	}
	data, err := os.ReadFile(target.File)
	if err != nil {
		fatal(err)
	}
	doc := vault.ParseDotenv(data)
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
		if err := vault.WriteDotenvFileAtomic(target.File, doc.Bytes()); err != nil {
			fatal(err)
		}
	}
	if *format {
		formatted, fmtChanged, err := vault.FormatVaultDotenv(doc)
		if err != nil {
			fatal(err)
		}
		if fmtChanged {
			if err := vault.WriteDotenvFileAtomic(target.File, formatted.Bytes()); err != nil {
				fatal(err)
			}
		}
	}

	fmt.Printf("file: %s\n", filepath.Clean(target.File))
	fmt.Printf("set:  %s\n", key)
}
