package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"si/tools/si/internal/vault"
)

func cmdVaultSet(args []string) {
	settings := loadSettingsOrDefault()
	args = stripeFlagsFirst(args, map[string]bool{"stdin": true, "format": true})
	fs := flag.NewFlagSet("vault set", flag.ExitOnError)
	fileFlag := fs.String("file", "", "vault scope (preferred: --scope)")
	scopeFlag := fs.String("scope", "", "vault scope")
	section := fs.String("section", "", "section name (e.g. stripe, workos)")
	stdin := fs.Bool("stdin", false, "read value from stdin (avoids shell history)")
	format := fs.Bool("format", false, "run `si vault fmt` after setting")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}

	rest := fs.Args()
	if len(rest) < 1 {
		printUsage("usage: si vault set <KEY> <VALUE> [--scope <name>] [--section <name>] [--stdin]")
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

	scope := strings.TrimSpace(*scopeFlag)
	if scope == "" {
		scope = strings.TrimSpace(*fileFlag)
	}
	target, err := vaultResolveTarget(settings, scope, false)
	if err != nil {
		fatal(err)
	}
	identity, err := vaultEnsureStrictSunIdentity(settings, "vault_set")
	if err != nil {
		fatal(err)
	}
	if identity == nil {
		fatal(fmt.Errorf("sun vault identity unavailable"))
	}

	cipher, err := vault.EncryptStringV1(value, []string{strings.TrimSpace(identity.Recipient().String())})
	if err != nil {
		fatal(err)
	}
	if err := vaultSunKVPutRawValue(settings, target, key, vault.RenderDotenvValuePlain(cipher), "vault_set", false); err != nil {
		fatal(err)
	}
	changed := true
	if *format {
		warnf("--format is ignored in Sun remote vault mode")
	}

	vaultAuditEvent(settings, target, "set", map[string]any{
		"scope":     strings.TrimSpace(target.File),
		"key":       key,
		"section":   strings.TrimSpace(*section),
		"changed":   changed,
		"fromStdin": *stdin,
	})

	fmt.Printf("scope: %s\n", strings.TrimSpace(target.File))
	fmt.Printf("set:  %s\n", key)
}
