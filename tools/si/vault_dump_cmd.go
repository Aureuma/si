package main

import (
	"flag"
	"fmt"
	"sort"
	"strings"

	"si/tools/si/internal/vault"
)

func cmdVaultDump(args []string) {
	settings := loadSettingsOrDefault()
	args = stripeFlagsFirst(args, map[string]bool{"reveal": true})
	fs := flag.NewFlagSet("vault dump", flag.ExitOnError)
	fileFlag := fs.String("file", "", "vault scope (preferred: --scope)")
	scopeFlag := fs.String("scope", "", "vault scope")
	reveal := fs.Bool("reveal", false, "print decrypted KEY=value lines")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if len(fs.Args()) != 0 {
		printUsage("usage: si vault dump [--scope <name>] [--reveal]")
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

	if !*reveal {
		fmt.Printf("scope: %s\n", strings.TrimSpace(target.File))
		fmt.Printf("source: sun-kv\n")
		for _, key := range keys {
			normalized, normErr := vault.NormalizeDotenvValue(values[key])
			if normErr != nil {
				fatal(fmt.Errorf("normalize %s: %w", key, normErr))
			}
			if vault.IsEncryptedValueV1(normalized) {
				fmt.Printf("%s\t(encrypted; use --reveal)\n", key)
			} else {
				fmt.Printf("%s\t(plaintext; use --reveal)\n", key)
			}
		}
		return
	}

	identity, err := vaultEnsureStrictSunIdentity(settings, "vault_dump")
	if err != nil {
		fatal(err)
	}
	if identity == nil {
		fatal(fmt.Errorf("sun vault identity unavailable"))
	}

	decryptedCount := 0
	for _, key := range keys {
		normalized, normErr := vault.NormalizeDotenvValue(values[key])
		if normErr != nil {
			fatal(fmt.Errorf("normalize %s: %w", key, normErr))
		}
		plain := normalized
		if vault.IsEncryptedValueV1(normalized) {
			dec, decErr := vault.DecryptStringV1(normalized, identity)
			if decErr != nil {
				fatal(fmt.Errorf("decrypt %s: %w", key, decErr))
			}
			plain = dec
			decryptedCount++
		}
		fmt.Printf("%s=%s\n", key, vault.RenderDotenvValuePlain(plain))
	}
	vaultAuditEvent(settings, target, "dump", map[string]any{
		"scope":           strings.TrimSpace(target.File),
		"reveal":          true,
		"key_count":       len(keys),
		"decrypted_count": decryptedCount,
	})
}
