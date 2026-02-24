package main

import (
	"flag"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"si/tools/si/internal/vault"
)

func cmdVaultList(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("vault list", flag.ExitOnError)
	fileFlag := fs.String("file", "", "explicit env file path (defaults to the configured vault.file)")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}

	if len(fs.Args()) != 0 {
		printUsage("usage: si vault list [--file <path>]")
		return
	}

	target, err := vaultResolveTarget(settings, strings.TrimSpace(*fileFlag), false)
	if err != nil {
		fatal(err)
	}
	if shouldEnforceVaultRepoScope(settings) {
		if err := vaultValidateImplicitTargetRepoScope(target); err != nil {
			warnf("%v", err)
		}
	}
	entries := []vault.Entry{}
	source := "local"
	if values, used, sunErr := vaultSunKVLoadRawValues(settings, target); sunErr != nil {
		fatal(sunErr)
	} else if used && len(values) > 0 {
		keys := make([]string, 0, len(values))
		for key := range values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		entries = make([]vault.Entry, 0, len(keys))
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
		source = "sun-kv"
	} else {
		doc, err := vault.ReadDotenvFile(target.File)
		if err != nil {
			fatal(err)
		}
		entries, err = vault.Entries(doc)
		if err != nil {
			fatal(err)
		}
	}

	fmt.Printf("file: %s\n", filepath.Clean(target.File))
	fmt.Printf("source: %s\n", source)
	for _, e := range entries {
		if e.Encrypted {
			fmt.Printf("%s\t(encrypted)\n", e.Key)
		} else {
			fmt.Printf("%s\t(plaintext)\n", e.Key)
		}
	}
}
