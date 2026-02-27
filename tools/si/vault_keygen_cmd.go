package main

import (
	"context"
	"flag"
	"fmt"
	"sort"
	"strings"
	"time"

	"si/tools/si/internal/vault"
)

func cmdVaultKeygen(args []string) {
	settings := loadVaultSettingsOrFail()
	fs := flag.NewFlagSet("vault keygen", flag.ExitOnError)
	repoFlag := fs.String("repo", "", "vault repo slug (default: current git repo directory name)")
	envFlag := fs.String("env", "", "vault environment (default: dev)")
	envFile := fs.String("env-file", defaultSIVaultDotenvFile, "dotenv file path used for context")
	fileAlias := fs.String("file", "", "alias for --env-file")
	rotate := fs.Bool("rotate", false, "generate a new keypair and keep previous private key in backups")
	jsonOut := fs.Bool("json", false, "json output")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si vault keypair [--repo <slug>] [--env <name>] [--env-file <path>] [--rotate] [--json]")
		return
	}
	fileValue := strings.TrimSpace(*envFile)
	if strings.TrimSpace(*fileAlias) != "" {
		fileValue = strings.TrimSpace(*fileAlias)
	}
	target, err := resolveSIVaultTarget(strings.TrimSpace(*repoFlag), strings.TrimSpace(*envFlag), fileValue)
	if err != nil {
		fatal(err)
	}
	material, err := ensureSIVaultKeyMaterial(settings, target)
	if err != nil {
		fatal(err)
	}
	if *rotate {
		client, clientErr := sunClientFromSettings(settings)
		if clientErr != nil {
			fatal(clientErr)
		}
		timeout := time.Duration(settings.Sun.TimeoutSeconds) * time.Second
		if timeout <= 0 {
			timeout = 20 * time.Second
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		publicKey, privateKey, genErr := vault.GenerateSIVaultKeyPair()
		if genErr != nil {
			fatal(genErr)
		}
		backups := append([]string{}, material.BackupPrivateKeys...)
		if strings.TrimSpace(material.PrivateKey) != "" {
			backups = append(backups, strings.TrimSpace(material.PrivateKey))
		}
		sort.Strings(backups)
		rotated, putErr := client.putVaultPrivateKey(ctx, sunVaultPrivateKey{
			Repo:              target.Repo,
			Env:               target.Env,
			PublicKey:         publicKey,
			PrivateKey:        privateKey,
			BackupPrivateKeys: backups,
		}, nil)
		if putErr != nil {
			fatal(putErr)
		}
		material, err = normalizeSunVaultMaterial(rotated, target)
		if err != nil {
			fatal(err)
		}
	}
	if *jsonOut {
		printJSON(map[string]interface{}{
			"repo":               target.Repo,
			"env":                target.Env,
			"env_file":           target.EnvFile,
			"public_key_name":    vault.SIVaultPublicKeyName,
			"private_key_name":   vault.SIVaultPrivateKeyName,
			"public_key":         material.PublicKey,
			"backup_key_count":   len(material.BackupPrivateKeys),
			"updated_at":         material.UpdatedAt,
			"sun_account":        settings.Sun.Account,
			"sun_base_url":       settings.Sun.BaseURL,
			"private_key_source": "sun",
		})
		return
	}
	fmt.Printf("repo:             %s\n", target.Repo)
	fmt.Printf("env:              %s\n", target.Env)
	fmt.Printf("env_file:         %s\n", target.EnvFile)
	fmt.Printf("public_key_name:  %s\n", vault.SIVaultPublicKeyName)
	fmt.Printf("private_key_name: %s\n", vault.SIVaultPrivateKeyName)
	fmt.Printf("public_key:       %s\n", material.PublicKey)
	fmt.Printf("backup_keys:      %d\n", len(material.BackupPrivateKeys))
	fmt.Printf("private_key:      sun\n")
}
