package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"si/tools/si/internal/vault"
)

func cmdVaultStatus(args []string) {
	settings := loadVaultSettingsOrFail()
	fs := flag.NewFlagSet("vault status", flag.ExitOnError)
	envFile := fs.String("env-file", defaultSIVaultDotenvFile, "dotenv file path")
	fileAlias := fs.String("file", "", "alias for --env-file")
	scopeAlias := fs.String("scope", "", "alias for --env")
	repoFlag := fs.String("repo", "", "vault repo slug")
	envFlag := fs.String("env", "", "vault environment")
	jsonOut := fs.Bool("json", false, "json output")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si vault status [--env-file <path>] [--repo <slug>] [--env <name>] [--json]")
		return
	}
	envName := strings.TrimSpace(*envFlag)
	if envName == "" {
		envName = strings.TrimSpace(*scopeAlias)
	}
	fileValue := strings.TrimSpace(*envFile)
	if strings.TrimSpace(*fileAlias) != "" {
		fileValue = strings.TrimSpace(*fileAlias)
	}

	target, err := resolveSIVaultTarget(strings.TrimSpace(*repoFlag), envName, fileValue)
	if err != nil {
		fatal(err)
	}
	material, keyErr := ensureSIVaultKeyMaterial(settings, target)
	doc, readErr := readDotenvOrEmpty(target.EnvFile)
	encryptedCount := 0
	plaintextCount := 0
	keyCount := 0
	var plaintextKeys []string
	decryptability := siVaultDecryptabilityStats{Undecryptable: []string{}}
	var decryptabilityErr error
	if readErr == nil {
		entries, entriesErr := vault.Entries(doc)
		if entriesErr == nil {
			for _, entry := range entries {
				if strings.TrimSpace(entry.Key) == vault.SIVaultPublicKeyName {
					continue
				}
				keyCount++
				if vault.IsSIVaultEncryptedValue(entry.ValueRaw) {
					encryptedCount++
				} else {
					plaintextCount++
					plaintextKeys = append(plaintextKeys, entry.Key)
				}
			}
			sort.Strings(plaintextKeys)
		} else {
			readErr = entriesErr
		}
		if readErr == nil && keyErr == nil {
			decryptability, decryptabilityErr = analyzeDotenvDecryptability(doc, siVaultPrivateKeyCandidates(material))
		}
	}

	if *jsonOut {
		payload := map[string]interface{}{
			"repo":             target.Repo,
			"env":              target.Env,
			"env_file":         target.EnvFile,
			"sun_base_url":     settings.Sun.BaseURL,
			"sun_account":      settings.Sun.Account,
			"key_name_public":  vault.SIVaultPublicKeyName,
			"key_name_private": vault.SIVaultPrivateKeyName,
			"file_keys":        keyCount,
			"encrypted_keys":   encryptedCount,
			"plaintext_keys":   plaintextCount,
			"plaintext_list":   plaintextKeys,
		}
		if keyErr == nil {
			payload["public_key"] = material.PublicKey
			payload["backup_key_count"] = len(material.BackupPrivateKeys)
		} else {
			payload["key_error"] = keyErr.Error()
		}
		if decryptabilityErr == nil {
			payload["decryptable_encrypted_keys"] = decryptability.Decryptable
			payload["undecryptable_encrypted_keys"] = len(decryptability.Undecryptable)
			payload["undecryptable_keys"] = decryptability.Undecryptable
		} else {
			payload["decryptability_error"] = decryptabilityErr.Error()
		}
		if readErr != nil {
			payload["file_error"] = readErr.Error()
		}
		printJSON(payload)
		if keyErr != nil || readErr != nil || decryptabilityErr != nil || len(decryptability.Undecryptable) > 0 {
			os.Exit(1)
		}
		return
	}

	fmt.Printf("repo/env:       %s/%s\n", target.Repo, target.Env)
	fmt.Printf("file:           %s\n", filepath.Clean(target.EnvFile))
	if keyErr != nil {
		fmt.Printf("sun_keys:       error (%v)\n", keyErr)
	} else {
		fmt.Printf("sun_keys:       ok\n")
		fmt.Printf("public_key:     %s\n", material.PublicKey)
		fmt.Printf("backup_keys:    %d\n", len(material.BackupPrivateKeys))
	}
	if readErr != nil {
		fmt.Printf("file_state:     error (%v)\n", readErr)
		os.Exit(1)
	}
	fmt.Printf("file_keys:      %d\n", keyCount)
	fmt.Printf("encrypted:      %d\n", encryptedCount)
	fmt.Printf("plaintext:      %d\n", plaintextCount)
	if keyErr == nil {
		switch {
		case decryptabilityErr != nil:
			fmt.Printf("decryptability: error (%v)\n", decryptabilityErr)
		case len(decryptability.Undecryptable) > 0:
			fmt.Printf("decryptability: error (%d undecryptable encrypted keys: %s)\n", len(decryptability.Undecryptable), strings.Join(decryptability.Undecryptable, ", "))
		default:
			fmt.Printf("decryptability: ok (%d/%d encrypted keys)\n", decryptability.Decryptable, decryptability.Encrypted)
		}
	}
	if plaintextCount > 0 {
		fmt.Printf("plaintext_keys: %s\n", strings.Join(plaintextKeys, ", "))
	}
	if keyErr != nil || decryptabilityErr != nil || len(decryptability.Undecryptable) > 0 {
		os.Exit(1)
	}
}
