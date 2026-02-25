package main

import (
	"flag"
	"fmt"
	"strings"

	"si/tools/si/internal/vault"
)

func cmdVaultGet(args []string) {
	settings := loadSettingsOrDefault()
	args = stripeFlagsFirst(args, map[string]bool{"reveal": true})
	fs := flag.NewFlagSet("vault get", flag.ExitOnError)
	fileFlag := fs.String("file", "", "vault scope (preferred: --scope)")
	scopeFlag := fs.String("scope", "", "vault scope")
	reveal := fs.Bool("reveal", false, "print the decrypted value to stdout")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}

	rest := fs.Args()
	if len(rest) != 1 {
		printUsage("usage: si vault get <KEY> [--scope <name>] [--reveal]")
		return
	}
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
	source := "local"
	raw, found, used, sunErr := vaultSunKVGetRawValue(settings, target, key)
	if sunErr != nil {
		fatal(sunErr)
	}
	if !used || !found {
		fatal(fmt.Errorf("key not found: %s", key))
	}
	source = "sun-kv"
	val, err := vault.NormalizeDotenvValue(raw)
	if err != nil {
		fatal(err)
	}
	if vault.IsEncryptedValueV1(val) {
		if !*reveal {
			fmt.Printf("scope: %s\n", strings.TrimSpace(target.File))
			fmt.Printf("source: %s\n", source)
			fmt.Printf("%s: encrypted (use --reveal)\n", key)
			return
		}
		identity, err := vaultEnsureStrictSunIdentity(settings, "vault_get")
		if err != nil {
			fatal(err)
		}
		if identity == nil {
			fatal(fmt.Errorf("sun vault identity unavailable"))
		}
		plain, err := vault.DecryptStringV1(val, identity)
		if err != nil {
			fatal(err)
		}
		vaultAuditEvent(settings, target, "reveal", map[string]any{
			"scope":     strings.TrimSpace(target.File),
			"key":       key,
			"encrypted": true,
		})
		fmt.Print(plain)
		if !strings.HasSuffix(plain, "\n") {
			fmt.Print("\n")
		}
		return
	}

	if !*reveal {
		fmt.Printf("scope: %s\n", strings.TrimSpace(target.File))
		fmt.Printf("source: %s\n", source)
		fmt.Printf("%s: plaintext\n", key)
		return
	}
	vaultAuditEvent(settings, target, "reveal", map[string]any{
		"scope":     strings.TrimSpace(target.File),
		"key":       key,
		"encrypted": false,
	})
	fmt.Print(val)
	if !strings.HasSuffix(val, "\n") {
		fmt.Print("\n")
	}
}
