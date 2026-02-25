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
	envFile := fs.String("env-file", defaultSIVaultDotenvFile, "dotenv file path")
	fileAlias := fs.String("file", "", "alias for --env-file")
	scopeAlias := fs.String("scope", "", "alias for --env")
	repoFlag := fs.String("repo", "", "vault repo slug")
	envFlag := fs.String("env", "", "vault environment")
	stdin := fs.Bool("stdin", false, "read value from stdin")
	encrypt := fs.Bool("encrypt", true, "encrypt stored value")
	plain := fs.Bool("plain", false, "store value as plaintext")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	rest := fs.Args()
	if len(rest) < 1 {
		printUsage("usage: si vault set <KEY> <VALUE> [--env-file <path>] [--repo <slug>] [--env <name>] [--stdin] [--plain]")
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
			fatal(fmt.Errorf("value required (or use --stdin)"))
		}
		value = rest[1]
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
	material, err := ensureSIVaultKeyMaterial(settings, target)
	if err != nil {
		fatal(err)
	}
	doc, err := readDotenvOrEmpty(target.EnvFile)
	if err != nil {
		fatal(err)
	}
	if _, err := ensureSIVaultPublicKeyHeader(&doc, material.PublicKey); err != nil {
		fatal(err)
	}

	storeValue := value
	shouldEncrypt := *encrypt && !*plain && key != vault.SIVaultPublicKeyName
	if shouldEncrypt {
		cipher, encErr := vault.EncryptSIVaultValue(value, material.PublicKey)
		if encErr != nil {
			fatal(encErr)
		}
		storeValue = cipher
	}
	if _, err := doc.Set(key, vault.RenderDotenvValuePlain(storeValue), vault.SetOptions{}); err != nil {
		fatal(err)
	}
	if err := writeDotenv(target.EnvFile, doc); err != nil {
		fatal(err)
	}
	fmt.Printf("file:     %s\n", filepath.Clean(target.EnvFile))
	fmt.Printf("repo/env: %s/%s\n", target.Repo, target.Env)
	if shouldEncrypt {
		fmt.Printf("set:      %s (encrypted)\n", key)
	} else {
		fmt.Printf("set:      %s (plaintext)\n", key)
	}
}
