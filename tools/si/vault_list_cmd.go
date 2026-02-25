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
	fs := flag.NewFlagSet("vault list", flag.ExitOnError)
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
		printUsage("usage: si vault list [--env-file <path>] [--repo <slug>] [--env <name>] [--json]")
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
	doc, err := vault.ReadDotenvFile(target.EnvFile)
	if err != nil {
		fatal(err)
	}
	entries, err := vault.Entries(doc)
	if err != nil {
		fatal(err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	if *jsonOut {
		type item struct {
			Key       string `json:"key"`
			Encrypted bool   `json:"encrypted"`
		}
		items := make([]item, 0, len(entries))
		for _, entry := range entries {
			if strings.TrimSpace(entry.Key) == vault.SIVaultPublicKeyName {
				continue
			}
			items = append(items, item{Key: entry.Key, Encrypted: vault.IsSIVaultEncryptedValue(entry.ValueRaw)})
		}
		printJSON(map[string]interface{}{
			"repo":     target.Repo,
			"env":      target.Env,
			"env_file": target.EnvFile,
			"items":    items,
		})
		return
	}
	fmt.Printf("file:     %s\n", filepath.Clean(target.EnvFile))
	fmt.Printf("repo/env: %s/%s\n", target.Repo, target.Env)
	for _, entry := range entries {
		if strings.TrimSpace(entry.Key) == vault.SIVaultPublicKeyName {
			continue
		}
		if vault.IsSIVaultEncryptedValue(entry.ValueRaw) {
			fmt.Printf("%s\t(encrypted)\n", entry.Key)
		} else {
			fmt.Printf("%s\t(plaintext)\n", entry.Key)
		}
	}
}
