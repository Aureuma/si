package main

import (
	"flag"
	"fmt"
	"sort"
	"strings"

	"si/tools/si/internal/vault"
)

func cmdVaultList(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("vault list", flag.ExitOnError)
	fileFlag := fs.String("file", "", "vault scope (preferred: --scope)")
	scopeFlag := fs.String("scope", "", "vault scope")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}

	if len(fs.Args()) != 0 {
		printUsage("usage: si vault list [--scope <name>]")
		return
	}

	scope := strings.TrimSpace(*scopeFlag)
	if scope == "" {
		scope = strings.TrimSpace(*fileFlag)
	}
	target, err := vaultResolveTarget(settings, scope, false)
	if err != nil {
		fatal(err)
	}
	if shouldEnforceVaultRepoScope(settings) {
		if err := vaultValidateImplicitTargetRepoScope(target); err != nil {
			warnf("%v", err)
		}
	}
	values, used, sunErr := vaultSunKVLoadRawValues(settings, target)
	if sunErr != nil {
		fatal(sunErr)
	}
	if !used {
		fatal(fmt.Errorf("sun vault unavailable: run `si sun auth login --url <url> --token <token> --account <slug>`"))
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	entries := make([]vault.Entry, 0, len(keys))
	for _, key := range keys {
		value, normErr := vault.NormalizeDotenvValue(values[key])
		if normErr != nil {
			continue
		}
		entries = append(entries, vault.Entry{
			Key:       key,
			ValueRaw:  value,
			Encrypted: vault.IsEncryptedValueV1(value),
		})
	}

	fmt.Printf("scope: %s\n", strings.TrimSpace(target.File))
	fmt.Printf("source: sun-kv\n")
	for _, e := range entries {
		if e.Encrypted {
			fmt.Printf("%s\t(encrypted)\n", e.Key)
		} else {
			fmt.Printf("%s\t(plaintext)\n", e.Key)
		}
	}
}
