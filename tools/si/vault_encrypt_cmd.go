package main

import (
	"flag"
	"fmt"
	"sort"
	"strings"

	"filippo.io/age"
	"si/tools/si/internal/vault"
)

func cmdVaultEncrypt(args []string) {
	settings := loadSettingsOrDefault()
	args = stripeFlagsFirst(args, map[string]bool{"format": true, "reencrypt": true})
	fs := flag.NewFlagSet("vault encrypt", flag.ExitOnError)
	var files multiFlag
	fs.Var(&files, "file", "vault scope (repeatable; preferred: --scope)")
	scopeFlag := fs.String("scope", "", "vault scope")
	format := fs.Bool("format", false, "ignored in Sun remote vault mode")
	reencrypt := fs.Bool("reencrypt", false, "re-encrypt already-encrypted values (intentional ciphertext churn)")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if len(fs.Args()) != 0 {
		printUsage("usage: si vault encrypt [--scope <name>] [--file <name>]... [--format] [--reencrypt]")
		return
	}
	if scope := strings.TrimSpace(*scopeFlag); scope != "" {
		files = append(files, scope)
	}

	var identity *age.X25519Identity
	identity, err := vaultEnsureStrictSunIdentity(settings, "vault_encrypt")
	if err != nil {
		fatal(err)
	}
	if identity == nil {
		fatal(fmt.Errorf("sun vault identity unavailable"))
	}
	recipients := []string{strings.TrimSpace(identity.Recipient().String())}
	if *format {
		warnf("--format is ignored in Sun remote vault mode")
	}

	runOne := func(scope string) {
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

		encryptedCount := 0
		reencryptedCount := 0
		skippedEncrypted := 0
		for _, key := range keys {
			norm, normErr := vault.NormalizeDotenvValue(values[key])
			if normErr != nil {
				fatal(fmt.Errorf("normalize %s: %w", key, normErr))
			}
			plain := norm
			if vault.IsEncryptedValueV1(norm) {
				if !*reencrypt {
					skippedEncrypted++
					continue
				}
				dec, decErr := vault.DecryptStringV1(norm, identity)
				if decErr != nil {
					fatal(fmt.Errorf("decrypt existing encrypted key %s: %w", key, decErr))
				}
				plain = dec
			}
			cipher, encErr := vault.EncryptStringV1(plain, recipients)
			if encErr != nil {
				fatal(fmt.Errorf("encrypt %s: %w", key, encErr))
			}
			if putErr := vaultSunKVPutRawValue(settings, target, key, vault.RenderDotenvValuePlain(cipher), "vault_encrypt", false); putErr != nil {
				fatal(putErr)
			}
			if vault.IsEncryptedValueV1(norm) {
				reencryptedCount++
			} else {
				encryptedCount++
			}
		}

		vaultAuditEvent(settings, target, "encrypt", map[string]any{
			"scope":            strings.TrimSpace(target.File),
			"reencrypt":        *reencrypt,
			"encryptedCount":   encryptedCount,
			"reencryptedCount": reencryptedCount,
			"skippedEncrypted": skippedEncrypted,
			"source":           "sun-kv",
		})

		fmt.Printf("scope: %s\n", strings.TrimSpace(target.File))
		fmt.Printf("encrypted: %d\n", encryptedCount)
		if *reencrypt {
			fmt.Printf("reencrypted: %d\n", reencryptedCount)
		}
		fmt.Printf("source: sun-kv\n")
	}

	if len(files) == 0 {
		runOne("")
		return
	}
	for _, file := range files {
		runOne(file)
	}
}
